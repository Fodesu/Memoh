package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	sdk "github.com/memohai/twilight-ai/sdk"

	"github.com/memohai/memoh/internal/orchestration"
	sessionpkg "github.com/memohai/memoh/internal/session"
)

type OrchestrationExecutionState struct {
	mu           sync.Mutex
	attempt      *orchestration.AttemptCompletion
	verification *orchestration.VerificationCompletion
	checkpoint   *orchestration.HumanCheckpoint
}

func (s *OrchestrationExecutionState) SubmitAttempt(completion orchestration.AttemptCompletion) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.attempt != nil {
		return errors.New("task result already submitted")
	}
	s.attempt = &completion
	return nil
}

func (s *OrchestrationExecutionState) AttemptCompletion() (orchestration.AttemptCompletion, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.attempt == nil {
		return orchestration.AttemptCompletion{}, false
	}
	return *s.attempt, true
}

func (s *OrchestrationExecutionState) SubmitCheckpoint(checkpoint orchestration.HumanCheckpoint) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.attempt != nil {
		return errors.New("task result already submitted")
	}
	if s.checkpoint != nil {
		return errors.New("human checkpoint already requested")
	}
	s.checkpoint = &checkpoint
	return nil
}

func (s *OrchestrationExecutionState) RequestedCheckpoint() (orchestration.HumanCheckpoint, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.checkpoint == nil {
		return orchestration.HumanCheckpoint{}, false
	}
	return *s.checkpoint, true
}

func (s *OrchestrationExecutionState) SubmitVerification(completion orchestration.VerificationCompletion) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.verification != nil {
		return errors.New("verification result already submitted")
	}
	s.verification = &completion
	return nil
}

func (s *OrchestrationExecutionState) VerificationCompletion() (orchestration.VerificationCompletion, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.verification == nil {
		return orchestration.VerificationCompletion{}, false
	}
	return *s.verification, true
}

type OrchestrationExecutorProvider struct {
	state   *OrchestrationExecutionState
	attempt *OrchestrationAttemptToolContext
}

func NewOrchestrationExecutorProvider(state *OrchestrationExecutionState) *OrchestrationExecutorProvider {
	return &OrchestrationExecutorProvider{state: state}
}

func NewOrchestrationAttemptExecutorProvider(state *OrchestrationExecutionState, attempt OrchestrationAttemptToolContext) *OrchestrationExecutorProvider {
	return &OrchestrationExecutorProvider{state: state, attempt: &attempt}
}

type OrchestrationAttemptToolContext struct {
	RunID     string
	TaskID    string
	AttemptID string
	Caller    orchestration.ControlIdentity
	Service   interface {
		CreateHumanCheckpoint(context.Context, orchestration.ControlIdentity, orchestration.CreateHumanCheckpointRequest) (*orchestration.CreateHumanCheckpointResult, error)
	}
}

func (p *OrchestrationExecutorProvider) Tools(ctx context.Context, session SessionContext) ([]sdk.Tool, error) {
	if p == nil || p.state == nil {
		return nil, nil
	}
	switch session.SessionType {
	case sessionpkg.TypeOrchestrationAttempt:
		return p.attemptTools(ctx), nil
	case sessionpkg.TypeOrchestrationVerification:
		return p.verificationTools(session), nil
	default:
		return nil, nil
	}
}

func (p *OrchestrationExecutorProvider) attemptTools(ctx context.Context) []sdk.Tool {
	tools := []sdk.Tool{
		{
			Name:        "submit_task_result",
			Description: "Finish the current orchestration task attempt. This tool is required to mark the task completed or failed.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"status":            map[string]any{"type": "string", "enum": []string{orchestration.TaskAttemptStatusCompleted, orchestration.TaskAttemptStatusFailed}},
					"summary":           map[string]any{"type": "string"},
					"failure_class":     map[string]any{"type": "string"},
					"terminal_reason":   map[string]any{"type": "string"},
					"request_replan":    map[string]any{"type": "boolean"},
					"structured_output": map[string]any{"type": "object"},
					"artifact_intents":  artifactIntentArraySchema(),
				},
				"required": []string{"status", "summary"},
			},
			Execute: func(_ *sdk.ToolExecContext, input any) (any, error) {
				args := inputAsMap(input)
				status := strings.TrimSpace(StringArg(args, "status"))
				if status == "" {
					status = orchestration.TaskAttemptStatusCompleted
				}
				if status != orchestration.TaskAttemptStatusCompleted && status != orchestration.TaskAttemptStatusFailed {
					return nil, fmt.Errorf("invalid status %q", status)
				}
				summary := strings.TrimSpace(StringArg(args, "summary"))
				if summary == "" {
					return nil, errors.New("summary is required")
				}
				requestReplan, _, err := BoolArg(args, "request_replan")
				if err != nil {
					return nil, err
				}
				completion := orchestration.AttemptCompletion{
					Status:           status,
					Summary:          summary,
					FailureClass:     strings.TrimSpace(StringArg(args, "failure_class")),
					TerminalReason:   strings.TrimSpace(StringArg(args, "terminal_reason")),
					RequestReplan:    requestReplan,
					StructuredOutput: normalizeToolObject(args["structured_output"]),
					ArtifactIntents:  parseArtifactIntents(args["artifact_intents"]),
				}
				if err := p.state.SubmitAttempt(completion); err != nil {
					return nil, err
				}
				return map[string]any{"accepted": true}, nil
			},
		},
		{
			Name:        "propose_tasks",
			Description: "Propose replacement DAG tasks when the current task needs replanning. Call submit_task_result with request_replan=true after this.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"tasks": map[string]any{
						"type":        "array",
						"description": "Task proposals using alias, role, goal, inputs, depends_on, worker_profile, priority, retry_policy, verification_policy, and blackboard_scope.",
						"items":       map[string]any{"type": "object"},
					},
				},
				"required": []string{"tasks"},
			},
			Execute: func(_ *sdk.ToolExecContext, input any) (any, error) {
				args := inputAsMap(input)
				tasks, ok := args["tasks"].([]any)
				if !ok || len(tasks) == 0 {
					return nil, errors.New("tasks is required")
				}
				completion := orchestration.AttemptCompletion{
					Status:           orchestration.TaskAttemptStatusCompleted,
					Summary:          "proposed replacement DAG tasks",
					RequestReplan:    true,
					StructuredOutput: map[string]any{"child_tasks": tasks},
				}
				if err := p.state.SubmitAttempt(completion); err != nil {
					return nil, err
				}
				return map[string]any{"accepted": true, "task_count": len(tasks)}, nil
			},
		},
		{
			Name:        "commit_artifact",
			Description: "Validate an artifact produced by the current task attempt. Include the same artifact object in submit_task_result artifact_intents so it is committed with the task result.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"kind":         map[string]any{"type": "string"},
					"uri":          map[string]any{"type": "string"},
					"version":      map[string]any{"type": "string"},
					"digest":       map[string]any{"type": "string"},
					"content_type": map[string]any{"type": "string"},
					"summary":      map[string]any{"type": "string"},
					"metadata":     map[string]any{"type": "object"},
				},
				"required": []string{"kind", "uri", "version", "digest"},
			},
			Execute: func(_ *sdk.ToolExecContext, input any) (any, error) {
				intent := parseArtifactIntent(inputAsMap(input))
				if intent.Kind == "" || intent.URI == "" || intent.Version == "" || intent.Digest == "" {
					return nil, errors.New("kind, uri, version, and digest are required")
				}
				return map[string]any{"accepted": true, "artifact_intent": intent}, nil
			},
		},
	}
	if p.attempt != nil && p.attempt.Service != nil {
		tools = append(tools, p.requestHumanCheckpointTool(ctx))
	}
	return tools
}

func (p *OrchestrationExecutorProvider) requestHumanCheckpointTool(ctx context.Context) sdk.Tool {
	return sdk.Tool{
		Name:        "request_human_checkpoint",
		Description: "Pause the current orchestration task and ask the user for a decision when the task goal is unclear, blocked by missing information, or requires a user preference. Provide concise options and a recommended default when possible.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"question":       map[string]any{"type": "string", "description": "Clear question to ask the user."},
				"reason_code":    map[string]any{"type": "string", "enum": []string{orchestration.CheckpointReasonClarification, orchestration.CheckpointReasonScopeChange, orchestration.CheckpointReasonBlocked, orchestration.CheckpointReasonRiskConfirmation}},
				"severity":       map[string]any{"type": "string", "enum": []string{orchestration.CheckpointSeverityLow, orchestration.CheckpointSeverityMedium, orchestration.CheckpointSeverityHigh}},
				"blocks_run":     map[string]any{"type": "boolean", "description": "Whether this decision should pause the whole run. Use true only when sibling tasks cannot safely continue."},
				"options":        map[string]any{"type": "array", "description": "Choice/freeform options. Each item should include id, kind, label, and optional description.", "items": map[string]any{"type": "object"}},
				"default_action": map[string]any{"type": "object", "description": "Optional default resolution using option and optional response."},
				"metadata":       map[string]any{"type": "object"},
			},
			"required": []string{"question", "options"},
		},
		Execute: func(execCtx *sdk.ToolExecContext, input any) (any, error) {
			args := inputAsMap(input)
			question := strings.TrimSpace(StringArg(args, "question"))
			if question == "" {
				return nil, errors.New("question is required")
			}
			options := parseCheckpointOptions(args["options"])
			if len(options) == 0 {
				return nil, errors.New("options is required")
			}
			blocksRun, _, err := BoolArg(args, "blocks_run")
			if err != nil {
				return nil, err
			}
			result, err := p.attempt.Service.CreateHumanCheckpoint(toolExecutionContext(ctx, execCtx), p.attempt.Caller, orchestration.CreateHumanCheckpointRequest{
				RunID:          p.attempt.RunID,
				TaskID:         p.attempt.TaskID,
				BlocksRun:      blocksRun,
				Kind:           orchestration.CheckpointKindSemantic,
				ReasonCode:     strings.TrimSpace(StringArg(args, "reason_code")),
				TriggeredBy:    orchestration.CheckpointTriggeredByAgent,
				Severity:       strings.TrimSpace(StringArg(args, "severity")),
				Question:       question,
				Options:        options,
				DefaultAction:  parseCheckpointDefaultAction(args["default_action"]),
				ResumePolicy:   &orchestration.CheckpointResumePolicy{ResumeMode: orchestration.CheckpointResumeModeNewAttempt},
				Metadata:       normalizeToolObject(args["metadata"]),
				IdempotencyKey: "checkpoint-" + p.attempt.AttemptID,
			})
			if err != nil {
				return nil, err
			}
			if err := p.state.SubmitCheckpoint(result.Checkpoint); err != nil {
				return nil, err
			}
			return map[string]any{
				"accepted":      true,
				"checkpoint_id": result.Checkpoint.ID,
				"run_id":        result.Checkpoint.RunID,
				"task_id":       result.Checkpoint.TaskID,
				"status":        result.Checkpoint.Status,
				"next_step":     "Stop work for this attempt. The orchestration runtime will resume after the checkpoint is resolved.",
			}, nil
		},
	}
}

func (p *OrchestrationExecutorProvider) verificationTools(_ SessionContext) []sdk.Tool {
	return []sdk.Tool{
		{
			Name:        "submit_verification_result",
			Description: "Finish the current orchestration verification. This tool is required to accept or reject the task result.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"status":          map[string]any{"type": "string", "enum": []string{orchestration.TaskVerificationStatusCompleted, orchestration.TaskVerificationStatusFailed}},
					"verdict":         map[string]any{"type": "string", "enum": []string{orchestration.VerificationVerdictAccepted, orchestration.VerificationVerdictRejected}},
					"summary":         map[string]any{"type": "string"},
					"failure_class":   map[string]any{"type": "string"},
					"terminal_reason": map[string]any{"type": "string"},
					"request_replan":  map[string]any{"type": "boolean"},
				},
				"required": []string{"status", "verdict", "summary"},
			},
			Execute: func(_ *sdk.ToolExecContext, input any) (any, error) {
				args := inputAsMap(input)
				status := strings.TrimSpace(StringArg(args, "status"))
				if status == "" {
					status = orchestration.TaskVerificationStatusCompleted
				}
				if status != orchestration.TaskVerificationStatusCompleted && status != orchestration.TaskVerificationStatusFailed {
					return nil, fmt.Errorf("invalid status %q", status)
				}
				verdict := strings.TrimSpace(StringArg(args, "verdict"))
				if verdict != orchestration.VerificationVerdictAccepted && verdict != orchestration.VerificationVerdictRejected {
					return nil, fmt.Errorf("invalid verdict %q", verdict)
				}
				summary := strings.TrimSpace(StringArg(args, "summary"))
				if summary == "" {
					return nil, errors.New("summary is required")
				}
				requestReplan, _, err := BoolArg(args, "request_replan")
				if err != nil {
					return nil, err
				}
				completion := orchestration.VerificationCompletion{
					Status:         status,
					Verdict:        verdict,
					Summary:        summary,
					FailureClass:   strings.TrimSpace(StringArg(args, "failure_class")),
					TerminalReason: strings.TrimSpace(StringArg(args, "terminal_reason")),
					RequestReplan:  requestReplan,
				}
				if err := p.state.SubmitVerification(completion); err != nil {
					return nil, err
				}
				return map[string]any{"accepted": true}, nil
			},
		},
	}
}

func artifactIntentArraySchema() map[string]any {
	return map[string]any{
		"type":  "array",
		"items": map[string]any{"type": "object"},
	}
}

func normalizeToolObject(raw any) map[string]any {
	value, ok := raw.(map[string]any)
	if !ok || value == nil {
		return map[string]any{}
	}
	return value
}

func parseCheckpointOptions(raw any) []orchestration.CheckpointOption {
	items, ok := raw.([]any)
	if !ok || len(items) == 0 {
		return nil
	}
	options := make([]orchestration.CheckpointOption, 0, len(items))
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			continue
		}
		options = append(options, orchestration.CheckpointOption{
			ID:          strings.TrimSpace(StringArg(object, "id")),
			Kind:        strings.TrimSpace(StringArg(object, "kind")),
			Label:       strings.TrimSpace(StringArg(object, "label")),
			Description: strings.TrimSpace(StringArg(object, "description")),
		})
	}
	return options
}

func parseCheckpointDefaultAction(raw any) *orchestration.CheckpointDefaultAction {
	object, ok := raw.(map[string]any)
	if !ok || len(object) == 0 {
		return nil
	}
	return &orchestration.CheckpointDefaultAction{
		Option:   strings.TrimSpace(StringArg(object, "option")),
		Response: StringArg(object, "response"),
	}
}

func parseArtifactIntents(raw any) []orchestration.AttemptArtifactIntent {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	result := make([]orchestration.AttemptArtifactIntent, 0, len(items))
	for _, item := range items {
		intent := parseArtifactIntent(inputAsMap(item))
		if intent.Kind == "" && intent.URI == "" && intent.Version == "" && intent.Digest == "" {
			continue
		}
		result = append(result, intent)
	}
	return result
}

func parseArtifactIntent(args map[string]any) orchestration.AttemptArtifactIntent {
	return orchestration.AttemptArtifactIntent{
		Kind:        strings.TrimSpace(StringArg(args, "kind")),
		URI:         strings.TrimSpace(StringArg(args, "uri")),
		Version:     strings.TrimSpace(StringArg(args, "version")),
		Digest:      strings.TrimSpace(StringArg(args, "digest")),
		ContentType: strings.TrimSpace(StringArg(args, "content_type")),
		Summary:     strings.TrimSpace(StringArg(args, "summary")),
		Metadata:    normalizeToolObject(args["metadata"]),
	}
}
