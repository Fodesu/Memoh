package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
