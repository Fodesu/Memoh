package decisionruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/memohai/memoh/internal/agent/application"
	agentpkg "github.com/memohai/memoh/internal/agent/runtime/native"
	"github.com/memohai/memoh/internal/agent/runtime/session"
)

func (r *Router) consumeEvents(ctx context.Context, handle sessionruntime.RunHandle, eventCh <-chan application.WSStreamEvent, output chan<- application.WSStreamEvent, cancel context.CancelFunc) error {
	var pending *agentpkg.StreamEvent
	var timer *time.Timer
	var timerC <-chan time.Time
	var runtimeErr error
	outputEnabled := output != nil

	stopTimer := func() {
		if timer != nil && !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer = nil
		timerC = nil
	}
	commit := func(event agentpkg.StreamEvent) {
		if runtimeErr != nil {
			return
		}
		if _, err := r.manager.HandleAgentEvent(ctx, handle, event); err != nil {
			runtimeErr = fmt.Errorf("update decision runtime: %w", err)
			cancel()
		}
	}
	flush := func() {
		if pending == nil {
			return
		}
		commit(*pending)
		pending = nil
		stopTimer()
	}
	enqueue := func(event agentpkg.StreamEvent) {
		batchable := event.Type == agentpkg.EventTextDelta || event.Type == agentpkg.EventReasoningDelta
		if !batchable || event.Delta == "" {
			flush()
			commit(event)
			return
		}
		if pending != nil && pending.Type != event.Type {
			flush()
		}
		if pending == nil {
			copyEvent := event
			pending = &copyEvent
			timer = time.NewTimer(textBatchWindow)
			timerC = timer.C
		} else {
			pending.Delta += event.Delta
		}
		if len(pending.Delta) >= textBatchBytes {
			flush()
		}
	}

	for eventCh != nil {
		select {
		case raw, ok := <-eventCh:
			if !ok {
				eventCh = nil
				break
			}
			var event agentpkg.StreamEvent
			if err := json.Unmarshal(raw, &event); err != nil {
				if runtimeErr == nil {
					runtimeErr = fmt.Errorf("decode decision stream event: %w", err)
					cancel()
				}
			} else if event.IsTerminal() {
				flush()
				if runtimeErr == nil {
					finalizeCtx, finalizeCancel := context.WithTimeout(context.WithoutCancel(ctx), terminalFinalizationTimeout)
					_, err := r.manager.FinalizeAgentEvent(finalizeCtx, handle, event, event.HistoryCommitted, "")
					finalizeCancel()
					if err != nil {
						runtimeErr = fmt.Errorf("finalize decision runtime: %w", err)
						cancel()
					}
				}
			} else {
				enqueue(event)
			}
			if outputEnabled {
				select {
				case output <- raw:
				case <-ctx.Done():
					outputEnabled = false
				}
			}
		case <-timerC:
			flush()
		}
	}
	flush()
	stopTimer()
	return runtimeErr
}
