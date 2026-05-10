package orchestration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestIntegrationCreateSideEffectApprovalTokenBindsAttemptFence(t *testing.T) {
	svc, pool, cleanup := setupOrchestrationIntegrationTest(t)
	defer cleanup()

	ctx := context.Background()
	caller := ControlIdentity{
		TenantID: "tenant-" + uuid.NewString(),
		Subject:  "subject-" + uuid.NewString(),
	}
	handle, err := svc.StartRun(ctx, caller, StartRunRequest{
		Goal:           "approve irreversible side effect",
		IdempotencyKey: "start-" + uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	processRunOrchestrationIntent(t, ctx, svc)
	defer cleanupOrchestrationIntegrationRun(t, ctx, pool, handle.RunID)
	defer cleanupOrchestrationIntegrationIdempotency(t, ctx, pool, caller.TenantID, caller.Subject)

	if dispatched, err := dispatchNextReadyTaskForTest(ctx, svc); err != nil {
		t.Fatalf("DispatchNextReadyTaskForWorkerProfiles() error = %v", err)
	} else if !dispatched {
		t.Fatal("DispatchNextReadyTaskForWorkerProfiles() = false, want true")
	}
	attempt, err := svc.ClaimNextAttempt(ctx, AttemptClaim{
		WorkerID:        "worker-" + uuid.NewString(),
		ExecutorID:      "executor",
		WorkerProfiles:  []string{DefaultRootWorkerProfile},
		LeaseToken:      "lease-" + uuid.NewString(),
		LeaseTTLSeconds: int((5 * time.Minute).Seconds()),
	})
	if err != nil {
		t.Fatalf("ClaimNextAttempt() error = %v", err)
	}

	token, err := svc.CreateSideEffectApprovalToken(ctx, CreateSideEffectApprovalTokenRequest{
		AttemptID:      attempt.ID,
		ApprovedBy:     caller.Subject,
		ApprovalReason: "user approved sending the email",
	})
	if err != nil {
		t.Fatalf("CreateSideEffectApprovalToken() error = %v", err)
	}
	if token.Token == "" {
		t.Fatal("token.Token is empty, want one-time secret")
	}
	if token.AttemptID != attempt.ID || token.RunID != attempt.RunID || token.TaskID != attempt.TaskID {
		t.Fatalf("token fence = (%s,%s,%s), want attempt/run/task (%s,%s,%s)", token.AttemptID, token.RunID, token.TaskID, attempt.ID, attempt.RunID, attempt.TaskID)
	}
	if token.ClaimEpoch != attempt.ClaimEpoch {
		t.Fatalf("token claim_epoch = %d, want %d", token.ClaimEpoch, attempt.ClaimEpoch)
	}
	if token.EnvSessionID != "" || token.EnvLeaseEpoch != 0 {
		t.Fatalf("token env fence = (%q,%d), want no-env zero fence", token.EnvSessionID, token.EnvLeaseEpoch)
	}

	rows, err := svc.queries.ListOrchestrationSideEffectApprovalTokensByAttempt(ctx, mustParsePGUUID(t, attempt.ID))
	if err != nil {
		t.Fatalf("ListOrchestrationSideEffectApprovalTokensByAttempt() error = %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("token rows len = %d, want 1", len(rows))
	}
	if rows[0].TokenHash == "" || strings.Contains(rows[0].TokenHash, token.Token) {
		t.Fatalf("stored token hash = %q, want hashed secret", rows[0].TokenHash)
	}
}
