package orchestrationagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	sdk "github.com/memohai/twilight-ai/sdk"

	agentpkg "github.com/memohai/memoh/internal/agent"
	agenttools "github.com/memohai/memoh/internal/agent/tools"
	"github.com/memohai/memoh/internal/conversation"
	"github.com/memohai/memoh/internal/db"
	dbsqlc "github.com/memohai/memoh/internal/db/postgres/sqlc"
	dbstore "github.com/memohai/memoh/internal/db/store"
	"github.com/memohai/memoh/internal/message"
	"github.com/memohai/memoh/internal/models"
	"github.com/memohai/memoh/internal/oauthctx"
	"github.com/memohai/memoh/internal/orchestration"
	"github.com/memohai/memoh/internal/providers"
	sessionpkg "github.com/memohai/memoh/internal/session"
	"github.com/memohai/memoh/internal/settings"
	tzutil "github.com/memohai/memoh/internal/timezone"
)

const (
	executorID                  = "server.in_process"
	generateRetryMaxAttempts    = 4
	generateRetryInitialBackoff = time.Second
	generateRetryMaxBackoff     = 8 * time.Second
)

var (
	retryableHTTPStatusPattern        = regexp.MustCompile(`(?i)(api error|status|status code)[:= ]+(429|5\d{2})`)
	retryableConnectionFailurePattern = regexp.MustCompile(`(?i)(EOF|connection reset|connection refused|timeout awaiting response headers|TLS handshake timeout|server closed idle connection)`)
)

type Executor struct {
	log           *slog.Logger
	queries       orchestration.Queries
	store         dbstore.Queries
	orchestration *orchestration.Service
	agent         *agentpkg.Agent
	settings      *settings.Service
	models        *models.Service
	sessions      *sessionpkg.Service
	messages      message.Writer
	baseProviders []agenttools.ToolProvider
	httpClient    *http.Client
	clockLocation *time.Location
}

type Deps struct {
	Logger        *slog.Logger
	Queries       orchestration.Queries
	Store         dbstore.Queries
	Orchestration *orchestration.Service
	Agent         *agentpkg.Agent
	Settings      *settings.Service
	Models        *models.Service
	Sessions      *sessionpkg.Service
	Messages      message.Writer
	BaseProviders []agenttools.ToolProvider
	ClockLocation *time.Location
}

func New(deps Deps) *Executor {
	log := deps.Logger
	if log == nil {
		log = slog.Default()
	}
	loc := deps.ClockLocation
	if loc == nil {
		loc = time.UTC
	}
	return &Executor{
		log:           log.With(slog.String("component", "orchestration.in_process_executor")),
		queries:       deps.Queries,
		store:         deps.Store,
		orchestration: deps.Orchestration,
		agent:         deps.Agent,
		settings:      deps.Settings,
		models:        deps.Models,
		sessions:      deps.Sessions,
		messages:      deps.Messages,
		baseProviders: append([]agenttools.ToolProvider(nil), deps.BaseProviders...),
		httpClient:    models.NewProviderHTTPClient(0),
		clockLocation: loc,
	}
}

func (e *Executor) ExecuteAttempt(ctx context.Context, attempt orchestration.TaskAttempt) orchestration.AttemptCompletion {
	base := orchestration.AttemptCompletion{
		AttemptID:          attempt.ID,
		ClaimToken:         attempt.ClaimToken,
		Status:             orchestration.TaskAttemptStatusFailed,
		Summary:            "orchestration attempt failed",
		StructuredOutput:   map[string]any{},
		CompletionMetadata: map[string]any{"executor": executorID},
	}
	execCtx, err := e.loadAttemptContext(ctx, attempt)
	if err != nil {
		return failAttempt(base, "attempt_context_load_failed", err)
	}
	session, err := e.createExecutionSession(ctx, sessionpkg.TypeOrchestrationAttempt, execCtx.BotID, execCtx.ParentSessionID, attempt.ID, execCtx.Task.Goal)
	if err != nil {
		return failAttempt(base, "attempt_session_create_failed", err)
	}
	defer e.finalizeSession(ctx, session.ID)
	if _, err := e.orchestration.AttachAttemptSession(ctx, attempt.ID, session.ID); err != nil {
		e.log.Warn("attach attempt session failed", slog.String("attempt_id", attempt.ID), slog.String("session_id", session.ID), slog.Any("error", err))
	}
	userPrompt := buildWorkerPrompt(execCtx)
	e.persistText(ctx, execCtx.BotID, session.ID, "user", userPrompt, nil)

	cfg, model, provider, err := e.buildRunConfig(ctx, execCtx.BotID, execCtx.Run.OwnerSubject, session.ID, sessionpkg.TypeOrchestrationAttempt)
	if err != nil {
		return failAttempt(base, "worker_model_resolution_failed", err)
	}
	cfg.System = agentpkg.OrchestrationAttemptSystemPrompt()
	cfg.Messages = []sdk.Message{sdk.UserMessage(userPrompt)}

	attemptTools := agenttools.OrchestrationAttemptToolContext{
		RunID:     execCtx.Run.ID.String(),
		TaskID:    execCtx.Task.ID.String(),
		AttemptID: attempt.ID,
		Caller: orchestration.ControlIdentity{
			TenantID: execCtx.Run.TenantID,
			Subject:  execCtx.Run.OwnerSubject,
		},
		Service: e.orchestration,
	}
	state, result, err := e.generateWithFreshState(ctx, session.ID, cfg, attemptTools)
	if err != nil {
		return failAttempt(base, "worker_generate_failed", err)
	}
	e.persistSDKMessages(ctx, execCtx.BotID, session.ID, result.Messages, model.ID)
	completion, ok := state.AttemptCompletion()
	if !ok {
		if checkpoint, checkpointOK := state.RequestedCheckpoint(); checkpointOK {
			return orchestration.AttemptCompletion{
				AttemptID:          attempt.ID,
				ClaimToken:         attempt.ClaimToken,
				Status:             orchestration.TaskAttemptStatusParked,
				Summary:            "human checkpoint requested",
				ParkCheckpointID:   checkpoint.ID,
				StructuredOutput:   map[string]any{"checkpoint_id": checkpoint.ID},
				CompletionMetadata: map[string]any{"executor": executorID},
			}
		}
		return failAttempt(base, "task_result_not_submitted", errors.New("attempt did not call submit_task_result"))
	}
	completion.AttemptID = attempt.ID
	completion.ClaimToken = attempt.ClaimToken
	if completion.StructuredOutput == nil {
		completion.StructuredOutput = map[string]any{}
	}
	if completion.CompletionMetadata == nil {
		completion.CompletionMetadata = map[string]any{}
	}
	completion.CompletionMetadata["executor"] = executorID
	completion.CompletionMetadata["model_id"] = model.ModelID
	completion.CompletionMetadata["provider"] = provider.ClientType
	return completion
}

func (e *Executor) ExecuteVerification(ctx context.Context, verification orchestration.TaskVerification) orchestration.VerificationCompletion {
	base := orchestration.VerificationCompletion{
		VerificationID: verification.ID,
		ClaimToken:     verification.ClaimToken,
		Status:         orchestration.TaskVerificationStatusFailed,
		Verdict:        orchestration.VerificationVerdictRejected,
		Summary:        "orchestration verification failed",
		FailureClass:   "verification_failed",
		TerminalReason: "orchestration verification failed",
	}
	execCtx, err := e.loadVerificationContext(ctx, verification)
	if err != nil {
		return failVerification(base, "verification_context_load_failed", err)
	}
	session, err := e.createExecutionSession(ctx, sessionpkg.TypeOrchestrationVerification, execCtx.BotID, execCtx.ParentSessionID, verification.ID, execCtx.Task.Goal)
	if err != nil {
		return failVerification(base, "verification_session_create_failed", err)
	}
	defer e.finalizeSession(ctx, session.ID)
	if _, err := e.orchestration.AttachVerificationSession(ctx, verification.ID, session.ID); err != nil {
		e.log.Warn("attach verification session failed", slog.String("verification_id", verification.ID), slog.String("session_id", session.ID), slog.Any("error", err))
	}
	userPrompt := buildVerifierPrompt(execCtx)
	e.persistText(ctx, execCtx.BotID, session.ID, "user", userPrompt, nil)

	cfg, model, _, err := e.buildRunConfig(ctx, execCtx.BotID, execCtx.Run.OwnerSubject, session.ID, sessionpkg.TypeOrchestrationVerification)
	if err != nil {
		return failVerification(base, "verifier_model_resolution_failed", err)
	}
	cfg.System = agentpkg.OrchestrationVerificationSystemPrompt()
	cfg.Messages = []sdk.Message{sdk.UserMessage(userPrompt)}

	state, result, err := e.generateWithFreshState(ctx, session.ID, cfg, agenttools.OrchestrationAttemptToolContext{})
	if err != nil {
		return failVerification(base, "verifier_generate_failed", err)
	}
	e.persistSDKMessages(ctx, execCtx.BotID, session.ID, result.Messages, model.ID)
	completion, ok := state.VerificationCompletion()
	if !ok {
		return failVerification(base, "verification_result_not_submitted", errors.New("verification did not call submit_verification_result"))
	}
	completion.VerificationID = verification.ID
	completion.ClaimToken = verification.ClaimToken
	return completion
}

func (e *Executor) createExecutionSession(ctx context.Context, sessionType, botID, parentSessionID, executionID, goal string) (sessionpkg.Session, error) {
	if e.sessions == nil {
		return sessionpkg.Session{}, errors.New("session service is not configured")
	}
	title := strings.TrimSpace(goal)
	if runes := []rune(title); len(runes) > 80 {
		title = string(runes[:80])
	}
	return e.sessions.Create(ctx, sessionpkg.CreateInput{
		BotID:           botID,
		Type:            sessionType,
		Title:           title,
		ParentSessionID: strings.TrimSpace(parentSessionID),
		Metadata: map[string]any{
			"orchestration_execution_id": strings.TrimSpace(executionID),
		},
	})
}

func (e *Executor) finalizeSession(ctx context.Context, sessionID string) {
	if e.sessions == nil || strings.TrimSpace(sessionID) == "" {
		return
	}
	if _, err := e.sessions.Finalize(context.WithoutCancel(ctx), sessionID); err != nil {
		e.log.Warn("finalize orchestration session failed", slog.String("session_id", sessionID), slog.Any("error", err))
	}
}

func (e *Executor) persistText(ctx context.Context, botID, sessionID, role, text string, usage *sdk.Usage) {
	if e.messages == nil || strings.TrimSpace(text) == "" {
		return
	}
	var usageRaw []byte
	if usage != nil {
		usageRaw, _ = json.Marshal(usage)
	}
	if _, err := e.messages.Persist(ctx, message.PersistInput{
		BotID:       botID,
		SessionID:   sessionID,
		Role:        role,
		Content:     conversation.NewTextContent(text),
		Usage:       usageRaw,
		DisplayText: text,
	}); err != nil {
		e.log.Warn("persist orchestration transcript message failed", slog.String("session_id", sessionID), slog.String("role", role), slog.Any("error", err))
	}
}

func (e *Executor) persistSDKMessages(ctx context.Context, botID, sessionID string, messages []sdk.Message, modelID string) {
	if e.messages == nil || len(messages) == 0 {
		return
	}
	for _, sdkMessage := range messages {
		role := strings.TrimSpace(string(sdkMessage.Role))
		if role == "" || role == "user" {
			continue
		}
		modelMessage, ok := sdkMessageToModelMessage(sdkMessage)
		if !ok || !modelMessage.HasContent() {
			continue
		}
		if _, err := e.messages.Persist(ctx, message.PersistInput{
			BotID:       botID,
			SessionID:   sessionID,
			Role:        role,
			Content:     modelMessage.Content,
			Usage:       modelMessage.Usage,
			ModelID:     modelID,
			DisplayText: modelMessage.TextContent(),
		}); err != nil {
			e.log.Warn("persist orchestration transcript message failed", slog.String("session_id", sessionID), slog.String("role", role), slog.Any("error", err))
		}
	}
}

// generateWithFreshState runs the orchestration generate call with retry. The
// resolved run config (model, provider, credentials, system prompt) is reused
// across retries; only the per-attempt execution state and its tool provider
// are rebuilt so a failed retry never sees stale tool submissions.
func (e *Executor) generateWithFreshState(ctx context.Context, sessionID string, cfg agentpkg.RunConfig, attemptTools agenttools.OrchestrationAttemptToolContext) (*agenttools.OrchestrationExecutionState, *agentpkg.GenerateResult, error) {
	baseProviders := cfg.ToolProviders
	var state *agenttools.OrchestrationExecutionState
	for attempt := 1; attempt <= generateRetryMaxAttempts; attempt++ {
		state = &agenttools.OrchestrationExecutionState{}
		provider := agenttools.NewOrchestrationExecutorProvider(state)
		if attemptTools.Service != nil {
			provider = agenttools.NewOrchestrationAttemptExecutorProvider(state, attemptTools)
		}
		cfg.ToolProviders = appendToolProvider(baseProviders, provider)
		result, err := e.agent.Generate(ctx, cfg)
		if err == nil {
			return state, result, nil
		}
		if attempt == generateRetryMaxAttempts || !isRetryableGenerateError(ctx, err) {
			return nil, nil, err
		}
		delay := generateRetryBackoff(attempt)
		e.log.Warn("orchestration generate failed, retrying",
			slog.String("session_id", sessionID),
			slog.Int("attempt", attempt),
			slog.Int("max_attempts", generateRetryMaxAttempts),
			slog.Duration("delay", delay),
			slog.Any("error", err),
		)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, nil, ctx.Err()
		case <-timer.C:
		}
	}
	return nil, nil, errors.New("orchestration generate exhausted retries")
}

func appendToolProvider(base []agenttools.ToolProvider, provider agenttools.ToolProvider) []agenttools.ToolProvider {
	providers := make([]agenttools.ToolProvider, 0, len(base)+1)
	providers = append(providers, base...)
	return append(providers, provider)
}

func isRetryableGenerateError(ctx context.Context, err error) bool {
	if err == nil {
		return false
	}
	if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	errText := err.Error()
	if strings.Contains(errText, "rate limit") || strings.Contains(errText, "rate_limit") {
		return true
	}
	return retryableHTTPStatusPattern.MatchString(errText) || retryableConnectionFailurePattern.MatchString(errText)
}

func generateRetryBackoff(attempt int) time.Duration {
	delay := generateRetryInitialBackoff
	for i := 1; i < attempt; i++ {
		delay *= 2
		if delay >= generateRetryMaxBackoff {
			return generateRetryMaxBackoff
		}
	}
	return delay
}

// buildRunConfig resolves the model, credentials, timezone, and base tool
// providers for an orchestration run. The orchestration executor tool provider
// (which holds per-attempt state) is appended later by generateWithFreshState
// so retries get a fresh state without re-resolving credentials or timezone.
func (e *Executor) buildRunConfig(ctx context.Context, botID, ownerSubject, sessionID, sessionType string) (agentpkg.RunConfig, models.GetResponse, dbsqlc.Provider, error) {
	if e.agent == nil {
		return agentpkg.RunConfig{}, models.GetResponse{}, dbsqlc.Provider{}, errors.New("agent is not configured")
	}
	model, provider, err := e.resolveBotChatModel(ctx, botID)
	if err != nil {
		return agentpkg.RunConfig{}, models.GetResponse{}, dbsqlc.Provider{}, err
	}
	credentialsResolver := providers.NewService(nil, e.store, "")
	authCtx := oauthctx.WithUserID(ctx, ownerSubject)
	creds, err := credentialsResolver.ResolveModelCredentials(authCtx, provider)
	if err != nil {
		return agentpkg.RunConfig{}, models.GetResponse{}, dbsqlc.Provider{}, fmt.Errorf("resolve model credentials: %w", err)
	}
	timezoneName, timezoneLocation := e.resolveBotTimezone(ctx, botID)
	return agentpkg.RunConfig{
		Model: models.NewSDKChatModel(models.SDKModelConfig{
			ModelID:        model.ModelID,
			ClientType:     provider.ClientType,
			APIKey:         creds.APIKey,
			CodexAccountID: creds.CodexAccountID,
			BaseURL:        providers.ProviderConfigString(provider, "base_url"),
			HTTPClient:     e.httpClient,
		}),
		SessionType:        sessionType,
		SupportsToolCall:   model.HasCompatibility(models.CompatToolCall),
		SupportsImageInput: model.HasCompatibility(models.CompatVision),
		Identity: agentpkg.SessionContext{
			BotID:            botID,
			ChatID:           botID,
			SessionID:        sessionID,
			Timezone:         timezoneName,
			TimezoneLocation: timezoneLocation,
		},
		LoopDetection: agentpkg.LoopDetectionConfig{Enabled: false},
		ToolProviders: append([]agenttools.ToolProvider(nil), e.baseProviders...),
	}, model, provider, nil
}

func (e *Executor) resolveBotChatModel(ctx context.Context, botID string) (models.GetResponse, dbsqlc.Provider, error) {
	if e.settings == nil || e.models == nil {
		return models.GetResponse{}, dbsqlc.Provider{}, errors.New("settings/models services are not configured")
	}
	botSettings, err := e.settings.GetBot(ctx, botID)
	if err != nil {
		return models.GetResponse{}, dbsqlc.Provider{}, fmt.Errorf("load bot settings: %w", err)
	}
	modelRef := strings.TrimSpace(botSettings.ChatModelID)
	if modelRef == "" {
		return models.GetResponse{}, dbsqlc.Provider{}, errors.New("bot chat model is not configured")
	}
	var model models.GetResponse
	if _, parseErr := db.ParseUUID(modelRef); parseErr == nil {
		model, err = e.models.GetByID(ctx, modelRef)
	} else {
		model, err = e.models.GetByModelID(ctx, modelRef)
	}
	if err != nil {
		return models.GetResponse{}, dbsqlc.Provider{}, fmt.Errorf("resolve chat model %q: %w", modelRef, err)
	}
	if model.Type != models.ModelTypeChat {
		return models.GetResponse{}, dbsqlc.Provider{}, errors.New("configured bot chat model is not a chat model")
	}
	provider, err := models.FetchProviderByID(ctx, e.store, model.ProviderID)
	if err != nil {
		return models.GetResponse{}, dbsqlc.Provider{}, fmt.Errorf("load provider: %w", err)
	}
	return model, provider, nil
}

func (e *Executor) resolveBotTimezone(ctx context.Context, botID string) (string, *time.Location) {
	if strings.TrimSpace(botID) != "" && e.store != nil {
		if botUUID, err := db.ParseUUID(botID); err == nil {
			if row, getErr := e.store.GetBotByID(ctx, botUUID); getErr == nil && row.Timezone.Valid {
				if loc, name, resolveErr := tzutil.Resolve(strings.TrimSpace(row.Timezone.String)); resolveErr == nil {
					return name, loc
				}
			}
		}
	}
	if e.clockLocation != nil {
		return e.clockLocation.String(), e.clockLocation
	}
	return tzutil.DefaultName, tzutil.MustResolve(tzutil.DefaultName)
}

func failAttempt(base orchestration.AttemptCompletion, failureClass string, err error) orchestration.AttemptCompletion {
	base.Status = orchestration.TaskAttemptStatusFailed
	base.FailureClass = strings.TrimSpace(failureClass)
	base.TerminalReason = err.Error()
	base.Summary = err.Error()
	if base.StructuredOutput == nil {
		base.StructuredOutput = map[string]any{}
	}
	return base
}

func failVerification(base orchestration.VerificationCompletion, failureClass string, err error) orchestration.VerificationCompletion {
	base.Status = orchestration.TaskVerificationStatusFailed
	base.Verdict = orchestration.VerificationVerdictRejected
	base.FailureClass = strings.TrimSpace(failureClass)
	base.TerminalReason = err.Error()
	base.Summary = err.Error()
	return base
}

type attemptContext struct {
	BotID           string
	ParentSessionID string
	Run             dbsqlc.OrchestrationRun
	Task            dbsqlc.OrchestrationTask
	Attempt         orchestration.TaskAttempt
	TaskInputs      map[string]any
	InputManifest   map[string]any
	Predecessors    []map[string]any
}

type verificationContext struct {
	BotID              string
	ParentSessionID    string
	Run                dbsqlc.OrchestrationRun
	Task               dbsqlc.OrchestrationTask
	Result             dbsqlc.OrchestrationTaskResult
	Verification       orchestration.TaskVerification
	VerificationPolicy map[string]any
	ResultArtifacts    []map[string]any
}

func (e *Executor) loadAttemptContext(ctx context.Context, attempt orchestration.TaskAttempt) (attemptContext, error) {
	if e.queries == nil {
		return attemptContext{}, errors.New("orchestration queries are not configured")
	}
	taskID, err := db.ParseUUID(attempt.TaskID)
	if err != nil {
		return attemptContext{}, fmt.Errorf("invalid task id: %w", err)
	}
	runID, err := db.ParseUUID(attempt.RunID)
	if err != nil {
		return attemptContext{}, fmt.Errorf("invalid run id: %w", err)
	}
	taskRow, err := e.queries.GetOrchestrationTaskByID(ctx, taskID)
	if err != nil {
		return attemptContext{}, fmt.Errorf("load task: %w", err)
	}
	runRow, err := e.queries.GetOrchestrationRunByID(ctx, runID)
	if err != nil {
		return attemptContext{}, fmt.Errorf("load run: %w", err)
	}
	sourceMetadata := decodeJSONObject(runRow.SourceMetadata)
	botID := strings.TrimSpace(stringValue(sourceMetadata["bot_id"]))
	if botID == "" {
		return attemptContext{}, errors.New("run source metadata is missing bot_id")
	}
	parentSessionID := strings.TrimSpace(stringValue(sourceMetadata["session_id"]))
	inputManifest := map[string]any{}
	if attempt.InputManifestID != "" {
		manifestID, manifestErr := db.ParseUUID(attempt.InputManifestID)
		if manifestErr == nil {
			if manifestRow, getErr := e.queries.GetOrchestrationInputManifestByID(ctx, manifestID); getErr == nil {
				inputManifest = map[string]any{
					"id":                            manifestRow.ID.String(),
					"captured_task_inputs":          decodeJSONObject(manifestRow.CapturedTaskInputs),
					"captured_artifact_versions":    decodeJSONArrayObjects(manifestRow.CapturedArtifactVersions),
					"captured_blackboard_revisions": decodeJSONArrayObjects(manifestRow.CapturedBlackboardRevisions),
					"projection_hash":               strings.TrimSpace(manifestRow.ProjectionHash),
				}
			}
		}
	}
	predecessors, err := e.loadPredecessorContexts(ctx, taskRow)
	if err != nil {
		return attemptContext{}, err
	}
	return attemptContext{
		BotID:           botID,
		ParentSessionID: parentSessionID,
		Run:             runRow,
		Task:            taskRow,
		Attempt:         attempt,
		TaskInputs:      decodeJSONObject(taskRow.Inputs),
		InputManifest:   inputManifest,
		Predecessors:    predecessors,
	}, nil
}

func (e *Executor) loadVerificationContext(ctx context.Context, verification orchestration.TaskVerification) (verificationContext, error) {
	if e.queries == nil {
		return verificationContext{}, errors.New("orchestration queries are not configured")
	}
	taskID, err := db.ParseUUID(verification.TaskID)
	if err != nil {
		return verificationContext{}, fmt.Errorf("invalid task id: %w", err)
	}
	runID, err := db.ParseUUID(verification.RunID)
	if err != nil {
		return verificationContext{}, fmt.Errorf("invalid run id: %w", err)
	}
	resultID, err := db.ParseUUID(verification.ResultID)
	if err != nil {
		return verificationContext{}, fmt.Errorf("invalid result id: %w", err)
	}
	taskRow, err := e.queries.GetOrchestrationTaskByID(ctx, taskID)
	if err != nil {
		return verificationContext{}, fmt.Errorf("load task: %w", err)
	}
	runRow, err := e.queries.GetOrchestrationRunByID(ctx, runID)
	if err != nil {
		return verificationContext{}, fmt.Errorf("load run: %w", err)
	}
	resultRow, err := e.queries.GetOrchestrationTaskResultByID(ctx, resultID)
	if err != nil {
		return verificationContext{}, fmt.Errorf("load task result: %w", err)
	}
	sourceMetadata := decodeJSONObject(runRow.SourceMetadata)
	botID := strings.TrimSpace(stringValue(sourceMetadata["bot_id"]))
	if botID == "" {
		return verificationContext{}, errors.New("run source metadata is missing bot_id")
	}
	parentSessionID := strings.TrimSpace(stringValue(sourceMetadata["session_id"]))
	artifacts, err := e.queries.ListOrchestrationArtifactsByTask(ctx, taskRow.ID)
	if err != nil {
		return verificationContext{}, fmt.Errorf("load task artifacts: %w", err)
	}
	return verificationContext{
		BotID:              botID,
		ParentSessionID:    parentSessionID,
		Run:                runRow,
		Task:               taskRow,
		Result:             resultRow,
		Verification:       verification,
		VerificationPolicy: decodeJSONObject(taskRow.VerificationPolicy),
		ResultArtifacts:    encodeArtifactsForAttempt(artifacts, resultRow.AttemptID),
	}, nil
}

func (e *Executor) loadPredecessorContexts(ctx context.Context, taskRow dbsqlc.OrchestrationTask) ([]map[string]any, error) {
	dependencies, err := e.queries.ListActiveOrchestrationTaskDependenciesBySuccessor(ctx, taskRow.ID)
	if err != nil {
		return nil, fmt.Errorf("load predecessor dependencies: %w", err)
	}
	if len(dependencies) == 0 {
		return nil, nil
	}
	tasksByRun, err := e.queries.ListCurrentOrchestrationTasksByRun(ctx, taskRow.RunID)
	if err != nil {
		return nil, fmt.Errorf("load run tasks: %w", err)
	}
	resultsByRun, err := e.queries.ListCurrentOrchestrationTaskResultsByRun(ctx, taskRow.RunID)
	if err != nil {
		return nil, fmt.Errorf("load run results: %w", err)
	}
	artifactsByRun, err := e.queries.ListCurrentOrchestrationArtifactsByRun(ctx, taskRow.RunID)
	if err != nil {
		return nil, fmt.Errorf("load run artifacts: %w", err)
	}
	tasksByID := make(map[string]dbsqlc.OrchestrationTask, len(tasksByRun))
	for _, candidate := range tasksByRun {
		tasksByID[candidate.ID.String()] = candidate
	}
	resultsByTaskID := make(map[string]dbsqlc.OrchestrationTaskResult, len(resultsByRun))
	for _, candidate := range resultsByRun {
		resultsByTaskID[candidate.TaskID.String()] = candidate
	}
	artifactsByTaskID := make(map[string][]dbsqlc.OrchestrationArtifact)
	for _, artifact := range artifactsByRun {
		artifactsByTaskID[artifact.TaskID.String()] = append(artifactsByTaskID[artifact.TaskID.String()], artifact)
	}
	predecessors := make([]map[string]any, 0, len(dependencies))
	for _, dependency := range dependencies {
		task, ok := tasksByID[dependency.PredecessorTaskID.String()]
		if !ok {
			continue
		}
		item := map[string]any{
			"task_id":          task.ID.String(),
			"goal":             strings.TrimSpace(task.Goal),
			"status":           strings.TrimSpace(task.Status),
			"worker_profile":   strings.TrimSpace(task.WorkerProfile),
			"inputs":           decodeJSONObject(task.Inputs),
			"blackboard_scope": strings.TrimSpace(task.BlackboardScope),
		}
		if result, ok := resultsByTaskID[task.ID.String()]; ok {
			item["result"] = map[string]any{
				"result_id":         result.ID.String(),
				"attempt_id":        pgUUIDString(result.AttemptID),
				"status":            strings.TrimSpace(result.Status),
				"summary":           strings.TrimSpace(result.Summary),
				"failure_class":     strings.TrimSpace(result.FailureClass),
				"request_replan":    result.RequestReplan,
				"artifact_intents":  decodeJSONArrayObjects(result.ArtifactIntents),
				"structured_output": decodeJSONObject(result.StructuredOutput),
			}
			item["artifacts"] = encodeArtifactsForAttempt(artifactsByTaskID[task.ID.String()], result.AttemptID)
		}
		predecessors = append(predecessors, item)
	}
	return predecessors, nil
}

func buildWorkerPrompt(execCtx attemptContext) string {
	payload := map[string]any{
		"run": map[string]any{
			"run_id":           execCtx.Run.ID.String(),
			"goal":             strings.TrimSpace(execCtx.Run.Goal),
			"planner_epoch":    execCtx.Run.PlannerEpoch,
			"lifecycle_status": strings.TrimSpace(execCtx.Run.LifecycleStatus),
			"source_metadata":  decodeJSONObject(execCtx.Run.SourceMetadata),
			"run_input":        decodeJSONObject(execCtx.Run.Input),
			"run_policies":     decodeJSONObject(execCtx.Run.Policies),
			"control_policy":   decodeJSONObject(execCtx.Run.ControlPolicy),
		},
		"task": map[string]any{
			"task_id":             execCtx.Task.ID.String(),
			"goal":                strings.TrimSpace(execCtx.Task.Goal),
			"kind":                strings.TrimSpace(execCtx.Task.Kind),
			"role":                strings.TrimSpace(execCtx.Task.Role),
			"worker_profile":      strings.TrimSpace(execCtx.Task.WorkerProfile),
			"priority":            execCtx.Task.Priority,
			"inputs":              execCtx.TaskInputs,
			"retry_policy":        decodeJSONObject(execCtx.Task.RetryPolicy),
			"verification_policy": decodeJSONObject(execCtx.Task.VerificationPolicy),
			"blackboard_scope":    strings.TrimSpace(execCtx.Task.BlackboardScope),
			"planner_epoch":       execCtx.Task.PlannerEpoch,
		},
		"attempt": map[string]any{
			"attempt_id":          execCtx.Attempt.ID,
			"attempt_no":          execCtx.Attempt.AttemptNo,
			"input_manifest":      execCtx.InputManifest,
			"predecessor_results": execCtx.Predecessors,
		},
	}
	return "Execute the following orchestration task. Finish by calling submit_task_result.\n\nContext JSON:\n" + mustJSON(payload)
}

func buildVerifierPrompt(execCtx verificationContext) string {
	payload := map[string]any{
		"run": map[string]any{
			"run_id":           execCtx.Run.ID.String(),
			"goal":             strings.TrimSpace(execCtx.Run.Goal),
			"planner_epoch":    execCtx.Run.PlannerEpoch,
			"lifecycle_status": strings.TrimSpace(execCtx.Run.LifecycleStatus),
		},
		"task": map[string]any{
			"task_id":             execCtx.Task.ID.String(),
			"goal":                strings.TrimSpace(execCtx.Task.Goal),
			"worker_profile":      strings.TrimSpace(execCtx.Task.WorkerProfile),
			"verification_policy": execCtx.VerificationPolicy,
		},
		"result": map[string]any{
			"result_id":         execCtx.Result.ID.String(),
			"attempt_id":        pgUUIDString(execCtx.Result.AttemptID),
			"status":            strings.TrimSpace(execCtx.Result.Status),
			"summary":           strings.TrimSpace(execCtx.Result.Summary),
			"failure_class":     strings.TrimSpace(execCtx.Result.FailureClass),
			"request_replan":    execCtx.Result.RequestReplan,
			"artifact_intents":  decodeJSONArrayObjects(execCtx.Result.ArtifactIntents),
			"structured_output": decodeJSONObject(execCtx.Result.StructuredOutput),
			"artifacts":         execCtx.ResultArtifacts,
		},
		"verification": map[string]any{
			"verification_id":  execCtx.Verification.ID,
			"attempt_no":       execCtx.Verification.AttemptNo,
			"verifier_profile": strings.TrimSpace(execCtx.Verification.VerifierProfile),
		},
	}
	return "Verify the following orchestration task result. Finish by calling submit_verification_result.\n\nContext JSON:\n" + mustJSON(payload)
}

func encodeArtifactsForAttempt(artifacts []dbsqlc.OrchestrationArtifact, attemptID pgtype.UUID) []map[string]any {
	filtered := make([]map[string]any, 0, len(artifacts))
	for _, artifact := range artifacts {
		if !attemptID.Valid {
			continue
		}
		if !artifact.AttemptID.Valid || artifact.AttemptID.String() != attemptID.String() {
			continue
		}
		filtered = append(filtered, map[string]any{
			"artifact_id":  artifact.ID.String(),
			"kind":         strings.TrimSpace(artifact.Kind),
			"uri":          strings.TrimSpace(artifact.Uri),
			"version":      strings.TrimSpace(artifact.Version),
			"digest":       strings.TrimSpace(artifact.Digest),
			"content_type": strings.TrimSpace(artifact.ContentType),
			"summary":      strings.TrimSpace(artifact.Summary),
			"metadata":     decodeJSONObject(artifact.Metadata),
			"created_at":   db.TimeFromPg(artifact.CreatedAt).UTC().Format(time.RFC3339Nano),
		})
	}
	return filtered
}

func decodeJSONObject(raw []byte) map[string]any {
	if len(raw) == 0 || string(raw) == "null" {
		return map[string]any{}
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		return map[string]any{}
	}
	return normalizeObject(value)
}

func decodeJSONArrayObjects(raw []byte) []map[string]any {
	if len(raw) == 0 || string(raw) == "null" {
		return []map[string]any{}
	}
	var value []map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		return []map[string]any{}
	}
	for i := range value {
		value[i] = normalizeObject(value[i])
	}
	return value
}

func normalizeObject(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	normalized := make(map[string]any, len(value))
	for key, item := range value {
		normalized[key] = normalizeValue(item)
	}
	return normalized
}

func normalizeValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return normalizeObject(typed)
	case []any:
		items := make([]any, 0, len(typed))
		for _, item := range typed {
			items = append(items, normalizeValue(item))
		}
		return items
	default:
		return typed
	}
}

func pgUUIDString(value interface{ String() string }) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(value.String())
}

func stringValue(raw any) string {
	value, _ := raw.(string)
	return value
}

func mustJSON(value any) string {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(data)
}

func sdkMessageToModelMessage(msg sdk.Message) (conversation.ModelMessage, bool) {
	data, err := json.Marshal(msg)
	if err != nil {
		return conversation.ModelMessage{}, false
	}
	var envelope struct {
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return conversation.ModelMessage{}, false
	}
	var usage json.RawMessage
	if msg.Usage != nil {
		usage, _ = json.Marshal(msg.Usage)
	}
	return conversation.ModelMessage{
		Role:    string(msg.Role),
		Content: envelope.Content,
		Usage:   usage,
	}, true
}
