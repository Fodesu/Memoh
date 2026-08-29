package application

import (
	"context"
	"errors"

	"github.com/google/uuid"

	sessionqueue "github.com/felinics/memoh/internal/agent/runtime/session/queue"
	db "github.com/felinics/memoh/internal/db"
	dbsqlc "github.com/felinics/memoh/internal/db/postgres/sqlc"
	dbstore "github.com/felinics/memoh/internal/db/store"
)

// SessionQueues is the application surface for user-facing queue operations.
// HTTP and channel adapters call these methods after authorization; the
// service owns transactions and the queue package owns the SQL primitives.
type SessionQueues struct {
	Steer    []sessionqueue.SteerItem
	FollowUp []sessionqueue.FollowUpItem
}

var errQueueTransactionsUnavailable = errors.New("session queue requires transactional queries")

func (s *Service) queueTx(ctx context.Context, fn func(dbstore.Queries) error) error {
	if s == nil || s.queries == nil {
		return errQueueTransactionsUnavailable
	}
	runner, ok := s.queries.(interface {
		InTx(context.Context, func(dbstore.Queries) error) error
	})
	if !ok {
		return errQueueTransactionsUnavailable
	}
	return runner.InTx(ctx, fn)
}

// EnqueueSteer admits one steer item for the session's active run. The SQL
// statement locks the session row, so no separate admission lock is taken.
func (s *Service) EnqueueSteer(ctx context.Context, botID, sessionID, invocationID string, payload []byte) (sessionqueue.SteerItem, error) {
	var item sessionqueue.SteerItem
	err := s.queueTx(ctx, func(q dbstore.Queries) error {
		var err error
		item, err = sessionqueue.NewPostgresStore(q).EnqueueSteer(ctx, botID, sessionID, uuid.NewString(), invocationID, payload)
		return err
	})
	return item, err
}

// EnqueueFollowUp admits one follow-up item during the session's active run.
func (s *Service) EnqueueFollowUp(ctx context.Context, botID, sessionID, invocationID string, payload []byte) (sessionqueue.FollowUpItem, error) {
	var item sessionqueue.FollowUpItem
	err := s.queueTx(ctx, func(q dbstore.Queries) error {
		var err error
		item, err = sessionqueue.NewPostgresStore(q).EnqueueFollowUp(ctx, botID, sessionID, uuid.NewString(), invocationID, payload)
		return err
	})
	return item, err
}

// ListSessionQueues returns the head of both pending queues from one
// statement snapshot. The list is bounded; a backlog beyond the limit is still
// consumed in FIFO order, it is only not rendered.
func (s *Service) ListSessionQueues(ctx context.Context, sessionID string) (SessionQueues, error) {
	if s == nil || s.queries == nil {
		return SessionQueues{}, errQueueTransactionsUnavailable
	}
	steers, followUps, err := sessionqueue.NewPostgresStore(s.queries).PendingQueues(ctx, sessionID, sessionqueue.DefaultPendingListLimit)
	if err != nil {
		return SessionQueues{}, err
	}
	return SessionQueues{Steer: steers, FollowUp: followUps}, nil
}

// ReorderSteer moves one accepted steer before another. The reorder statement
// locks the session row itself.
func (s *Service) ReorderSteer(ctx context.Context, sessionID string, item, before sessionqueue.SteerPendingRef) ([]sessionqueue.SteerItem, error) {
	var items []sessionqueue.SteerItem
	err := s.queueTx(ctx, func(q dbstore.Queries) error {
		var err error
		items, err = sessionqueue.NewPostgresStore(q).ReorderSteer(ctx, sessionID, item, before)
		return err
	})
	return items, err
}

func (s *Service) ReorderFollowUp(ctx context.Context, sessionID string, item, before sessionqueue.FollowUpPendingRef) ([]sessionqueue.FollowUpItem, error) {
	var items []sessionqueue.FollowUpItem
	err := s.queueTx(ctx, func(q dbstore.Queries) error {
		var err error
		items, err = sessionqueue.NewPostgresStore(q).ReorderFollowUp(ctx, sessionID, item, before)
		return err
	})
	return items, err
}

// UpdateSteer edits an accepted steer. The UPDATE is status-guarded, so a
// concurrent claim makes it return ErrNotPending instead of racing.
func (s *Service) UpdateSteer(ctx context.Context, sessionID, itemID string, payload []byte) (sessionqueue.SteerItem, error) {
	var item sessionqueue.SteerItem
	err := s.queueTx(ctx, func(q dbstore.Queries) error {
		var err error
		item, err = sessionqueue.NewPostgresStore(q).UpdateSteer(ctx, sessionID, itemID, payload)
		return err
	})
	return item, err
}

func (s *Service) UpdateFollowUp(ctx context.Context, sessionID, itemID string, payload []byte) (sessionqueue.FollowUpItem, error) {
	var item sessionqueue.FollowUpItem
	err := s.queueTx(ctx, func(q dbstore.Queries) error {
		var err error
		item, err = sessionqueue.NewPostgresStore(q).UpdateFollowUp(ctx, sessionID, itemID, payload)
		return err
	})
	return item, err
}

func (s *Service) CancelSteer(ctx context.Context, sessionID, itemID string) error {
	return s.queueTx(ctx, func(q dbstore.Queries) error {
		return sessionqueue.NewPostgresStore(q).CancelSteer(ctx, sessionID, itemID)
	})
}

func (s *Service) CancelFollowUp(ctx context.Context, sessionID, itemID string) error {
	return s.queueTx(ctx, func(q dbstore.Queries) error {
		return sessionqueue.NewPostgresStore(q).CancelFollowUp(ctx, sessionID, itemID)
	})
}

// PromoteFollowUpToSteer moves explicit user intent from the follow-up queue
// into the steer queue of the active run. The promotion reads, inserts, and
// cancels across two tables, so it takes the session admission lock first;
// CommitStep takes the same row lock, which serializes assignment against it.
func (s *Service) PromoteFollowUpToSteer(ctx context.Context, botID, sessionID string, followUp sessionqueue.FollowUpPendingRef) (sessionqueue.PromoteFollowUpResult, error) {
	var result sessionqueue.PromoteFollowUpResult
	err := s.queueTx(ctx, func(q dbstore.Queries) error {
		botUUID, err := db.ParseUUID(botID)
		if err != nil {
			return err
		}
		sessionUUID, err := db.ParseUUID(sessionID)
		if err != nil {
			return err
		}
		if _, err = q.LockSessionForQueueAdmission(ctx, dbsqlc.LockSessionForQueueAdmissionParams{BotID: botUUID, SessionID: sessionUUID}); err != nil {
			return err
		}
		result, err = sessionqueue.NewPromotionCoordinator(q).PromoteFollowUpToSteer(ctx, sessionqueue.PromoteFollowUpRequest{
			BotID: botID, SessionID: sessionID, FollowUp: followUp,
		})
		return err
	})
	return result, err
}
