package orchestrationfacts

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/memohai/memoh/internal/db/postgres/sqlc"
	"github.com/memohai/memoh/internal/orchestrationbus"
)

type fakeSource struct {
	attempts      map[string]sqlc.OrchestrationTaskAttempt
	verifications map[string]sqlc.OrchestrationTaskVerification
	attemptErr    error
	verifyErr     error
}

func (f *fakeSource) GetOrchestrationTaskAttemptByID(_ context.Context, id pgtype.UUID) (sqlc.OrchestrationTaskAttempt, error) {
	if f.attemptErr != nil {
		return sqlc.OrchestrationTaskAttempt{}, f.attemptErr
	}
	row, ok := f.attempts[id.String()]
	if !ok {
		return sqlc.OrchestrationTaskAttempt{}, pgx.ErrNoRows
	}
	return row, nil
}

func (f *fakeSource) GetOrchestrationTaskVerificationByID(_ context.Context, id pgtype.UUID) (sqlc.OrchestrationTaskVerification, error) {
	if f.verifyErr != nil {
		return sqlc.OrchestrationTaskVerification{}, f.verifyErr
	}
	row, ok := f.verifications[id.String()]
	if !ok {
		return sqlc.OrchestrationTaskVerification{}, pgx.ErrNoRows
	}
	return row, nil
}

func newPgUUID(t *testing.T, value string) pgtype.UUID {
	t.Helper()
	parsed, err := uuid.Parse(value)
	if err != nil {
		t.Fatalf("parse uuid %q: %v", value, err)
	}
	out := pgtype.UUID{Valid: true}
	copy(out.Bytes[:], parsed[:])
	return out
}

func discardLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

type recorder struct {
	mu    sync.Mutex
	pairs []struct {
		env     orchestrationbus.AttemptFactEnvelope
		outcome Outcome
	}
}

func (r *recorder) hook(env orchestrationbus.AttemptFactEnvelope, outcome Outcome) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pairs = append(r.pairs, struct {
		env     orchestrationbus.AttemptFactEnvelope
		outcome Outcome
	}{env, outcome})
}

func (r *recorder) wait(t *testing.T, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		r.mu.Lock()
		count := len(r.pairs)
		r.mu.Unlock()
		if count >= want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d outcomes (have %d)", want, count)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (r *recorder) outcome(idx int) Outcome {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.pairs[idx].outcome
}

func TestConsumerProcessAttemptAccepted(t *testing.T) {
	runID := uuid.NewString()
	taskID := uuid.NewString()
	attemptID := uuid.NewString()
	source := &fakeSource{
		attempts: map[string]sqlc.OrchestrationTaskAttempt{
			attemptID: {
				ID:         newPgUUID(t, attemptID),
				RunID:      newPgUUID(t, runID),
				TaskID:     newPgUUID(t, taskID),
				ClaimEpoch: 7,
				ClaimToken: "tok-7",
			},
		},
	}
	consumer := New(discardLogger(), source, nil)
	outcome := consumer.process(context.Background(), orchestrationbus.AttemptFactEnvelope{
		FactID:     "fact-1",
		RunID:      runID,
		TaskID:     taskID,
		AttemptID:  attemptID,
		ClaimEpoch: 7,
		ClaimToken: "tok-7",
		Type:       "attempt.started",
		ObservedAt: time.Now(),
	})
	if outcome.Status != StatusAccepted {
		t.Fatalf("expected accepted, got %+v", outcome)
	}
}

func TestConsumerProcessAttemptOrphan(t *testing.T) {
	runID := uuid.NewString()
	attemptID := uuid.NewString()
	source := &fakeSource{}
	consumer := New(discardLogger(), source, nil)
	outcome := consumer.process(context.Background(), orchestrationbus.AttemptFactEnvelope{
		FactID:     "fact-orphan",
		RunID:      runID,
		AttemptID:  attemptID,
		ClaimEpoch: 1,
		Type:       "attempt.completed",
		ObservedAt: time.Now(),
	})
	if outcome.Status != StatusOrphan {
		t.Fatalf("expected orphan, got %+v", outcome)
	}
}

func TestConsumerProcessAttemptStaleEpoch(t *testing.T) {
	runID := uuid.NewString()
	taskID := uuid.NewString()
	attemptID := uuid.NewString()
	source := &fakeSource{
		attempts: map[string]sqlc.OrchestrationTaskAttempt{
			attemptID: {
				ID:         newPgUUID(t, attemptID),
				RunID:      newPgUUID(t, runID),
				TaskID:     newPgUUID(t, taskID),
				ClaimEpoch: 9,
				ClaimToken: "tok-9",
			},
		},
	}
	consumer := New(discardLogger(), source, nil)
	outcome := consumer.process(context.Background(), orchestrationbus.AttemptFactEnvelope{
		FactID:     "fact-stale",
		RunID:      runID,
		TaskID:     taskID,
		AttemptID:  attemptID,
		ClaimEpoch: 5,
		Type:       "attempt.failed",
		ObservedAt: time.Now(),
	})
	if outcome.Status != StatusStale {
		t.Fatalf("expected stale, got %+v", outcome)
	}
}

func TestConsumerProcessAttemptIdentityMismatch(t *testing.T) {
	runID := uuid.NewString()
	otherRunID := uuid.NewString()
	taskID := uuid.NewString()
	attemptID := uuid.NewString()
	source := &fakeSource{
		attempts: map[string]sqlc.OrchestrationTaskAttempt{
			attemptID: {
				ID:         newPgUUID(t, attemptID),
				RunID:      newPgUUID(t, runID),
				TaskID:     newPgUUID(t, taskID),
				ClaimEpoch: 3,
			},
		},
	}
	consumer := New(discardLogger(), source, nil)
	outcome := consumer.process(context.Background(), orchestrationbus.AttemptFactEnvelope{
		FactID:     "fact-mismatch",
		RunID:      otherRunID,
		AttemptID:  attemptID,
		ClaimEpoch: 3,
		Type:       "attempt.started",
		ObservedAt: time.Now(),
	})
	if outcome.Status != StatusMismatch {
		t.Fatalf("expected mismatch, got %+v", outcome)
	}
	if outcome.Reason != "run_id" {
		t.Fatalf("expected reason run_id, got %q", outcome.Reason)
	}
}

func TestConsumerProcessVerificationAccepted(t *testing.T) {
	runID := uuid.NewString()
	taskID := uuid.NewString()
	verifID := uuid.NewString()
	source := &fakeSource{
		verifications: map[string]sqlc.OrchestrationTaskVerification{
			verifID: {
				ID:         newPgUUID(t, verifID),
				RunID:      newPgUUID(t, runID),
				TaskID:     newPgUUID(t, taskID),
				ClaimEpoch: 4,
				ClaimToken: "vtok",
			},
		},
	}
	consumer := New(discardLogger(), source, nil)
	outcome := consumer.process(context.Background(), orchestrationbus.AttemptFactEnvelope{
		FactID:     "fact-verif",
		RunID:      runID,
		TaskID:     taskID,
		AttemptID:  verifID,
		ClaimEpoch: 4,
		ClaimToken: "vtok",
		Type:       "verification.completed",
		ObservedAt: time.Now(),
	})
	if outcome.Class != ClassVerification {
		t.Fatalf("expected verification class, got %s", outcome.Class)
	}
	if outcome.Status != StatusAccepted {
		t.Fatalf("expected accepted, got %+v", outcome)
	}
}

func TestConsumerProcessUnknownClass(t *testing.T) {
	consumer := New(discardLogger(), &fakeSource{}, nil)
	outcome := consumer.process(context.Background(), orchestrationbus.AttemptFactEnvelope{
		FactID:     "fact-unknown",
		RunID:      uuid.NewString(),
		AttemptID:  uuid.NewString(),
		ClaimEpoch: 1,
		Type:       "weird.event",
		ObservedAt: time.Now(),
	})
	if outcome.Class != ClassUnknown || outcome.Status != StatusInvalid {
		t.Fatalf("expected unknown/invalid, got %+v", outcome)
	}
}

func TestConsumerRunDeliversThroughBus(t *testing.T) {
	bus := orchestrationbus.NewInMemoryBus(0)
	t.Cleanup(func() { _ = bus.Close() })

	runID := uuid.NewString()
	taskID := uuid.NewString()
	attemptID := uuid.NewString()
	verifID := uuid.NewString()

	source := &fakeSource{
		attempts: map[string]sqlc.OrchestrationTaskAttempt{
			attemptID: {
				ID:         newPgUUID(t, attemptID),
				RunID:      newPgUUID(t, runID),
				TaskID:     newPgUUID(t, taskID),
				ClaimEpoch: 2,
			},
		},
		verifications: map[string]sqlc.OrchestrationTaskVerification{
			verifID: {
				ID:         newPgUUID(t, verifID),
				RunID:      newPgUUID(t, runID),
				TaskID:     newPgUUID(t, taskID),
				ClaimEpoch: 2,
			},
		},
	}

	consumer := New(discardLogger(), source, bus)
	rec := &recorder{}
	consumer.SetHook(rec.hook)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	done := make(chan error, 1)
	go func() { done <- consumer.Run(ctx) }()

	select {
	case <-consumer.Ready():
	case <-time.After(2 * time.Second):
		t.Fatal("consumer did not become ready in time")
	}

	publish := func(env orchestrationbus.AttemptFactEnvelope) {
		t.Helper()
		if err := bus.PublishAttemptFact(context.Background(), env); err != nil {
			t.Fatalf("publish: %v", err)
		}
	}

	publish(orchestrationbus.AttemptFactEnvelope{
		FactID:     "f-attempt",
		RunID:      runID,
		TaskID:     taskID,
		AttemptID:  attemptID,
		ClaimEpoch: 2,
		Type:       "attempt.started",
		ObservedAt: time.Now(),
	})
	publish(orchestrationbus.AttemptFactEnvelope{
		FactID:     "f-verif",
		RunID:      runID,
		TaskID:     taskID,
		AttemptID:  verifID,
		ClaimEpoch: 2,
		Type:       "verification.completed",
		ObservedAt: time.Now(),
	})

	rec.wait(t, 2, 2*time.Second)
	if got := rec.outcome(0); got.Status != StatusAccepted || got.Class != ClassAttempt {
		t.Fatalf("attempt outcome wrong: %+v", got)
	}
	if got := rec.outcome(1); got.Status != StatusAccepted || got.Class != ClassVerification {
		t.Fatalf("verification outcome wrong: %+v", got)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop after cancel")
	}
}
