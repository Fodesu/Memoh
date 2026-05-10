package orchestrationexec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	sdk "github.com/memohai/twilight-ai/sdk"

	agentpkg "github.com/memohai/memoh/internal/agent"
	"github.com/memohai/memoh/internal/models"
	"github.com/memohai/memoh/internal/orchestration"
)

const (
	rootPlannerSubmitPlanToolName = "submit_start_plan"
	plannerSubmitPlanToolName     = "submit_plan"
	plannerDefineRootTaskToolName = "define_root_task"
	plannerAddChildTaskToolName   = "add_child_task"
)

type plannerSubmitPlanInput struct {
	Summary string `json:"summary,omitempty"`
}

func (r *Runtime) PlanStartRun(ctx context.Context, input orchestration.StartRunPlanningInput) (*orchestration.StartRunPlanningResult, error) {
	if r == nil {
		return nil, errors.New("planner runtime is not configured")
	}
	botID := strings.TrimSpace(stringValue(input.Run.SourceMetadata["bot_id"]))
	if botID == "" {
		return &orchestration.StartRunPlanningResult{}, nil
	}
	cfg, _, _, err := r.buildBotRunConfig(ctx, botID, input.Run.OwnerSubject)
	if err != nil {
		return nil, err
	}
	envResources, err := r.loadEnvResourceCatalog(ctx, input.Run.TenantID)
	if err != nil {
		return nil, err
	}
	cfg.System = startRunPlannerSystemPrompt
	cfg.Messages = []sdk.Message{sdk.UserMessage(buildStartRunPlannerPrompt(input))}
	plan, err := r.generatePlannerToolPlan(ctx, cfg, rootPlannerSubmitPlanToolName, false, true, envResources)
	if err != nil {
		return nil, fmt.Errorf("decode start run planner schema: %w", err)
	}
	return plan, nil
}

func (r *Runtime) PlanReplan(ctx context.Context, input orchestration.ReplanPlanningInput) (*orchestration.ReplanPlanningResult, error) {
	if r == nil {
		return nil, errors.New("replanner runtime is not configured")
	}
	botID := strings.TrimSpace(stringValue(input.Run.SourceMetadata["bot_id"]))
	if botID == "" {
		return &orchestration.ReplanPlanningResult{}, nil
	}
	cfg, _, _, err := r.buildBotRunConfig(ctx, botID, input.Run.OwnerSubject)
	if err != nil {
		return nil, err
	}
	envResources, err := r.loadEnvResourceCatalog(ctx, input.Run.TenantID)
	if err != nil {
		return nil, err
	}
	cfg.System = replanPlannerSystemPrompt
	cfg.Messages = []sdk.Message{sdk.UserMessage(buildReplanPlannerPrompt(input))}
	plan, err := r.generatePlannerToolPlan(ctx, cfg, plannerSubmitPlanToolName, true, false, envResources)
	if err != nil {
		return nil, fmt.Errorf("%w: decode replanner schema: %w", orchestration.ErrPlanningIntentInvalid, err)
	}
	return &orchestration.ReplanPlanningResult{
		Summary:    plan.Summary,
		ChildTasks: plan.ChildTasks,
	}, nil
}

func (r *Runtime) generatePlannerToolPlan(ctx context.Context, cfg agentpkg.RunConfig, toolName string, requireChildTasks bool, requireRootTask bool, envResources envResourceCatalog) (*orchestration.StartRunPlanningResult, error) {
	if !cfg.SupportsToolCall {
		return nil, errors.New("planner model must support tool calling")
	}
	logger := slog.Default()
	if r != nil && r.logger != nil {
		logger = r.logger
	}
	toolName = strings.TrimSpace(toolName)
	if toolName == "" {
		toolName = plannerSubmitPlanToolName
	}
	var submitted bool
	var submittedPlan *orchestration.StartRunPlanningResult
	submitErrors := make([]map[string]any, 0)
	var rootTask *orchestration.PlannedTaskSpec
	childTasks := make([]orchestration.PlannedTaskSpec, 0)
	defineRootTool := sdk.Tool{
		Name:        plannerDefineRootTaskToolName,
		Description: "Define the root task execution using flat fields. Use this exactly once for initial planning.",
		Parameters:  plannerFlatTaskSchema(false),
		Execute: func(execCtx *sdk.ToolExecContext, input any) (any, error) {
			toolCallID := ""
			if execCtx != nil {
				toolCallID = execCtx.ToolCallID
			}
			logger.Debug("planner define root task called",
				slog.String("tool", plannerDefineRootTaskToolName),
				slog.String("tool_call_id", toolCallID),
				slog.String("input", plannerDebugString(input, 4000)),
			)
			if !requireRootTask {
				err := errors.New("root task definition is only allowed for start-run planning")
				submitErrors = append(submitErrors, plannerToolErrorDebug(plannerDefineRootTaskToolName, toolCallID, input, err))
				return nil, err
			}
			if rootTask != nil {
				err := errors.New("define_root_task may only be called once")
				submitErrors = append(submitErrors, plannerToolErrorDebug(plannerDefineRootTaskToolName, toolCallID, input, err))
				logger.Warn("planner define root task rejected",
					slog.String("tool", plannerDefineRootTaskToolName),
					slog.String("tool_call_id", toolCallID),
					slog.Any("error", err),
				)
				return nil, err
			}
			task, err := plannerFlatTaskInputToSpec("root_task", plannerInputAsMap(input), envResources, false)
			if err != nil {
				submitErrors = append(submitErrors, plannerToolErrorDebug(plannerDefineRootTaskToolName, toolCallID, input, err))
				logger.Warn("planner define root task rejected",
					slog.String("tool", plannerDefineRootTaskToolName),
					slog.String("tool_call_id", toolCallID),
					slog.String("input", plannerDebugString(input, 4000)),
					slog.Any("error", err),
				)
				return nil, err
			}
			rootTask = &task
			logger.Debug("planner define root task accepted",
				slog.String("tool", plannerDefineRootTaskToolName),
				slog.String("tool_call_id", toolCallID),
				slog.Bool("env_required", task.EnvPreconditions.Required),
				slog.String("env_kind", task.EnvPreconditions.Kind),
				slog.String("env_resource_name", task.EnvPreconditions.ResourceName),
			)
			return map[string]any{"accepted": true}, nil
		},
	}
	addChildTool := sdk.Tool{
		Name:        plannerAddChildTaskToolName,
		Description: "Add one child task using flat fields. Call once per child task.",
		Parameters:  plannerFlatTaskSchema(true),
		Execute: func(execCtx *sdk.ToolExecContext, input any) (any, error) {
			toolCallID := ""
			if execCtx != nil {
				toolCallID = execCtx.ToolCallID
			}
			logger.Debug("planner add child task called",
				slog.String("tool", plannerAddChildTaskToolName),
				slog.String("tool_call_id", toolCallID),
				slog.String("input", plannerDebugString(input, 4000)),
			)
			task, err := plannerFlatTaskInputToSpec(fmt.Sprintf("child_tasks[%d]", len(childTasks)), plannerInputAsMap(input), envResources, true)
			if err != nil {
				submitErrors = append(submitErrors, plannerToolErrorDebug(plannerAddChildTaskToolName, toolCallID, input, err))
				logger.Warn("planner add child task rejected",
					slog.String("tool", plannerAddChildTaskToolName),
					slog.String("tool_call_id", toolCallID),
					slog.String("input", plannerDebugString(input, 4000)),
					slog.Any("error", err),
				)
				return nil, err
			}
			childTasks = append(childTasks, task)
			logger.Debug("planner add child task accepted",
				slog.String("tool", plannerAddChildTaskToolName),
				slog.String("tool_call_id", toolCallID),
				slog.String("alias", task.Alias),
				slog.Int("child_task_count", len(childTasks)),
			)
			return map[string]any{"accepted": true, "child_task_count": len(childTasks)}, nil
		},
	}
	submitTool := sdk.NewTool[plannerSubmitPlanInput](
		toolName,
		"Finalize the plan after defining the root task and/or adding child tasks. Only provide a summary.",
		func(execCtx *sdk.ToolExecContext, input plannerSubmitPlanInput) (any, error) {
			toolCallID := ""
			if execCtx != nil {
				toolCallID = execCtx.ToolCallID
			}
			logger.Debug("planner submit tool called",
				slog.String("tool", toolName),
				slog.String("tool_call_id", toolCallID),
				slog.String("input", plannerDebugString(input, 2000)),
			)
			if submitted {
				err := fmt.Errorf("%s may only be called once", toolName)
				submitErrors = append(submitErrors, plannerToolErrorDebug(toolName, toolCallID, input, err))
				logger.Warn("planner submit tool rejected",
					slog.String("tool", toolName),
					slog.String("tool_call_id", toolCallID),
					slog.Any("error", err),
				)
				return nil, err
			}
			if requireRootTask && rootTask == nil {
				err := errors.New("define_root_task must be called before submit_start_plan")
				submitErrors = append(submitErrors, plannerToolErrorDebug(toolName, toolCallID, input, err))
				logger.Warn("planner submit tool rejected",
					slog.String("tool", toolName),
					slog.String("tool_call_id", toolCallID),
					slog.Any("error", err),
				)
				return nil, err
			}
			if requireChildTasks && len(childTasks) == 0 {
				err := errors.New("child_tasks must contain at least one replacement task")
				submitErrors = append(submitErrors, plannerToolErrorDebug(toolName, toolCallID, input, err))
				logger.Warn("planner submit tool rejected",
					slog.String("tool", toolName),
					slog.String("tool_call_id", toolCallID),
					slog.String("input", plannerDebugString(input, 4000)),
					slog.Any("error", err),
				)
				return nil, err
			}
			submitted = true
			submittedPlan = &orchestration.StartRunPlanningResult{
				Summary:    strings.TrimSpace(input.Summary),
				RootTask:   rootTask,
				ChildTasks: append([]orchestration.PlannedTaskSpec(nil), childTasks...),
			}
			logger.Debug("planner submit tool accepted",
				slog.String("tool", toolName),
				slog.String("tool_call_id", toolCallID),
				slog.Bool("has_root_task", submittedPlan.RootTask != nil),
				slog.Int("child_task_count", len(submittedPlan.ChildTasks)),
			)
			return map[string]any{"accepted": true}, nil
		},
	)
	logger.Debug("planner generation starting",
		slog.String("submit_tool", toolName),
		slog.Bool("require_root_task", requireRootTask),
		slog.Bool("require_child_tasks", requireChildTasks),
		slog.Int("env_resource_count", len(envResources.PromptItems())),
		slog.Any("available_tools", []string{listEnvResourcesToolName, plannerDefineRootTaskToolName, plannerAddChildTaskToolName, toolName}),
	)
	tools := []sdk.Tool{newListEnvResourcesTool(envResources), addChildTool, submitTool}
	if requireRootTask {
		tools = append([]sdk.Tool{newListEnvResourcesTool(envResources), defineRootTool}, addChildTool, submitTool)
	}
	opts := []sdk.GenerateOption{
		sdk.WithModel(cfg.Model),
		sdk.WithSystem(cfg.System),
		sdk.WithMessages(cfg.Messages),
		sdk.WithTools(tools),
		sdk.WithToolChoice("auto"),
		sdk.WithMaxSteps(10),
	}
	opts = append(opts, models.BuildReasoningOptions(models.SDKModelConfig{
		ClientType: models.ResolveClientType(cfg.Model),
		ReasoningConfig: &models.ReasoningConfig{
			Enabled: cfg.ReasoningEffort != "",
			Effort:  cfg.ReasoningEffort,
		},
	})...)
	result, err := sdk.GenerateTextResult(ctx, opts...)
	if err != nil {
		logger.Warn("planner generation failed",
			slog.String("submit_tool", toolName),
			slog.Any("error", err),
		)
		return nil, err
	}
	if !submitted {
		logger.Warn("planner generation ended without accepted submit tool",
			slog.String("expected_tool", toolName),
			slog.String("finish_reason", string(result.FinishReason)),
			slog.String("raw_finish_reason", result.RawFinishReason),
			slog.String("text", truncatePlannerDebugString(result.Text, 1200)),
			slog.Any("submit_errors", submitErrors),
			slog.Any("steps", summarizePlannerGenerateSteps(result)),
		)
		return nil, fmt.Errorf("planner did not call %s; finish_reason=%s text=%q", toolName, result.FinishReason, strings.TrimSpace(result.Text))
	}
	if submittedPlan == nil {
		logger.Warn("planner submit tool was called without accepted plan",
			slog.String("expected_tool", toolName),
			slog.String("finish_reason", string(result.FinishReason)),
			slog.String("raw_finish_reason", result.RawFinishReason),
			slog.Any("submit_errors", submitErrors),
			slog.Any("steps", summarizePlannerGenerateSteps(result)),
		)
		return nil, fmt.Errorf("planner submitted %s but no plan was accepted", toolName)
	}
	logger.Debug("planner generation completed",
		slog.String("submit_tool", toolName),
		slog.String("finish_reason", string(result.FinishReason)),
		slog.String("raw_finish_reason", result.RawFinishReason),
		slog.Bool("has_root_task", submittedPlan.RootTask != nil),
		slog.Int("child_task_count", len(submittedPlan.ChildTasks)),
		slog.Any("steps", summarizePlannerGenerateSteps(result)),
	)
	return submittedPlan, nil
}

func plannerToolErrorDebug(toolName, toolCallID string, input any, err error) map[string]any {
	item := map[string]any{
		"tool":         toolName,
		"tool_call_id": toolCallID,
		"input":        plannerDebugString(input, 4000),
	}
	if err != nil {
		item["error"] = err.Error()
	}
	return item
}

func plannerFlatTaskSchema(requireAlias bool) map[string]any {
	required := []string{"goal", "env_required"}
	if requireAlias {
		required = append([]string{"alias"}, required...)
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"alias":             map[string]any{"type": "string", "description": "Stable alias for this child task. Required for child tasks."},
			"kind":              map[string]any{"type": "string", "description": "Optional task kind, such as task or step."},
			"goal":              map[string]any{"type": "string", "description": "Executable task goal."},
			"depends_on":        map[string]any{"type": "string", "description": "Comma-separated child task aliases this child depends on."},
			"worker_profile":    map[string]any{"type": "string", "description": "Worker profile. Usually llm.default."},
			"priority":          map[string]any{"type": "integer", "description": "Optional priority."},
			"verification_mode": map[string]any{"type": "string", "description": "Optional verification policy mode."},
			"blackboard_scope":  map[string]any{"type": "string", "description": "Optional blackboard scope."},
			"env_required":      map[string]any{"type": "boolean", "description": "Whether this task needs a runtime environment."},
			"env_kind":          map[string]any{"type": "string", "enum": []string{"", orchestration.EnvPreconditionsKindContainer, orchestration.EnvPreconditionsKindBrowser}, "description": "Environment kind when env_required is true."},
			"env_resource_name": map[string]any{"type": "string", "description": "Active env resource name selected from list_env_resources when env_required is true."},
			"env_mode":          map[string]any{"type": "string", "description": "Optional env mode. For browser use context by default; exclusive only for dedicated long-lived browser sessions."},
			"env_effect_class":  map[string]any{"type": "string", "description": "Optional env effect class."},
		},
		"required":             required,
		"additionalProperties": false,
	}
}

func plannerInputAsMap(input any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	if args, ok := input.(map[string]any); ok {
		return args
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return map[string]any{}
	}
	var args map[string]any
	_ = json.Unmarshal(raw, &args)
	if args == nil {
		args = map[string]any{}
	}
	return args
}

func plannerFlatTaskInputToSpec(path string, args map[string]any, catalog envResourceCatalog, requireAlias bool) (orchestration.PlannedTaskSpec, error) {
	alias := plannerFlatString(args, "alias")
	if requireAlias && alias == "" {
		return orchestration.PlannedTaskSpec{}, fmt.Errorf("%s: alias is required", path)
	}
	goal := plannerFlatString(args, "goal")
	if goal == "" {
		return orchestration.PlannedTaskSpec{}, fmt.Errorf("%s: goal is required", path)
	}
	workerProfile := plannerFlatString(args, "worker_profile")
	if strings.EqualFold(workerProfile, "browser-capable") {
		return orchestration.PlannedTaskSpec{}, fmt.Errorf("%s: worker_profile %q is invalid; browser access must be declared with env_kind=%q", path, workerProfile, orchestration.EnvPreconditionsKindBrowser)
	}
	envPreconditions, err := plannerFlatEnvPreconditions(args, catalog)
	if err != nil {
		return orchestration.PlannedTaskSpec{}, fmt.Errorf("%s: %w", path, err)
	}
	verificationPolicy := map[string]any{}
	if mode := plannerFlatString(args, "verification_mode"); mode != "" {
		verificationPolicy["mode"] = mode
	}
	return orchestration.PlannedTaskSpec{
		Alias:              alias,
		Kind:               plannerFlatString(args, "kind"),
		Goal:               goal,
		Inputs:             map[string]any{},
		DependsOnAliases:   plannerFlatStringList(args, "depends_on"),
		WorkerProfile:      workerProfile,
		Priority:           plannerFlatInt(args, "priority"),
		RetryPolicy:        map[string]any{},
		VerificationPolicy: verificationPolicy,
		EnvPreconditions:   envPreconditions,
		BlackboardScope:    plannerFlatString(args, "blackboard_scope"),
	}, nil
}

func plannerFlatEnvPreconditions(args map[string]any, catalog envResourceCatalog) (orchestration.EnvPreconditions, error) {
	required := plannerFlatBool(args, "env_required")
	if !required {
		return orchestration.EnvPreconditions{Required: false}, nil
	}
	kind := plannerFlatString(args, "env_kind")
	switch kind {
	case orchestration.EnvPreconditionsKindContainer, orchestration.EnvPreconditionsKindBrowser:
	case "":
		return orchestration.EnvPreconditions{}, errors.New("env_kind is required when env_required=true")
	default:
		return orchestration.EnvPreconditions{}, fmt.Errorf("env_kind must be %q or %q", orchestration.EnvPreconditionsKindContainer, orchestration.EnvPreconditionsKindBrowser)
	}
	resourceName := plannerFlatString(args, "env_resource_name")
	if resourceName == "" {
		return orchestration.EnvPreconditions{}, errors.New("env_resource_name is required when env_required=true")
	}
	if err := validateEnvResourceReference(catalog, kind, resourceName); err != nil {
		return orchestration.EnvPreconditions{}, err
	}
	return orchestration.EnvPreconditions{
		Required:     true,
		Kind:         kind,
		ResourceName: resourceName,
		Mode:         plannerFlatEnvMode(args, kind),
		EffectClass:  plannerFlatString(args, "env_effect_class"),
	}, nil
}

func plannerFlatEnvMode(args map[string]any, kind string) string {
	mode := plannerFlatString(args, "env_mode")
	if kind == orchestration.EnvPreconditionsKindBrowser && mode == "" {
		return orchestration.EnvPreconditionsModeContext
	}
	return mode
}

func plannerFlatString(args map[string]any, key string) string {
	raw, ok := args[key]
	if !ok || raw == nil {
		return ""
	}
	value, ok := raw.(string)
	if !ok {
		return strings.TrimSpace(fmt.Sprintf("%v", raw))
	}
	return strings.TrimSpace(value)
}

func plannerFlatBool(args map[string]any, key string) bool {
	raw, ok := args[key]
	if !ok || raw == nil {
		return false
	}
	value, ok := raw.(bool)
	return ok && value
}

func plannerFlatInt(args map[string]any, key string) int {
	raw, ok := args[key]
	if !ok || raw == nil {
		return 0
	}
	switch value := raw.(type) {
	case int:
		return value
	case int32:
		return int(value)
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}

func plannerFlatStringList(args map[string]any, key string) []string {
	raw := plannerFlatString(args, key)
	if raw == "" {
		return nil
	}
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == ';'
	})
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		trimmed := strings.TrimSpace(field)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func summarizePlannerGenerateSteps(result *sdk.GenerateResult) []map[string]any {
	if result == nil {
		return nil
	}
	steps := make([]map[string]any, 0, len(result.Steps))
	for i, step := range result.Steps {
		item := map[string]any{
			"index":             i,
			"finish_reason":     step.FinishReason,
			"raw_finish_reason": step.RawFinishReason,
			"text":              truncatePlannerDebugString(strings.TrimSpace(step.Text), 1200),
			"reasoning_len":     len(step.Reasoning),
		}
		if len(step.ToolCalls) > 0 {
			calls := make([]map[string]any, 0, len(step.ToolCalls))
			for _, call := range step.ToolCalls {
				calls = append(calls, map[string]any{
					"tool_call_id": call.ToolCallID,
					"tool":         call.ToolName,
					"input":        plannerDebugString(call.Input, 4000),
				})
			}
			item["tool_calls"] = calls
		}
		if len(step.ToolResults) > 0 {
			results := make([]map[string]any, 0, len(step.ToolResults))
			for _, result := range step.ToolResults {
				results = append(results, map[string]any{
					"tool_call_id": result.ToolCallID,
					"tool":         result.ToolName,
					"input":        plannerDebugString(result.Input, 2000),
					"output":       plannerDebugString(result.Output, 4000),
				})
			}
			item["tool_results"] = results
		}
		steps = append(steps, item)
	}
	return steps
}

func plannerDebugString(value any, limit int) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return truncatePlannerDebugString(text, limit)
	}
	data, err := json.Marshal(value)
	if err != nil {
		return truncatePlannerDebugString(fmt.Sprintf("%v", value), limit)
	}
	return truncatePlannerDebugString(string(data), limit)
}

func truncatePlannerDebugString(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	if limit <= 3 {
		return value[:limit]
	}
	return value[:limit-3] + "..."
}

func buildStartRunPlannerPrompt(input orchestration.StartRunPlanningInput) string {
	return buildOrchestrationUserPrompt("start_run_planning",
		orchestrationPromptSection{
			name: "run",
			value: map[string]any{
				"run_id":          input.Run.ID,
				"tenant_id":       input.Run.TenantID,
				"owner_subject":   input.Run.OwnerSubject,
				"goal":            input.Run.Goal,
				"input":           input.Run.Input,
				"output_schema":   input.Run.OutputSchema,
				"source_metadata": input.Run.SourceMetadata,
			},
		},
		orchestrationPromptSection{
			name: "root_task",
			value: map[string]any{
				"task_id":           input.RootTask.ID,
				"goal":              input.RootTask.Goal,
				"inputs":            input.RootTask.Inputs,
				"worker_profile":    input.RootTask.WorkerProfile,
				"env_preconditions": input.RootTask.EnvPreconditions,
			},
		},
	)
}

func buildReplanPlannerPrompt(input orchestration.ReplanPlanningInput) string {
	return buildOrchestrationUserPrompt("replanning",
		orchestrationPromptSection{
			name: "run",
			value: map[string]any{
				"run_id":        input.Run.ID,
				"tenant_id":     input.Run.TenantID,
				"owner_subject": input.Run.OwnerSubject,
				"goal":          input.Run.Goal,
				"input":         input.Run.Input,
				"output_schema": input.Run.OutputSchema,
				"planner_epoch": input.Run.PlannerEpoch,
			},
		},
		orchestrationPromptSection{name: "source_task", value: input.SourceTask},
		orchestrationPromptSection{name: "source_attempt", value: replanPromptAttempt(input.SourceAttempt)},
		orchestrationPromptSection{name: "source_result", value: input.SourceResult},
		orchestrationPromptSection{name: "subtree_tasks", value: input.SubtreeTasks},
		orchestrationPromptSection{name: "dependencies", value: input.Dependencies},
		orchestrationPromptSection{name: "reason", value: input.Reason},
		orchestrationPromptSection{name: "injected_hint", value: input.InjectedHint},
	)
}

func replanPromptAttempt(attempt *orchestration.TaskAttempt) any {
	if attempt == nil {
		return nil
	}
	return map[string]any{
		"id":              attempt.ID,
		"task_id":         attempt.TaskID,
		"attempt_no":      attempt.AttemptNo,
		"status":          attempt.Status,
		"failure_class":   attempt.FailureClass,
		"terminal_reason": attempt.TerminalReason,
		"started_at":      attempt.StartedAt,
		"finished_at":     attempt.FinishedAt,
	}
}

const startRunPlannerSystemPrompt = `You are the initial planner for a Memoh orchestration run.

Decide whether the root goal should start as a single executable task or first be decomposed into a small DAG of leaf tasks.

Rules:
- Always call define_root_task once. Treat the provided root task as a draft record; define_root_task is the final execution definition when no child tasks are needed.
- If the goal is already a single concrete task, do not call add_child_task. Define the root task with the exact execution requirements for that task.
- If the goal obviously contains multiple stages, validation gates, or parallelizable branches, decompose it.
- Only output executable leaf tasks. Do not output abstract manager/planner/meta tasks.
- Keep the graph small and useful. Prefer 2-5 child tasks unless the request clearly needs more.
- Use add_child_task once per child task. Use the flat depends_on field as a comma-separated alias list to form an acyclic DAG.
- Default worker_profile should usually be "llm.default". Do not invent profiles such as "browser-capable"; browser/container access is declared with env_preconditions, not worker_profile.
- Use verification_mode only when a child clearly needs an explicit verifier gate.
- Env requirements are flat tool fields: env_required, env_kind, env_resource_name, env_mode, env_effect_class. Set env_required=false for pure-LLM steps. Set env_required=true with env_kind="container" or env_kind="browser" only when the task must touch an external runtime (file edits, shell commands, browser navigation). For browser tasks use env_mode="context" by default; use env_mode="exclusive" only when the task explicitly needs a dedicated long-lived browser.
- Before setting env_required=true, call list_env_resources and choose env_resource_name from the returned active resources. Do not invent resource names.
- Finalize the initial plan by calling submit_start_plan exactly once with only a summary. Do not answer in chat after the tool call.`

const replanPlannerSystemPrompt = `You are the replanner for a Memoh orchestration run.

Replace the provided source task subtree with a small executable DAG that can recover from the reported failure, verification rejection, or explicit replan reason.

Rules:
- Output only replacement child tasks for the source task. Do not include the superseded source task itself.
- Use the source result, failed attempt, subtree tasks, and dependencies to preserve useful completed work only when it is represented as input context.
- Only output executable leaf tasks. Do not output abstract manager/planner/meta tasks.
- Keep the replacement graph small and useful. Prefer 1-5 child tasks unless the failure clearly requires more.
- Use add_child_task once per replacement child task. Use the flat depends_on field as a comma-separated alias list to form an acyclic DAG.
- Default worker_profile should usually be "llm.default". Do not invent profiles such as "browser-capable"; browser/container access is declared with env_preconditions, not worker_profile.
- Use verification_mode only when a replacement child clearly needs an explicit verifier gate.
- Env requirements are flat tool fields: env_required, env_kind, env_resource_name, env_mode, env_effect_class. Set env_required=false for pure-LLM steps. Set env_required=true with env_kind="container" or env_kind="browser" only when the task must touch an external runtime. For browser tasks use env_mode="context" by default; use env_mode="exclusive" only when the task explicitly needs a dedicated long-lived browser. Carry the same env requirement the original task had unless the replan reason explicitly demands a different one.
- Finalize the replacement plan by calling submit_plan exactly once with only a summary. At least one add_child_task call is required. If no safe replacement can be produced, add the smallest safe diagnostic task. Do not answer in chat after the tool call.`
