package orchestrationbus

import (
	"context"
	"errors"
)

// ErrSubscriptionClosed is returned by RunEventSubscription.Next when the
// subscription has been closed and no further events will be delivered.
var ErrSubscriptionClosed = errors.New("orchestrationbus: subscription closed")

// RunEventSubscription is a read-only handle for receiving committed run events
// from the bus. Implementations must guarantee:
//
//   - Events are delivered in monotonically increasing Seq order per run.
//   - Closing the subscription releases all backing resources (channels,
//     JetStream consumers).
//
// Subscriptions are scoped to a single run; cross-run fan-out is intentionally
// not exposed yet because the orchestration UI consumes one run at a time.
type RunEventSubscription interface {
	// Events returns a receive channel of envelopes. The channel is closed
	// after Close is invoked (or the bus shuts down).
	Events() <-chan RunEventEnvelope

	// Close stops the subscription. Safe to call multiple times.
	Close() error
}

// AttemptFactSubscription delivers attempt facts published by workerd and
// verifyd. Per-run subscriptions see one run; the global subscription
// (Bus.SubscribeAllAttemptFacts) sees them all. The kernel is responsible
// for matching each fact to its active attempt and dropping anything stale.
type AttemptFactSubscription interface {
	Facts() <-chan AttemptFactEnvelope
	Close() error
}

// RunEventPublisher publishes committed run events to the bus.
type RunEventPublisher interface {
	PublishRunEvent(ctx context.Context, env RunEventEnvelope) error
}

// AttemptFactPublisher publishes attempt facts to the bus.
type AttemptFactPublisher interface {
	PublishAttemptFact(ctx context.Context, env AttemptFactEnvelope) error
}

// Bus is the orchestration message bus.
//
// The outbox dispatcher, workerd and verifyd publish through it; the
// orchestration handler (WatchRun) and the kernel fact loop read from it.
// Implementations must be safe for concurrent use.
type Bus interface {
	RunEventPublisher
	AttemptFactPublisher

	SubscribeRunEvents(ctx context.Context, runID string) (RunEventSubscription, error)
	SubscribeAttemptFacts(ctx context.Context, runID string) (AttemptFactSubscription, error)

	// SubscribeAllAttemptFacts gives the kernel one channel for every
	// attempt fact so it does not have to track which runs are active.
	SubscribeAllAttemptFacts(ctx context.Context) (AttemptFactSubscription, error)

	// Close releases backing resources and closes outstanding subscriptions.
	Close() error
}
