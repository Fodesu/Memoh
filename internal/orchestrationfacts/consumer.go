// Package orchestrationfacts validates the attempt.* / verification.*
// envelopes that workerd and verifyd publish to the bus. The Consumer
// subscribes globally and cross-checks each envelope against Postgres so we
// can detect orphans, stale claims, and identity mismatches before the bus
// contract is trusted to drive state.
//
// It is read-only for now. Postgres is still the source of truth and the
// daemons still commit transitions through direct service calls. When every
// state change has a matching observed fact, the consumer can take over
// writes without the envelope schema changing.
package orchestrationfacts

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/memohai/memoh/internal/db"
	"github.com/memohai/memoh/internal/db/postgres/sqlc"
	"github.com/memohai/memoh/internal/orchestrationbus"
)

// Source is the read surface used to validate envelopes. cmd/agent passes a
// *sqlc.Queries; tests pass a fake.
type Source interface {
	GetOrchestrationTaskAttemptByID(ctx context.Context, id pgtype.UUID) (sqlc.OrchestrationTaskAttempt, error)
	GetOrchestrationTaskVerificationByID(ctx context.Context, id pgtype.UUID) (sqlc.OrchestrationTaskVerification, error)
}

// Subscriber is the slice of orchestrationbus.Bus the consumer actually uses.
// Splitting it out keeps the unit tests from pulling in the full bus surface.
type Subscriber interface {
	SubscribeAllAttemptFacts(ctx context.Context) (orchestrationbus.AttemptFactSubscription, error)
}

// Class names the orchestration entity a fact is about.
type Class string

const (
	ClassAttempt      Class = "attempt"
	ClassVerification Class = "verification"
	ClassUnknown      Class = "unknown"
)

// Status is the validation verdict for one envelope.
type Status string

const (
	// StatusAccepted: the envelope matches the Postgres row on run, task,
	// claim_epoch, and (when present) claim_token.
	StatusAccepted Status = "accepted"
	// StatusStale: the row exists, but the envelope was emitted under an
	// older claim (lower claim_epoch or a different claim_token).
	StatusStale Status = "stale"
	// StatusOrphan: no matching row in Postgres. Workers should never produce
	// this in steady state; seeing it means the bus or the kernel is wrong.
	StatusOrphan Status = "orphan"
	// StatusMismatch: the envelope and the row disagree on run_id or task_id.
	// Normally a bug or a manual replay.
	StatusMismatch Status = "mismatch"
	// StatusInvalid: the envelope failed structural validation, or the
	// lookup itself blew up.
	StatusInvalid Status = "invalid"
)

// Outcome is what the consumer recorded for one envelope. Exposed so tests
// can assert against the same data the logs carry.
type Outcome struct {
	Class  Class
	Status Status
	Reason string
}

// Hook fires after every envelope. Production leaves it nil; tests use it
// instead of scraping logs.
type Hook func(env orchestrationbus.AttemptFactEnvelope, outcome Outcome)

// Consumer subscribes to attempt facts and checks them against Postgres.
type Consumer struct {
	logger  *slog.Logger
	source  Source
	bus     Subscriber
	hook    Hook
	ready   chan struct{}
	readyMu sync.Mutex
}

// New returns a consumer ready for Run. A nil logger falls back to slog.Default.
func New(logger *slog.Logger, source Source, bus Subscriber) *Consumer {
	if logger == nil {
		logger = slog.Default()
	}
	return &Consumer{
		logger: logger.With(slog.String("component", "orchestrationfacts.consumer")),
		source: source,
		bus:    bus,
		ready:  make(chan struct{}),
	}
}

// Ready is closed once the consumer is subscribed and dispatching. Tests
// wait on it before publishing so they don't race the subscription.
func (c *Consumer) Ready() <-chan struct{} {
	c.readyMu.Lock()
	defer c.readyMu.Unlock()
	if c.ready == nil {
		c.ready = make(chan struct{})
	}
	return c.ready
}

func (c *Consumer) signalReady() {
	c.readyMu.Lock()
	defer c.readyMu.Unlock()
	if c.ready == nil {
		c.ready = make(chan struct{})
	}
	select {
	case <-c.ready:
		// already closed
	default:
		close(c.ready)
	}
}

// SetHook attaches a callback called after every envelope. It is safe to
// call on a running consumer, but the swap is not synchronised with
// in-flight envelopes. Tests usually set the hook before Run.
func (c *Consumer) SetHook(hook Hook) {
	c.hook = hook
}

// Run blocks until ctx is cancelled or the bus drops the subscription.
// Returns nil on context cancel, ErrSubscriptionClosed if the bus closed
// the channel from under us.
func (c *Consumer) Run(ctx context.Context) error {
	sub, err := c.bus.SubscribeAllAttemptFacts(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = sub.Close() }()
	c.signalReady()

	for {
		select {
		case <-ctx.Done():
			return nil
		case env, ok := <-sub.Facts():
			if !ok {
				return orchestrationbus.ErrSubscriptionClosed
			}
			outcome := c.process(ctx, env)
			if c.hook != nil {
				c.hook(env, outcome)
			}
		}
	}
}

func (c *Consumer) process(ctx context.Context, env orchestrationbus.AttemptFactEnvelope) Outcome {
	if err := env.Validate(); err != nil {
		c.logger.Warn("attempt fact rejected: invalid envelope",
			slog.String("fact_id", env.FactID),
			slog.String("type", env.Type),
			slog.Any("error", err),
		)
		return Outcome{Class: ClassUnknown, Status: StatusInvalid, Reason: err.Error()}
	}
	class := classify(env.Type)
	switch class {
	case ClassAttempt:
		return c.processAttempt(ctx, env)
	case ClassVerification:
		return c.processVerification(ctx, env)
	default:
		c.logger.Warn("attempt fact has unknown class",
			slog.String("fact_id", env.FactID),
			slog.String("type", env.Type),
		)
		return Outcome{Class: ClassUnknown, Status: StatusInvalid, Reason: "unknown fact type"}
	}
}

func (c *Consumer) processAttempt(ctx context.Context, env orchestrationbus.AttemptFactEnvelope) Outcome {
	pgID, err := db.ParseUUID(env.AttemptID)
	if err != nil {
		c.logger.Warn("attempt fact has unparseable attempt id",
			slog.String("attempt_id", env.AttemptID),
			slog.String("type", env.Type),
		)
		return Outcome{Class: ClassAttempt, Status: StatusInvalid, Reason: "invalid attempt_id"}
	}
	row, err := c.source.GetOrchestrationTaskAttemptByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.logger.Warn("attempt fact orphan",
				slog.String("attempt_id", env.AttemptID),
				slog.String("run_id", env.RunID),
				slog.String("type", env.Type),
			)
			return Outcome{Class: ClassAttempt, Status: StatusOrphan, Reason: "attempt not found"}
		}
		c.logger.Error("attempt fact lookup failed",
			slog.String("attempt_id", env.AttemptID),
			slog.String("type", env.Type),
			slog.Any("error", err),
		)
		return Outcome{Class: ClassAttempt, Status: StatusInvalid, Reason: err.Error()}
	}
	if mismatch, reason := identityMismatch(env.RunID, row.RunID.String(), env.TaskID, row.TaskID.String()); mismatch {
		c.logger.Warn("attempt fact identity mismatch",
			slog.String("attempt_id", env.AttemptID),
			slog.String("type", env.Type),
			slog.String("reason", reason),
		)
		return Outcome{Class: ClassAttempt, Status: StatusMismatch, Reason: reason}
	}
	if env.ClaimEpoch < row.ClaimEpoch {
		c.logger.Info("attempt fact stale: lower claim_epoch",
			slog.String("attempt_id", env.AttemptID),
			slog.String("type", env.Type),
			slog.Int64("envelope_epoch", env.ClaimEpoch),
			slog.Int64("row_epoch", row.ClaimEpoch),
		)
		return Outcome{Class: ClassAttempt, Status: StatusStale, Reason: "claim_epoch behind row"}
	}
	if env.ClaimToken != "" && row.ClaimToken != "" && env.ClaimToken != row.ClaimToken {
		c.logger.Info("attempt fact stale: claim_token mismatch",
			slog.String("attempt_id", env.AttemptID),
			slog.String("type", env.Type),
		)
		return Outcome{Class: ClassAttempt, Status: StatusStale, Reason: "claim_token mismatch"}
	}
	c.logger.Debug("attempt fact accepted",
		slog.String("attempt_id", env.AttemptID),
		slog.String("type", env.Type),
		slog.Int64("claim_epoch", env.ClaimEpoch),
	)
	return Outcome{Class: ClassAttempt, Status: StatusAccepted}
}

func (c *Consumer) processVerification(ctx context.Context, env orchestrationbus.AttemptFactEnvelope) Outcome {
	pgID, err := db.ParseUUID(env.AttemptID)
	if err != nil {
		c.logger.Warn("verification fact has unparseable id",
			slog.String("verification_id", env.AttemptID),
			slog.String("type", env.Type),
		)
		return Outcome{Class: ClassVerification, Status: StatusInvalid, Reason: "invalid verification id"}
	}
	row, err := c.source.GetOrchestrationTaskVerificationByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.logger.Warn("verification fact orphan",
				slog.String("verification_id", env.AttemptID),
				slog.String("run_id", env.RunID),
				slog.String("type", env.Type),
			)
			return Outcome{Class: ClassVerification, Status: StatusOrphan, Reason: "verification not found"}
		}
		c.logger.Error("verification fact lookup failed",
			slog.String("verification_id", env.AttemptID),
			slog.String("type", env.Type),
			slog.Any("error", err),
		)
		return Outcome{Class: ClassVerification, Status: StatusInvalid, Reason: err.Error()}
	}
	if mismatch, reason := identityMismatch(env.RunID, row.RunID.String(), env.TaskID, row.TaskID.String()); mismatch {
		c.logger.Warn("verification fact identity mismatch",
			slog.String("verification_id", env.AttemptID),
			slog.String("type", env.Type),
			slog.String("reason", reason),
		)
		return Outcome{Class: ClassVerification, Status: StatusMismatch, Reason: reason}
	}
	if env.ClaimEpoch < row.ClaimEpoch {
		c.logger.Info("verification fact stale: lower claim_epoch",
			slog.String("verification_id", env.AttemptID),
			slog.String("type", env.Type),
			slog.Int64("envelope_epoch", env.ClaimEpoch),
			slog.Int64("row_epoch", row.ClaimEpoch),
		)
		return Outcome{Class: ClassVerification, Status: StatusStale, Reason: "claim_epoch behind row"}
	}
	if env.ClaimToken != "" && row.ClaimToken != "" && env.ClaimToken != row.ClaimToken {
		c.logger.Info("verification fact stale: claim_token mismatch",
			slog.String("verification_id", env.AttemptID),
			slog.String("type", env.Type),
		)
		return Outcome{Class: ClassVerification, Status: StatusStale, Reason: "claim_token mismatch"}
	}
	c.logger.Debug("verification fact accepted",
		slog.String("verification_id", env.AttemptID),
		slog.String("type", env.Type),
		slog.Int64("claim_epoch", env.ClaimEpoch),
	)
	return Outcome{Class: ClassVerification, Status: StatusAccepted}
}

func classify(t string) Class {
	switch {
	case strings.HasPrefix(t, "attempt."):
		return ClassAttempt
	case strings.HasPrefix(t, "verification."):
		return ClassVerification
	default:
		return ClassUnknown
	}
}

func identityMismatch(envRun, rowRun, envTask, rowTask string) (bool, string) {
	if envRun != rowRun {
		return true, "run_id"
	}
	if envTask != "" && rowTask != "" && envTask != rowTask {
		return true, "task_id"
	}
	return false, ""
}
