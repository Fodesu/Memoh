package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	sdk "github.com/memohai/twilight-ai/sdk"

	"github.com/memohai/memoh/internal/bots"
	"github.com/memohai/memoh/internal/orchestration"
)

type fakeOrchestrationToolService struct {
	startRun           func(context.Context, orchestration.ControlIdentity, orchestration.StartRunRequest) (orchestration.RunHandle, error)
	cancelRun          func(context.Context, orchestration.ControlIdentity, string, orchestration.CancelRunRequest) (*orchestration.CancelRunResult, error)
	resumeRun          func(context.Context, orchestration.ControlIdentity, string, orchestration.ResumeRunRequest) (*orchestration.ResumeRunResult, error)
	getRunSnapshot     func(context.Context, orchestration.ControlIdentity, string) (*orchestration.RunSnapshot, error)
	listRunTasks       func(context.Context, orchestration.ControlIdentity, string, orchestration.ListRunTasksRequest) (*orchestration.TaskPage, error)
	listRunCheckpoints func(context.Context, orchestration.ControlIdentity, string, orchestration.ListRunCheckpointsRequest) (*orchestration.HumanCheckpointPage, error)
	listRunResults     func(context.Context, orchestration.ControlIdentity, string, orchestration.ListRunResultsRequest) (*orchestration.TaskResultPage, error)
	listRunArtifacts   func(context.Context, orchestration.ControlIdentity, string, orchestration.ListRunArtifactsRequest) (*orchestration.ArtifactPage, error)
	resolveCheckpoint  func(context.Context, orchestration.ControlIdentity, string, orchestration.CheckpointResolution) (*orchestration.ResolveCheckpointResult, error)
}

func (f fakeOrchestrationToolService) StartRun(ctx context.Context, caller orchestration.ControlIdentity, req orchestration.StartRunRequest) (orchestration.RunHandle, error) {
	return f.startRun(ctx, caller, req)
}

func (f fakeOrchestrationToolService) CancelRun(ctx context.Context, caller orchestration.ControlIdentity, runID string, req orchestration.CancelRunRequest) (*orchestration.CancelRunResult, error) {
	return f.cancelRun(ctx, caller, runID, req)
}

func (f fakeOrchestrationToolService) ResumeRun(ctx context.Context, caller orchestration.ControlIdentity, runID string, req orchestration.ResumeRunRequest) (*orchestration.ResumeRunResult, error) {
	return f.resumeRun(ctx, caller, runID, req)
}

func (f fakeOrchestrationToolService) GetRunSnapshot(ctx context.Context, caller orchestration.ControlIdentity, runID string) (*orchestration.RunSnapshot, error) {
	return f.getRunSnapshot(ctx, caller, runID)
}

func (f fakeOrchestrationToolService) ListRunTasks(ctx context.Context, caller orchestration.ControlIdentity, runID string, req orchestration.ListRunTasksRequest) (*orchestration.TaskPage, error) {
	return f.listRunTasks(ctx, caller, runID, req)
}

func (f fakeOrchestrationToolService) ListRunCheckpoints(ctx context.Context, caller orchestration.ControlIdentity, runID string, req orchestration.ListRunCheckpointsRequest) (*orchestration.HumanCheckpointPage, error) {
	return f.listRunCheckpoints(ctx, caller, runID, req)
}

func (f fakeOrchestrationToolService) ListRunResults(ctx context.Context, caller orchestration.ControlIdentity, runID string, req orchestration.ListRunResultsRequest) (*orchestration.TaskResultPage, error) {
	return f.listRunResults(ctx, caller, runID, req)
}

func (f fakeOrchestrationToolService) ListRunArtifacts(ctx context.Context, caller orchestration.ControlIdentity, runID string, req orchestration.ListRunArtifactsRequest) (*orchestration.ArtifactPage, error) {
	return f.listRunArtifacts(ctx, caller, runID, req)
}

func (f fakeOrchestrationToolService) ResolveCheckpoint(ctx context.Context, caller orchestration.ControlIdentity, checkpointID string, req orchestration.CheckpointResolution) (*orchestration.ResolveCheckpointResult, error) {
	return f.resolveCheckpoint(ctx, caller, checkpointID, req)
}

type fakeOrchestrationBotReader struct {
	get func(context.Context, string) (bots.Bot, error)
}

func (f fakeOrchestrationBotReader) Get(ctx context.Context, botID string) (bots.Bot, error) {
	return f.get(ctx, botID)
}

func TestStartOrchestrationRunToolUsesOwnerSessionIdentity(t *testing.T) {
	var idempotencyKeys []string
	var capturedInput map[string]any
	provider := NewOrchestrationProvider(nil, fakeOrchestrationToolService{
		startRun: func(_ context.Context, caller orchestration.ControlIdentity, req orchestration.StartRunRequest) (orchestration.RunHandle, error) {
			if caller != (orchestration.ControlIdentity{TenantID: "owner-1", Subject: "owner-1"}) {
				t.Fatalf("caller = %+v", caller)
			}
			if req.BotID != "bot-1" {
				t.Fatalf("bot_id = %q", req.BotID)
			}
			if req.Goal != "ship orchestration tool" {
				t.Fatalf("goal = %q", req.Goal)
			}
			if req.IdempotencyKey == "" {
				t.Fatal("idempotency_key = empty, want generated value")
			}
			idempotencyKeys = append(idempotencyKeys, req.IdempotencyKey)
			capturedInput = req.Input
			if got := req.SourceMetadata["bot_id"]; got != "bot-1" {
				t.Fatalf("source_metadata.bot_id = %#v, want %q", got, "bot-1")
			}
			return orchestration.RunHandle{RunID: "run-1", RootTaskID: "task-1", SnapshotSeq: 7}, nil
		},
	}, fakeOrchestrationBotReader{
		get: func(_ context.Context, botID string) (bots.Bot, error) {
			if botID != "bot-1" {
				t.Fatalf("botID = %q", botID)
			}
			return bots.Bot{ID: botID, OwnerUserID: "owner-1"}, nil
		},
	})

	tools, err := provider.Tools(context.Background(), SessionContext{
		BotID: "bot-1",
		Attachments: []Attachment{{
			Type:        "image",
			ContentHash: "hash-1",
			Name:        "diagram.png",
			Mime:        "image/png",
			Size:        123,
		}},
	})
	if err != nil {
		t.Fatalf("Tools() error = %v", err)
	}
	startTool := findTool(t, tools, "start_orchestration_run")
	execCtx := &sdk.ToolExecContext{ToolCallID: "call-start-1"}
	result, err := startTool.Execute(execCtx, map[string]any{
		"goal": "ship orchestration tool",
	})
	if err != nil {
		t.Fatalf("start_orchestration_run error = %v", err)
	}
	payload := result.(map[string]any)
	if payload["run_id"] != "run-1" {
		t.Fatalf("run_id = %#v, want %q", payload["run_id"], "run-1")
	}
	attachments, ok := capturedInput["attachments"].([]map[string]any)
	if !ok || len(attachments) != 1 {
		t.Fatalf("input.attachments = %#v, want one attachment ref", capturedInput["attachments"])
	}
	if got := attachments[0]["content_hash"]; got != "hash-1" {
		t.Fatalf("input.attachments[0].content_hash = %#v, want %q", got, "hash-1")
	}
	_, err = startTool.Execute(execCtx, map[string]any{
		"goal": "ship orchestration tool",
	})
	if err != nil {
		t.Fatalf("retry start_orchestration_run error = %v", err)
	}
	if len(idempotencyKeys) != 2 || idempotencyKeys[0] != idempotencyKeys[1] {
		t.Fatalf("idempotency keys = %#v, want stable key for same tool call", idempotencyKeys)
	}
}

func TestGetOrchestrationStatusToolSummarizesRun(t *testing.T) {
	provider := NewOrchestrationProvider(nil, fakeOrchestrationToolService{
		getRunSnapshot: func(_ context.Context, _ orchestration.ControlIdentity, runID string) (*orchestration.RunSnapshot, error) {
			return &orchestration.RunSnapshot{
				Run: orchestration.Run{
					ID:              runID,
					Goal:            "inspect run",
					LifecycleStatus: orchestration.LifecycleStatusWaitingHuman,
					PlanningStatus:  orchestration.PlanningStatusIdle,
				},
				SnapshotSeq: 12,
			}, nil
		},
		listRunTasks: func(_ context.Context, _ orchestration.ControlIdentity, _ string, req orchestration.ListRunTasksRequest) (*orchestration.TaskPage, error) {
			if req.AsOfSeq != 12 {
				t.Fatalf("as_of_seq = %d, want %d", req.AsOfSeq, 12)
			}
			return &orchestration.TaskPage{Items: []orchestration.Task{
				{ID: "task-a", Goal: "wait for approval", Status: orchestration.TaskStatusWaitingHuman, WaitingCheckpointID: "cp-1", WaitingScope: "run"},
				{ID: "task-b", Goal: "done", Status: orchestration.TaskStatusCompleted},
				{ID: "task-c", DecomposedFromTaskID: "task-a", Goal: "blocked by dependency", Status: orchestration.TaskStatusBlocked, BlockedReason: "dependency failed"},
				{ID: "task-d", Goal: "cancelled branch", Status: orchestration.TaskStatusCancelled, TerminalReason: "run cancelled"},
				{ID: "task-e", Goal: "actual failure", Status: orchestration.TaskStatusFailed, TerminalReason: "worker failed"},
				{ID: "task-f", DecomposedFromTaskID: "task-a", Role: orchestration.TaskRoleFinal, Goal: "active child", Status: orchestration.TaskStatusRunning, WorkerProfile: orchestration.DefaultWorkerProfile, LatestResultID: "result-1"},
			}}, nil
		},
		listRunCheckpoints: func(_ context.Context, _ orchestration.ControlIdentity, _ string, req orchestration.ListRunCheckpointsRequest) (*orchestration.HumanCheckpointPage, error) {
			if req.AsOfSeq != 12 {
				t.Fatalf("checkpoint as_of_seq = %d, want %d", req.AsOfSeq, 12)
			}
			return &orchestration.HumanCheckpointPage{Items: []orchestration.HumanCheckpoint{
				{
					ID:        "cp-1",
					TaskID:    "task-a",
					Question:  "approve?",
					Status:    orchestration.CheckpointStatusOpen,
					BlocksRun: true,
					Options:   []orchestration.CheckpointOption{{ID: "approve", Kind: "choice", Label: "Approve"}},
					DefaultAction: &orchestration.CheckpointDefaultAction{
						Option: "approve",
					},
					ResumePolicy: &orchestration.CheckpointResumePolicy{ResumeMode: orchestration.CheckpointResumeModeNewAttempt},
				},
			}}, nil
		},
		listRunResults: func(_ context.Context, _ orchestration.ControlIdentity, _ string, req orchestration.ListRunResultsRequest) (*orchestration.TaskResultPage, error) {
			if req.AsOfSeq != 12 {
				t.Fatalf("result as_of_seq = %d, want %d", req.AsOfSeq, 12)
			}
			return &orchestration.TaskResultPage{Items: []orchestration.TaskResult{
				{
					ID:        "result-1",
					TaskID:    "task-f",
					AttemptID: "attempt-1",
					Status:    "completed",
					Summary:   "implemented feature",
					StructuredOutput: map[string]any{
						"changed_files": []any{"main.go"},
					},
				},
			}}, nil
		},
		listRunArtifacts: func(_ context.Context, _ orchestration.ControlIdentity, _ string, req orchestration.ListRunArtifactsRequest) (*orchestration.ArtifactPage, error) {
			if req.AsOfSeq != 12 {
				t.Fatalf("artifact as_of_seq = %d, want %d", req.AsOfSeq, 12)
			}
			return &orchestration.ArtifactPage{Items: []orchestration.Artifact{
				{
					ID:          "artifact-1",
					TaskID:      "task-f",
					AttemptID:   "attempt-1",
					Kind:        "file",
					URI:         "workspace://main.go",
					Version:     "v1",
					Digest:      "sha256:abc",
					ContentType: "text/x-go",
					Summary:     "generated source file",
				},
			}}, nil
		},
	}, fakeOrchestrationBotReader{
		get: func(_ context.Context, _ string) (bots.Bot, error) {
			return bots.Bot{ID: "bot-1", OwnerUserID: "owner-1"}, nil
		},
	})

	tools, err := provider.Tools(context.Background(), SessionContext{BotID: "bot-1"})
	if err != nil {
		t.Fatalf("Tools() error = %v", err)
	}
	result, err := findTool(t, tools, "get_orchestration_status").Execute(nil, map[string]any{
		"run_id": "run-1",
	})
	if err != nil {
		t.Fatalf("get_orchestration_status error = %v", err)
	}
	payload := result.(orchestrationStatusResult)
	if payload.Status != orchestration.LifecycleStatusWaitingHuman {
		t.Fatalf("status = %#v", payload.Status)
	}
	if payload.Summary.Completed != 1 {
		t.Fatalf("summary.completed = %d, want %d", payload.Summary.Completed, 1)
	}
	if payload.Summary.Failed != 1 {
		t.Fatalf("summary.failed = %d, want %d", payload.Summary.Failed, 1)
	}
	if payload.Summary.Blocked != 1 || payload.Summary.Cancelled != 1 || payload.Summary.WaitingHuman != 1 || payload.Summary.Active != 1 {
		t.Fatalf("summary = %+v", payload.Summary)
	}
	if !payload.Verdict.NeedsHuman || payload.Verdict.IsTerminal || payload.Verdict.NextAction != "handle_checkpoint" || payload.Verdict.ReasonCode != "awaiting_checkpoint" {
		t.Fatalf("verdict = %+v", payload.Verdict)
	}
	if len(payload.ActiveTasks) != 2 || payload.ActiveTasks[0].ID != "task-a" || payload.ActiveTasks[1].ID != "task-f" {
		t.Fatalf("active tasks = %+v", payload.ActiveTasks)
	}
	if payload.ActiveTasks[1].ParentTaskID != "task-a" {
		t.Fatalf("active parent_task_id = %q, want %q", payload.ActiveTasks[1].ParentTaskID, "task-a")
	}
	if payload.ActiveTasks[1].Role != orchestration.TaskRoleFinal {
		t.Fatalf("active role = %q, want %q", payload.ActiveTasks[1].Role, orchestration.TaskRoleFinal)
	}
	if len(payload.FailedTasks) != 2 || payload.FailedTasks[0].ID != "task-c" || payload.FailedTasks[1].ID != "task-e" {
		t.Fatalf("failed tasks = %+v", payload.FailedTasks)
	}
	if len(payload.OpenCheckpoints) != 1 {
		t.Fatalf("len(open_checkpoints) = %d, want 1", len(payload.OpenCheckpoints))
	}
	if payload.OpenCheckpoints[0].ResolutionHint != "default_available" {
		t.Fatalf("resolution_hint = %q", payload.OpenCheckpoints[0].ResolutionHint)
	}
	if payload.OpenCheckpoints[0].DefaultAction == nil {
		t.Fatal("default_action = nil, want populated checkpoint resolution hint")
	}
	if payload.OpenCheckpoints[0].ResumePolicy == nil {
		t.Fatal("resume_policy = nil, want populated resume policy")
	}
	if options := payload.OpenCheckpoints[0].Options; len(options) != 1 || options[0].ID != "approve" {
		t.Fatalf("checkpoint options = %#v", options)
	}
	if len(payload.UsableResults) != 1 || payload.UsableResults[0].Summary != "implemented feature" {
		t.Fatalf("usable_results = %+v", payload.UsableResults)
	}
	if payload.UsableResults[0].StructuredOutput["changed_files"] == nil {
		t.Fatalf("result structured_output = %#v", payload.UsableResults[0].StructuredOutput)
	}
	if len(payload.Artifacts) != 1 || payload.Artifacts[0].URI != "workspace://main.go" {
		t.Fatalf("artifacts = %+v", payload.Artifacts)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal status result: %v", err)
	}
	jsonPayload := string(encoded)
	if strings.Contains(jsonPayload, "snapshot_seq") {
		t.Fatalf("status result leaked snapshot_seq: %s", jsonPayload)
	}
	if strings.Contains(jsonPayload, `"seq"`) {
		t.Fatalf("status result leaked event seq: %s", jsonPayload)
	}
	assertJSONPartOrder(t, jsonPayload,
		`{"status_message":`,
		`,"run_id":`,
		`,"goal":`,
		`,"status":`,
		`,"verdict":`,
		`,"summary":`,
		`,"open_checkpoints":`,
		`,"active_tasks":`,
		`,"failed_tasks":`,
		`,"usable_results":`,
		`,"artifacts":`,
	)
	activeTaskIndex := strings.Index(jsonPayload, `{"id":"task-f"`)
	if activeTaskIndex < 0 {
		t.Fatalf("active task JSON missing: %s", jsonPayload)
	}
	activeTaskEnd := strings.Index(jsonPayload[activeTaskIndex:], `}]`)
	if activeTaskEnd < 0 {
		t.Fatalf("active task JSON end missing: %s", jsonPayload)
	}
	assertJSONFieldOrder(t, jsonPayload[activeTaskIndex:activeTaskIndex+activeTaskEnd], "id", "parent_task_id", "status", "role", "goal", "worker_profile", "latest_result_id")
	if strings.Contains(jsonPayload, "lifecycle_status") {
		t.Fatalf("status result leaked lifecycle_status: %s", jsonPayload)
	}
	if strings.Contains(jsonPayload, "recent_events") {
		t.Fatalf("status result leaked recent_events: %s", jsonPayload)
	}
}

func TestOrchestrationToolsHiddenForSubagent(t *testing.T) {
	provider := NewOrchestrationProvider(nil, fakeOrchestrationToolService{}, fakeOrchestrationBotReader{
		get: func(context.Context, string) (bots.Bot, error) {
			return bots.Bot{}, nil
		},
	})

	tools, err := provider.Tools(context.Background(), SessionContext{BotID: "bot-1", IsSubagent: true})
	if err != nil {
		t.Fatalf("Tools() error = %v", err)
	}
	if len(tools) != 0 {
		t.Fatalf("len(tools) = %d, want 0", len(tools))
	}
}

func TestCancelOrchestrationRunToolForwardsRequest(t *testing.T) {
	provider := NewOrchestrationProvider(nil, fakeOrchestrationToolService{
		cancelRun: func(_ context.Context, caller orchestration.ControlIdentity, runID string, req orchestration.CancelRunRequest) (*orchestration.CancelRunResult, error) {
			if caller != (orchestration.ControlIdentity{TenantID: "owner-1", Subject: "owner-1"}) {
				t.Fatalf("caller = %+v", caller)
			}
			if runID != "run-1" {
				t.Fatalf("runID = %q", runID)
			}
			if req.IdempotencyKey == "" {
				t.Fatal("idempotency_key = empty, want generated value")
			}
			return &orchestration.CancelRunResult{
				RunID:           runID,
				LifecycleStatus: orchestration.LifecycleStatusCancelling,
				SnapshotSeq:     19,
			}, nil
		},
	}, fakeOrchestrationBotReader{
		get: func(_ context.Context, _ string) (bots.Bot, error) {
			return bots.Bot{ID: "bot-1", OwnerUserID: "owner-1"}, nil
		},
	})

	tools, err := provider.Tools(context.Background(), SessionContext{BotID: "bot-1", ChannelIdentityID: "owner-1"})
	if err != nil {
		t.Fatalf("Tools() error = %v", err)
	}
	result, err := findTool(t, tools, "cancel_orchestration_run").Execute(nil, map[string]any{
		"run_id": "run-1",
	})
	if err != nil {
		t.Fatalf("cancel_orchestration_run error = %v", err)
	}
	payload := result.(map[string]any)
	if payload["lifecycle_status"] != orchestration.LifecycleStatusCancelling {
		t.Fatalf("lifecycle_status = %#v, want %q", payload["lifecycle_status"], orchestration.LifecycleStatusCancelling)
	}
	if payload["snapshot_seq"] != uint64(19) {
		t.Fatalf("snapshot_seq = %#v, want %d", payload["snapshot_seq"], 19)
	}
}

func TestOrchestrationToolsRequireOwnerLookup(t *testing.T) {
	provider := NewOrchestrationProvider(nil, fakeOrchestrationToolService{
		startRun: func(context.Context, orchestration.ControlIdentity, orchestration.StartRunRequest) (orchestration.RunHandle, error) {
			return orchestration.RunHandle{}, nil
		},
	}, fakeOrchestrationBotReader{
		get: func(context.Context, string) (bots.Bot, error) {
			return bots.Bot{}, errors.New("bot missing")
		},
	})

	tools, err := provider.Tools(context.Background(), SessionContext{BotID: "bot-1"})
	if err != nil {
		t.Fatalf("Tools() error = %v", err)
	}
	_, err = findTool(t, tools, "start_orchestration_run").Execute(nil, map[string]any{
		"goal": "ship orchestration tool",
	})
	if err == nil {
		t.Fatal("start_orchestration_run error = nil, want owner lookup failure")
	}
}

func TestOrchestrationToolsExposeFocusedSchemas(t *testing.T) {
	provider := NewOrchestrationProvider(nil, fakeOrchestrationToolService{}, fakeOrchestrationBotReader{
		get: func(context.Context, string) (bots.Bot, error) {
			return bots.Bot{ID: "bot-1", OwnerUserID: "owner-1"}, nil
		},
	})

	tools, err := provider.Tools(context.Background(), SessionContext{BotID: "bot-1", ChannelIdentityID: "owner-1"})
	if err != nil {
		t.Fatalf("Tools() error = %v", err)
	}
	if len(tools) != 6 {
		t.Fatalf("len(tools) = %d, want 6", len(tools))
	}
	want := []string{
		"start_orchestration_run",
		"get_orchestration_status",
		"resolve_orchestration_checkpoint",
		"request_human_checkpoint_decision",
		"resume_orchestration_run",
		"cancel_orchestration_run",
	}
	for _, name := range want {
		if findTool(t, tools, name).Name != name {
			t.Fatalf("tool %q not found", name)
		}
	}

	startTool := findTool(t, tools, "start_orchestration_run")
	assertRequiredFields(t, startTool, "goal")
	assertToolProperties(t, startTool, "goal")
	assertRequiredFields(t, findTool(t, tools, "get_orchestration_status"), "run_id")
	assertRequiredFields(t, findTool(t, tools, "resolve_orchestration_checkpoint"), "checkpoint_id", "option")
	assertRequiredFields(t, findTool(t, tools, "request_human_checkpoint_decision"), "checkpoint_id", "run_id")
	assertRequiredFields(t, findTool(t, tools, "resume_orchestration_run"), "run_id")
	assertRequiredFields(t, findTool(t, tools, "cancel_orchestration_run"), "run_id")

	resolveTool := findTool(t, tools, "resolve_orchestration_checkpoint")
	resolveSchema := resolveTool.Parameters.(map[string]any)
	props := resolveSchema["properties"].(map[string]any)
	if _, ok := props["mode"]; ok {
		t.Fatal("resolve_orchestration_checkpoint exposes mode, want option/response only")
	}
	assertRequiredFields(t, resolveTool, "checkpoint_id", "option")
}

func TestOrchestrationToolsHiddenForNonOwnerChannelIdentity(t *testing.T) {
	provider := NewOrchestrationProvider(nil, fakeOrchestrationToolService{
		startRun: func(context.Context, orchestration.ControlIdentity, orchestration.StartRunRequest) (orchestration.RunHandle, error) {
			t.Fatal("StartRun should not be called for non-owner session")
			return orchestration.RunHandle{}, nil
		},
	}, fakeOrchestrationBotReader{
		get: func(context.Context, string) (bots.Bot, error) {
			return bots.Bot{ID: "bot-1", OwnerUserID: "owner-1"}, nil
		},
	})

	tools, err := provider.Tools(context.Background(), SessionContext{BotID: "bot-1", ChannelIdentityID: "guest-1"})
	if err != nil {
		t.Fatalf("Tools() error = %v", err)
	}
	if len(tools) != 0 {
		t.Fatalf("len(tools) = %d, want 0", len(tools))
	}
}

func TestResolveOrchestrationCheckpointToolForwardsRequest(t *testing.T) {
	provider := NewOrchestrationProvider(nil, fakeOrchestrationToolService{
		resolveCheckpoint: func(_ context.Context, caller orchestration.ControlIdentity, checkpointID string, req orchestration.CheckpointResolution) (*orchestration.ResolveCheckpointResult, error) {
			if caller != (orchestration.ControlIdentity{TenantID: "owner-1", Subject: "owner-1"}) {
				t.Fatalf("caller = %+v", caller)
			}
			if checkpointID != "cp-1" {
				t.Fatalf("checkpointID = %q", checkpointID)
			}
			if req.Option != "approve" {
				t.Fatalf("resolution = %+v", req)
			}
			if req.IdempotencyKey == "" {
				t.Fatal("idempotency_key = empty, want generated value")
			}
			return &orchestration.ResolveCheckpointResult{SnapshotSeq: 23}, nil
		},
	}, fakeOrchestrationBotReader{
		get: func(_ context.Context, _ string) (bots.Bot, error) {
			return bots.Bot{ID: "bot-1", OwnerUserID: "owner-1"}, nil
		},
	})

	tools, err := provider.Tools(context.Background(), SessionContext{BotID: "bot-1", ChannelIdentityID: "owner-1"})
	if err != nil {
		t.Fatalf("Tools() error = %v", err)
	}
	result, err := findTool(t, tools, "resolve_orchestration_checkpoint").Execute(nil, map[string]any{
		"checkpoint_id": "cp-1",
		"option":        "approve",
	})
	if err != nil {
		t.Fatalf("resolve_orchestration_checkpoint error = %v", err)
	}
	payload := result.(map[string]any)
	if payload["snapshot_seq"] != uint64(23) {
		t.Fatalf("snapshot_seq = %#v, want %d", payload["snapshot_seq"], 23)
	}
}

func TestRequestHumanCheckpointDecisionToolReturnsServerCheckpoint(t *testing.T) {
	defaultAction := &orchestration.CheckpointDefaultAction{Option: "opt-skip"}
	checkpoints := []orchestration.HumanCheckpoint{
		{ID: "cp-other", RunID: "run-1", Status: orchestration.CheckpointStatusOpen, Question: "ignored"},
		{
			ID:            "cp-pick",
			RunID:         "run-1",
			TaskID:        "task-9",
			Status:        orchestration.CheckpointStatusOpen,
			Question:      "Approve deploy?",
			BlocksRun:     true,
			Options:       []orchestration.CheckpointOption{{ID: "opt-go", Kind: "choice", Label: "Go"}, {ID: "opt-skip", Kind: "choice", Label: "Skip"}},
			DefaultAction: defaultAction,
		},
	}
	var listedRun string
	var listedStatus []string
	provider := NewOrchestrationProvider(nil, fakeOrchestrationToolService{
		listRunCheckpoints: func(_ context.Context, caller orchestration.ControlIdentity, runID string, req orchestration.ListRunCheckpointsRequest) (*orchestration.HumanCheckpointPage, error) {
			if caller != (orchestration.ControlIdentity{TenantID: "owner-1", Subject: "owner-1"}) {
				t.Fatalf("caller = %+v", caller)
			}
			listedRun = runID
			listedStatus = req.Status
			return &orchestration.HumanCheckpointPage{Items: checkpoints}, nil
		},
	}, fakeOrchestrationBotReader{
		get: func(_ context.Context, _ string) (bots.Bot, error) {
			return bots.Bot{ID: "bot-1", OwnerUserID: "owner-1"}, nil
		},
	})

	tools, err := provider.Tools(context.Background(), SessionContext{BotID: "bot-1", ChannelIdentityID: "owner-1"})
	if err != nil {
		t.Fatalf("Tools() error = %v", err)
	}
	result, err := findTool(t, tools, "request_human_checkpoint_decision").Execute(nil, map[string]any{
		"checkpoint_id": "cp-pick",
		"run_id":        "run-1",
		"prompt":        "  decide carefully  ",
	})
	if err != nil {
		t.Fatalf("request_human_checkpoint_decision error = %v", err)
	}
	if listedRun != "run-1" {
		t.Fatalf("listedRun = %q", listedRun)
	}
	if len(listedStatus) != 1 || listedStatus[0] != orchestration.CheckpointStatusOpen {
		t.Fatalf("listedStatus = %#v", listedStatus)
	}
	payload := result.(map[string]any)
	if msg, _ := payload["status_message"].(string); msg == "" {
		t.Fatalf("status_message missing: %#v", payload)
	}
	if payload["prompt"] != "decide carefully" {
		t.Fatalf("prompt = %#v, want trimmed value", payload["prompt"])
	}
	cp, ok := payload["checkpoint"].(map[string]any)
	if !ok {
		t.Fatalf("checkpoint payload type = %T", payload["checkpoint"])
	}
	if cp["id"] != "cp-pick" || cp["run_id"] != "run-1" || cp["task_id"] != "task-9" {
		t.Fatalf("checkpoint identifiers = %#v", cp)
	}
	if cp["question"] != "Approve deploy?" || cp["blocks_run"] != true {
		t.Fatalf("checkpoint shape = %#v", cp)
	}
	if cp["default_action"].(*orchestration.CheckpointDefaultAction) != defaultAction {
		t.Fatalf("default_action passthrough = %#v", cp["default_action"])
	}
	if _, ok := cp["options"].([]orchestration.CheckpointOption); !ok {
		t.Fatalf("options type = %T", cp["options"])
	}
}

func TestRequestHumanCheckpointDecisionToolErrorsWhenMissing(t *testing.T) {
	provider := NewOrchestrationProvider(nil, fakeOrchestrationToolService{
		listRunCheckpoints: func(_ context.Context, _ orchestration.ControlIdentity, _ string, _ orchestration.ListRunCheckpointsRequest) (*orchestration.HumanCheckpointPage, error) {
			return &orchestration.HumanCheckpointPage{}, nil
		},
	}, fakeOrchestrationBotReader{
		get: func(_ context.Context, _ string) (bots.Bot, error) {
			return bots.Bot{ID: "bot-1", OwnerUserID: "owner-1"}, nil
		},
	})
	tools, err := provider.Tools(context.Background(), SessionContext{BotID: "bot-1", ChannelIdentityID: "owner-1"})
	if err != nil {
		t.Fatalf("Tools() error = %v", err)
	}
	_, err = findTool(t, tools, "request_human_checkpoint_decision").Execute(nil, map[string]any{
		"checkpoint_id": "cp-missing",
		"run_id":        "run-1",
	})
	if !errors.Is(err, orchestration.ErrCheckpointNotFound) {
		t.Fatalf("err = %v, want ErrCheckpointNotFound", err)
	}
}

func findTool(t *testing.T, tools []sdk.Tool, name string) sdk.Tool {
	t.Helper()
	for _, tool := range tools {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("tool %q not found in %#v", name, tools)
	return sdk.Tool{}
}

func assertRequiredFields(t *testing.T, tool sdk.Tool, want ...string) {
	t.Helper()
	schema := tool.Parameters.(map[string]any)
	required, ok := schema["required"].([]string)
	if !ok {
		t.Fatalf("%s required fields type = %T", tool.Name, schema["required"])
	}
	if len(required) != len(want) {
		t.Fatalf("%s required = %#v, want %#v", tool.Name, required, want)
	}
	for i := range want {
		if required[i] != want[i] {
			t.Fatalf("%s required = %#v, want %#v", tool.Name, required, want)
		}
	}
}

func assertToolProperties(t *testing.T, tool sdk.Tool, want ...string) {
	t.Helper()
	schema := tool.Parameters.(map[string]any)
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("%s properties type = %T", tool.Name, schema["properties"])
	}
	if len(props) != len(want) {
		t.Fatalf("%s properties = %#v, want %v", tool.Name, props, want)
	}
	for _, name := range want {
		if _, ok := props[name]; !ok {
			t.Fatalf("%s properties = %#v, missing %q", tool.Name, props, name)
		}
	}
}

func assertJSONFieldOrder(t *testing.T, payload string, fields ...string) {
	t.Helper()
	last := -1
	for _, field := range fields {
		index := strings.Index(payload, `"`+field+`":`)
		if index < 0 {
			t.Fatalf("field %q missing from JSON: %s", field, payload)
		}
		if index < last {
			t.Fatalf("field %q appeared out of order in JSON: %s", field, payload)
		}
		last = index
	}
}

func assertJSONPartOrder(t *testing.T, payload string, parts ...string) {
	t.Helper()
	last := -1
	for _, part := range parts {
		index := strings.Index(payload, part)
		if index < 0 {
			t.Fatalf("JSON part %q missing from JSON: %s", part, payload)
		}
		if index < last {
			t.Fatalf("JSON part %q appeared out of order in JSON: %s", part, payload)
		}
		last = index
	}
}
