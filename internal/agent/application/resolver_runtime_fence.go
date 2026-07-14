package application

import (
	"context"
	"errors"
	"fmt"

	dbpkg "github.com/memohai/memoh/internal/db"
	"github.com/memohai/memoh/internal/runtimefence"
)

var ErrRuntimeDecisionOwnerUnavailable = errors.New("runtime decision owner is unavailable on this server")

type runtimeFenceAllocator interface {
	NextSessionRuntimeFenceToken(ctx context.Context) (int64, error)
}

// AllocateRuntimePersistenceFence reserves a globally monotonic token before
// Redis admission. Tokens from rejected admissions are harmless gaps.
func (s *Service) AllocateRuntimePersistenceFence(ctx context.Context, botID, sessionID string) (runtimefence.Fence, error) {
	if s == nil || s.queries == nil {
		return runtimefence.Fence{}, errors.New("runtime persistence store is not configured")
	}
	allocator, ok := s.queries.(runtimeFenceAllocator)
	if !ok {
		return runtimefence.Fence{}, errors.New("runtime persistence store does not support fence allocation")
	}
	if _, err := dbpkg.ParseUUID(botID); err != nil {
		return runtimefence.Fence{}, fmt.Errorf("invalid runtime fence bot id: %w", err)
	}
	if _, err := dbpkg.ParseUUID(sessionID); err != nil {
		return runtimefence.Fence{}, fmt.Errorf("invalid runtime fence session id: %w", err)
	}
	token, err := allocator.NextSessionRuntimeFenceToken(ctx)
	if err != nil {
		return runtimefence.Fence{}, fmt.Errorf("allocate runtime persistence fence: %w", err)
	}
	fence := runtimefence.Fence{BotID: botID, SessionID: sessionID, Token: token}
	if !fence.Valid() {
		return runtimefence.Fence{}, errors.New("runtime persistence store returned an invalid fence")
	}
	return fence, nil
}

// ActivateRuntimePersistenceFence makes an admitted run authoritative for
// PostgreSQL writes before the agent starts executing.
func (s *Service) ActivateRuntimePersistenceFence(ctx context.Context, fence runtimefence.Fence) error {
	return s.ActivateRuntimePersistenceFenceWithOptions(ctx, fence, runtimefence.ActivationOptions{})
}

func (s *Service) ActivateRuntimePersistenceFenceWithOptions(ctx context.Context, fence runtimefence.Fence, options runtimefence.ActivationOptions) error {
	if s == nil || s.queries == nil {
		return errors.New("runtime persistence store is not configured")
	}
	if !fence.Valid() {
		return errors.New("runtime persistence fence is invalid")
	}
	return runtimefence.ActivateWithOptions(ctx, s.queries, fence, options)
}
