package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/memohai/memoh/internal/runtimefence"
)

// ErrSessionDecisionPending means a session has a durable interactive decision
// that must be resolved before starting another ordinary run.
var ErrSessionDecisionPending = errors.New("session has a pending interactive decision")

// EnsureSessionCanStartRun prevents a new send, retry, or edit from creating a
// second conversation branch while an approval or ask_user response is pending.
// Decision response continuations deliberately do not call this method.
func (s *Service) EnsureSessionCanStartRun(ctx context.Context, botID, sessionID string) error {
	if s == nil {
		return nil
	}
	botID = strings.TrimSpace(botID)
	sessionID = strings.TrimSpace(sessionID)
	if botID == "" || sessionID == "" {
		return errors.New("bot id and session id are required")
	}
	if s.toolApproval != nil {
		pending, err := s.toolApproval.ListPendingBySession(ctx, botID, sessionID)
		if err != nil {
			return fmt.Errorf("list pending tool approvals: %w", err)
		}
		if len(pending) > 0 {
			return ErrSessionDecisionPending
		}
	}
	if s.userInput != nil {
		pending, err := s.userInput.ListPendingBySession(ctx, botID, sessionID)
		if err != nil {
			return fmt.Errorf("list pending user inputs: %w", err)
		}
		if len(pending) > 0 {
			return ErrSessionDecisionPending
		}
	}
	return nil
}

// abortedRunDecisionReason matches the runtime projection wording so the
// durable rows and the projected decision cards close with the same reason.
const abortedRunDecisionReason = "run aborted"

// cancelPendingSessionDecisionsAfterAbort durably closes decisions that an
// aborted run left pending. It is the durable twin of the runtime
// projection's cancelPendingDecisions: without it the composer gate keeps
// blocking a session whose decision cards already show "cancelled". A fenced
// context makes the cancel stale-safe — once a newer run activated its fence,
// the cancel is rejected with ErrStale and skipped. Unfenced (in-process)
// runtimes cancel unconditionally, mirroring the projection.
func (s *Service) cancelPendingSessionDecisionsAfterAbort(ctx context.Context, botID, sessionID string) {
	if s == nil {
		return
	}
	log := s.logger
	if log == nil {
		log = slog.Default()
	}
	if s.toolApproval != nil {
		cancelled, err := s.toolApproval.CancelPendingForSession(ctx, botID, sessionID, abortedRunDecisionReason)
		switch {
		case err != nil && !errors.Is(err, runtimefence.ErrStale):
			log.Warn("cancel pending tool approvals after abort failed",
				slog.String("session_id", sessionID), slog.Any("error", err))
		case len(cancelled) > 0:
			log.Info("cancelled pending tool approvals with aborted run",
				slog.String("session_id", sessionID), slog.Int("count", len(cancelled)))
		}
	}
	if s.userInput != nil {
		cancelled, err := s.userInput.CancelPendingForSession(ctx, botID, sessionID, abortedRunDecisionReason)
		switch {
		case err != nil && !errors.Is(err, runtimefence.ErrStale):
			log.Warn("cancel pending user inputs after abort failed",
				slog.String("session_id", sessionID), slog.Any("error", err))
		case len(cancelled) > 0:
			log.Info("cancelled pending user inputs with aborted run",
				slog.String("session_id", sessionID), slog.Int("count", len(cancelled)))
		}
	}
}
