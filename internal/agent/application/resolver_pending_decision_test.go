package application

import (
	"context"
	"errors"
	"testing"

	userinput "github.com/memohai/memoh/internal/agent/decision/input"
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
