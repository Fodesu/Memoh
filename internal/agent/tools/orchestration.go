package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	sdk "github.com/memohai/twilight-ai/sdk"

	"github.com/memohai/memoh/internal/bots"
	"github.com/memohai/memoh/internal/orchestration"
	sessionpkg "github.com/memohai/memoh/internal/session"
)

type orchestrationService interface {
	StartRun(context.Context, orchestration.ControlIdentity, orchestration.StartRunRequest) (orchestration.RunHandle, error)
	CancelRun(context.Context, orchestration.ControlIdentity, string, orchestration.CancelRunRequest) (*orchestration.CancelRunResult, error)
	ResumeRun(context.Context, orchestration.ControlIdentity, string, orchestration.ResumeRunRequest) (*orchestration.ResumeRunResult, error)
	GetRunSnapshot(context.Context, orchestration.ControlIdentity, string) (*orchestration.RunSnapshot, error)
	ListRunTasks(context.Context, orchestration.ControlIdentity, string, orchestration.ListRunTasksRequest) (*orchestration.TaskPage, error)
	ListRunCheckpoints(context.Context, orchestration.ControlIdentity, string, orchestration.ListRunCheckpointsRequest) (*orchestration.HumanCheckpointPage, error)
	ListRunResults(context.Context, orchestration.ControlIdentity, string, orchestration.ListRunResultsRequest) (*orchestration.TaskResultPage, error)
	ListRunArtifacts(context.Context, orchestration.ControlIdentity, string, orchestration.ListRunArtifactsRequest) (*orchestration.ArtifactPage, error)
	ResolveCheckpoint(context.Context, orchestration.ControlIdentity, string, orchestration.CheckpointResolution) (*orchestration.ResolveCheckpointResult, error)
}

type orchestrationBotReader interface {
	Get(context.Context, string) (bots.Bot, error)
}

type OrchestrationProvider struct {
	service orchestrationService
	bots    orchestrationBotReader
	logger  *slog.Logger
}

type orchestrationStatusResult struct {
	StatusMessage   string                           `json:"status_message"`
	RunID           string                           `json:"run_id"`
	Goal            string                           `json:"goal"`
	Status          string                           `json:"status"`
	TerminalReason  string                           `json:"terminal_reason,omitempty"`
	Verdict         orchestrationStatusVerdict       `json:"verdict"`
	FinalResult     *orchestrationFinalResult        `json:"final_result,omitempty"`
	Summary         orchestrationStatusSummary       `json:"summary"`
	OpenCheckpoints []orchestrationCheckpointSummary `json:"open_checkpoints"`
	ActiveTasks     []orchestrationTaskSummary       `json:"active_tasks"`
	FailedTasks     []orchestrationTaskSummary       `json:"failed_tasks"`
	UsableResults   []orchestrationResultSummary     `json:"usable_results"`
	Artifacts       []orchestrationArtifactSummary   `json:"artifacts"`
}

type orchestrationStatusVerdict struct {
	IsTerminal bool   `json:"is_terminal"`
	NeedsHuman bool   `json:"needs_human"`
	NextAction string `json:"next_action"`
	ReasonCode string `json:"reason_code"`
	ReasonText string `json:"reason_text"`
}

type orchestrationStatusSummary struct {
	Active       int `json:"active"`
	WaitingHuman int `json:"waiting_human"`
	Blocked      int `json:"blocked"`
	Failed       int `json:"failed"`
	Completed    int `json:"completed"`
	Cancelled    int `json:"cancelled"`
}

type orchestrationFinalResult struct {
	TaskID      string   `json:"task_id"`
	ResultID    string   `json:"result_id"`
	Snippet     string   `json:"snippet,omitempty"`
	ArtifactIDs []string `json:"artifact_ids"`
}

type orchestrationTaskSummary struct {
	ID                  string `json:"id"`
	ParentTaskID        string `json:"parent_task_id,omitempty"`
	Status              string `json:"status"`
	Role                string `json:"role,omitempty"`
	Goal                string `json:"goal"`
	WorkerProfile       string `json:"worker_profile,omitempty"`
	WaitingCheckpointID string `json:"waiting_checkpoint_id,omitempty"`
	WaitingScope        string `json:"waiting_scope,omitempty"`
	BlockedReason       string `json:"blocked_reason,omitempty"`
	TerminalReason      string `json:"terminal_reason,omitempty"`
	LatestResultID      string `json:"latest_result_id,omitempty"`
}

type orchestrationCheckpointSummary struct {
	ID             string                                 `json:"id"`
	TaskID         string                                 `json:"task_id"`
	Kind           string                                 `json:"kind"`
	ReasonCode     string                                 `json:"reason_code"`
	TriggeredBy    string                                 `json:"triggered_by"`
	Severity       string                                 `json:"severity"`
	Question       string                                 `json:"question"`
	BlocksRun      bool                                   `json:"blocks_run"`
	ResolutionHint string                                 `json:"resolution_hint"`
	Options        []orchestration.CheckpointOption       `json:"options"`
	DefaultAction  *orchestration.CheckpointDefaultAction `json:"default_action,omitempty"`
	ResumePolicy   *orchestration.CheckpointResumePolicy  `json:"resume_policy,omitempty"`
	TimeoutAt      *time.Time                             `json:"timeout_at,omitempty"`
}

type orchestrationResultSummary struct {
	ID               string         `json:"id"`
	TaskID           string         `json:"task_id"`
	AttemptID        string         `json:"attempt_id,omitempty"`
	Status           string         `json:"status"`
	Summary          string         `json:"summary,omitempty"`
	FailureClass     string         `json:"failure_class,omitempty"`
	RequestReplan    bool           `json:"request_replan"`
	StructuredOutput map[string]any `json:"structured_output,omitempty"`
	CreatedAt        time.Time      `json:"created_at"`
}

type orchestrationArtifactSummary struct {
	ID          string         `json:"id"`
	TaskID      string         `json:"task_id"`
	AttemptID   string         `json:"attempt_id,omitempty"`
	Kind        string         `json:"kind"`
	URI         string         `json:"uri"`
	Version     string         `json:"version,omitempty"`
	Digest      string         `json:"digest,omitempty"`
	ContentType string         `json:"content_type,omitempty"`
	Summary     string         `json:"summary,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
}

func NewOrchestrationProvider(log *slog.Logger, service orchestrationService, bots orchestrationBotReader) *OrchestrationProvider {
	if log == nil {
		log = slog.Default()
	}
	return &OrchestrationProvider{
		service: service,
		bots:    bots,
		logger:  log.With(slog.String("tool", "orchestration")),
	}
}

func (p *OrchestrationProvider) Tools(ctx context.Context, session SessionContext) ([]sdk.Tool, error) {
	if session.IsSubagent || session.SessionType == sessionpkg.TypeOrchestrationAttempt || session.SessionType == sessionpkg.TypeOrchestrationVerification || p.service == nil || p.bots == nil {
		return nil, nil
	}
	allowed, err := p.toolsAllowedForSession(ctx, session)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, nil
	}
	sess := session
	return []sdk.Tool{
		{
			Name:        "start_orchestration_run",
			Description: "Start a tracked orchestration run for the current bot when work should be delegated to the orchestration runtime instead of handled directly in the chat turn.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"goal": map[string]any{"type": "string", "description": "Clear goal for the orchestration run."},
				},
				"required": []string{"goal"},
			},
			Execute: func(execCtx *sdk.ToolExecContext, input any) (any, error) {
				return p.executeStart(toolExecutionContext(ctx, execCtx), sess, inputAsMap(input), toolCallID(execCtx))
			},
		},
		{
			Name:        "get_orchestration_status",
			Description: "Get the current status of an orchestration run, including active tasks, open checkpoints, and recent events.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"run_id": map[string]any{"type": "string", "description": "Run ID returned by start_orchestration_run."},
				},
				"required": []string{"run_id"},
			},
			Execute: func(execCtx *sdk.ToolExecContext, input any) (any, error) {
				return p.executeStatus(toolExecutionContext(ctx, execCtx), sess, inputAsMap(input))
			},
		},
		{
			Name:        "resolve_orchestration_checkpoint",
			Description: "Resolve an open human checkpoint yourself when the decision is routine and does not require user judgment (e.g. accepting a safe default). If the user should pick, prefer request_human_checkpoint_decision so the chat UI surfaces an interactive prompt instead of you deciding silently.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"checkpoint_id":   map[string]any{"type": "string", "description": "Open checkpoint ID from get_orchestration_status."},
					"option":          map[string]any{"type": "string", "description": "ID of the checkpoint option to choose."},
					"response":        map[string]any{"type": "string", "description": "Text response when the selected option is freeform."},
					"idempotency_key": map[string]any{"type": "string", "description": "Optional stable key for retrying the same checkpoint resolution. Omit unless retrying deliberately."},
				},
				"required": []string{"checkpoint_id", "option"},
			},
			Execute: func(execCtx *sdk.ToolExecContext, input any) (any, error) {
				return p.executeResolve(toolExecutionContext(ctx, execCtx), sess, inputAsMap(input))
			},
		},
		{
			Name:        "request_human_checkpoint_decision",
			Description: "Defer an open orchestration checkpoint to the chat user. The chat UI will render the checkpoint question and its options as clickable buttons so the user can pick interactively. Use this whenever the checkpoint warrants user judgment instead of auto-resolving via resolve_orchestration_checkpoint. After calling this tool you should end your turn; the user's selection is committed via the UI and the orchestration run resumes automatically.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"checkpoint_id": map[string]any{"type": "string", "description": "Open checkpoint ID from get_orchestration_status."},
					"run_id":        map[string]any{"type": "string", "description": "Run ID that owns the checkpoint."},
					"prompt":        map[string]any{"type": "string", "description": "Optional plain-language prompt to show above the checkpoint question. Use this to clarify why the user is being asked or to surface extra context the checkpoint question alone does not convey."},
				},
				"required": []string{"checkpoint_id", "run_id"},
			},
			Execute: func(execCtx *sdk.ToolExecContext, input any) (any, error) {
				return p.executeRequestHumanCheckpoint(toolExecutionContext(ctx, execCtx), sess, inputAsMap(input))
			},
		},
		{
			Name:        "resume_orchestration_run",
			Description: "Resume a failed orchestration run from its retryable failed task. Use only after get_orchestration_status shows the run is failed and the user wants to continue instead of starting a new run.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"run_id":          map[string]any{"type": "string", "description": "Failed run ID to resume."},
					"idempotency_key": map[string]any{"type": "string", "description": "Optional stable key for retrying the same resume request. Omit unless retrying deliberately."},
				},
				"required": []string{"run_id"},
			},
			Execute: func(execCtx *sdk.ToolExecContext, input any) (any, error) {
				return p.executeResume(toolExecutionContext(ctx, execCtx), sess, inputAsMap(input))
			},
		},
		{
			Name:        "cancel_orchestration_run",
			Description: "Request cancellation of an orchestration run that is no longer needed or should stop.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"run_id":          map[string]any{"type": "string", "description": "Run ID to cancel."},
					"idempotency_key": map[string]any{"type": "string", "description": "Optional stable key for retrying the same cancel request. Omit unless retrying deliberately."},
				},
				"required": []string{"run_id"},
			},
			Execute: func(execCtx *sdk.ToolExecContext, input any) (any, error) {
				return p.executeCancel(toolExecutionContext(ctx, execCtx), sess, inputAsMap(input))
			},
		},
	}, nil
}

func (p *OrchestrationProvider) toolsAllowedForSession(ctx context.Context, session SessionContext) (bool, error) {
	channelIdentityID := strings.TrimSpace(session.ChannelIdentityID)
	if channelIdentityID == "" {
		return true, nil
	}
	botID := strings.TrimSpace(session.BotID)
	if botID == "" {
		return false, nil
	}
	bot, err := p.bots.Get(ctx, botID)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(bot.OwnerUserID) == channelIdentityID, nil
}

func (p *OrchestrationProvider) executeStart(ctx context.Context, session SessionContext, args map[string]any, toolCallID string) (any, error) {
	caller, err := p.controlIdentityForSession(ctx, session)
	if err != nil {
		return nil, err
	}
	goal := StringArg(args, "goal")
	if goal == "" {
		return nil, errors.New("goal is required")
	}
	idempotencyKey := StringArg(args, "idempotency_key")
	if idempotencyKey == "" {
		idempotencyKey = startRunToolIdempotencyKey(session, toolCallID)
	}
	handle, err := p.service.StartRun(ctx, caller, orchestration.StartRunRequest{
		Goal:           goal,
		BotID:          strings.TrimSpace(session.BotID),
		Input:          startRunInputFromSession(session),
		IdempotencyKey: idempotencyKey,
		SourceMetadata: map[string]any{
			"bot_id":     strings.TrimSpace(session.BotID),
			"session_id": strings.TrimSpace(session.SessionID),
			"chat_id":    strings.TrimSpace(session.ChatID),
		},
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"run_id":         handle.RunID,
		"root_task_id":   handle.RootTaskID,
		"snapshot_seq":   handle.SnapshotSeq,
		"status_message": "orchestration run started",
	}, nil
}

func (p *OrchestrationProvider) executeStatus(ctx context.Context, session SessionContext, args map[string]any) (any, error) {
	caller, err := p.controlIdentityForSession(ctx, session)
	if err != nil {
		return nil, err
	}
	runID, err := requiredStringArg(args, "run_id")
	if err != nil {
		return nil, err
	}
	snapshot, err := p.service.GetRunSnapshot(ctx, caller, runID)
	if err != nil {
		return nil, err
	}
	asOfSeq := snapshot.SnapshotSeq
	taskPage, err := p.service.ListRunTasks(ctx, caller, runID, orchestration.ListRunTasksRequest{
		Limit:   200,
		AsOfSeq: asOfSeq,
	})
	if err != nil {
		return nil, err
	}
	checkpointPage, err := p.service.ListRunCheckpoints(ctx, caller, runID, orchestration.ListRunCheckpointsRequest{
		Status:  []string{orchestration.CheckpointStatusOpen},
		Limit:   50,
		AsOfSeq: asOfSeq,
	})
	if err != nil {
		return nil, err
	}
	resultPage, err := p.service.ListRunResults(ctx, caller, runID, orchestration.ListRunResultsRequest{
		Limit:   50,
		AsOfSeq: asOfSeq,
	})
	if err != nil {
		return nil, err
	}
	artifactPage, err := p.service.ListRunArtifacts(ctx, caller, runID, orchestration.ListRunArtifactsRequest{
		Limit:   50,
		AsOfSeq: asOfSeq,
	})
	if err != nil {
		return nil, err
	}
	activeTasks := []orchestrationTaskSummary{}
	failedTasks := []orchestrationTaskSummary{}
	summary := orchestrationStatusSummary{}
	finalTaskID := ""
	for _, task := range taskPage.Items {
		if task.Role == orchestration.TaskRoleFinal {
			finalTaskID = task.ID
		}
		switch task.Status {
		case orchestration.TaskStatusReady, orchestration.TaskStatusDispatching, orchestration.TaskStatusRunning, orchestration.TaskStatusVerifying:
			summary.Active++
			activeTasks = append(activeTasks, summarizeTask(task))
		case orchestration.TaskStatusWaitingHuman:
			summary.WaitingHuman++
			activeTasks = append(activeTasks, summarizeTask(task))
		case orchestration.TaskStatusCompleted:
			summary.Completed++
		case orchestration.TaskStatusBlocked:
			summary.Blocked++
			failedTasks = append(failedTasks, summarizeTask(task))
		case orchestration.TaskStatusFailed:
			summary.Failed++
			failedTasks = append(failedTasks, summarizeTask(task))
		case orchestration.TaskStatusCancelled:
			summary.Cancelled++
		}
	}
	openCheckpoints := make([]orchestrationCheckpointSummary, 0, len(checkpointPage.Items))
	for _, checkpoint := range checkpointPage.Items {
		openCheckpoints = append(openCheckpoints, summarizeCheckpoint(checkpoint))
	}
	artifactIDsByTask := map[string][]string{}
	artifacts := make([]orchestrationArtifactSummary, 0, len(artifactPage.Items))
	for _, artifact := range artifactPage.Items {
		artifacts = append(artifacts, summarizeArtifact(artifact))
		artifactIDsByTask[artifact.TaskID] = append(artifactIDsByTask[artifact.TaskID], artifact.ID)
	}
	usableResults := []orchestrationResultSummary{}
	var finalResult *orchestrationFinalResult
	for _, result := range resultPage.Items {
		if isUsableTaskResult(result) {
			usableResults = append(usableResults, summarizeResult(result))
		}
		if finalTaskID != "" && result.TaskID == finalTaskID && isUsableTaskResult(result) && snapshot.Run.LifecycleStatus == orchestration.LifecycleStatusCompleted {
			summaryText := strings.TrimSpace(result.Summary)
			if summaryText == "" {
				summaryText = summarizeStructuredOutput(result.StructuredOutput)
			}
			finalResult = &orchestrationFinalResult{
				TaskID:      result.TaskID,
				ResultID:    result.ID,
				Snippet:     summaryText,
				ArtifactIDs: artifactIDsByTask[result.TaskID],
			}
		}
	}
	verdict := decideStatusVerdict(snapshot.Run, summary, openCheckpoints, finalResult != nil)
	return orchestrationStatusResult{
		StatusMessage:   summarizeRunStatus(snapshot.Run, len(openCheckpoints), len(activeTasks)),
		RunID:           snapshot.Run.ID,
		Goal:            snapshot.Run.Goal,
		Status:          externalRunStatus(snapshot.Run.LifecycleStatus),
		TerminalReason:  strings.TrimSpace(snapshot.Run.TerminalReason),
		Verdict:         verdict,
		FinalResult:     finalResult,
		Summary:         summary,
		OpenCheckpoints: openCheckpoints,
		ActiveTasks:     activeTasks,
		FailedTasks:     failedTasks,
		UsableResults:   usableResults,
		Artifacts:       artifacts,
	}, nil
}

func (p *OrchestrationProvider) executeResolve(ctx context.Context, session SessionContext, args map[string]any) (any, error) {
	caller, err := p.controlIdentityForSession(ctx, session)
	if err != nil {
		return nil, err
	}
	checkpointID, err := requiredStringArg(args, "checkpoint_id")
	if err != nil {
		return nil, err
	}
	option, err := requiredStringArg(args, "option")
	if err != nil {
		return nil, err
	}
	idempotencyKey := optionalIdempotencyKey(args, "orchestration-resolve")
	result, err := p.service.ResolveCheckpoint(ctx, caller, checkpointID, orchestration.CheckpointResolution{
		Option:         option,
		Response:       StringArg(args, "response"),
		IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"checkpoint_id":  checkpointID,
		"snapshot_seq":   result.SnapshotSeq,
		"status_message": "checkpoint resolved",
	}, nil
}

func (p *OrchestrationProvider) executeRequestHumanCheckpoint(ctx context.Context, session SessionContext, args map[string]any) (any, error) {
	caller, err := p.controlIdentityForSession(ctx, session)
	if err != nil {
		return nil, err
	}
	checkpointID, err := requiredStringArg(args, "checkpoint_id")
	if err != nil {
		return nil, err
	}
	runID, err := requiredStringArg(args, "run_id")
	if err != nil {
		return nil, err
	}
	prompt := strings.TrimSpace(StringArg(args, "prompt"))
	// Fetch the open checkpoint set for the run so the tool result carries a
	// trusted, server-side snapshot of question + options rather than relying on
	// model-supplied fields. The UI binds directly against this payload.
	page, err := p.service.ListRunCheckpoints(ctx, caller, runID, orchestration.ListRunCheckpointsRequest{
		Status: []string{orchestration.CheckpointStatusOpen},
		Limit:  50,
	})
	if err != nil {
		return nil, err
	}
	var found *orchestration.HumanCheckpoint
	for i := range page.Items {
		if page.Items[i].ID == checkpointID {
			found = &page.Items[i]
			break
		}
	}
	if found == nil {
		return nil, fmt.Errorf("%w: checkpoint %s is not open or not part of run %s", orchestration.ErrCheckpointNotFound, checkpointID, runID)
	}
	return map[string]any{
		"status_message": "awaiting user input on checkpoint",
		"prompt":         prompt,
		"checkpoint":     summarizeCheckpointForUser(*found),
	}, nil
}

func summarizeCheckpointForUser(checkpoint orchestration.HumanCheckpoint) map[string]any {
	out := map[string]any{
		"id":           checkpoint.ID,
		"run_id":       checkpoint.RunID,
		"task_id":      checkpoint.TaskID,
		"kind":         checkpoint.Kind,
		"reason_code":  checkpoint.ReasonCode,
		"triggered_by": checkpoint.TriggeredBy,
		"severity":     checkpoint.Severity,
		"question":     checkpoint.Question,
		"status":       checkpoint.Status,
		"blocks_run":   checkpoint.BlocksRun,
		"options":      checkpoint.Options,
	}
	if checkpoint.DefaultAction != nil {
		out["default_action"] = checkpoint.DefaultAction
	}
	if checkpoint.ResumePolicy != nil {
		out["resume_policy"] = checkpoint.ResumePolicy
	}
	if checkpoint.TimeoutAt != nil {
		out["timeout_at"] = checkpoint.TimeoutAt
	}
	return out
}

func (p *OrchestrationProvider) executeCancel(ctx context.Context, session SessionContext, args map[string]any) (any, error) {
	caller, err := p.controlIdentityForSession(ctx, session)
	if err != nil {
		return nil, err
	}
	runID, err := requiredStringArg(args, "run_id")
	if err != nil {
		return nil, err
	}
	result, err := p.service.CancelRun(ctx, caller, runID, orchestration.CancelRunRequest{
		IdempotencyKey: optionalIdempotencyKey(args, "orchestration-cancel"),
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"run_id":           result.RunID,
		"lifecycle_status": result.LifecycleStatus,
		"snapshot_seq":     result.SnapshotSeq,
		"status_message":   "orchestration run cancellation requested",
	}, nil
}

func (p *OrchestrationProvider) executeResume(ctx context.Context, session SessionContext, args map[string]any) (any, error) {
	caller, err := p.controlIdentityForSession(ctx, session)
	if err != nil {
		return nil, err
	}
	runID, err := requiredStringArg(args, "run_id")
	if err != nil {
		return nil, err
	}
	result, err := p.service.ResumeRun(ctx, caller, runID, orchestration.ResumeRunRequest{
		IdempotencyKey: optionalIdempotencyKey(args, "orchestration-resume"),
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"run_id":           result.RunID,
		"lifecycle_status": result.LifecycleStatus,
		"resumed_task_ids": result.ResumedTaskIDs,
		"snapshot_seq":     result.SnapshotSeq,
		"status_message":   "orchestration run resumed",
		"next_observation": "Use get_orchestration_status to monitor the resumed run.",
	}, nil
}

func (p *OrchestrationProvider) controlIdentityForSession(ctx context.Context, session SessionContext) (orchestration.ControlIdentity, error) {
	botID := strings.TrimSpace(session.BotID)
	if botID == "" {
		return orchestration.ControlIdentity{}, errors.New("bot_id is required")
	}
	bot, err := p.bots.Get(ctx, botID)
	if err != nil {
		return orchestration.ControlIdentity{}, err
	}
	ownerID := strings.TrimSpace(bot.OwnerUserID)
	if ownerID == "" {
		return orchestration.ControlIdentity{}, errors.New("bot owner is required")
	}
	channelIdentityID := strings.TrimSpace(session.ChannelIdentityID)
	if channelIdentityID != "" && channelIdentityID != ownerID {
		return orchestration.ControlIdentity{}, errors.New("orchestration tools require the bot owner session")
	}
	return orchestration.ControlIdentity{
		TenantID: ownerID,
		Subject:  ownerID,
	}, nil
}

func newToolIdempotencyKey(prefix string) string {
	return prefix + "-" + uuid.NewString()
}

func requiredStringArg(args map[string]any, key string) (string, error) {
	value := StringArg(args, key)
	if value == "" {
		return "", fmt.Errorf("%s is required", key)
	}
	return value, nil
}

func optionalIdempotencyKey(args map[string]any, prefix string) string {
	if key := StringArg(args, "idempotency_key"); key != "" {
		return key
	}
	return newToolIdempotencyKey(prefix)
}

func startRunInputFromSession(session SessionContext) map[string]any {
	attachments := orchestrationInputAttachments(session.Attachments)
	if len(attachments) == 0 {
		return map[string]any{}
	}
	return map[string]any{
		"attachments": attachments,
	}
}

func orchestrationInputAttachments(attachments []Attachment) []map[string]any {
	if len(attachments) == 0 {
		return nil
	}
	items := make([]map[string]any, 0, len(attachments))
	for _, attachment := range attachments {
		item := orchestrationInputAttachment(attachment)
		if len(item) == 0 {
			continue
		}
		items = append(items, item)
	}
	return items
}

func orchestrationInputAttachment(attachment Attachment) map[string]any {
	item := map[string]any{}
	if value := strings.TrimSpace(attachment.Type); value != "" {
		item["type"] = value
	}
	if value := strings.TrimSpace(attachment.ContentHash); value != "" {
		item["content_hash"] = value
	}
	if value := strings.TrimSpace(attachment.Path); value != "" {
		item["path"] = value
	}
	if value := strings.TrimSpace(attachment.URL); value != "" {
		item["url"] = value
	}
	if value := strings.TrimSpace(attachment.PlatformKey); value != "" {
		item["platform_key"] = value
	}
	if value := strings.TrimSpace(attachment.Name); value != "" {
		item["name"] = value
	}
	if value := strings.TrimSpace(attachment.Mime); value != "" {
		item["mime"] = value
	}
	if attachment.Size > 0 {
		item["size"] = attachment.Size
	}
	if len(attachment.Metadata) > 0 {
		item["metadata"] = attachment.Metadata
	}
	return item
}

func startRunToolIdempotencyKey(session SessionContext, toolCallID string) string {
	trimmedToolCallID := strings.TrimSpace(toolCallID)
	if trimmedToolCallID == "" {
		return newToolIdempotencyKey("orchestration-start")
	}
	hash := sha256.Sum256([]byte(strings.Join([]string{
		strings.TrimSpace(session.BotID),
		strings.TrimSpace(session.SessionID),
		strings.TrimSpace(session.ChatID),
		trimmedToolCallID,
	}, "\x00")))
	return "orchestration-start-" + hex.EncodeToString(hash[:])[:32]
}

func toolCallID(execCtx *sdk.ToolExecContext) string {
	if execCtx == nil {
		return ""
	}
	return strings.TrimSpace(execCtx.ToolCallID)
}

func toolExecutionContext(fallback context.Context, execCtx *sdk.ToolExecContext) context.Context {
	if execCtx != nil && execCtx.Context != nil {
		return execCtx.Context
	}
	if fallback != nil {
		return fallback
	}
	return context.Background()
}

func summarizeTask(task orchestration.Task) orchestrationTaskSummary {
	return orchestrationTaskSummary{
		ID:                  task.ID,
		ParentTaskID:        task.DecomposedFromTaskID,
		Status:              task.Status,
		Role:                task.Role,
		Goal:                task.Goal,
		WorkerProfile:       task.WorkerProfile,
		WaitingCheckpointID: task.WaitingCheckpointID,
		WaitingScope:        task.WaitingScope,
		BlockedReason:       task.BlockedReason,
		TerminalReason:      task.TerminalReason,
		LatestResultID:      task.LatestResultID,
	}
}

func summarizeCheckpoint(checkpoint orchestration.HumanCheckpoint) orchestrationCheckpointSummary {
	return orchestrationCheckpointSummary{
		ID:             checkpoint.ID,
		TaskID:         checkpoint.TaskID,
		Kind:           checkpoint.Kind,
		ReasonCode:     checkpoint.ReasonCode,
		TriggeredBy:    checkpoint.TriggeredBy,
		Severity:       checkpoint.Severity,
		Question:       checkpoint.Question,
		BlocksRun:      checkpoint.BlocksRun,
		ResolutionHint: checkpointResolutionHint(checkpoint),
		Options:        checkpoint.Options,
		DefaultAction:  checkpoint.DefaultAction,
		ResumePolicy:   checkpoint.ResumePolicy,
		TimeoutAt:      checkpoint.TimeoutAt,
	}
}

func summarizeResult(result orchestration.TaskResult) orchestrationResultSummary {
	return orchestrationResultSummary{
		ID:               result.ID,
		TaskID:           result.TaskID,
		AttemptID:        result.AttemptID,
		Status:           result.Status,
		Summary:          result.Summary,
		FailureClass:     result.FailureClass,
		RequestReplan:    result.RequestReplan,
		StructuredOutput: result.StructuredOutput,
		CreatedAt:        result.CreatedAt,
	}
}

func summarizeArtifact(artifact orchestration.Artifact) orchestrationArtifactSummary {
	return orchestrationArtifactSummary{
		ID:          artifact.ID,
		TaskID:      artifact.TaskID,
		AttemptID:   artifact.AttemptID,
		Kind:        artifact.Kind,
		URI:         artifact.URI,
		Version:     artifact.Version,
		Digest:      artifact.Digest,
		ContentType: artifact.ContentType,
		Summary:     artifact.Summary,
		Metadata:    artifact.Metadata,
		CreatedAt:   artifact.CreatedAt,
	}
}

func summarizeRunStatus(run orchestration.Run, openCheckpoints int, activeTasks int) string {
	switch run.LifecycleStatus {
	case orchestration.LifecycleStatusWaitingHuman:
		return fmt.Sprintf("run is waiting for human input with %d open checkpoints", openCheckpoints)
	case orchestration.LifecycleStatusCompleted:
		return "run completed"
	case orchestration.LifecycleStatusFailed:
		if strings.TrimSpace(run.TerminalReason) != "" {
			return "run failed: " + strings.TrimSpace(run.TerminalReason)
		}
		return "run failed"
	case orchestration.LifecycleStatusCancelled:
		return "run cancelled"
	case orchestration.LifecycleStatusCancelling:
		return "run is cancelling"
	default:
		if activeTasks > 0 {
			return fmt.Sprintf("run is active with %d non-terminal tasks", activeTasks)
		}
		return "run is active"
	}
}

func decideStatusVerdict(run orchestration.Run, summary orchestrationStatusSummary, openCheckpoints []orchestrationCheckpointSummary, hasFinalResult bool) orchestrationStatusVerdict {
	needsHuman := run.LifecycleStatus == orchestration.LifecycleStatusWaitingHuman || summary.WaitingHuman > 0 || len(openCheckpoints) > 0
	verdict := orchestrationStatusVerdict{
		IsTerminal: isTerminalRunStatus(run.LifecycleStatus),
		NeedsHuman: needsHuman,
		NextAction: "wait",
		ReasonCode: "running_tasks",
		ReasonText: "Run still has active work.",
	}
	switch run.LifecycleStatus {
	case orchestration.LifecycleStatusCreated:
		verdict.NextAction = "wait"
		verdict.ReasonCode = "pending_start"
		verdict.ReasonText = "Run has been created and is waiting for planning or dispatch."
	case orchestration.LifecycleStatusWaitingHuman:
		verdict.NextAction = "handle_checkpoint"
		verdict.ReasonCode = "awaiting_checkpoint"
		verdict.ReasonText = fmt.Sprintf("%d open checkpoint(s) need a decision.", len(openCheckpoints))
	case orchestration.LifecycleStatusCancelling:
		verdict.NextAction = "wait"
		verdict.ReasonCode = "cancelling"
		verdict.ReasonText = "Run cancellation is in progress."
	case orchestration.LifecycleStatusCompleted:
		verdict.NextAction = "summarize_for_user"
		verdict.ReasonCode = "completed_with_final"
		verdict.ReasonText = "Run completed and final result is available."
		if !hasFinalResult {
			verdict.NextAction = "none"
			verdict.ReasonCode = "completed_without_final"
			verdict.ReasonText = "Run completed but no final result was found."
		}
	case orchestration.LifecycleStatusFailed:
		verdict.NextAction = "resume_run"
		verdict.ReasonCode = "retryable_failure"
		verdict.ReasonText = "Run failed; resume_orchestration_run can retry failed work."
		if summary.Failed == 0 && summary.Blocked == 0 {
			verdict.NextAction = "none"
			verdict.ReasonCode = "failed_terminal"
			verdict.ReasonText = "Run failed and no failed or blocked task is visible for resume."
		}
	case orchestration.LifecycleStatusCancelled:
		verdict.NextAction = "none"
		verdict.ReasonCode = "cancelled"
		verdict.ReasonText = "Run was cancelled."
	default:
		if needsHuman {
			verdict.NextAction = "handle_checkpoint"
			verdict.ReasonCode = "awaiting_checkpoint"
			verdict.ReasonText = fmt.Sprintf("%d open checkpoint(s) need a decision.", len(openCheckpoints))
		}
	}
	return verdict
}

func externalRunStatus(status string) string {
	switch status {
	case orchestration.LifecycleStatusCreated:
		return "pending"
	case orchestration.LifecycleStatusRunning,
		orchestration.LifecycleStatusWaitingHuman,
		orchestration.LifecycleStatusCancelling,
		orchestration.LifecycleStatusCompleted,
		orchestration.LifecycleStatusFailed,
		orchestration.LifecycleStatusCancelled:
		return status
	default:
		return "running"
	}
}

func checkpointResolutionHint(checkpoint orchestration.HumanCheckpoint) string {
	if checkpoint.DefaultAction != nil {
		return "default_available"
	}
	if len(checkpoint.Options) == 1 && checkpoint.Options[0].Kind == orchestration.CheckpointOptionKindChoice {
		return "agent_may_choose_option"
	}
	return "ask_user"
}

func isUsableTaskResult(result orchestration.TaskResult) bool {
	return result.Status == "completed" && !result.RequestReplan
}

func summarizeStructuredOutput(output map[string]any) string {
	if len(output) == 0 {
		return ""
	}
	return "structured output is available"
}

func isTerminalRunStatus(status string) bool {
	switch status {
	case orchestration.LifecycleStatusCompleted, orchestration.LifecycleStatusFailed, orchestration.LifecycleStatusCancelled:
		return true
	default:
		return false
	}
}
