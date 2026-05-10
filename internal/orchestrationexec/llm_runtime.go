package orchestrationexec

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	sdk "github.com/memohai/twilight-ai/sdk"

	agentpkg "github.com/memohai/memoh/internal/agent"
	"github.com/memohai/memoh/internal/db"
	"github.com/memohai/memoh/internal/db/postgres/sqlc"
	postgresstore "github.com/memohai/memoh/internal/db/postgres/store"
	dbstore "github.com/memohai/memoh/internal/db/store"
	"github.com/memohai/memoh/internal/models"
	"github.com/memohai/memoh/internal/oauthctx"
	"github.com/memohai/memoh/internal/orchestration"
	"github.com/memohai/memoh/internal/providers"
	"github.com/memohai/memoh/internal/settings"
	tzutil "github.com/memohai/memoh/internal/timezone"
)

const (
	LLMWorkerExecutorID   = "llm.workerd"
	LLMVerifierExecutorID = "llm.verifyd"

	addAttemptArtifactToolName = "add_attempt_artifact"
	finishAttemptToolName      = "finish_attempt"
	finishVerificationToolName = "finish_verification"
)

type finishAttemptInput struct {
	Status               string `json:"status"`
	Summary              string `json:"summary"`
	FailureClass         string `json:"failure_class,omitempty"`
	TerminalReason       string `json:"terminal_reason,omitempty"`
	RequestReplan        bool   `json:"request_replan,omitempty"`
	StructuredOutputJSON string `json:"structured_output_json,omitempty"`
}

type finishVerificationInput struct {
	Status         string `json:"status"`
	Verdict        string `json:"verdict"`
	Summary        string `json:"summary"`
	FailureClass   string `json:"failure_class,omitempty"`
	TerminalReason string `json:"terminal_reason,omitempty"`
	RequestReplan  bool   `json:"request_replan,omitempty"`
}

type Runtime struct {
	logger          *slog.Logger
	queries         *sqlc.Queries
	storeQueries    dbstore.Queries
	settingsService *settings.Service
	modelsService   *models.Service
	agent           *agentpkg.Agent
	httpClient      *http.Client
	clockLocation   *time.Location
}

type actionLedgerSubject struct {
	runID          pgtype.UUID
	taskID         pgtype.UUID
	attemptID      pgtype.UUID
	verificationID pgtype.UUID
	claimEpoch     int64
	envSessionID   pgtype.UUID
	envLeaseEpoch  int64
}

type actionLedgerObserver struct {
	queries *sqlc.Queries
	subject actionLedgerSubject
}

func NewRuntime(
	log *slog.Logger,
	queries *sqlc.Queries,
	settingsService *settings.Service,
	modelsService *models.Service,
	agent *agentpkg.Agent,
	clockLocation *time.Location,
) *Runtime {
	if log == nil {
		log = slog.Default()
	}
	if clockLocation == nil {
		clockLocation = time.UTC
	}
	return &Runtime{
		logger:          log.With(slog.String("component", "orchestration_llm_runtime")),
		queries:         queries,
		storeQueries:    postgresstore.NewQueries(queries),
		settingsService: settingsService,
		modelsService:   modelsService,
		agent:           agent,
		httpClient: &http.Client{
			Timeout: 10 * time.Minute,
			Transport: &http.Transport{
				DialContext: (&net.Dialer{
					Timeout:   30 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				TLSHandshakeTimeout:   10 * time.Second,
				ResponseHeaderTimeout: 30 * time.Second,
				MaxIdleConns:          100,
				MaxIdleConnsPerHost:   10,
				IdleConnTimeout:       90 * time.Second,
			},
		},
		clockLocation: clockLocation,
	}
}

func newActionLedgerObserver(queries *sqlc.Queries, subject actionLedgerSubject) agentpkg.ToolCallObserver {
	if queries == nil {
		return nil
	}
	return &actionLedgerObserver{
		queries: queries,
		subject: subject,
	}
}

func (o *actionLedgerObserver) OnToolCallStart(ctx context.Context, observation agentpkg.ToolCallObservation) error {
	if o == nil || o.queries == nil {
		return nil
	}
	recordID := uuid.New()
	effectClass := classifyToolEffect(observation.ToolName)
	if effectClass == "external_irreversible" {
		if err := o.consumeSideEffectApprovalToken(ctx, observation); err != nil {
			return err
		}
	}
	inputPayload, err := marshalJSONValue(observation.Input)
	if err != nil {
		return fmt.Errorf("marshal action input payload: %w", err)
	}
	if o.subject.attemptID.Valid {
		_, err = o.queries.CreateOrchestrationAttemptActionRecord(ctx, sqlc.CreateOrchestrationAttemptActionRecordParams{
			ID:           pgUUIDFromUUID(recordID),
			RunID:        o.subject.runID,
			TaskID:       o.subject.taskID,
			AttemptID:    o.subject.attemptID,
			ActionKind:   "tool_call",
			Status:       "running",
			EffectClass:  effectClass,
			ToolName:     strings.TrimSpace(observation.ToolName),
			ToolCallID:   strings.TrimSpace(observation.ToolCallID),
			InputPayload: inputPayload,
		})
		if err != nil {
			return fmt.Errorf("create attempt action record: %w", err)
		}
		return nil
	}
	_, err = o.queries.CreateOrchestrationVerificationActionRecord(ctx, sqlc.CreateOrchestrationVerificationActionRecordParams{
		ID:             pgUUIDFromUUID(recordID),
		RunID:          o.subject.runID,
		TaskID:         o.subject.taskID,
		VerificationID: o.subject.verificationID,
		ActionKind:     "tool_call",
		Status:         "running",
		EffectClass:    effectClass,
		ToolName:       strings.TrimSpace(observation.ToolName),
		ToolCallID:     strings.TrimSpace(observation.ToolCallID),
		InputPayload:   inputPayload,
	})
	if err != nil {
		return fmt.Errorf("create verification action record: %w", err)
	}
	return nil
}

func (o *actionLedgerObserver) OnToolCallFinish(ctx context.Context, observation agentpkg.ToolCallObservation) error {
	if o == nil || o.queries == nil {
		return nil
	}
	status := "completed"
	if observation.Err != nil {
		status = "failed"
	}
	outputPayload, err := marshalJSONValue(observation.Result)
	if err != nil {
		return fmt.Errorf("marshal action output payload: %w", err)
	}
	errorPayload, err := marshalJSONValue(actionErrorPayload(observation.Err))
	if err != nil {
		return fmt.Errorf("marshal action error payload: %w", err)
	}
	summary := summarizeActionObservation(observation)
	if o.subject.attemptID.Valid {
		_, err = o.queries.CompleteOrchestrationAttemptActionRecord(ctx, sqlc.CompleteOrchestrationAttemptActionRecordParams{
			AttemptID:     o.subject.attemptID,
			ToolCallID:    strings.TrimSpace(observation.ToolCallID),
			Status:        status,
			OutputPayload: outputPayload,
			ErrorPayload:  errorPayload,
			Summary:       summary,
		})
		if err != nil {
			return fmt.Errorf("complete attempt action record: %w", err)
		}
		return nil
	}
	_, err = o.queries.CompleteOrchestrationVerificationActionRecord(ctx, sqlc.CompleteOrchestrationVerificationActionRecordParams{
		VerificationID: o.subject.verificationID,
		ToolCallID:     strings.TrimSpace(observation.ToolCallID),
		Status:         status,
		OutputPayload:  outputPayload,
		ErrorPayload:   errorPayload,
		Summary:        summary,
	})
	if err != nil {
		return fmt.Errorf("complete verification action record: %w", err)
	}
	return nil
}

func (o *actionLedgerObserver) consumeSideEffectApprovalToken(ctx context.Context, observation agentpkg.ToolCallObservation) error {
	if !o.subject.attemptID.Valid {
		return errors.New("external irreversible tool calls are only allowed from task attempts")
	}
	if o.subject.claimEpoch <= 0 {
		return errors.New("external irreversible tool call missing attempt claim fence")
	}
	token := approvalTokenFromInput(observation.Input)
	if token == "" {
		return fmt.Errorf("external irreversible tool %q requires an approval_token", strings.TrimSpace(observation.ToolName))
	}
	_, err := o.queries.ConsumeOrchestrationSideEffectApprovalToken(ctx, sqlc.ConsumeOrchestrationSideEffectApprovalTokenParams{
		ToolCallID:    strings.TrimSpace(observation.ToolCallID),
		TokenHash:     sideEffectApprovalTokenHash(token),
		AttemptID:     o.subject.attemptID,
		ClaimEpoch:    o.subject.claimEpoch,
		EnvSessionID:  o.subject.envSessionID,
		EnvLeaseEpoch: o.subject.envLeaseEpoch,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("external irreversible tool %q approval token is missing, expired, consumed, or fenced to a different attempt/env lease", strings.TrimSpace(observation.ToolName))
		}
		return fmt.Errorf("consume side-effect approval token: %w", err)
	}
	return nil
}

func classifyToolEffect(toolName string) string {
	switch strings.TrimSpace(toolName) {
	case "read", "list", "bg_status":
		return "env_local_read"
	case "write", "edit", "exec":
		return "env_local_mutation"
	case "web_search", "web_fetch", "browser_observe", "list_email", "read_email", "list_schedule", "get_schedule", "get_contacts", "search_messages", "list_sessions", "list_email_accounts":
		return "external_read"
	case "send_email":
		return "external_irreversible"
	case "send", "react", "browser_action", "browser_remote_session", "create_schedule", "update_schedule", "delete_schedule", "generate_image", "speak":
		return "external_write"
	default:
		return ""
	}
}

func approvalTokenFromInput(input any) string {
	args := mapValue(input)
	return strings.TrimSpace(stringValue(args["approval_token"]))
}

func sideEffectApprovalTokenHash(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

func sideEffectEnvFence(inputManifest map[string]any) (pgtype.UUID, int64) {
	env := mapValue(inputManifest["captured_env_preconditions"])
	sessionID := strings.TrimSpace(stringValue(env["session_id"]))
	if sessionID == "" {
		return pgtype.UUID{}, 0
	}
	return parseUUIDOrZero(sessionID), int64Value(env["lease_epoch"])
}

func marshalJSONValue(value any) ([]byte, error) {
	if value == nil {
		return []byte("null"), nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func actionErrorPayload(err error) any {
	if err == nil {
		return nil
	}
	return map[string]any{
		"message": err.Error(),
	}
}

func summarizeActionObservation(observation agentpkg.ToolCallObservation) string {
	if observation.Err != nil {
		return truncateSummary(observation.Err.Error(), 240)
	}
	if text := summarizeActionValue(observation.Result); text != "" {
		return truncateSummary(text, 240)
	}
	return strings.TrimSpace(observation.ToolName)
}

func summarizeActionValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	case map[string]any:
		if message := strings.TrimSpace(stringValue(typed["message"])); message != "" {
			return message
		}
		if text := strings.TrimSpace(stringValue(typed["text"])); text != "" {
			return text
		}
	case []any:
		if len(typed) == 0 {
			return ""
		}
		return summarizeActionValue(typed[0])
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(raw)
}

func (r *Runtime) generateWithThinkingTrace(ctx context.Context, cfg agentpkg.RunConfig, recordThinking func(string), recordOutput func(string)) (*agentpkg.GenerateResult, error) {
	var text strings.Builder
	var textChunk strings.Builder
	var reasoning strings.Builder
	var usage *sdk.Usage
	var streamErr error

	flushReasoning := func() {
		delta := strings.TrimSpace(reasoning.String())
		reasoning.Reset()
		if delta == "" || recordThinking == nil {
			return
		}
		recordThinking(delta)
	}
	flushText := func() {
		delta := strings.TrimSpace(textChunk.String())
		textChunk.Reset()
		if delta == "" || recordOutput == nil {
			return
		}
		recordOutput(delta)
	}

	for event := range r.agent.Stream(ctx, cfg) {
		switch event.Type {
		case agentpkg.EventTextDelta:
			text.WriteString(event.Delta)
			textChunk.WriteString(event.Delta)
			if textChunk.Len() >= 600 {
				flushText()
			}
		case agentpkg.EventTextEnd:
			flushText()
		case agentpkg.EventReasoningDelta:
			reasoning.WriteString(event.Delta)
			if reasoning.Len() >= 600 {
				flushReasoning()
			}
		case agentpkg.EventReasoningEnd:
			flushReasoning()
		case agentpkg.EventError:
			if strings.TrimSpace(event.Error) != "" {
				streamErr = errors.New(event.Error)
			}
		case agentpkg.EventAgentAbort:
			if streamErr == nil {
				streamErr = errors.New("agent stream aborted")
			}
		case agentpkg.EventAgentEnd:
			flushText()
			flushReasoning()
			streamErr = nil
			if len(event.Usage) > 0 {
				var parsed sdk.Usage
				if err := json.Unmarshal(event.Usage, &parsed); err == nil {
					usage = &parsed
				}
			}
		}
	}
	flushText()
	flushReasoning()
	if streamErr != nil {
		return nil, streamErr
	}
	return &agentpkg.GenerateResult{
		Text:  text.String(),
		Usage: usage,
	}, nil
}

func (r *Runtime) recordAttemptThinking(ctx context.Context, execCtx attemptExecutionContext, attemptID pgtype.UUID, role, delta string) {
	r.recordAttemptAgentTrace(ctx, execCtx, attemptID, "agent.thinking", role, "reasoning_delta", delta)
}

func (r *Runtime) recordAttemptAgentOutput(ctx context.Context, execCtx attemptExecutionContext, attemptID pgtype.UUID, role, delta string) {
	r.recordAttemptAgentTrace(ctx, execCtx, attemptID, "agent.output", role, "text_delta", delta)
}

func (r *Runtime) recordAttemptAgentTrace(ctx context.Context, execCtx attemptExecutionContext, attemptID pgtype.UUID, toolName, role, eventName, delta string) {
	if r == nil || r.queries == nil || !attemptID.Valid {
		return
	}
	toolCallID, payload := agentTraceRecordPayload(role, eventName, delta)
	if strings.TrimSpace(toolCallID) == "" {
		return
	}
	_, err := r.queries.CreateOrchestrationAttemptActionRecord(ctx, sqlc.CreateOrchestrationAttemptActionRecordParams{
		ID:           pgUUIDFromUUID(uuid.New()),
		RunID:        execCtx.Run.ID,
		TaskID:       execCtx.Task.ID,
		AttemptID:    attemptID,
		ActionKind:   "tool_call",
		Status:       "running",
		ToolName:     toolName,
		ToolCallID:   toolCallID,
		InputPayload: []byte(`{"event":"agent_stream"}`),
	})
	if err != nil {
		r.logger.Warn("create attempt agent trace record failed", slog.Any("error", err))
		return
	}
	_, err = r.queries.CompleteOrchestrationAttemptActionRecord(ctx, sqlc.CompleteOrchestrationAttemptActionRecordParams{
		Status:        "completed",
		OutputPayload: payload,
		ErrorPayload:  []byte("null"),
		Summary:       truncateSummary(delta, 240),
		AttemptID:     attemptID,
		ToolCallID:    toolCallID,
	})
	if err != nil {
		r.logger.Warn("complete attempt agent trace record failed", slog.Any("error", err))
	}
}

func (r *Runtime) recordVerificationThinking(ctx context.Context, execCtx verificationExecutionContext, verificationID pgtype.UUID, role, delta string) {
	r.recordVerificationAgentTrace(ctx, execCtx, verificationID, "agent.thinking", role, "reasoning_delta", delta)
}

func (r *Runtime) recordVerificationAgentOutput(ctx context.Context, execCtx verificationExecutionContext, verificationID pgtype.UUID, role, delta string) {
	r.recordVerificationAgentTrace(ctx, execCtx, verificationID, "agent.output", role, "text_delta", delta)
}

func (r *Runtime) recordVerificationAgentTrace(ctx context.Context, execCtx verificationExecutionContext, verificationID pgtype.UUID, toolName, role, eventName, delta string) {
	if r == nil || r.queries == nil || !verificationID.Valid {
		return
	}
	toolCallID, payload := agentTraceRecordPayload(role, eventName, delta)
	if strings.TrimSpace(toolCallID) == "" {
		return
	}
	_, err := r.queries.CreateOrchestrationVerificationActionRecord(ctx, sqlc.CreateOrchestrationVerificationActionRecordParams{
		ID:             pgUUIDFromUUID(uuid.New()),
		RunID:          execCtx.Run.ID,
		TaskID:         execCtx.Task.ID,
		VerificationID: verificationID,
		ActionKind:     "tool_call",
		Status:         "running",
		ToolName:       toolName,
		ToolCallID:     toolCallID,
		InputPayload:   []byte(`{"event":"agent_stream"}`),
	})
	if err != nil {
		r.logger.Warn("create verification agent trace record failed", slog.Any("error", err))
		return
	}
	_, err = r.queries.CompleteOrchestrationVerificationActionRecord(ctx, sqlc.CompleteOrchestrationVerificationActionRecordParams{
		Status:         "completed",
		OutputPayload:  payload,
		ErrorPayload:   []byte("null"),
		Summary:        truncateSummary(delta, 240),
		VerificationID: verificationID,
		ToolCallID:     toolCallID,
	})
	if err != nil {
		r.logger.Warn("complete verification agent trace record failed", slog.Any("error", err))
	}
}

func agentTraceRecordPayload(role, eventName, delta string) (string, []byte) {
	delta = strings.TrimSpace(delta)
	if delta == "" {
		return "", nil
	}
	payload, err := marshalJSONValue(map[string]any{
		"event": strings.TrimSpace(eventName),
		"role":  strings.TrimSpace(role),
		"delta": delta,
	})
	if err != nil {
		return "", nil
	}
	return strings.TrimSpace(eventName) + "-" + uuid.NewString(), payload
}

func truncateSummary(value string, maxLen int) string {
	value = strings.TrimSpace(value)
	if maxLen <= 0 || len(value) <= maxLen {
		return value
	}
	if maxLen <= 3 {
		return value[:maxLen]
	}
	return value[:maxLen-3] + "..."
}

func pgUUIDFromUUID(value uuid.UUID) pgtype.UUID {
	var bytes [16]byte
	copy(bytes[:], value[:])
	return pgtype.UUID{Bytes: bytes, Valid: true}
}

func parseUUIDOrZero(value string) pgtype.UUID {
	parsed, err := uuid.Parse(strings.TrimSpace(value))
	if err != nil {
		return pgtype.UUID{}
	}
	return pgUUIDFromUUID(parsed)
}

func (r *Runtime) ExecuteAttempt(ctx context.Context, attempt orchestration.TaskAttempt) orchestration.AttemptCompletion {
	completion := orchestration.AttemptCompletion{
		AttemptID:          attempt.ID,
		ClaimToken:         attempt.ClaimToken,
		Status:             orchestration.TaskAttemptStatusFailed,
		Summary:            "orchestration worker failed",
		StructuredOutput:   map[string]any{},
		CompletionMetadata: map[string]any{"executor": LLMWorkerExecutorID},
	}
	execCtx, err := r.loadAttemptExecutionContext(ctx, attempt)
	if err != nil {
		return failAttemptCompletion(completion, "attempt_context_load_failed", err)
	}
	cfg, model, provider, err := r.buildBotRunConfig(ctx, execCtx.BotID, execCtx.Run.OwnerSubject)
	if err != nil {
		return failAttemptCompletion(completion, "worker_model_resolution_failed", err)
	}
	if !cfg.SupportsToolCall {
		return failAttemptCompletion(completion, "worker_model_resolution_failed", errors.New("worker model must support tool calling"))
	}
	envResources, err := r.loadEnvResourceCatalog(ctx, execCtx.Run.TenantID)
	if err != nil {
		return failAttemptCompletion(completion, "attempt_context_load_failed", err)
	}
	cfg.System = workerSystemPrompt
	cfg.Messages = []sdk.Message{sdk.UserMessage(buildWorkerPrompt(execCtx))}
	cfg.ResponseFormat = nil
	var submittedCompletion *finishAttemptInput
	artifactIntents := make([]orchestration.AttemptArtifactIntent, 0)
	addArtifactTool := sdk.Tool{
		Name:        addAttemptArtifactToolName,
		Description: "Record one artifact produced by this attempt using flat fields. Call once per artifact before finish_attempt.",
		Parameters:  addAttemptArtifactSchema(),
		Execute: func(_ *sdk.ToolExecContext, input any) (any, error) {
			intent, err := decodeFlatArtifactIntent(toolInputMap(input))
			if err != nil {
				return nil, err
			}
			artifactIntents = append(artifactIntents, intent)
			return map[string]any{"accepted": true, "artifact_count": len(artifactIntents)}, nil
		},
	}
	finishAttemptTool := sdk.Tool{
		Name:        finishAttemptToolName,
		Description: "Finish the orchestration task attempt using flat fields. Call exactly once after any add_attempt_artifact calls.",
		Parameters:  finishAttemptSchema(),
		Execute: func(_ *sdk.ToolExecContext, input any) (any, error) {
			if submittedCompletion != nil {
				return nil, errors.New("finish_attempt may only be called once")
			}
			completionInput, err := decodeFlatFinishAttemptInput(toolInputMap(input))
			if err != nil {
				return nil, err
			}
			if _, err := decodeAttemptCompletionPayload(attempt, execCtx.Task, finishAttemptInputPayload(completionInput, artifactIntents), envResources); err != nil {
				return nil, err
			}
			submittedCompletion = &completionInput
			return map[string]any{"accepted": true}, nil
		},
	}
	cfg.ExtraTools = append(cfg.ExtraTools, newListEnvResourcesTool(envResources), addArtifactTool, finishAttemptTool)
	envSessionID, envLeaseEpoch := sideEffectEnvFence(execCtx.InputManifest)
	cfg.ToolCallObserver = newActionLedgerObserver(r.queries, actionLedgerSubject{
		runID:         execCtx.Run.ID,
		taskID:        execCtx.Task.ID,
		attemptID:     parseUUIDOrZero(attempt.ID),
		claimEpoch:    int64(attempt.ClaimEpoch), //nolint:gosec // claim epochs are persisted as positive BIGINT values.
		envSessionID:  envSessionID,
		envLeaseEpoch: envLeaseEpoch,
	})

	_, err = r.generateWithThinkingTrace(ctx, cfg, func(delta string) {
		r.recordAttemptThinking(ctx, execCtx, parseUUIDOrZero(attempt.ID), "worker", delta)
	}, func(delta string) {
		r.recordAttemptAgentOutput(ctx, execCtx, parseUUIDOrZero(attempt.ID), "worker", delta)
	})
	if err != nil {
		return failAttemptCompletion(completion, "worker_generate_failed", err)
	}
	if submittedCompletion == nil {
		return failAttemptCompletion(completion, "worker_response_invalid", fmt.Errorf("worker did not call %s", finishAttemptToolName))
	}
	parsed, err := decodeAttemptCompletionPayload(attempt, execCtx.Task, finishAttemptInputPayload(*submittedCompletion, artifactIntents), envResources)
	if err != nil {
		return failAttemptCompletion(completion, "worker_response_invalid", err)
	}
	if parsed.CompletionMetadata == nil {
		parsed.CompletionMetadata = map[string]any{}
	}
	parsed.CompletionMetadata["executor"] = LLMWorkerExecutorID
	parsed.CompletionMetadata["model_id"] = model.ModelID
	parsed.CompletionMetadata["provider"] = provider.ClientType
	return parsed
}

func (r *Runtime) ExecuteVerification(ctx context.Context, verification orchestration.TaskVerification) orchestration.VerificationCompletion {
	completion := orchestration.VerificationCompletion{
		VerificationID: verification.ID,
		ClaimToken:     verification.ClaimToken,
		Status:         orchestration.TaskVerificationStatusFailed,
		Verdict:        orchestration.VerificationVerdictRejected,
		Summary:        "orchestration verification failed",
		FailureClass:   "verification_failed",
		TerminalReason: "orchestration verification failed",
	}
	execCtx, err := r.loadVerificationExecutionContext(ctx, verification)
	if err != nil {
		return failVerificationCompletion(completion, "verification_context_load_failed", err)
	}
	cfg, _, _, err := r.buildBotRunConfig(ctx, execCtx.BotID, execCtx.Run.OwnerSubject)
	if err != nil {
		return failVerificationCompletion(completion, "verifier_model_resolution_failed", err)
	}
	if !cfg.SupportsToolCall {
		return failVerificationCompletion(completion, "verifier_model_resolution_failed", errors.New("verifier model must support tool calling"))
	}
	cfg.System = verifierSystemPrompt
	cfg.Messages = []sdk.Message{sdk.UserMessage(buildVerifierPrompt(execCtx))}
	cfg.ResponseFormat = nil
	var submittedCompletion *finishVerificationInput
	cfg.ExtraTools = append(cfg.ExtraTools, sdk.NewTool[finishVerificationInput](
		finishVerificationToolName,
		"Finish the orchestration task verification. This is the only accepted way to report a verification verdict.",
		func(_ *sdk.ToolExecContext, input finishVerificationInput) (any, error) {
			if submittedCompletion != nil {
				return nil, errors.New("finish_verification may only be called once")
			}
			submittedCompletion = &input
			return map[string]any{"accepted": true}, nil
		},
	))
	cfg.ToolCallObserver = newActionLedgerObserver(r.queries, actionLedgerSubject{
		runID:          execCtx.Run.ID,
		taskID:         execCtx.Task.ID,
		verificationID: parseUUIDOrZero(verification.ID),
	})

	_, err = r.generateWithThinkingTrace(ctx, cfg, func(delta string) {
		r.recordVerificationThinking(ctx, execCtx, parseUUIDOrZero(verification.ID), "verifier", delta)
	}, func(delta string) {
		r.recordVerificationAgentOutput(ctx, execCtx, parseUUIDOrZero(verification.ID), "verifier", delta)
	})
	if err != nil {
		return failVerificationCompletion(completion, "verifier_generate_failed", err)
	}
	if submittedCompletion == nil {
		return failVerificationCompletion(completion, "verifier_response_invalid", fmt.Errorf("verifier did not call %s", finishVerificationToolName))
	}
	parsed, err := decodeVerificationCompletionPayload(verification, execCtx.Task, execCtx.Result, finishVerificationInputPayload(*submittedCompletion))
	if err != nil {
		return failVerificationCompletion(completion, "verifier_response_invalid", err)
	}
	return parsed
}

type attemptExecutionContext struct {
	BotID         string
	Run           sqlc.OrchestrationRun
	Task          sqlc.OrchestrationTask
	Attempt       orchestration.TaskAttempt
	TaskInputs    map[string]any
	InputManifest map[string]any
	Predecessors  []map[string]any
}

func (r *Runtime) loadAttemptExecutionContext(ctx context.Context, attempt orchestration.TaskAttempt) (attemptExecutionContext, error) {
	attemptTaskID, err := db.ParseUUID(attempt.TaskID)
	if err != nil {
		return attemptExecutionContext{}, fmt.Errorf("invalid task id: %w", err)
	}
	runID, err := db.ParseUUID(attempt.RunID)
	if err != nil {
		return attemptExecutionContext{}, fmt.Errorf("invalid run id: %w", err)
	}
	taskRow, err := r.queries.GetOrchestrationTaskByID(ctx, attemptTaskID)
	if err != nil {
		return attemptExecutionContext{}, fmt.Errorf("load task: %w", err)
	}
	runRow, err := r.queries.GetOrchestrationRunByID(ctx, runID)
	if err != nil {
		return attemptExecutionContext{}, fmt.Errorf("load run: %w", err)
	}
	sourceMetadata := decodeJSONObject(runRow.SourceMetadata)
	botID := strings.TrimSpace(stringValue(sourceMetadata["bot_id"]))
	if botID == "" {
		return attemptExecutionContext{}, errors.New("run source metadata is missing bot_id")
	}

	inputManifest := map[string]any{}
	if attempt.InputManifestID != "" {
		manifestID, manifestErr := db.ParseUUID(attempt.InputManifestID)
		if manifestErr == nil {
			if manifestRow, getErr := r.queries.GetOrchestrationInputManifestByID(ctx, manifestID); getErr == nil {
				inputManifest = map[string]any{
					"id":                            manifestRow.ID.String(),
					"captured_task_inputs":          decodeJSONObject(manifestRow.CapturedTaskInputs),
					"captured_artifact_versions":    decodeJSONArrayObjects(manifestRow.CapturedArtifactVersions),
					"captured_blackboard_revisions": decodeJSONArrayObjects(manifestRow.CapturedBlackboardRevisions),
					"captured_env_preconditions":    decodeJSONObject(manifestRow.CapturedEnvPreconditions),
					"projection_hash":               strings.TrimSpace(manifestRow.ProjectionHash),
				}
			}
		}
	}

	predecessors, err := r.loadPredecessorContexts(ctx, taskRow)
	if err != nil {
		return attemptExecutionContext{}, err
	}
	return attemptExecutionContext{
		BotID:         botID,
		Run:           runRow,
		Task:          taskRow,
		Attempt:       attempt,
		TaskInputs:    decodeJSONObject(taskRow.Inputs),
		InputManifest: inputManifest,
		Predecessors:  predecessors,
	}, nil
}

type verificationExecutionContext struct {
	BotID              string
	Run                sqlc.OrchestrationRun
	Task               sqlc.OrchestrationTask
	Result             sqlc.OrchestrationTaskResult
	Verification       orchestration.TaskVerification
	VerificationPolicy map[string]any
	ResultArtifacts    []map[string]any
	EnvDrift           map[string]any
}

func (r *Runtime) loadVerificationExecutionContext(ctx context.Context, verification orchestration.TaskVerification) (verificationExecutionContext, error) {
	taskID, err := db.ParseUUID(verification.TaskID)
	if err != nil {
		return verificationExecutionContext{}, fmt.Errorf("invalid task id: %w", err)
	}
	runID, err := db.ParseUUID(verification.RunID)
	if err != nil {
		return verificationExecutionContext{}, fmt.Errorf("invalid run id: %w", err)
	}
	resultID, err := db.ParseUUID(verification.ResultID)
	if err != nil {
		return verificationExecutionContext{}, fmt.Errorf("invalid result id: %w", err)
	}
	taskRow, err := r.queries.GetOrchestrationTaskByID(ctx, taskID)
	if err != nil {
		return verificationExecutionContext{}, fmt.Errorf("load task: %w", err)
	}
	runRow, err := r.queries.GetOrchestrationRunByID(ctx, runID)
	if err != nil {
		return verificationExecutionContext{}, fmt.Errorf("load run: %w", err)
	}
	resultRow, err := r.queries.GetOrchestrationTaskResultByID(ctx, resultID)
	if err != nil {
		return verificationExecutionContext{}, fmt.Errorf("load task result: %w", err)
	}
	sourceMetadata := decodeJSONObject(runRow.SourceMetadata)
	botID := strings.TrimSpace(stringValue(sourceMetadata["bot_id"]))
	if botID == "" {
		return verificationExecutionContext{}, errors.New("run source metadata is missing bot_id")
	}
	artifacts, err := r.queries.ListOrchestrationArtifactsByTask(ctx, taskRow.ID)
	if err != nil {
		return verificationExecutionContext{}, fmt.Errorf("load task artifacts: %w", err)
	}
	envDrift, err := r.loadEnvDriftContext(ctx, resultRow.AttemptID)
	if err != nil {
		return verificationExecutionContext{}, err
	}
	return verificationExecutionContext{
		BotID:              botID,
		Run:                runRow,
		Task:               taskRow,
		Result:             resultRow,
		Verification:       verification,
		VerificationPolicy: decodeJSONObject(taskRow.VerificationPolicy),
		ResultArtifacts:    encodeArtifactsForAttempt(artifacts, resultRow.AttemptID),
		EnvDrift:           envDrift,
	}, nil
}

func (r *Runtime) loadEnvDriftContext(ctx context.Context, attemptID pgtype.UUID) (map[string]any, error) {
	if !attemptID.Valid {
		return map[string]any{"status": "not_applicable"}, nil
	}
	rows, err := r.queries.ListOrchestrationEnvSnapshotsByAttempt(ctx, attemptID)
	if err != nil {
		return nil, fmt.Errorf("load env snapshots for drift context: %w", err)
	}
	return buildEnvDriftContext(rows), nil
}

func buildEnvDriftContext(rows []sqlc.OrchestrationEnvSnapshot) map[string]any {
	if len(rows) == 0 {
		return map[string]any{"status": "not_applicable", "snapshots": []map[string]any{}}
	}
	snapshots := make([]map[string]any, 0, len(rows))
	var firstPre, lastPost *sqlc.OrchestrationEnvSnapshot
	periodicCount := 0
	for i := range rows {
		row := rows[i]
		snapshot := map[string]any{
			"snapshot_id":  row.ID.String(),
			"session_id":   row.SessionID.String(),
			"kind":         strings.TrimSpace(row.Kind),
			"effect_class": strings.TrimSpace(row.EffectClass),
			"digest":       strings.TrimSpace(row.Digest),
			"size_bytes":   row.SizeBytes,
			"runtime_ref":  decodeJSONObject(row.RuntimeRef),
			"metadata":     decodeJSONObject(row.Metadata),
			"created_at":   row.CreatedAt.Time,
		}
		snapshots = append(snapshots, snapshot)
		switch strings.TrimSpace(row.Kind) {
		case "pre_action":
			if firstPre == nil {
				firstPre = &row
			}
		case "post_action":
			lastPost = &row
		case "periodic":
			periodicCount++
		}
	}
	status := "unknown"
	beforeDigest := ""
	afterDigest := ""
	if firstPre != nil && lastPost != nil {
		beforeDigest = strings.TrimSpace(firstPre.Digest)
		afterDigest = strings.TrimSpace(lastPost.Digest)
		switch {
		case beforeDigest == "" || afterDigest == "":
			status = "unknown"
		case beforeDigest == afterDigest:
			status = "unchanged"
		default:
			status = "changed"
		}
	}
	return map[string]any{
		"status":         status,
		"changed":        status == "changed",
		"before_digest":  beforeDigest,
		"after_digest":   afterDigest,
		"periodic_count": periodicCount,
		"snapshots":      snapshots,
	}
}

func (r *Runtime) loadPredecessorContexts(ctx context.Context, taskRow sqlc.OrchestrationTask) ([]map[string]any, error) {
	dependencies, err := r.queries.ListActiveOrchestrationTaskDependenciesBySuccessor(ctx, taskRow.ID)
	if err != nil {
		return nil, fmt.Errorf("load predecessor dependencies: %w", err)
	}
	if len(dependencies) == 0 {
		return nil, nil
	}
	tasksByRun, err := r.queries.ListCurrentOrchestrationTasksByRun(ctx, taskRow.RunID)
	if err != nil {
		return nil, fmt.Errorf("load run tasks: %w", err)
	}
	resultsByRun, err := r.queries.ListCurrentOrchestrationTaskResultsByRun(ctx, taskRow.RunID)
	if err != nil {
		return nil, fmt.Errorf("load run results: %w", err)
	}
	artifactsByRun, err := r.queries.ListCurrentOrchestrationArtifactsByRun(ctx, taskRow.RunID)
	if err != nil {
		return nil, fmt.Errorf("load run artifacts: %w", err)
	}

	tasksByID := make(map[string]sqlc.OrchestrationTask, len(tasksByRun))
	for _, candidate := range tasksByRun {
		tasksByID[candidate.ID.String()] = candidate
	}
	resultsByTaskID := make(map[string]sqlc.OrchestrationTaskResult, len(resultsByRun))
	for _, candidate := range resultsByRun {
		resultsByTaskID[candidate.TaskID.String()] = candidate
	}
	artifactsByTaskID := make(map[string][]sqlc.OrchestrationArtifact)
	for _, artifact := range artifactsByRun {
		key := artifact.TaskID.String()
		artifactsByTaskID[key] = append(artifactsByTaskID[key], artifact)
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

func (r *Runtime) buildBotRunConfig(ctx context.Context, botID, ownerSubject string) (agentpkg.RunConfig, models.GetResponse, sqlc.Provider, error) {
	if r.agent == nil {
		return agentpkg.RunConfig{}, models.GetResponse{}, sqlc.Provider{}, errors.New("agent is not configured")
	}
	if r.modelsService == nil {
		return agentpkg.RunConfig{}, models.GetResponse{}, sqlc.Provider{}, errors.New("models service is not configured")
	}
	if r.settingsService == nil {
		return agentpkg.RunConfig{}, models.GetResponse{}, sqlc.Provider{}, errors.New("settings service is not configured")
	}
	botSettings, err := r.settingsService.GetBot(ctx, botID)
	if err != nil {
		return agentpkg.RunConfig{}, models.GetResponse{}, sqlc.Provider{}, fmt.Errorf("load bot settings: %w", err)
	}
	modelRef := strings.TrimSpace(botSettings.ChatModelID)
	if modelRef == "" {
		return agentpkg.RunConfig{}, models.GetResponse{}, sqlc.Provider{}, errors.New("bot chat model is not configured")
	}
	model, provider, err := r.resolveChatModel(ctx, modelRef)
	if err != nil {
		return agentpkg.RunConfig{}, models.GetResponse{}, sqlc.Provider{}, err
	}
	reasoningEffort := ""
	if model.HasCompatibility(models.CompatReasoning) && botSettings.ReasoningEnabled {
		reasoningEffort = strings.TrimSpace(botSettings.ReasoningEffort)
	}
	var reasoningConfig *models.ReasoningConfig
	if reasoningEffort != "" {
		reasoningConfig = &models.ReasoningConfig{Enabled: true, Effort: reasoningEffort}
	}

	credentialsResolver := providers.NewService(nil, postgresstore.NewQueries(r.queries), "")
	authCtx := oauthctx.WithUserID(ctx, ownerSubject)
	creds, err := credentialsResolver.ResolveModelCredentials(authCtx, provider)
	if err != nil {
		return agentpkg.RunConfig{}, models.GetResponse{}, sqlc.Provider{}, fmt.Errorf("resolve model credentials: %w", err)
	}
	timezoneName, timezoneLocation := r.resolveBotTimezone(ctx, botID)
	return agentpkg.RunConfig{
		Model: models.NewSDKChatModel(models.SDKModelConfig{
			ModelID:         model.ModelID,
			ClientType:      provider.ClientType,
			APIKey:          creds.APIKey,
			CodexAccountID:  creds.CodexAccountID,
			BaseURL:         providers.ProviderConfigString(provider, "base_url"),
			HTTPClient:      r.httpClient,
			ReasoningConfig: reasoningConfig,
		}),
		ReasoningEffort:    reasoningEffort,
		SessionType:        "orchestration",
		SupportsToolCall:   model.HasCompatibility(models.CompatToolCall),
		SupportsImageInput: model.HasCompatibility(models.CompatVision),
		Identity: agentpkg.SessionContext{
			BotID:            botID,
			ChatID:           botID,
			SessionID:        botID,
			Timezone:         timezoneName,
			TimezoneLocation: timezoneLocation,
		},
		LoopDetection: agentpkg.LoopDetectionConfig{Enabled: false},
	}, model, provider, nil
}

func (r *Runtime) resolveChatModel(ctx context.Context, modelRef string) (models.GetResponse, sqlc.Provider, error) {
	var model models.GetResponse
	var err error
	if _, parseErr := db.ParseUUID(modelRef); parseErr == nil {
		model, err = r.modelsService.GetByID(ctx, modelRef)
		if err == nil {
			goto resolved
		}
	}
	model, err = r.modelsService.GetByModelID(ctx, modelRef)
	if err != nil {
		return models.GetResponse{}, sqlc.Provider{}, fmt.Errorf("resolve chat model %q: %w", modelRef, err)
	}

resolved:
	if model.Type != models.ModelTypeChat {
		return models.GetResponse{}, sqlc.Provider{}, errors.New("configured bot chat model is not a chat model")
	}
	provider, err := models.FetchProviderByID(ctx, postgresstore.NewQueries(r.queries), model.ProviderID)
	if err != nil {
		return models.GetResponse{}, sqlc.Provider{}, fmt.Errorf("load provider: %w", err)
	}
	return model, provider, nil
}

func (r *Runtime) resolveBotTimezone(ctx context.Context, botID string) (string, *time.Location) {
	if strings.TrimSpace(botID) != "" && r.queries != nil {
		if botUUID, err := db.ParseUUID(botID); err == nil {
			if row, getErr := r.queries.GetBotByID(ctx, botUUID); getErr == nil && row.Timezone.Valid {
				if loc, name, resolveErr := tzutil.Resolve(strings.TrimSpace(row.Timezone.String)); resolveErr == nil {
					return name, loc
				}
			}
		}
	}
	if r.clockLocation != nil {
		return r.clockLocation.String(), r.clockLocation
	}
	return tzutil.DefaultName, tzutil.MustResolve(tzutil.DefaultName)
}

const workerSystemPrompt = `You are the execution runtime for a single orchestration task.

Complete only the current task. Use tools when needed, especially container file and command tools. Prefer real execution over guessing.

If env_preconditions.kind is "browser", use browser_action, browser_observe, or browser_remote_session tools for browser automation. Do not install Playwright or launch a local browser in the workspace unless the browser tools are unavailable and the task explicitly asks for local browser setup.

For human-readable fields such as summary, terminal_reason, artifact summaries, and child task goals, use the same language as the run goal / task goal / user request. Preserve Chinese when the request is Chinese.

If the attempt produces artifacts, call add_attempt_artifact once per artifact before finishing. Use flat fields: kind, uri, version, digest, content_type, summary, metadata_json.

Finish by calling the finish_attempt tool exactly once. Use only flat fields: status, summary, failure_class, terminal_reason, request_replan, structured_output_json.

When the task needs a different decomposition or recovery path, set request_replan=true and explain the blocker in summary or structured_output_json. Do not create replacement tasks from the worker; replanning is handled by the replanner.
Use status="completed" for a replan-only handoff. Use status="failed" with request_replan=true only when this attempt genuinely failed and the replanner should replace the failed subtree.

If the task succeeds, use status="completed". If it cannot be completed, use status="failed" with clear failure_class and terminal_reason.`

const verifierSystemPrompt = `You are the verification runtime for a single orchestration task result.

Inspect the task goal, verification policy, produced structured output, and artifacts. Use tools only when necessary to validate the result.

If env_drift.status is changed, decide whether that mutation was expected by the task and verification_policy. Reject or request replan when the environment changed outside the task's allowed effect.

For human-readable fields such as summary and terminal_reason, use the same language as the task goal / produced result / user request. Preserve Chinese when the request is Chinese.

Finish by calling the finish_verification tool exactly once. Use status, verdict, summary, failure_class, terminal_reason, and request_replan fields directly in that tool call.

Use status="completed", verdict="accepted" when the result is valid.
Use verdict="rejected" when validation fails.
When verification_policy.on_reject is "retry", use status="failed", verdict="rejected", failure_class="verifier_retryable", and request_replan=false for retryable invalid results.
Set request_replan=true when the result is rejected and the task should be replaced by a replanned subtree instead of retried or failed. Do not create replacement tasks from the verifier; replanning is handled by the replanner.`

func buildWorkerPrompt(execCtx attemptExecutionContext) string {
	return buildOrchestrationUserPrompt("task_attempt",
		orchestrationPromptSection{
			name: "run",
			value: map[string]any{
				"run_id":           execCtx.Run.ID.String(),
				"tenant_id":        strings.TrimSpace(execCtx.Run.TenantID),
				"owner_subject":    strings.TrimSpace(execCtx.Run.OwnerSubject),
				"goal":             strings.TrimSpace(execCtx.Run.Goal),
				"planner_epoch":    execCtx.Run.PlannerEpoch,
				"lifecycle_status": strings.TrimSpace(execCtx.Run.LifecycleStatus),
				"source_metadata":  decodeJSONObject(execCtx.Run.SourceMetadata),
				"requested_output": decodeJSONObject(execCtx.Run.OutputSchema),
				"run_input":        decodeJSONObject(execCtx.Run.Input),
				"run_policies":     decodeJSONObject(execCtx.Run.Policies),
				"control_policy":   decodeJSONObject(execCtx.Run.ControlPolicy),
			},
		},
		orchestrationPromptSection{
			name: "task",
			value: map[string]any{
				"task_id":             execCtx.Task.ID.String(),
				"goal":                strings.TrimSpace(execCtx.Task.Goal),
				"kind":                strings.TrimSpace(execCtx.Task.Kind),
				"worker_profile":      strings.TrimSpace(execCtx.Task.WorkerProfile),
				"priority":            execCtx.Task.Priority,
				"inputs":              execCtx.TaskInputs,
				"retry_policy":        decodeJSONObject(execCtx.Task.RetryPolicy),
				"verification_policy": decodeJSONObject(execCtx.Task.VerificationPolicy),
				"env_preconditions":   decodeJSONObject(execCtx.Task.EnvPreconditions),
				"blackboard_scope":    strings.TrimSpace(execCtx.Task.BlackboardScope),
				"planner_epoch":       execCtx.Task.PlannerEpoch,
			},
		},
		orchestrationPromptSection{
			name: "attempt",
			value: map[string]any{
				"attempt_id": execCtx.Attempt.ID,
				"attempt_no": execCtx.Attempt.AttemptNo,
			},
		},
		orchestrationPromptSection{name: "input_manifest", value: execCtx.InputManifest},
		orchestrationPromptSection{name: "predecessor_results", value: execCtx.Predecessors},
	)
}

func buildVerifierPrompt(execCtx verificationExecutionContext) string {
	return buildOrchestrationUserPrompt("task_verification",
		orchestrationPromptSection{
			name: "run",
			value: map[string]any{
				"run_id":           execCtx.Run.ID.String(),
				"tenant_id":        strings.TrimSpace(execCtx.Run.TenantID),
				"owner_subject":    strings.TrimSpace(execCtx.Run.OwnerSubject),
				"goal":             strings.TrimSpace(execCtx.Run.Goal),
				"planner_epoch":    execCtx.Run.PlannerEpoch,
				"lifecycle_status": strings.TrimSpace(execCtx.Run.LifecycleStatus),
			},
		},
		orchestrationPromptSection{
			name: "task",
			value: map[string]any{
				"task_id":             execCtx.Task.ID.String(),
				"goal":                strings.TrimSpace(execCtx.Task.Goal),
				"worker_profile":      strings.TrimSpace(execCtx.Task.WorkerProfile),
				"verification_policy": execCtx.VerificationPolicy,
			},
		},
		orchestrationPromptSection{
			name: "result",
			value: map[string]any{
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
		},
		orchestrationPromptSection{
			name: "verification",
			value: map[string]any{
				"verification_id":  execCtx.Verification.ID,
				"attempt_no":       execCtx.Verification.AttemptNo,
				"verifier_profile": strings.TrimSpace(execCtx.Verification.VerifierProfile),
			},
		},
		orchestrationPromptSection{name: "env_drift", value: execCtx.EnvDrift},
	)
}

func decodeAttemptCompletionPayload(attempt orchestration.TaskAttempt, taskRow sqlc.OrchestrationTask, payload map[string]any, envResources ...envResourceCatalog) (orchestration.AttemptCompletion, error) {
	status := normalizeAttemptStatus(payload["status"])
	if status == "" {
		return orchestration.AttemptCompletion{}, errors.New("worker response is missing a valid status")
	}
	_ = envResources
	structuredOutput := normalizeObject(mapValue(payload["structured_output"]))
	delete(structuredOutput, "child_tasks")
	summary := strings.TrimSpace(stringValue(payload["summary"]))
	if summary == "" {
		summary = strings.TrimSpace(taskRow.Goal)
	}
	terminalReason := strings.TrimSpace(stringValue(payload["terminal_reason"]))
	if status == orchestration.TaskAttemptStatusFailed && terminalReason == "" {
		terminalReason = summary
	}
	return orchestration.AttemptCompletion{
		AttemptID:        attempt.ID,
		ClaimToken:       attempt.ClaimToken,
		Status:           status,
		Summary:          summary,
		StructuredOutput: structuredOutput,
		FailureClass:     strings.TrimSpace(stringValue(payload["failure_class"])),
		TerminalReason:   terminalReason,
		RequestReplan:    boolValue(payload["request_replan"]),
		ArtifactIntents:  decodeAttemptArtifactIntentsFromAny(payload["artifact_intents"]),
	}, nil
}

func finishAttemptSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"status":                 map[string]any{"type": "string", "enum": []string{orchestration.TaskAttemptStatusCompleted, orchestration.TaskAttemptStatusFailed}},
			"summary":                map[string]any{"type": "string"},
			"failure_class":          map[string]any{"type": "string"},
			"terminal_reason":        map[string]any{"type": "string"},
			"request_replan":         map[string]any{"type": "boolean"},
			"structured_output_json": map[string]any{"type": "string", "description": "Optional JSON object encoded as a string. Do not include child_tasks."},
		},
		"required":             []string{"status", "summary"},
		"additionalProperties": false,
	}
}

func addAttemptArtifactSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"kind":          map[string]any{"type": "string", "description": "Artifact kind, such as screenshot, file, report, log, or image."},
			"uri":           map[string]any{"type": "string", "description": "Artifact URI or path."},
			"version":       map[string]any{"type": "string"},
			"digest":        map[string]any{"type": "string"},
			"content_type":  map[string]any{"type": "string", "description": "MIME type when known."},
			"summary":       map[string]any{"type": "string"},
			"metadata_json": map[string]any{"type": "string", "description": "Optional metadata JSON object encoded as a string."},
		},
		"required":             []string{"kind", "uri", "summary"},
		"additionalProperties": false,
	}
}

func decodeFlatFinishAttemptInput(input map[string]any) (finishAttemptInput, error) {
	if err := rejectUnknownToolKeys(input, map[string]struct{}{
		"status":                 {},
		"summary":                {},
		"failure_class":          {},
		"terminal_reason":        {},
		"request_replan":         {},
		"structured_output_json": {},
	}); err != nil {
		return finishAttemptInput{}, err
	}
	out := finishAttemptInput{
		Status:               toolString(input, "status"),
		Summary:              toolString(input, "summary"),
		FailureClass:         toolString(input, "failure_class"),
		TerminalReason:       toolString(input, "terminal_reason"),
		RequestReplan:        toolBool(input, "request_replan"),
		StructuredOutputJSON: toolString(input, "structured_output_json"),
	}
	if out.Status == "" {
		return finishAttemptInput{}, errors.New("status is required")
	}
	if out.Summary == "" {
		return finishAttemptInput{}, errors.New("summary is required")
	}
	if _, err := decodeOptionalJSONObjectTextStrict(out.StructuredOutputJSON); err != nil {
		return finishAttemptInput{}, fmt.Errorf("structured_output_json: %w", err)
	}
	return out, nil
}

func decodeFlatArtifactIntent(input map[string]any) (orchestration.AttemptArtifactIntent, error) {
	if err := rejectUnknownToolKeys(input, map[string]struct{}{
		"kind":          {},
		"uri":           {},
		"version":       {},
		"digest":        {},
		"content_type":  {},
		"summary":       {},
		"metadata_json": {},
	}); err != nil {
		return orchestration.AttemptArtifactIntent{}, err
	}
	metadata, err := decodeOptionalJSONObjectTextStrict(toolString(input, "metadata_json"))
	if err != nil {
		return orchestration.AttemptArtifactIntent{}, fmt.Errorf("metadata_json: %w", err)
	}
	intent := orchestration.AttemptArtifactIntent{
		Kind:        toolString(input, "kind"),
		URI:         toolString(input, "uri"),
		Version:     toolString(input, "version"),
		Digest:      toolString(input, "digest"),
		ContentType: toolString(input, "content_type"),
		Summary:     toolString(input, "summary"),
		Metadata:    metadata,
	}
	if intent.Kind == "" {
		return orchestration.AttemptArtifactIntent{}, errors.New("kind is required")
	}
	if intent.URI == "" {
		return orchestration.AttemptArtifactIntent{}, errors.New("uri is required")
	}
	if intent.Summary == "" {
		return orchestration.AttemptArtifactIntent{}, errors.New("summary is required")
	}
	return intent, nil
}

func finishAttemptInputPayload(input finishAttemptInput, artifactIntents []orchestration.AttemptArtifactIntent) map[string]any {
	return map[string]any{
		"status":            input.Status,
		"summary":           input.Summary,
		"failure_class":     input.FailureClass,
		"terminal_reason":   input.TerminalReason,
		"request_replan":    input.RequestReplan,
		"artifact_intents":  artifactIntentsToAnySlice(artifactIntents),
		"structured_output": decodeOptionalJSONObjectText(input.StructuredOutputJSON),
	}
}

func artifactIntentsToAnySlice(items []orchestration.AttemptArtifactIntent) []any {
	if len(items) == 0 {
		return nil
	}
	out := make([]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{
			"kind":         item.Kind,
			"uri":          item.URI,
			"version":      item.Version,
			"digest":       item.Digest,
			"content_type": item.ContentType,
			"summary":      item.Summary,
			"metadata":     item.Metadata,
		})
	}
	return out
}

func toolInputMap(input any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	if value, ok := input.(map[string]any); ok {
		return value
	}
	data, err := json.Marshal(input)
	if err != nil {
		return map[string]any{}
	}
	var value map[string]any
	_ = json.Unmarshal(data, &value)
	if value == nil {
		value = map[string]any{}
	}
	return value
}

func rejectUnknownToolKeys(input map[string]any, allowed map[string]struct{}) error {
	for key := range input {
		if _, ok := allowed[key]; !ok {
			return fmt.Errorf("unknown field %q", key)
		}
	}
	return nil
}

func toolString(input map[string]any, key string) string {
	raw, ok := input[key]
	if !ok || raw == nil {
		return ""
	}
	if value, ok := raw.(string); ok {
		return strings.TrimSpace(value)
	}
	return strings.TrimSpace(fmt.Sprintf("%v", raw))
}

func toolBool(input map[string]any, key string) bool {
	raw, ok := input[key]
	if !ok || raw == nil {
		return false
	}
	value, ok := raw.(bool)
	return ok && value
}

func decodeOptionalJSONObjectText(raw string) map[string]any {
	value, err := decodeOptionalJSONObjectTextStrict(raw)
	if err != nil {
		return map[string]any{}
	}
	return value
}

func decodeOptionalJSONObjectTextStrict(raw string) (map[string]any, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string]any{}, nil
	}
	value, err := decodeJSONObjectText(raw)
	if err != nil {
		return nil, err
	}
	delete(value, "child_tasks")
	return value, nil
}

func decodeVerificationCompletionPayload(
	verification orchestration.TaskVerification,
	taskRow sqlc.OrchestrationTask,
	resultRow sqlc.OrchestrationTaskResult,
	payload map[string]any,
) (orchestration.VerificationCompletion, error) {
	status := normalizeVerificationStatus(payload["status"])
	if status == "" {
		return orchestration.VerificationCompletion{}, errors.New("verifier response is missing a valid status")
	}
	verdict := normalizeVerificationVerdict(payload["verdict"])
	if verdict == "" {
		return orchestration.VerificationCompletion{}, errors.New("verifier response is missing a valid verdict")
	}
	summary := strings.TrimSpace(stringValue(payload["summary"]))
	if summary == "" {
		summary = strings.TrimSpace(resultRow.Summary)
	}
	if summary == "" {
		summary = strings.TrimSpace(taskRow.Goal)
	}
	terminalReason := strings.TrimSpace(stringValue(payload["terminal_reason"]))
	if verdict == orchestration.VerificationVerdictRejected && terminalReason == "" {
		terminalReason = summary
	}
	return orchestration.VerificationCompletion{
		VerificationID: verification.ID,
		ClaimToken:     verification.ClaimToken,
		Status:         status,
		Verdict:        verdict,
		Summary:        summary,
		FailureClass:   strings.TrimSpace(stringValue(payload["failure_class"])),
		TerminalReason: terminalReason,
		RequestReplan:  boolValue(payload["request_replan"]),
	}, nil
}

func finishVerificationInputPayload(input finishVerificationInput) map[string]any {
	return map[string]any{
		"status":          input.Status,
		"verdict":         input.Verdict,
		"summary":         input.Summary,
		"failure_class":   input.FailureClass,
		"terminal_reason": input.TerminalReason,
		"request_replan":  input.RequestReplan,
	}
}

func decodeAttemptArtifactIntentsFromAny(raw any) []orchestration.AttemptArtifactIntent {
	items, ok := raw.([]any)
	if !ok || len(items) == 0 {
		return nil
	}
	intents := make([]orchestration.AttemptArtifactIntent, 0, len(items))
	for _, item := range items {
		payload := normalizeObject(mapValue(item))
		if len(payload) == 0 {
			continue
		}
		intents = append(intents, orchestration.AttemptArtifactIntent{
			Kind:        strings.TrimSpace(stringValue(payload["kind"])),
			URI:         strings.TrimSpace(stringValue(payload["uri"])),
			Version:     strings.TrimSpace(stringValue(payload["version"])),
			Digest:      strings.TrimSpace(stringValue(payload["digest"])),
			ContentType: strings.TrimSpace(stringValue(payload["content_type"])),
			Summary:     strings.TrimSpace(stringValue(payload["summary"])),
			Metadata:    normalizeObject(mapValue(payload["metadata"])),
		})
	}
	if len(intents) == 0 {
		return nil
	}
	return intents
}

func encodeArtifactsForAttempt(artifacts []sqlc.OrchestrationArtifact, attemptID pgtype.UUID) []map[string]any {
	filtered := make([]map[string]any, 0, len(artifacts))
	for _, artifact := range artifacts {
		if !attemptID.Valid {
			continue
		}
		if !artifact.AttemptID.Valid || artifact.AttemptID.String() != attemptID.String() {
			continue
		}
		createdAt := ""
		if artifact.CreatedAt.Valid {
			createdAt = artifact.CreatedAt.Time.UTC().Format(time.RFC3339Nano)
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
			"created_at":   createdAt,
		})
	}
	return filtered
}

func decodeJSONObjectText(raw string) (map[string]any, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, errors.New("empty model response")
	}
	if strings.HasPrefix(trimmed, "```") {
		trimmed = strings.TrimPrefix(trimmed, "```json")
		trimmed = strings.TrimPrefix(trimmed, "```JSON")
		trimmed = strings.TrimPrefix(trimmed, "```")
		trimmed = strings.TrimSuffix(trimmed, "```")
		trimmed = strings.TrimSpace(trimmed)
	}
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start >= 0 && end >= start {
		trimmed = trimmed[start : end+1]
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return nil, fmt.Errorf("decode json response: %w", err)
	}
	return normalizeObject(payload), nil
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

func mustJSON(value any) string {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(data)
}

func failAttemptCompletion(base orchestration.AttemptCompletion, failureClass string, err error) orchestration.AttemptCompletion {
	base.Status = orchestration.TaskAttemptStatusFailed
	base.FailureClass = strings.TrimSpace(failureClass)
	base.TerminalReason = err.Error()
	base.Summary = err.Error()
	if base.StructuredOutput == nil {
		base.StructuredOutput = map[string]any{}
	}
	return base
}

func failVerificationCompletion(base orchestration.VerificationCompletion, failureClass string, err error) orchestration.VerificationCompletion {
	base.Status = orchestration.TaskVerificationStatusFailed
	base.Verdict = orchestration.VerificationVerdictRejected
	base.FailureClass = strings.TrimSpace(failureClass)
	base.TerminalReason = err.Error()
	base.Summary = err.Error()
	return base
}

func normalizeAttemptStatus(raw any) string {
	switch strings.TrimSpace(stringValue(raw)) {
	case "", orchestration.TaskAttemptStatusCompleted:
		return orchestration.TaskAttemptStatusCompleted
	case orchestration.TaskAttemptStatusFailed:
		return orchestration.TaskAttemptStatusFailed
	default:
		return ""
	}
}

func normalizeVerificationStatus(raw any) string {
	switch strings.TrimSpace(stringValue(raw)) {
	case "", orchestration.TaskVerificationStatusCompleted:
		return orchestration.TaskVerificationStatusCompleted
	case orchestration.TaskVerificationStatusFailed:
		return orchestration.TaskVerificationStatusFailed
	default:
		return ""
	}
}

func normalizeVerificationVerdict(raw any) string {
	switch strings.TrimSpace(stringValue(raw)) {
	case orchestration.VerificationVerdictAccepted:
		return orchestration.VerificationVerdictAccepted
	case orchestration.VerificationVerdictRejected:
		return orchestration.VerificationVerdictRejected
	default:
		return ""
	}
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

func mapValue(raw any) map[string]any {
	value, _ := raw.(map[string]any)
	if value == nil {
		return map[string]any{}
	}
	return value
}

func boolValue(raw any) bool {
	value, _ := raw.(bool)
	return value
}

func int64Value(raw any) int64 {
	switch value := raw.(type) {
	case int64:
		return value
	case int:
		return int64(value)
	case float64:
		return int64(value)
	case json.Number:
		parsed, _ := value.Int64()
		return parsed
	default:
		return 0
	}
}

func stringValue(raw any) string {
	value, _ := raw.(string)
	return value
}
