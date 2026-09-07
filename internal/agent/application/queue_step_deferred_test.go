package application

import (
	"context"
	"errors"
	"testing"

	sessionruntime "github.com/felinics/memoh/internal/agent/runtime/session"
	sessionqueue "github.com/felinics/memoh/internal/agent/runtime/session/queue"
	"github.com/felinics/memoh/internal/agent/turn"
	messagepkg "github.com/felinics/memoh/internal/chat/message"
	dbstore "github.com/felinics/memoh/internal/db/store"
)

type nilQueries struct{ dbstore.Queries }

// newDeferredSteerTestService builds a Service whose queue step transaction
// can run without PostgreSQL: steps carry no messages, so history persistence
// is skipped, and queue state lives in a memory backend with one active run.
func newDeferredSteerTestService(t *testing.T) (*Service, sessionruntime.RunHandle) {
	t.Helper()
	backend := sessionruntime.NewMemoryBackend()
	key := sessionruntime.Key{BotID: "bot", SessionID: "session"}
	_, _, err := backend.Update(context.Background(), key, func(snapshot sessionruntime.Snapshot, _ bool) (sessionruntime.Snapshot, bool, error) {
		snapshot.BotID, snapshot.SessionID = key.BotID, key.SessionID
		snapshot.CurrentRunView = &sessionruntime.CurrentRunView{
			RunID: "run-1", TurnID: "turn-1", Generation: "gen-1", OwnerID: "owner-1", Status: sessionruntime.RunStatusRunning,
		}
		return snapshot, true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	manager := sessionruntime.NewManager(backend, sessionruntime.Options{OwnerID: "owner-1"})
	t.Cleanup(func() { _ = manager.Close() })
	service := &Service{
		sessionManager: manager,
		messageService: &recordingStepPersister{recordingMessageService: &recordingMessageService{}},
		queries:        nilQueries{},
	}
	handle := sessionruntime.RunHandle{
		BotID: key.BotID, SessionID: key.SessionID, RunID: "run-1", TurnID: "turn-1",
		OwnerID: "owner-1", Generation: "gen-1", FencingToken: 1,
	}
	return service, handle
}

func drainInject(ch chan turn.InjectMessage) []string {
	var texts []string
	for {
		select {
		case msg := <-ch:
			texts = append(texts, msg.Text)
		default:
			return texts
		}
	}
}

// A deferred step parks the loop, so its commit must not claim a steer: the
// inject channel is never read again and a claim would sit unapplied across
// the decision and across any owner change. The continuation's first committed
// step claims and injects it instead, and the next step applies it once.
func TestDeferredStepDoesNotClaimSteerAndContinuationDeliversIt(t *testing.T) {
	service, handle := newDeferredSteerTestService(t)
	ctx := context.Background()
	key := sessionruntime.Key{BotID: handle.BotID, SessionID: handle.SessionID}
	item, err := service.EnqueueSteer(ctx, handle.BotID, handle.SessionID, "invoke-1", []byte(`{"text":"steer me"}`))
	if err != nil {
		t.Fatal(err)
	}

	// Original run: the deferred step commits without touching the queue.
	parkedInject := make(chan turn.InjectMessage, 16)
	original := newQueueStepTransaction(service, ChatRequest{
		BotID: handle.BotID, ThreadID: handle.SessionID, RunID: handle.RunID,
		RunHandle: handle, QueueInjectCh: parkedInject,
	}, "model")
	if original == nil {
		t.Fatal("queue step transaction unavailable")
	}
	outcome, err := original.commit(ctx, 0, "", queueStepDeferredDecision, messagepkg.AgentStep{RunID: handle.RunID}, nil)
	if err != nil {
		t.Fatalf("deferred commit: %v", err)
	}
	if outcome.claimedSteer != nil || outcome.appliedSteerItemID != "" {
		t.Fatalf("deferred step touched the steer queue: %#v", outcome)
	}
	if got := drainInject(parkedInject); len(got) != 0 {
		t.Fatalf("parked inject channel received %v", got)
	}
	if steers, _, err := service.sessionManager.PendingQueues(ctx, key, 0); err != nil || len(steers) != 1 || steers[0].Status != sessionqueue.Accepted {
		t.Fatalf("steer should stay accepted across the park: %#v, %v", steers, err)
	}

	// Continuation after the decision: a fresh request, no QueueSteerClaim,
	// a fresh inject channel. This mirrors continueToolApprovalSession.
	continuationInject := make(chan turn.InjectMessage, 16)
	continuation := newQueueStepTransaction(service, ChatRequest{
		BotID: handle.BotID, ThreadID: handle.SessionID, RunID: handle.RunID,
		RunHandle: handle, QueueInjectCh: continuationInject, UserMessagePersisted: true,
	}, "model")
	if continuation == nil {
		t.Fatal("continuation queue step transaction unavailable")
	}

	// Step N+1: the model call that consumed the approved tool result. Its
	// commit claims the steer for step N+2.
	outcome, err = continuation.commit(ctx, 1, "", queueStepToolLoop, messagepkg.AgentStep{RunID: handle.RunID}, nil)
	if err != nil {
		t.Fatalf("continuation commit: %v", err)
	}
	if outcome.appliedSteerItemID != "" {
		t.Fatalf("continuation applied a steer it never injected: %#v", outcome)
	}
	if outcome.claimedSteer == nil || outcome.claimedSteer.ID != item.ID {
		t.Fatalf("continuation did not claim the steer: %#v", outcome)
	}
	if got := drainInject(continuationInject); len(got) != 1 || got[0] != "steer me" {
		t.Fatalf("continuation inject channel = %v", got)
	}
	steers, _, err := service.sessionManager.PendingQueues(ctx, key, 0)
	if err != nil || len(steers) != 0 {
		t.Fatalf("pending steers while claimed = %#v, %v", steers, err)
	}

	// Step N+2 saw the steer; its commit applies the claim exactly once.
	outcome, err = continuation.commit(ctx, 2, "", queueStepFinal, messagepkg.AgentStep{RunID: handle.RunID}, nil)
	if err != nil {
		t.Fatalf("final commit: %v", err)
	}
	if outcome.appliedSteerItemID != string(item.ID) || outcome.claimedSteer != nil || outcome.continueAfterFinal {
		t.Fatalf("final outcome = %#v", outcome)
	}
	if _, err := service.sessionManager.UpdateSteer(ctx, key, item.ID, []byte("x")); err == nil {
		t.Fatal("applied steer still mutable")
	}
	if _, err := service.EnqueueSteer(ctx, handle.BotID, handle.SessionID, "invoke-2", []byte(`{"text":"late"}`)); !errors.Is(err, sessionqueue.ErrNoActiveRun) {
		t.Fatalf("late steer after sealed final = %v", err)
	}
}
