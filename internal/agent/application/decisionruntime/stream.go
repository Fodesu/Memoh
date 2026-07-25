package decisionruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/memohai/memoh/internal/agent/application"
	agentpkg "github.com/memohai/memoh/internal/agent/runtime/native"
	sessionruntime "github.com/memohai/memoh/internal/agent/runtime/session"
	conversation "github.com/memohai/memoh/internal/agent/view"
)

// EventPumpManager is the slice of the session runtime manager the shared
// event pump commits through.
type EventPumpManager interface {
	HandleAgentEvent(context.Context, sessionruntime.RunHandle, agentpkg.StreamEvent) ([]conversation.UIMessage, error)
	FinalizeAgentEvent(context.Context, sessionruntime.RunHandle, agentpkg.StreamEvent, bool, string) ([]conversation.UIMessage, error)
}

// EventPumpHooks lets one transport attach its delivery behavior to the
// shared agent-event pump. Every hook is optional; the zero value reproduces
// the plain decision-continuation pump.
type EventPumpHooks struct {
	// Transform preprocesses one raw event into zero or more replacement
	// events before projection and delivery (the WS transport ingests
	// attachments here). Nil keeps the raw event as-is.
	Transform func(raw application.StreamEventPayload) []application.StreamEventPayload
	// CommitContext supplies the context for non-terminal runtime commits.
	// Nil uses the pump context.
	CommitContext func() context.Context
	// FinalizeContext supplies the context for the terminal runtime commit.
	// Nil uses a bounded context detached from the pump context.
	FinalizeContext func() context.Context
	// BeforeFinalize runs after the terminal flush and before the terminal
	// runtime commit. It reports canonical readiness plus a finalization
	// error the terminal commit records (the WS transport links outbound
	// assets here). A non-nil error also fails the pump after the terminal
	// commit. Nil reports (event.HistoryCommitted, nil).
	BeforeFinalize func(terminal agentpkg.StreamEvent) (bool, error)
	// OnEvent observes each processed non-terminal event after it entered
	// the projection batch; OnTerminal observes the terminal event after a
	// successful terminal commit (the WS transport forwards legacy frames).
	OnEvent    func(ctx context.Context, event agentpkg.StreamEvent)
	OnTerminal func(ctx context.Context, event agentpkg.StreamEvent)
	// OnRaw observes every incoming raw event after it was handled
	// (decision transports copy it to their response stream).
	OnRaw func(raw application.StreamEventPayload)
	// OnDecodeError maps an undecodable processed event to a pump error.
	// Nil skips the event, matching historical WS behavior.
	OnDecodeError func(err error) error
	// AfterDrain runs once after the event channel closes when the pump has
	// not failed; its error fails the pump.
	AfterDrain func() error
}

// PumpRunEvents is the single agent-event pump behind every transport: it
// batches text deltas, commits projections through the manager, finalizes the
// terminal event, and reports the first failure after cancelling execution.
//
//nolint:contextcheck // Commit and finalization contexts intentionally come from hooks so terminal work outlives execution cancellation.
func PumpRunEvents(ctx context.Context, manager EventPumpManager, handle sessionruntime.RunHandle, eventCh <-chan application.StreamEventPayload, cancel context.CancelFunc, hooks EventPumpHooks) error {
	var pending *agentpkg.StreamEvent
	var timer *time.Timer
	var timerC <-chan time.Time
	var runtimeErr error

	fail := func(err error) {
		if runtimeErr != nil || err == nil {
			return
		}
		runtimeErr = err
		if cancel != nil {
			cancel()
		}
	}
	commitCtx := func() context.Context {
		if hooks.CommitContext != nil {
			return hooks.CommitContext()
		}
		return ctx
	}
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
		if _, err := manager.HandleAgentEvent(commitCtx(), handle, event); err != nil {
			fail(fmt.Errorf("update session runtime: %w", err))
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
	finalize := func(event agentpkg.StreamEvent) {
		flush()
		if runtimeErr != nil {
			return
		}
		canonicalReady := event.HistoryCommitted
		var finalizeHookErr error
		if hooks.BeforeFinalize != nil {
			canonicalReady, finalizeHookErr = hooks.BeforeFinalize(event)
		}
		finalizationError := ""
		if finalizeHookErr != nil {
			finalizationError = finalizeHookErr.Error()
		}
		var finalizeCtx context.Context
		if hooks.FinalizeContext != nil {
			finalizeCtx = hooks.FinalizeContext()
		} else {
			var finalizeCancel context.CancelFunc
			finalizeCtx, finalizeCancel = context.WithTimeout(context.WithoutCancel(ctx), terminalFinalizationTimeout)
			defer finalizeCancel()
		}
		if _, err := manager.FinalizeAgentEvent(finalizeCtx, handle, event, canonicalReady, finalizationError); err != nil {
			fail(fmt.Errorf("finalize session runtime: %w", err))
			return
		}
		if hooks.OnTerminal != nil {
			hooks.OnTerminal(commitCtx(), event)
		}
		fail(finalizeHookErr)
	}
	handleProcessed := func(raw application.StreamEventPayload) {
		var event agentpkg.StreamEvent
		if err := json.Unmarshal(raw, &event); err != nil {
			if hooks.OnDecodeError != nil {
				fail(hooks.OnDecodeError(err))
			}
			return
		}
		if event.IsTerminal() {
			finalize(event)
			return
		}
		enqueue(event)
		if runtimeErr != nil {
			return
		}
		if hooks.OnEvent != nil {
			hooks.OnEvent(commitCtx(), event)
		}
	}

	for eventCh != nil {
		select {
		case raw, ok := <-eventCh:
			if !ok {
				eventCh = nil
				break
			}
			if runtimeErr == nil {
				if hooks.Transform != nil {
					for _, processed := range hooks.Transform(raw) {
						handleProcessed(processed)
					}
				} else {
					handleProcessed(raw)
				}
			}
			if hooks.OnRaw != nil {
				hooks.OnRaw(raw)
			}
		case <-timerC:
			flush()
		}
	}
	flush()
	stopTimer()
	if runtimeErr == nil && hooks.AfterDrain != nil {
		fail(hooks.AfterDrain())
	}
	return runtimeErr
}

func (a *NativeContinuationAdmission) consumeEvents(ctx context.Context, handle sessionruntime.RunHandle, eventCh <-chan application.StreamEventPayload, output chan<- application.StreamEventPayload, cancel context.CancelFunc, transport EventPumpHooks) error {
	hooks := transport
	if hooks.OnDecodeError == nil {
		hooks.OnDecodeError = func(err error) error {
			return fmt.Errorf("decode decision stream event: %w", err)
		}
	}
	if output != nil {
		transportOnRaw := hooks.OnRaw
		outputEnabled := true
		hooks.OnRaw = func(raw application.StreamEventPayload) {
			if transportOnRaw != nil {
				transportOnRaw(raw)
			}
			if !outputEnabled {
				return
			}
			select {
			case output <- raw:
			case <-ctx.Done():
				outputEnabled = false
			}
		}
	}
	return PumpRunEvents(ctx, a.manager, handle, eventCh, cancel, hooks)
}
