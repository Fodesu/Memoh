package application

import (
	"context"

	"github.com/google/uuid"

	sessionruntime "github.com/felinics/memoh/internal/agent/runtime/session"
	sessionqueue "github.com/felinics/memoh/internal/agent/runtime/session/queue"
)

// SessionQueues is the application surface for user-facing queue operations.
// Items are transient and live in the configured memory or Redis runtime.
type SessionQueues struct {
	Steer    []sessionqueue.SteerItem
	FollowUp []sessionqueue.FollowUpItem
}

func (s *Service) liveQueueRuntime() (*sessionruntime.Manager, error) {
	if s == nil || s.sessionManager == nil {
		return nil, sessionruntime.ErrLiveQueueUnavailable
	}
	return s.sessionManager, nil
}

func (s *Service) EnqueueSteer(ctx context.Context, botID, sessionID, invocationID string, payload []byte) (sessionqueue.SteerItem, error) {
	runtime, err := s.liveQueueRuntime()
	if err != nil {
		return sessionqueue.SteerItem{}, err
	}
	return runtime.EnqueueSteer(ctx, sessionruntime.Key{BotID: botID, SessionID: sessionID}, uuid.NewString(), invocationID, payload)
}

func (s *Service) EnqueueFollowUp(ctx context.Context, botID, sessionID, invocationID string, payload []byte) (sessionqueue.FollowUpItem, error) {
	runtime, err := s.liveQueueRuntime()
	if err != nil {
		return sessionqueue.FollowUpItem{}, err
	}
	item, err := runtime.EnqueueFollowUp(ctx, sessionruntime.Key{BotID: botID, SessionID: sessionID}, uuid.NewString(), invocationID, payload)
	if err != nil {
		return sessionqueue.FollowUpItem{}, err
	}
	if item.Status == sessionqueue.Accepted {
		s.kickFollowUpIfIdle(ctx, botID, sessionID, item.EnqueuedDuringRunID)
	}
	return item, nil
}

func (s *Service) ListSessionQueues(ctx context.Context, botID, sessionID string) (SessionQueues, error) {
	runtime, err := s.liveQueueRuntime()
	if err != nil {
		return SessionQueues{}, err
	}
	steers, followUps, err := runtime.PendingQueues(ctx, sessionruntime.Key{BotID: botID, SessionID: sessionID}, sessionqueue.DefaultPendingListLimit)
	if err != nil {
		return SessionQueues{}, err
	}
	return SessionQueues{Steer: steers, FollowUp: followUps}, nil
}

func (s *Service) ReorderSteer(ctx context.Context, botID, sessionID string, item, before sessionqueue.SteerPendingRef) ([]sessionqueue.SteerItem, error) {
	runtime, err := s.liveQueueRuntime()
	if err != nil {
		return nil, err
	}
	return runtime.ReorderSteer(ctx, sessionruntime.Key{BotID: botID, SessionID: sessionID}, item, before)
}

func (s *Service) ReorderFollowUp(ctx context.Context, botID, sessionID string, item, before sessionqueue.FollowUpPendingRef) ([]sessionqueue.FollowUpItem, error) {
	runtime, err := s.liveQueueRuntime()
	if err != nil {
		return nil, err
	}
	return runtime.ReorderFollowUp(ctx, sessionruntime.Key{BotID: botID, SessionID: sessionID}, item, before)
}

func (s *Service) UpdateSteer(ctx context.Context, botID, sessionID, itemID string, payload []byte) (sessionqueue.SteerItem, error) {
	runtime, err := s.liveQueueRuntime()
	if err != nil {
		return sessionqueue.SteerItem{}, err
	}
	return runtime.UpdateSteer(ctx, sessionruntime.Key{BotID: botID, SessionID: sessionID}, sessionqueue.SteerItemID(itemID), payload)
}

func (s *Service) UpdateFollowUp(ctx context.Context, botID, sessionID, itemID string, payload []byte) (sessionqueue.FollowUpItem, error) {
	runtime, err := s.liveQueueRuntime()
	if err != nil {
		return sessionqueue.FollowUpItem{}, err
	}
	return runtime.UpdateFollowUp(ctx, sessionruntime.Key{BotID: botID, SessionID: sessionID}, sessionqueue.FollowUpItemID(itemID), payload)
}

func (s *Service) CancelSteer(ctx context.Context, botID, sessionID, itemID string) error {
	runtime, err := s.liveQueueRuntime()
	if err != nil {
		return err
	}
	return runtime.CancelSteer(ctx, sessionruntime.Key{BotID: botID, SessionID: sessionID}, sessionqueue.SteerItemID(itemID))
}

func (s *Service) CancelFollowUp(ctx context.Context, botID, sessionID, itemID string) error {
	runtime, err := s.liveQueueRuntime()
	if err != nil {
		return err
	}
	return runtime.CancelFollowUp(ctx, sessionruntime.Key{BotID: botID, SessionID: sessionID}, sessionqueue.FollowUpItemID(itemID))
}

func (s *Service) PromoteFollowUpToSteer(ctx context.Context, botID, sessionID string, followUp sessionqueue.FollowUpPendingRef) (sessionqueue.PromoteFollowUpResult, error) {
	runtime, err := s.liveQueueRuntime()
	if err != nil {
		return sessionqueue.PromoteFollowUpResult{}, err
	}
	return runtime.PromoteFollowUpToSteer(ctx, sessionruntime.Key{BotID: botID, SessionID: sessionID}, followUp)
}
