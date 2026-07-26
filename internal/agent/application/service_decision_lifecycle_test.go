package application

import (
	"context"
	"errors"
	"testing"

	userinput "github.com/memohai/memoh/internal/agent/decision/input"
	"github.com/memohai/memoh/internal/runtimefence"
)

func TestEnsureSessionCanStartRunRejectsPendingUserInput(t *testing.T) {
	service := &Service{userInput: &fakeUserInputService{
		pending: []userinput.Request{{ID: "input-1", Status: userinput.StatusPending}},
	}}

	err := service.EnsureSessionCanStartRun(context.Background(), "bot-1", "session-1")
	if !errors.Is(err, ErrSessionDecisionPending) {
		t.Fatalf("EnsureSessionCanStartRun() error = %v, want pending decision", err)
	}
}

func TestEnsureSessionCanStartRunAllowsResolvedUserInput(t *testing.T) {
	service := &Service{userInput: &fakeUserInputService{}}
	if err := service.EnsureSessionCanStartRun(context.Background(), "bot-1", "session-1"); err != nil {
		t.Fatalf("EnsureSessionCanStartRun() error = %v", err)
	}
}

func TestCancelPendingSessionDecisionsAfterAbortCancelsDurably(t *testing.T) {
	fake := &fakeUserInputService{cancelSessionResult: []userinput.Request{{ID: "input-1"}}}
	service := &Service{userInput: fake}

	service.CancelPendingSessionDecisionsAfterAbort(context.Background(), "bot-1", "session-1")

	if len(fake.cancelSessionCalls) != 1 {
		t.Fatalf("cancel calls = %d, want 1", len(fake.cancelSessionCalls))
	}
	if want := "bot-1|session-1|" + abortedRunDecisionReason; fake.cancelSessionCalls[0] != want {
		t.Fatalf("cancel call = %q, want %q", fake.cancelSessionCalls[0], want)
	}
}

func TestCancelPendingSessionDecisionsAfterAbortToleratesStaleFence(t *testing.T) {
	// A newer run already activated its fence and superseded the decisions;
	// the aborted run's cancel must be a silent no-op, not a failure.
	fake := &fakeUserInputService{cancelSessionErr: runtimefence.ErrStale}
	service := &Service{userInput: fake}

	service.CancelPendingSessionDecisionsAfterAbort(context.Background(), "bot-1", "session-1")

	if len(fake.cancelSessionCalls) != 1 {
		t.Fatalf("cancel calls = %d, want 1", len(fake.cancelSessionCalls))
	}
}
