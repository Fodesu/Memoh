package handlers

import (
	"context"
	"errors"

	"github.com/felinics/memoh/internal/botsetup"
)

// createBotSetupStepEvent is the SSE event for one setup step transition. It
// is only emitted for requests that carried `setup`, so older clients whose
// event validator rejects unknown types never see it.
type createBotSetupStepEvent struct {
	Type   string `json:"type"`
	Step   string `json:"step"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// streamBotSetup relays setup step transitions to the create stream until the
// awaited (Final) setup answers. Like the workspace relay, only the awaited
// observation decides the outcome: subscriber-side terminal events are hints.
// It returns false when the stream must stop (client gone or setup failed),
// having already written the error event in the latter case.
func streamBotSetup(
	ctx context.Context,
	send func(payload any) bool,
	events <-chan botsetup.Event,
	await func(ctx context.Context) (botsetup.Setup, error),
	sendError func(code, i18nKey, message string),
) bool {
	awaitCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	type result struct {
		setup botsetup.Setup
		err   error
	}
	done := make(chan result, 1)
	go func() {
		s, err := await(awaitCtx)
		done <- result{setup: s, err: err}
	}()
	relay := func(ev botsetup.Event) bool {
		if ev.Type != botsetup.EventStep {
			return true
		}
		return send(createBotSetupStepEvent{Type: "setup_step", Step: ev.Step, Status: ev.Status, Error: ev.Error})
	}
	for {
		select {
		case ev := <-events:
			if !relay(ev) {
				return false
			}
		case res := <-done:
			for {
				select {
				case ev := <-events:
					if !relay(ev) {
						return false
					}
					continue
				default:
				}
				break
			}
			if res.err != nil {
				if errors.Is(res.err, context.DeadlineExceeded) || errors.Is(res.err, context.Canceled) {
					sendError("bot_setup_timeout", "bots.create.setupFailedSubtitle", "bot setup did not finish in time")
				} else {
					sendError("bot_setup_failed", "bots.create.setupFailedSubtitle", res.err.Error())
				}
				return false
			}
			if res.setup.State == botsetup.StateFailed {
				detail := "bot setup failed"
				for _, step := range res.setup.Steps {
					if (step.Status == botsetup.StatusFailed || step.Status == botsetup.StatusRetrying) && step.LastError != "" {
						detail = step.Step + ": " + step.LastError
					}
				}
				sendError("bot_setup_failed", "bots.create.setupFailedSubtitle", detail)
				return false
			}
			return true
		}
	}
}
