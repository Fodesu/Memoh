package orchestrationbus

import (
	"context"
	"math"
	"testing"
	"time"
)

func newRunEnvelope(runID, eventType string, seq uint64) RunEventEnvelope {
	return RunEventEnvelope{
		EventID:          eventType + "-" + runID,
		RunID:            runID,
		Seq:              seq,
		AggregateType:    "run",
		AggregateID:      runID,
		AggregateVersion: seq,
		Type:             eventType,
		CreatedAt:        time.Unix(0, int64(seq&math.MaxInt64)), //nolint:gosec // test seqs fit in int64
		PublishedAt:      time.Unix(0, int64(seq&math.MaxInt64)), //nolint:gosec // test seqs fit in int64
	}
}

func newFactEnvelope(runID, attemptID, factType string, epoch int64) AttemptFactEnvelope {
	return AttemptFactEnvelope{
		FactID:     factType + "-" + attemptID,
		RunID:      runID,
		AttemptID:  attemptID,
		ClaimEpoch: epoch,
		Type:       factType,
		ObservedAt: time.Now(),
	}
}

func TestInMemoryBusRunEventRoundTrip(t *testing.T) {
	ctx := context.Background()
	bus := NewInMemoryBus(0)
	t.Cleanup(func() { _ = bus.Close() })

	subA, err := bus.SubscribeRunEvents(ctx, "run-1")
	if err != nil {
		t.Fatalf("subscribe a: %v", err)
	}
	subB, err := bus.SubscribeRunEvents(ctx, "run-1")
	if err != nil {
		t.Fatalf("subscribe b: %v", err)
	}
	otherSub, err := bus.SubscribeRunEvents(ctx, "run-2")
	if err != nil {
		t.Fatalf("subscribe other: %v", err)
	}

	if err := bus.PublishRunEvent(ctx, newRunEnvelope("run-1", "task.created", 1)); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := bus.PublishRunEvent(ctx, newRunEnvelope("run-1", "task.completed", 2)); err != nil {
		t.Fatalf("publish 2: %v", err)
	}

	for label, sub := range map[string]RunEventSubscription{"a": subA, "b": subB} {
		want := []string{"task.created", "task.completed"}
		for _, expected := range want {
			select {
			case env := <-sub.Events():
				if env.Type != expected {
					t.Fatalf("subscriber %s: expected %s, got %s", label, expected, env.Type)
				}
			case <-time.After(time.Second):
				t.Fatalf("subscriber %s: timeout waiting for %s", label, expected)
			}
		}
	}

	select {
	case env := <-otherSub.Events():
		t.Fatalf("other run subscriber should not receive run-1 events, got %s", env.Type)
	case <-time.After(50 * time.Millisecond):
	}

	_ = subB.Close()
	if err := bus.PublishRunEvent(ctx, newRunEnvelope("run-1", "task.failed", 3)); err != nil {
		t.Fatalf("publish 3: %v", err)
	}
	select {
	case env := <-subA.Events():
		if env.Type != "task.failed" {
			t.Fatalf("expected task.failed after sub b closed, got %s", env.Type)
		}
	case <-time.After(time.Second):
		t.Fatalf("subscriber a did not receive event after b closed")
	}

	if _, ok := <-subB.Events(); ok {
		t.Fatalf("closed subscriber b channel still delivers events")
	}
}

func TestInMemoryBusValidatesEnvelopes(t *testing.T) {
	ctx := context.Background()
	bus := NewInMemoryBus(0)
	t.Cleanup(func() { _ = bus.Close() })

	bad := RunEventEnvelope{Type: "noop", RunID: "run", EventID: "id"}
	if err := bus.PublishRunEvent(ctx, bad); err == nil {
		t.Fatalf("expected validation error, got nil")
	}

	badFact := AttemptFactEnvelope{RunID: "run", AttemptID: "a", FactID: "f", Type: "x"}
	if err := bus.PublishAttemptFact(ctx, badFact); err == nil {
		t.Fatalf("expected fact validation error, got nil")
	}
}

func TestInMemoryBusAttemptFactRoundTrip(t *testing.T) {
	ctx := context.Background()
	bus := NewInMemoryBus(0)
	t.Cleanup(func() { _ = bus.Close() })

	sub, err := bus.SubscribeAttemptFacts(ctx, "run-1")
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	if err := bus.PublishAttemptFact(ctx, newFactEnvelope("run-1", "att-1", "heartbeat", 1)); err != nil {
		t.Fatalf("publish: %v", err)
	}

	select {
	case env := <-sub.Facts():
		if env.AttemptID != "att-1" {
			t.Fatalf("expected att-1 fact, got %+v", env)
		}
	case <-time.After(time.Second):
		t.Fatalf("timeout waiting for fact")
	}
}

func TestInMemoryBusGlobalAttemptFactSubscription(t *testing.T) {
	ctx := context.Background()
	bus := NewInMemoryBus(0)
	t.Cleanup(func() { _ = bus.Close() })

	global, err := bus.SubscribeAllAttemptFacts(ctx)
	if err != nil {
		t.Fatalf("subscribe all: %v", err)
	}
	scoped, err := bus.SubscribeAttemptFacts(ctx, "run-1")
	if err != nil {
		t.Fatalf("subscribe run-1: %v", err)
	}

	if err := bus.PublishAttemptFact(ctx, newFactEnvelope("run-1", "att-1", "attempt.started", 1)); err != nil {
		t.Fatalf("publish run-1: %v", err)
	}
	if err := bus.PublishAttemptFact(ctx, newFactEnvelope("run-2", "att-9", "attempt.completed", 1)); err != nil {
		t.Fatalf("publish run-2: %v", err)
	}

	wantTypes := []string{"attempt.started", "attempt.completed"}
	for _, expected := range wantTypes {
		select {
		case env := <-global.Facts():
			if env.Type != expected {
				t.Fatalf("global subscriber expected %s, got %s", expected, env.Type)
			}
		case <-time.After(time.Second):
			t.Fatalf("global subscriber: timeout waiting for %s", expected)
		}
	}

	select {
	case env := <-scoped.Facts():
		if env.RunID != "run-1" {
			t.Fatalf("scoped subscriber should only see run-1, got %s", env.RunID)
		}
	case <-time.After(time.Second):
		t.Fatal("scoped subscriber did not receive run-1 fact")
	}
	select {
	case env := <-scoped.Facts():
		t.Fatalf("scoped subscriber should not receive run-2 fact: %+v", env)
	case <-time.After(50 * time.Millisecond):
	}

	if err := global.Close(); err != nil {
		t.Fatalf("close global: %v", err)
	}
	if err := bus.PublishAttemptFact(ctx, newFactEnvelope("run-3", "att-3", "attempt.started", 1)); err != nil {
		t.Fatalf("publish after global close: %v", err)
	}
	select {
	case env, ok := <-global.Facts():
		if ok {
			t.Fatalf("closed global subscriber received fact: %+v", env)
		}
	case <-time.After(100 * time.Millisecond):
	}
}

func TestInMemoryBusCloseUnblocksSubscribers(t *testing.T) {
	ctx := context.Background()
	bus := NewInMemoryBus(0)

	sub, err := bus.SubscribeRunEvents(ctx, "run-1")
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range sub.Events() {
		}
	}()

	if err := bus.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("subscriber did not exit after bus close")
	}

	if err := bus.PublishRunEvent(ctx, newRunEnvelope("run-1", "task.created", 99)); err == nil {
		t.Fatalf("expected publish to fail after close")
	}
}
