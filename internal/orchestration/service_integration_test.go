package orchestration

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/memohai/memoh/internal/db"
	"github.com/memohai/memoh/internal/db/postgres/sqlc"
)

func setupOrchestrationIntegrationTest(t *testing.T) (*Service, *pgxpool.Pool, func()) {
	t.Helper()

	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("skip integration test: TEST_POSTGRES_DSN is not set")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("skip integration test: cannot connect to database: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("skip integration test: database ping failed: %v", err)
	}
	if _, err := pool.Exec(ctx, `
ALTER TABLE IF EXISTS orchestration_task_attempts
  ADD COLUMN IF NOT EXISTS worker_lease_token TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS orchestration_task_verifications
  ADD COLUMN IF NOT EXISTS worker_lease_token TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX IF NOT EXISTS idx_orchestration_human_checkpoints_open_run_barrier_unique
  ON orchestration_human_checkpoints (run_id)
  WHERE blocks_run = TRUE AND status = 'open';
`); err != nil {
		pool.Close()
		t.Skipf("skip integration test: database schema bootstrap failed: %v", err)
	}

	logger := slog.New(slog.DiscardHandler)
	return NewService(logger, NewPostgresStore(pool, sqlc.New(pool))), pool, func() { pool.Close() }
}

func cleanupOrchestrationIntegrationRun(t *testing.T, ctx context.Context, pool *pgxpool.Pool, runID string) {
	t.Helper()

	pgRunID, err := db.ParseUUID(runID)
	if err != nil {
		t.Fatalf("parse run uuid: %v", err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM orchestration_runs WHERE id = $1", pgRunID); err != nil {
		t.Fatalf("delete orchestration run: %v", err)
	}
}

func cleanupOrchestrationIntegrationIdempotency(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, subject string) {
	t.Helper()

	if _, err := pool.Exec(ctx, "DELETE FROM orchestration_idempotency_records WHERE tenant_id = $1 AND caller_subject = $2", tenantID, subject); err != nil {
		t.Fatalf("delete orchestration idempotency records: %v", err)
	}
}

func processRunPlanningIntent(t *testing.T, ctx context.Context, svc *Service) {
	t.Helper()

	processed, err := svc.ProcessNextPlanningIntent(ctx)
	if err != nil {
		t.Fatalf("ProcessNextPlanningIntent() error = %v", err)
	}
	if !processed {
		t.Fatal("ProcessNextPlanningIntent() = false, want true")
	}
}

func drainRunPlanningIntents(t *testing.T, ctx context.Context, svc *Service) {
	t.Helper()

	for {
		processed, err := svc.ProcessNextPlanningIntent(ctx)
		if err != nil {
			t.Fatalf("ProcessNextPlanningIntent() error = %v", err)
		}
		if !processed {
			return
		}
	}
}

func dispatchAndClaimAttemptForProfiles(t *testing.T, ctx context.Context, svc *Service, claim AttemptClaim, maxDispatches int) *TaskAttempt {
	t.Helper()

	if strings.TrimSpace(claim.LeaseToken) == "" {
		claim.LeaseToken = "lease-" + uuid.NewString()
	}

	for i := 0; i < maxDispatches; i++ {
		attempt, err := svc.ClaimNextAttempt(ctx, claim)
		if err == nil {
			return attempt
		}
		if !errors.Is(err, ErrNoRunnableAttempt) {
			t.Fatalf("ClaimNextAttempt() error = %v", err)
		}
		dispatched, dispatchErr := svc.DispatchNextReadyTask(ctx)
		if dispatchErr != nil {
			t.Fatalf("DispatchNextReadyTask() error = %v", dispatchErr)
		}
		if !dispatched {
			break
		}
	}

	attempt, err := svc.ClaimNextAttempt(ctx, claim)
	if err != nil {
		t.Fatalf("ClaimNextAttempt() error = %v", err)
	}
	return attempt
}

func TestIntegrationTenantScopedAuthorizationRejectsSameSubjectAcrossTenants(t *testing.T) {
	svc, pool, cleanup := setupOrchestrationIntegrationTest(t)
	defer cleanup()

	ctx := context.Background()
	subject := "shared-subject-" + uuid.NewString()
	caller := ControlIdentity{
		TenantID: "tenant-a-" + uuid.NewString(),
		Subject:  subject,
	}
	otherOwnerCaller := ControlIdentity{
		TenantID: caller.TenantID,
		Subject:  "subject-" + uuid.NewString(),
	}
	otherTenantCaller := ControlIdentity{
		TenantID: "tenant-b-" + uuid.NewString(),
		Subject:  subject,
	}

	handle, err := svc.StartRun(ctx, caller, StartRunRequest{
		Goal:           "authorize only within the owning tenant",
		IdempotencyKey: "start-" + uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	processRunPlanningIntent(t, ctx, svc)
	defer cleanupOrchestrationIntegrationRun(t, ctx, pool, handle.RunID)
	defer cleanupOrchestrationIntegrationIdempotency(t, ctx, pool, caller.TenantID, caller.Subject)
	defer cleanupOrchestrationIntegrationIdempotency(t, ctx, pool, otherOwnerCaller.TenantID, otherOwnerCaller.Subject)
	defer cleanupOrchestrationIntegrationIdempotency(t, ctx, pool, otherTenantCaller.TenantID, otherTenantCaller.Subject)

	if _, err := svc.GetRunSnapshot(ctx, otherTenantCaller, handle.RunID); !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("GetRunSnapshot(cross-tenant same-subject) error = %v, want %v", err, ErrRunNotFound)
	}
	if _, err := svc.GetRunSnapshot(ctx, otherOwnerCaller, handle.RunID); !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("GetRunSnapshot(same-tenant non-owner) error = %v, want %v", err, ErrRunNotFound)
	}

	checkpointResult, err := svc.CreateHumanCheckpoint(ctx, caller, CreateHumanCheckpointRequest{
		RunID:          handle.RunID,
		TaskID:         handle.RootTaskID,
		Question:       "approve execution?",
		BlocksRun:      false,
		IdempotencyKey: "checkpoint-" + uuid.NewString(),
		Options: []CheckpointOption{
			{ID: "approve", Kind: CheckpointOptionKindChoice, Label: "Approve"},
		},
		Metadata: map[string]any{"context": "tenant-authorization"},
	})
	if err != nil {
		t.Fatalf("CreateHumanCheckpoint() error = %v", err)
	}

	_, err = svc.ResolveCheckpoint(ctx, otherTenantCaller, checkpointResult.Checkpoint.ID, CheckpointResolution{
		Option:         "approve",
		Metadata:       map[string]any{"reviewer": "bob"},
		IdempotencyKey: "resolve-" + uuid.NewString(),
	})
	if !errors.Is(err, ErrCheckpointNotFound) {
		t.Fatalf("ResolveCheckpoint(cross-tenant same-subject) error = %v, want %v", err, ErrCheckpointNotFound)
	}
	_, err = svc.ResolveCheckpoint(ctx, otherOwnerCaller, checkpointResult.Checkpoint.ID, CheckpointResolution{
		Option:         "approve",
		Metadata:       map[string]any{"reviewer": "carol"},
		IdempotencyKey: "resolve-" + uuid.NewString(),
	})
	if !errors.Is(err, ErrCheckpointNotFound) {
		t.Fatalf("ResolveCheckpoint(same-tenant non-owner) error = %v, want %v", err, ErrCheckpointNotFound)
	}

	page, err := svc.ListRunCheckpoints(ctx, caller, handle.RunID, ListRunCheckpointsRequest{Limit: 10})
	if err != nil {
		t.Fatalf("ListRunCheckpoints() error = %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("ListRunCheckpoints() len = %d, want 1", len(page.Items))
	}
	if page.Items[0].Status != CheckpointStatusOpen {
		t.Fatalf("checkpoint status after denied resolve = %q, want %q", page.Items[0].Status, CheckpointStatusOpen)
	}
}

func TestIntegrationListBotRunsFiltersByBotID(t *testing.T) {
	svc, pool, cleanup := setupOrchestrationIntegrationTest(t)
	defer cleanup()

	ctx := context.Background()
	caller := ControlIdentity{
		TenantID: "tenant-" + uuid.NewString(),
		Subject:  "subject-" + uuid.NewString(),
	}
	botID := uuid.NewString()
	otherBotID := uuid.NewString()

	handle, err := svc.StartRun(ctx, caller, StartRunRequest{
		Goal:           "list bot runs",
		BotID:          botID,
		IdempotencyKey: "start-" + uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("StartRun(bot) error = %v", err)
	}
	otherHandle, err := svc.StartRun(ctx, caller, StartRunRequest{
		Goal:           "other bot run",
		BotID:          otherBotID,
		IdempotencyKey: "start-" + uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("StartRun(other bot) error = %v", err)
	}
	defer cleanupOrchestrationIntegrationRun(t, ctx, pool, handle.RunID)
	defer cleanupOrchestrationIntegrationRun(t, ctx, pool, otherHandle.RunID)
	defer cleanupOrchestrationIntegrationIdempotency(t, ctx, pool, caller.TenantID, caller.Subject)

	page, err := svc.ListBotRuns(ctx, caller, botID, ListBotRunsRequest{Limit: 10})
	if err != nil {
		t.Fatalf("ListBotRuns() error = %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("ListBotRuns() len = %d, want 1", len(page.Items))
	}
	if page.Items[0].ID != handle.RunID {
		t.Fatalf("ListBotRuns() run id = %q, want %q", page.Items[0].ID, handle.RunID)
	}
}

func TestIntegrationGetRunInspectorIncludesExecutionSpansAndInputManifests(t *testing.T) {
	svc, pool, cleanup := setupOrchestrationIntegrationTest(t)
	defer cleanup()

	ctx := context.Background()
	caller := ControlIdentity{
		TenantID: "tenant-" + uuid.NewString(),
		Subject:  "subject-" + uuid.NewString(),
	}

	handle, err := svc.StartRun(ctx, caller, StartRunRequest{
		Goal:           "inspector execution detail contract",
		IdempotencyKey: "start-" + uuid.NewString(),
		Input: map[string]any{
			"builtin_workerd": map[string]any{
				"request_replan": true,
				"child_tasks": []map[string]any{
					{
						"alias":          "verify-child",
						"kind":           "child",
						"goal":           "verified child",
						"worker_profile": BuiltinEchoWorkerProfile,
						"verification_policy": map[string]any{
							"mode":                      VerificationModeBuiltinBasic,
							"require_structured_output": true,
						},
						"inputs": map[string]any{
							"builtin_workerd": map[string]any{
								"summary": "verified child complete",
							},
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	defer cleanupOrchestrationIntegrationRun(t, ctx, pool, handle.RunID)
	defer cleanupOrchestrationIntegrationIdempotency(t, ctx, pool, caller.TenantID, caller.Subject)

	processRunPlanningIntent(t, ctx, svc)

	rootAttempt := dispatchAndClaimAttemptForProfiles(t, ctx, svc, AttemptClaim{
		WorkerID:        "worker-root",
		ExecutorID:      DefaultWorkerExecutorID,
		WorkerProfiles:  []string{DefaultWorkerProfile},
		LeaseTTLSeconds: 30,
	}, 2)
	rootRunning, err := svc.StartAttempt(ctx, rootAttempt.ID, rootAttempt.ClaimToken)
	if err != nil {
		t.Fatalf("StartAttempt(root) error = %v", err)
	}
	if _, err := svc.CompleteAttempt(ctx, AttemptCompletion{
		AttemptID:     rootRunning.ID,
		ClaimToken:    rootRunning.ClaimToken,
		Status:        TaskAttemptStatusCompleted,
		Summary:       "planned child task",
		RequestReplan: true,
		StructuredOutput: map[string]any{
			"child_tasks": []map[string]any{
				{
					"alias":          "verify-child",
					"kind":           "child",
					"goal":           "verified child",
					"worker_profile": BuiltinEchoWorkerProfile,
					"verification_policy": map[string]any{
						"mode":                      VerificationModeBuiltinBasic,
						"require_structured_output": true,
					},
					"inputs": map[string]any{
						"builtin_workerd": map[string]any{
							"summary": "verified child complete",
						},
					},
				},
			},
		},
	}); err != nil {
		t.Fatalf("CompleteAttempt(root) error = %v", err)
	}

	drainRunPlanningIntents(t, ctx, svc)

	childAttempt := dispatchAndClaimAttemptForProfiles(t, ctx, svc, AttemptClaim{
		WorkerID:        "worker-child",
		ExecutorID:      DefaultWorkerExecutorID,
		WorkerProfiles:  []string{DefaultWorkerProfile},
		LeaseTTLSeconds: 30,
	}, 4)
	childRunning, err := svc.StartAttempt(ctx, childAttempt.ID, childAttempt.ClaimToken)
	if err != nil {
		t.Fatalf("StartAttempt(child) error = %v", err)
	}
	if _, err := svc.CompleteAttempt(ctx, AttemptCompletion{
		AttemptID:        childRunning.ID,
		ClaimToken:       childRunning.ClaimToken,
		Status:           TaskAttemptStatusCompleted,
		Summary:          "verified child complete",
		StructuredOutput: map[string]any{"ok": true},
	}); err != nil {
		t.Fatalf("CompleteAttempt(child) error = %v", err)
	}
	processRunPlanningIntent(t, ctx, svc)

	verification, err := svc.ClaimNextVerification(ctx, VerificationClaim{
		WorkerID:         "verify-worker",
		ExecutorID:       DefaultVerifierExecutorID,
		VerifierProfiles: []string{DefaultVerifierProfile},
		LeaseToken:       "lease-" + uuid.NewString(),
		LeaseTTLSeconds:  30,
	})
	if err != nil {
		t.Fatalf("ClaimNextVerification() error = %v", err)
	}
	runningVerification, err := svc.StartVerification(ctx, verification.ID, verification.ClaimToken)
	if err != nil {
		t.Fatalf("StartVerification() error = %v", err)
	}
	if _, err := svc.CompleteVerification(ctx, VerificationCompletion{
		VerificationID: runningVerification.ID,
		ClaimToken:     runningVerification.ClaimToken,
		Status:         TaskVerificationStatusCompleted,
		Verdict:        VerificationVerdictAccepted,
		Summary:        "accepted",
	}); err != nil {
		t.Fatalf("CompleteVerification() error = %v", err)
	}

	inspector, err := svc.GetRunInspector(ctx, caller, handle.RunID)
	if err != nil {
		t.Fatalf("GetRunInspector() error = %v", err)
	}

	if len(inspector.InputManifests) != 2 {
		t.Fatalf("len(InputManifests) = %d, want 2", len(inspector.InputManifests))
	}
	if len(inspector.ExecutionSpans) != 3 {
		t.Fatalf("len(ExecutionSpans) = %d, want 3", len(inspector.ExecutionSpans))
	}

	var rootSpan, childSpan, verificationSpan *RunExecutionSpan
	for i := range inspector.ExecutionSpans {
		span := &inspector.ExecutionSpans[i]
		switch {
		case span.Kind == "attempt" && span.ID == rootAttempt.ID:
			rootSpan = span
		case span.Kind == "attempt" && span.ID == childAttempt.ID:
			childSpan = span
		case span.Kind == "verification" && span.ID == verification.ID:
			verificationSpan = span
		}
	}
	if rootSpan == nil {
		t.Fatal("root attempt span not found")
	}
	if childSpan == nil {
		t.Fatal("child attempt span not found")
	}
	if verificationSpan == nil {
		t.Fatal("verification span not found")
	}

	if rootSpan.CreatedSeq == 0 || rootSpan.ClaimedSeq == 0 || rootSpan.StartedSeq == 0 || rootSpan.TerminalSeq == 0 {
		t.Fatalf("root span seqs = %#v, want non-zero lifecycle seqs", rootSpan)
	}
	if rootSpan.InputManifestID == "" || rootSpan.ResultID == "" {
		t.Fatalf("root span manifest/result = %#v, want non-empty", rootSpan)
	}
	if rootSpan.Summary != "planned child task" {
		t.Fatalf("root span summary = %q, want %q", rootSpan.Summary, "planned child task")
	}
	if !reflect.DeepEqual(rootSpan.RelatedEventTypes, []string{
		"run.event.attempt.created",
		"run.event.attempt.claimed",
		"run.event.attempt.binding",
		"run.event.attempt.running",
		"run.event.attempt.completed",
	}) {
		t.Fatalf("root span related events = %#v", rootSpan.RelatedEventTypes)
	}

	if childSpan.InputManifestID == "" || childSpan.ResultID == "" {
		t.Fatalf("child span manifest/result = %#v, want non-empty", childSpan)
	}
	if childSpan.Summary != "verified child complete" {
		t.Fatalf("child span summary = %q, want %q", childSpan.Summary, "verified child complete")
	}

	if verificationSpan.ResultID == "" || verificationSpan.Verdict != VerificationVerdictAccepted {
		t.Fatalf("verification span = %#v, want result id and accepted verdict", verificationSpan)
	}
	if verificationSpan.CreatedSeq == 0 || verificationSpan.ClaimedSeq == 0 || verificationSpan.StartedSeq == 0 || verificationSpan.TerminalSeq == 0 {
		t.Fatalf("verification span seqs = %#v, want non-zero lifecycle seqs", verificationSpan)
	}

	for _, entry := range inspector.Timeline {
		if _, ok := entry.Payload["claim_token"]; ok {
			t.Fatalf("inspector timeline payload leaked claim_token: %#v", entry.Payload)
		}
	}
}
