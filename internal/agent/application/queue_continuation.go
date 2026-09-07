package application

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	sessionruntime "github.com/felinics/memoh/internal/agent/runtime/session"
	"github.com/felinics/memoh/internal/agent/turn"
)

// startFollowUpAfterTerminal hands one transient follow-up to the ordinary
// turn admission path after a run has reached a terminal boundary. The queue
// claim is intentionally separate from run admission: admission remains the
// single owner/fencing authority, while the live queue only selects payload.
func (s *Service) startFollowUpAfterTerminal(ctx context.Context, terminal sessionruntime.TerminalRun) {
	if s == nil || s.sessionManager == nil || terminal.RunID == "" || terminal.BotID == "" || terminal.SessionID == "" {
		return
	}
	go s.startFollowUp(ctx, terminal)
}

func (s *Service) startFollowUp(parent context.Context, terminal sessionruntime.TerminalRun) {
	ctx := context.WithoutCancel(parent)
	key := sessionruntime.Key{BotID: terminal.BotID, SessionID: terminal.SessionID}
	item, claim, ok, err := s.sessionManager.ClaimNextFollowUp(ctx, key, terminal.RunID)
	if err != nil || !ok {
		return
	}

	text := continuationPayloadText(item.Payload)
	if strings.TrimSpace(text) == "" {
		_ = s.sessionManager.ReleaseFollowUp(ctx, key, claim)
		return
	}
	cmd := turn.StartTurnCommand{
		NoDefer:         true,
		TeamID:          s.allowedTeam,
		Mode:            turn.ModeChat,
		BotID:           item.BotID,
		ChatID:          item.BotID,
		ThreadID:        item.SessionID,
		Query:           text,
		UserVisibleText: text,
		IdempotencyKey:  "follow-up:" + string(item.ID),
	}
	if cmd.TeamID == "" {
		// Self-hosted deployments may not set allowedTeam. Admission will apply
		// its normal team validation when the command is supplied by a channel;
		// a continuation has no external team field, so fail closed here.
		_ = s.sessionManager.ReleaseFollowUp(ctx, key, claim)
		return
	}
	var handle turn.RunHandle
	for attempt := 0; ; attempt++ {
		handle, err = s.StartTurn(ctx, cmd)
		if !errors.Is(err, turn.ErrSessionBusy) || attempt >= 7 {
			break
		}
		timer := time.NewTimer(time.Duration(1<<attempt) * 10 * time.Millisecond)
		var waitErr error
		select {
		case <-ctx.Done():
			timer.Stop()
			waitErr = ctx.Err()
		case <-timer.C:
		}
		if waitErr != nil {
			err = waitErr
			break
		}
	}
	if err != nil {
		_ = s.sessionManager.ReleaseFollowUp(ctx, key, claim)
		return
	}
	if err := s.sessionManager.ApplyFollowUp(ctx, key, claim); err != nil && s.logger != nil {
		s.logger.Warn("apply transient follow-up failed",
			slog.String("item_id", string(item.ID)),
			slog.String("run_id", handle.RunID()),
			slog.Any("error", err),
		)
	}
	// Server-owned continuation handles have no external consumer. Drain them
	// so the run can publish and finish through the same application path.
	drainDeferredTurn(handle)
}

func continuationPayloadText(payload []byte) string {
	var body struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(payload, &body) == nil && strings.TrimSpace(body.Text) != "" {
		return body.Text
	}
	return strings.TrimSpace(string(payload))
}
