package orchestrationexec

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/memohai/memoh/internal/db/postgres/sqlc"
	"github.com/memohai/memoh/internal/orchestration"
)

func testEnvResourceCatalog() envResourceCatalog {
	return newEnvResourceCatalog([]sqlc.OrchestrationEnvResource{
		{Name: "browser", Kind: orchestration.EnvPreconditionsKindBrowser, Status: "active"},
		{Name: "ubuntu-default", Kind: orchestration.EnvPreconditionsKindContainer, Status: "active"},
	})
}

func TestDecodeJSONObjectTextStripsCodeFence(t *testing.T) {
	payload, err := decodeJSONObjectText("```json\n{\"status\":\"completed\",\"summary\":\"ok\"}\n```")
	if err != nil {
		t.Fatalf("decodeJSONObjectText error = %v", err)
	}
	if got := payload["status"]; got != "completed" {
		t.Fatalf("status = %v, want completed", got)
	}
}

func TestClassifyToolEffectMarksSendEmailIrreversible(t *testing.T) {
	if got := classifyToolEffect("send_email"); got != "external_irreversible" {
		t.Fatalf("classifyToolEffect(send_email) = %q, want external_irreversible", got)
	}
	if got := classifyToolEffect("send"); got != "external_write" {
		t.Fatalf("classifyToolEffect(send) = %q, want external_write", got)
	}
}

func TestSideEffectApprovalTokenHelpers(t *testing.T) {
	token := " approval-secret "
	if got := approvalTokenFromInput(map[string]any{"approval_token": token}); got != "approval-secret" {
		t.Fatalf("approvalTokenFromInput() = %q, want trimmed token", got)
	}
	if sideEffectApprovalTokenHash(token) != sideEffectApprovalTokenHash("approval-secret") {
		t.Fatal("sideEffectApprovalTokenHash() should trim tokens before hashing")
	}
	sessionID := "00000000-0000-0000-0000-000000000123"
	envSessionID, envLeaseEpoch := sideEffectEnvFence(map[string]any{
		"captured_env_preconditions": map[string]any{
			"session_id":  sessionID,
			"lease_epoch": float64(7),
		},
	})
	if !envSessionID.Valid || envSessionID.String() != sessionID || envLeaseEpoch != 7 {
		t.Fatalf("sideEffectEnvFence() = (%s,%d), want (%s,7)", envSessionID.String(), envLeaseEpoch, sessionID)
	}
}

func TestBuildEnvDriftContextDetectsDigestChanges(t *testing.T) {
	rows := []sqlc.OrchestrationEnvSnapshot{
		{
			ID:          mustPGUUIDForExecTest(t, "00000000-0000-0000-0000-000000000001"),
			SessionID:   mustPGUUIDForExecTest(t, "00000000-0000-0000-0000-000000000010"),
			Kind:        "pre_action",
			EffectClass: "env_local_mutation",
			Digest:      "before",
			RuntimeRef:  []byte(`{"ref":"pre"}`),
			Metadata:    []byte(`{"phase":"before"}`),
		},
		{
			ID:          mustPGUUIDForExecTest(t, "00000000-0000-0000-0000-000000000002"),
			SessionID:   mustPGUUIDForExecTest(t, "00000000-0000-0000-0000-000000000010"),
			Kind:        "periodic",
			EffectClass: "env_local_mutation",
			Digest:      "middle",
		},
		{
			ID:          mustPGUUIDForExecTest(t, "00000000-0000-0000-0000-000000000003"),
			SessionID:   mustPGUUIDForExecTest(t, "00000000-0000-0000-0000-000000000010"),
			Kind:        "post_action",
			EffectClass: "env_local_mutation",
			Digest:      "after",
		},
	}
	ctx := buildEnvDriftContext(rows)
	if ctx["status"] != "changed" || ctx["changed"] != true {
		t.Fatalf("env drift context = %#v, want changed=true", ctx)
	}
	if ctx["before_digest"] != "before" || ctx["after_digest"] != "after" || ctx["periodic_count"] != 1 {
		t.Fatalf("env drift digest summary = %#v, want before/after plus periodic count", ctx)
	}
	snapshots, ok := ctx["snapshots"].([]map[string]any)
	if !ok || len(snapshots) != 3 {
		t.Fatalf("snapshots = %#v, want three projected snapshots", ctx["snapshots"])
	}
	if snapshots[0]["runtime_ref"].(map[string]any)["ref"] != "pre" {
		t.Fatalf("runtime_ref = %#v, want decoded ref", snapshots[0]["runtime_ref"])
	}
}

func TestBuildEnvDriftContextReportsUnchanged(t *testing.T) {
	ctx := buildEnvDriftContext([]sqlc.OrchestrationEnvSnapshot{
		{Kind: "pre_action", Digest: "same"},
		{Kind: "post_action", Digest: "same"},
	})
	if ctx["status"] != "unchanged" || ctx["changed"] != false {
		t.Fatalf("env drift context = %#v, want unchanged", ctx)
	}
}

func TestPlannerToolInputAcceptsBrowserEnvPreconditions(t *testing.T) {
	task, err := plannerFlatTaskInputToSpec("child_tasks[0]", map[string]any{
		"alias":             "open",
		"goal":              "open example.com and report the title",
		"worker_profile":    orchestration.DefaultRootWorkerProfile,
		"env_required":      true,
		"env_kind":          orchestration.EnvPreconditionsKindBrowser,
		"env_resource_name": "browser",
		"env_mode":          orchestration.EnvPreconditionsModeContext,
		"env_effect_class":  orchestration.EnvPreconditionsEffectExternalIdempotent,
	}, testEnvResourceCatalog(), true)
	if err != nil {
		t.Fatalf("plannerFlatTaskInputToSpec() error = %v", err)
	}
	if task.WorkerProfile != orchestration.DefaultRootWorkerProfile {
		t.Fatalf("worker_profile = %q, want %q", task.WorkerProfile, orchestration.DefaultRootWorkerProfile)
	}
	if !task.EnvPreconditions.Required || task.EnvPreconditions.Kind != orchestration.EnvPreconditionsKindBrowser || task.EnvPreconditions.ResourceName != "browser" || task.EnvPreconditions.Mode != orchestration.EnvPreconditionsModeContext {
		t.Fatalf("env_preconditions = %#v, want browser context resource", task.EnvPreconditions)
	}
}

func TestPlannerToolInputAcceptsBrowserEnvOnRootTaskWithoutChildren(t *testing.T) {
	rootTask, err := plannerFlatTaskInputToSpec("root_task", map[string]any{
		"goal":              "open example.com and report the title",
		"worker_profile":    orchestration.DefaultRootWorkerProfile,
		"env_required":      true,
		"env_kind":          orchestration.EnvPreconditionsKindBrowser,
		"env_resource_name": "browser",
	}, testEnvResourceCatalog(), false)
	if err != nil {
		t.Fatalf("plannerFlatTaskInputToSpec() error = %v", err)
	}
	if !rootTask.EnvPreconditions.Required || rootTask.EnvPreconditions.Kind != orchestration.EnvPreconditionsKindBrowser || rootTask.EnvPreconditions.ResourceName != "browser" || rootTask.EnvPreconditions.Mode != orchestration.EnvPreconditionsModeContext {
		t.Fatalf("root env_preconditions = %#v, want browser context resource", rootTask.EnvPreconditions)
	}
}

func TestPlannerToolInputRejectsUnknownEnvResource(t *testing.T) {
	_, err := plannerFlatTaskInputToSpec("child_tasks[0]", map[string]any{
		"alias":             "open",
		"goal":              "patch repo",
		"env_required":      true,
		"env_kind":          orchestration.EnvPreconditionsKindContainer,
		"env_resource_name": "interactive_browser",
	}, testEnvResourceCatalog(), true)
	if err == nil || !strings.Contains(err.Error(), "must reference an existing env resource") {
		t.Fatalf("plannerFlatTaskInputToSpec error = %v, want unknown env resource rejection", err)
	}
}

func TestPlannerToolInputRejectsEnvResourceKindMismatch(t *testing.T) {
	_, err := plannerFlatTaskInputToSpec("child_tasks[0]", map[string]any{
		"alias":             "open",
		"goal":              "open example.com",
		"env_required":      true,
		"env_kind":          orchestration.EnvPreconditionsKindBrowser,
		"env_resource_name": "ubuntu-default",
	}, testEnvResourceCatalog(), true)
	if err == nil || !strings.Contains(err.Error(), `has kind "container", not "browser"`) {
		t.Fatalf("plannerFlatTaskInputToSpec error = %v, want kind mismatch rejection", err)
	}
}

func TestPlannerToolInputRejectsBrowserCapableWorkerProfile(t *testing.T) {
	_, err := plannerFlatTaskInputToSpec("child_tasks[0]", map[string]any{
		"alias":             "open",
		"goal":              "open example.com",
		"worker_profile":    "browser-capable",
		"env_required":      true,
		"env_kind":          orchestration.EnvPreconditionsKindBrowser,
		"env_resource_name": "browser",
	}, testEnvResourceCatalog(), true)
	if err == nil || !strings.Contains(err.Error(), "browser access must be declared") {
		t.Fatalf("plannerFlatTaskInputToSpec() error = %v, want browser-capable rejection", err)
	}
}

func TestBuildReplanPlannerPromptIncludesFailureContext(t *testing.T) {
	prompt := buildReplanPlannerPrompt(orchestration.ReplanPlanningInput{
		Run: orchestration.Run{
			ID:             "run-1",
			Goal:           "ship the report",
			PlannerEpoch:   2,
			SourceMetadata: map[string]any{"private_note": "do not send to replanner"},
		},
		SourceTask: orchestration.Task{
			ID:   "task-1",
			Goal: "write the report",
		},
		SourceAttempt: &orchestration.TaskAttempt{
			ID:              "attempt-1",
			Status:          orchestration.TaskAttemptStatusFailed,
			WorkerID:        "internal-worker-id",
			ExecutorID:      "internal-executor-id",
			ClaimToken:      "secret-claim-token",
			InputManifestID: "internal-manifest-id",
		},
		SourceResult: &orchestration.TaskResult{
			ID:      "result-1",
			Status:  orchestration.TaskAttemptStatusFailed,
			Summary: "missing required artifact",
		},
		SubtreeTasks: []orchestration.Task{
			{ID: "task-1", Goal: "write the report"},
		},
		Reason:       "verification rejected output",
		InjectedHint: map[string]any{"constraint": "keep it concise"},
	})
	for _, expected := range []string{
		"ship the report",
		"write the report",
		"missing required artifact",
		"verification rejected output",
		"keep it concise",
	} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("replan prompt missing %q: %s", expected, prompt)
		}
	}
	if strings.Contains(prompt, "secret-claim-token") || strings.Contains(prompt, "claim_token") {
		t.Fatalf("replan prompt leaked claim token fields: %s", prompt)
	}
	for _, unexpected := range []string{"do not send to replanner", "private_note", "internal-worker-id", "internal-executor-id", "internal-manifest-id"} {
		if strings.Contains(prompt, unexpected) {
			t.Fatalf("replan prompt leaked %q: %s", unexpected, prompt)
		}
	}
}

func TestBuildPlannerPromptsUseOrchestrationEnvelope(t *testing.T) {
	startPrompt := buildStartRunPlannerPrompt(orchestration.StartRunPlanningInput{
		Run: orchestration.Run{
			ID:       "run-1",
			TenantID: "tenant-1",
			Goal:     "open a page and summarize it",
			Input:    map[string]any{"url": "https://example.com"},
		},
		RootTask: orchestration.Task{
			ID:            "task-root",
			Goal:          "open a page and summarize it",
			Inputs:        map[string]any{"url": "https://example.com"},
			WorkerProfile: orchestration.DefaultRootWorkerProfile,
			EnvPreconditions: orchestration.EnvPreconditions{
				Required: false,
			},
		},
	})
	for _, expected := range []string{
		`<orchestration-context kind="start_run_planning">`,
		"<run>",
		"<root_task>",
		"open a page and summarize it",
		"https://example.com",
	} {
		if !strings.Contains(startPrompt, expected) {
			t.Fatalf("start planner prompt missing %q: %s", expected, startPrompt)
		}
	}

	replanPrompt := buildReplanPlannerPrompt(orchestration.ReplanPlanningInput{
		Run:        orchestration.Run{ID: "run-2", Goal: "repair report", PlannerEpoch: 3},
		SourceTask: orchestration.Task{ID: "task-2", Goal: "write report"},
		Reason:     "verification rejected output",
	})
	for _, expected := range []string{
		`<orchestration-context kind="replanning">`,
		"<source_task>",
		"<source_attempt>",
		"<source_result>",
		"<subtree_tasks>",
		"<dependencies>",
		"<injected_hint>",
		"verification rejected output",
	} {
		if !strings.Contains(replanPrompt, expected) {
			t.Fatalf("replan prompt missing %q: %s", expected, replanPrompt)
		}
	}
}

func TestBuildRuntimePromptsUseOrchestrationEnvelope(t *testing.T) {
	workerPrompt := buildWorkerPrompt(attemptExecutionContext{
		Run: sqlc.OrchestrationRun{
			TenantID:       "tenant-1",
			OwnerSubject:   "user-1",
			Goal:           "run shell command",
			SourceMetadata: []byte(`{"bot_id":"bot-1"}`),
		},
		Task: sqlc.OrchestrationTask{
			Goal:          "print env",
			Kind:          "step",
			WorkerProfile: orchestration.DefaultRootWorkerProfile,
		},
		Attempt: orchestration.TaskAttempt{ID: "attempt-1", AttemptNo: 1},
		InputManifest: map[string]any{
			"captured_env_preconditions": map[string]any{
				"required": true,
			},
		},
		Predecessors: []map[string]any{{"summary": "prepared"}},
	})
	for _, expected := range []string{
		`<orchestration-context kind="task_attempt">`,
		"<run>",
		"<task>",
		"<attempt>",
		"<input_manifest>",
		"<predecessor_results>",
		"captured_env_preconditions",
		"prepared",
	} {
		if !strings.Contains(workerPrompt, expected) {
			t.Fatalf("worker prompt missing %q: %s", expected, workerPrompt)
		}
	}

	verifierPrompt := buildVerifierPrompt(verificationExecutionContext{
		Run: sqlc.OrchestrationRun{
			TenantID:     "tenant-1",
			OwnerSubject: "user-1",
			Goal:         "verify result",
		},
		Task:         sqlc.OrchestrationTask{Goal: "print env", WorkerProfile: orchestration.DefaultRootWorkerProfile},
		Result:       sqlc.OrchestrationTaskResult{Status: orchestration.TaskAttemptStatusCompleted, Summary: "done"},
		Verification: orchestration.TaskVerification{ID: "verification-1", AttemptNo: 1, VerifierProfile: orchestration.DefaultVerifierProfile},
		EnvDrift:     map[string]any{"status": "unchanged"},
	})
	for _, expected := range []string{
		`<orchestration-context kind="task_verification">`,
		"<result>",
		"<verification>",
		"<env_drift>",
		"unchanged",
	} {
		if !strings.Contains(verifierPrompt, expected) {
			t.Fatalf("verifier prompt missing %q: %s", expected, verifierPrompt)
		}
	}
}

func TestOrchestrationUserPromptsDoNotUseLegacyContextJSONPrefix(t *testing.T) {
	prompts := []string{
		buildStartRunPlannerPrompt(orchestration.StartRunPlanningInput{}),
		buildReplanPlannerPrompt(orchestration.ReplanPlanningInput{}),
		buildWorkerPrompt(attemptExecutionContext{}),
		buildVerifierPrompt(verificationExecutionContext{}),
	}
	for index, prompt := range prompts {
		for _, unexpected := range []string{
			"Context JSON:",
			"Execute the following orchestration task.",
			"Verify the following orchestration task result.",
		} {
			if strings.Contains(prompt, unexpected) {
				t.Fatalf("prompt %d contains legacy prefix %q: %s", index, unexpected, prompt)
			}
		}
	}
}

func TestPlannerSystemPromptsDoNotDescribeJSONOutput(t *testing.T) {
	for name, prompt := range map[string]string{
		"start_run": startRunPlannerSystemPrompt,
		"replan":    replanPlannerSystemPrompt,
	} {
		for _, unexpected := range []string{
			"JSON",
			"markdown",
			"free-form",
			"child_tasks=[]",
		} {
			if strings.Contains(prompt, unexpected) {
				t.Fatalf("%s planner prompt contains legacy output wording %q: %s", name, unexpected, prompt)
			}
		}
	}
}

func TestDecodeAttemptCompletionPayloadDoesNotPromoteChildTasks(t *testing.T) {
	completion, err := decodeAttemptCompletionPayload(
		orchestration.TaskAttempt{ID: "attempt-1", ClaimToken: "claim-1"},
		sqlc.OrchestrationTask{Goal: "compute fib"},
		map[string]any{
			"status":         "completed",
			"summary":        "needs decomposition",
			"request_replan": true,
			"child_tasks": []any{
				map[string]any{
					"goal": "step a",
					"env_preconditions": map[string]any{
						"required": false,
					},
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("decodeAttemptCompletionPayload error = %v", err)
	}
	if !completion.RequestReplan {
		t.Fatal("RequestReplan = false, want true")
	}
	if _, ok := completion.StructuredOutput["child_tasks"]; ok {
		t.Fatalf("structured_output.child_tasks = %#v, want no worker-created replacement tasks", completion.StructuredOutput["child_tasks"])
	}
}

func TestFinishAttemptInputPayloadReusesCompletionValidation(t *testing.T) {
	completion, err := decodeAttemptCompletionPayload(
		orchestration.TaskAttempt{ID: "attempt-1", ClaimToken: "claim-1"},
		sqlc.OrchestrationTask{Goal: "open page"},
		finishAttemptInputPayload(finishAttemptInput{
			Status:               orchestration.TaskAttemptStatusCompleted,
			Summary:              "needs browser follow-up",
			RequestReplan:        true,
			StructuredOutputJSON: `{"child_tasks":[{"goal":"open example.com"}],"note":"browser follow-up"}`,
		}, nil),
		testEnvResourceCatalog(),
	)
	if err != nil {
		t.Fatalf("decodeAttemptCompletionPayload error = %v", err)
	}
	if !completion.RequestReplan {
		t.Fatal("RequestReplan = false, want true")
	}
	if _, ok := completion.StructuredOutput["child_tasks"]; ok {
		t.Fatalf("structured_output.child_tasks = %#v, want no worker-created replacement tasks", completion.StructuredOutput["child_tasks"])
	}
	if completion.StructuredOutput["note"] != "browser follow-up" {
		t.Fatalf("structured_output = %#v, want preserved note", completion.StructuredOutput)
	}
}

func TestFlatFinishAttemptCollectsArtifactIntents(t *testing.T) {
	artifact, err := decodeFlatArtifactIntent(map[string]any{
		"kind":          "screenshot",
		"uri":           "/data/screenshots/github.png",
		"content_type":  "image/png",
		"summary":       "GitHub screenshot",
		"metadata_json": `{"bytes":1722214}`,
	})
	if err != nil {
		t.Fatalf("decodeFlatArtifactIntent error = %v", err)
	}
	completion, err := decodeAttemptCompletionPayload(
		orchestration.TaskAttempt{ID: "attempt-1", ClaimToken: "claim-1"},
		sqlc.OrchestrationTask{Goal: "open page"},
		finishAttemptInputPayload(finishAttemptInput{
			Status:  orchestration.TaskAttemptStatusCompleted,
			Summary: "captured screenshot",
		}, []orchestration.AttemptArtifactIntent{artifact}),
	)
	if err != nil {
		t.Fatalf("decodeAttemptCompletionPayload error = %v", err)
	}
	if len(completion.ArtifactIntents) != 1 {
		t.Fatalf("ArtifactIntents = %#v, want 1 item", completion.ArtifactIntents)
	}
	if completion.ArtifactIntents[0].URI != "/data/screenshots/github.png" || completion.ArtifactIntents[0].Metadata["bytes"] != float64(1722214) {
		t.Fatalf("ArtifactIntents[0] = %#v, want screenshot metadata", completion.ArtifactIntents[0])
	}
}

func TestFlatFinishAttemptRejectsLegacyNestedFields(t *testing.T) {
	_, err := decodeFlatFinishAttemptInput(map[string]any{
		"status":           orchestration.TaskAttemptStatusCompleted,
		"summary":          "done",
		"artifact_intents": "[]",
	})
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("decodeFlatFinishAttemptInput error = %v, want unknown legacy field rejection", err)
	}
}

func TestDecodeVerificationCompletionPayloadDefaultsRejectReason(t *testing.T) {
	completion, err := decodeVerificationCompletionPayload(
		orchestration.TaskVerification{ID: "verification-1", ClaimToken: "claim-1"},
		sqlc.OrchestrationTask{Goal: "verify fib"},
		sqlc.OrchestrationTaskResult{Summary: "worker said fib=1346269"},
		map[string]any{
			"status":  "failed",
			"verdict": "rejected",
			"summary": "result is inconsistent",
		},
	)
	if err != nil {
		t.Fatalf("decodeVerificationCompletionPayload error = %v", err)
	}
	if completion.TerminalReason != "result is inconsistent" {
		t.Fatalf("TerminalReason = %q, want summary fallback", completion.TerminalReason)
	}
}

func TestFinishVerificationInputPayloadReusesCompletionValidation(t *testing.T) {
	completion, err := decodeVerificationCompletionPayload(
		orchestration.TaskVerification{ID: "verification-1", ClaimToken: "claim-1"},
		sqlc.OrchestrationTask{Goal: "verify page title"},
		sqlc.OrchestrationTaskResult{Summary: "worker result"},
		finishVerificationInputPayload(finishVerificationInput{
			Status:         orchestration.TaskVerificationStatusFailed,
			Verdict:        orchestration.VerificationVerdictRejected,
			Summary:        "title was wrong",
			FailureClass:   "verifier_retryable",
			TerminalReason: "expected Example Domain",
		}),
	)
	if err != nil {
		t.Fatalf("decodeVerificationCompletionPayload error = %v", err)
	}
	if completion.Verdict != orchestration.VerificationVerdictRejected || completion.FailureClass != "verifier_retryable" {
		t.Fatalf("verification completion = %#v, want rejected retryable", completion)
	}
}

func mustPGUUIDForExecTest(t *testing.T, raw string) pgtype.UUID {
	t.Helper()
	var id pgtype.UUID
	if err := id.Scan(raw); err != nil {
		t.Fatalf("parse uuid %q: %v", raw, err)
	}
	return id
}
