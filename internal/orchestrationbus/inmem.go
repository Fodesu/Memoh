package orchestrationbus

import (
	"context"
	"sync"

	"github.com/google/uuid"
)

const defaultInMemBuffer = 128

// InMemoryBus is a process-local Bus implementation used by tests and by
// deployments where NATS is not configured. It fans out published envelopes to
// all matching subscribers in order, dropping events for any subscriber whose
// channel buffer is full so a slow consumer cannot stall the kernel.
type InMemoryBus struct {
	mu             sync.RWMutex
	closed         bool
	bufferSize     int
	runEvents      map[string]map[string]chan RunEventEnvelope
	attemptFacts   map[string]map[string]chan AttemptFactEnvelope
	globalFactSubs map[string]chan AttemptFactEnvelope
}

// NewInMemoryBus returns a ready-to-use process-local bus. bufferSize controls
// the per-subscriber channel capacity; pass 0 for the default.
func NewInMemoryBus(bufferSize int) *InMemoryBus {
	if bufferSize <= 0 {
		bufferSize = defaultInMemBuffer
	}
	return &InMemoryBus{
		bufferSize:     bufferSize,
		runEvents:      map[string]map[string]chan RunEventEnvelope{},
		attemptFacts:   map[string]map[string]chan AttemptFactEnvelope{},
		globalFactSubs: map[string]chan AttemptFactEnvelope{},
	}
}

// PublishRunEvent fans out env to subscribers of the matching run. The provided
// context is honoured: if it is already cancelled, the envelope is not
// dispatched.
func (b *InMemoryBus) PublishRunEvent(ctx context.Context, env RunEventEnvelope) error {
	if err := env.Validate(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.closed {
		return ErrSubscriptionClosed
	}

	channels := b.runEvents[env.RunID]
	for _, ch := range channels {
		select {
		case ch <- env:
		default:
			// Drop event for slow subscriber rather than block the publisher.
		}
	}
	return nil
}

// PublishAttemptFact fans out fact to subscribers of the matching run.
func (b *InMemoryBus) PublishAttemptFact(ctx context.Context, env AttemptFactEnvelope) error {
	if err := env.Validate(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.closed {
		return ErrSubscriptionClosed
	}

	channels := b.attemptFacts[env.RunID]
	for _, ch := range channels {
		select {
		case ch <- env:
		default:
		}
	}
	for _, ch := range b.globalFactSubs {
		select {
		case ch <- env:
		default:
		}
	}
	return nil
}

// SubscribeRunEvents registers a per-run subscription. The returned
// subscription channel is closed when Close is invoked or when the bus shuts
// down. The provided context is consulted only at subscription time; callers
// should call Close when done.
func (b *InMemoryBus) SubscribeRunEvents(ctx context.Context, runID string) (RunEventSubscription, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, ErrSubscriptionClosed
	}

	subs, ok := b.runEvents[runID]
	if !ok {
		subs = map[string]chan RunEventEnvelope{}
		b.runEvents[runID] = subs
	}

	id := uuid.NewString()
	ch := make(chan RunEventEnvelope, b.bufferSize)
	subs[id] = ch

	return &inMemRunSub{
		id:    id,
		runID: runID,
		bus:   b,
		ch:    ch,
	}, nil
}

// SubscribeAttemptFacts registers a per-run fact subscription.
func (b *InMemoryBus) SubscribeAttemptFacts(ctx context.Context, runID string) (AttemptFactSubscription, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, ErrSubscriptionClosed
	}

	subs, ok := b.attemptFacts[runID]
	if !ok {
		subs = map[string]chan AttemptFactEnvelope{}
		b.attemptFacts[runID] = subs
	}

	id := uuid.NewString()
	ch := make(chan AttemptFactEnvelope, b.bufferSize)
	subs[id] = ch

	return &inMemFactSub{
		id:    id,
		runID: runID,
		bus:   b,
		ch:    ch,
	}, nil
}

// SubscribeAllAttemptFacts registers a global subscription that sees every
// fact regardless of run. The kernel fact loop uses this so it does not have
// to track which runs are active.
func (b *InMemoryBus) SubscribeAllAttemptFacts(ctx context.Context) (AttemptFactSubscription, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, ErrSubscriptionClosed
	}

	id := uuid.NewString()
	ch := make(chan AttemptFactEnvelope, b.bufferSize)
	b.globalFactSubs[id] = ch

	return &inMemFactSub{
		id:     id,
		bus:    b,
		ch:     ch,
		global: true,
	}, nil
}

// Close releases all subscriptions and rejects subsequent publishes.
func (b *InMemoryBus) Close() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true

	runEvents := b.runEvents
	attemptFacts := b.attemptFacts
	globalFactSubs := b.globalFactSubs
	b.runEvents = map[string]map[string]chan RunEventEnvelope{}
	b.attemptFacts = map[string]map[string]chan AttemptFactEnvelope{}
	b.globalFactSubs = map[string]chan AttemptFactEnvelope{}
	b.mu.Unlock()

	for _, subs := range runEvents {
		for _, ch := range subs {
			close(ch)
		}
	}
	for _, subs := range attemptFacts {
		for _, ch := range subs {
			close(ch)
		}
	}
	for _, ch := range globalFactSubs {
		close(ch)
	}
	return nil
}

func (b *InMemoryBus) removeRunEventSub(runID, id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	subs := b.runEvents[runID]
	if subs == nil {
		return
	}
	if ch, ok := subs[id]; ok {
		delete(subs, id)
		close(ch)
	}
	if len(subs) == 0 {
		delete(b.runEvents, runID)
	}
}

func (b *InMemoryBus) removeAttemptFactSub(runID, id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	subs := b.attemptFacts[runID]
	if subs == nil {
		return
	}
	if ch, ok := subs[id]; ok {
		delete(subs, id)
		close(ch)
	}
	if len(subs) == 0 {
		delete(b.attemptFacts, runID)
	}
}

func (b *InMemoryBus) removeGlobalFactSub(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if ch, ok := b.globalFactSubs[id]; ok {
		delete(b.globalFactSubs, id)
		close(ch)
	}
}

type inMemRunSub struct {
	id    string
	runID string
	bus   *InMemoryBus
	ch    chan RunEventEnvelope
	once  sync.Once
}

func (s *inMemRunSub) Events() <-chan RunEventEnvelope { return s.ch }

func (s *inMemRunSub) Close() error {
	s.once.Do(func() { s.bus.removeRunEventSub(s.runID, s.id) })
	return nil
}

type inMemFactSub struct {
	id     string
	runID  string
	bus    *InMemoryBus
	ch     chan AttemptFactEnvelope
	global bool
	once   sync.Once
}

func (s *inMemFactSub) Facts() <-chan AttemptFactEnvelope { return s.ch }

func (s *inMemFactSub) Close() error {
	s.once.Do(func() {
		if s.global {
			s.bus.removeGlobalFactSub(s.id)
			return
		}
		s.bus.removeAttemptFactSub(s.runID, s.id)
	})
	return nil
}
