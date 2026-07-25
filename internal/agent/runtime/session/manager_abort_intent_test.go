package sessionruntime

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestAbortIntentRejectsDeferredDecisionContinuation pins the backend CAS
// contract for the abort-versus-deferred-admission race: an abort that found
// no active run must still win against a decision continuation admitted
// afterwards, while ordinary runs clear the intent.
func TestAbortIntentRejectsDeferredDecisionContinuation(t *testing.T) {
	manager := NewManager(NewMemoryBackend(), Options{})
	ctx := context.Background()

	if aborted, err := manager.Abort(ctx, testBotID, testSessionID, "stale-stream"); err != nil || aborted {
		t.Fatalf("no-run abort = (%v, %v), want (false, nil)", aborted, err)
	}

	if _, err := manager.StartRunWithOptions(ctx, RunStartOptions{
		BotID: testBotID, SessionID: testSessionID, StreamID: "continuation-1",
		DecisionContinuation: true,
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("continuation inside abort intent window error = %v, want context.Canceled", err)
	}

	handle, err := manager.StartRunWithOptions(ctx, RunStartOptions{
		BotID: testBotID, SessionID: testSessionID, StreamID: "ordinary-1",
	})
	if err != nil {
		t.Fatalf("ordinary run after abort intent error = %v", err)
	}
	if err := manager.FinishRun(ctx, handle, RunStatusCompleted, ""); err != nil {
		t.Fatalf("finish ordinary run: %v", err)
	}

	handle, err = manager.StartRunWithOptions(ctx, RunStartOptions{
		BotID: testBotID, SessionID: testSessionID, StreamID: "continuation-2",
		DecisionContinuation: true,
	})
	if err != nil {
		t.Fatalf("continuation after intent cleared error = %v", err)
	}
	if err := manager.FinishRun(ctx, handle, RunStatusCompleted, ""); err != nil {
		t.Fatalf("finish continuation: %v", err)
	}
}

// TestAbortIntentExpiresWithGraceWindow keeps a stale intent from blocking a
// later legitimate decision continuation forever.
func TestAbortIntentExpiresWithGraceWindow(t *testing.T) {
	manager := NewManager(NewMemoryBackend(), Options{AbortGraceTimeout: 10 * time.Millisecond})
	ctx := context.Background()

	if aborted, err := manager.Abort(ctx, testBotID, testSessionID, "stale-stream"); err != nil || aborted {
		t.Fatalf("no-run abort = (%v, %v), want (false, nil)", aborted, err)
	}
	time.Sleep(30 * time.Millisecond)

	handle, err := manager.StartRunWithOptions(ctx, RunStartOptions{
		BotID: testBotID, SessionID: testSessionID, StreamID: "continuation-after-expiry",
		DecisionContinuation: true,
	})
	if err != nil {
		t.Fatalf("continuation after intent expiry error = %v", err)
	}
	if err := manager.FinishRun(ctx, handle, RunStatusCompleted, ""); err != nil {
		t.Fatalf("finish continuation: %v", err)
	}
}
