// Package runprojection owns the single agent-event pump behind every
// transport: text-delta batching, runtime projection commits, and terminal
// finalization. Transports attach delivery behavior through Hooks; every hook
// receives the pump's authority context and never supplies one, so a
// transport can observe, encode, and deliver events but cannot decide what
// authority runtime commits or asset work run under.
package runprojection

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	agentpkg "github.com/memohai/memoh/internal/agent/runtime/native"
	sessionruntime "github.com/memohai/memoh/internal/agent/runtime/session"
	conversation "github.com/memohai/memoh/internal/agent/view"
)

const (
	textBatchWindow     = 20 * time.Millisecond
	textBatchBytes      = 4 * 1024
	finalizationTimeout = 10 * time.Second
)

// Manager is the slice of the session runtime manager the pump commits
// through.
type Manager interface {
	HandleAgentEvent(context.Context, sessionruntime.RunHandle, agentpkg.StreamEvent) ([]conversation.UIMessage, error)
	FinalizeAgentEvent(context.Context, sessionruntime.RunHandle, agentpkg.StreamEvent, bool, string) ([]conversation.UIMessage, error)
}

// Hooks attach one transport's delivery behavior to the pump. Every hook is
// optional; the zero value reproduces the plain decision-continuation pump.
type Hooks struct {
	// Transform preprocesses one raw event into zero or more replacement
	// events before projection and delivery (the WS transport ingests
	// attachments here). Nil keeps the raw event as-is.
	Transform func(ctx context.Context, raw json.RawMessage) []json.RawMessage
	// BeforeFinalize runs after the terminal flush and before the terminal
	// runtime commit. It reports canonical readiness plus a finalization
	// error the terminal commit records (the WS transport links outbound
	// assets here). A non-nil error also fails the pump after the terminal
	// commit. Nil reports (event.HistoryCommitted, nil).
	BeforeFinalize func(ctx context.Context, terminal agentpkg.StreamEvent) (bool, error)
	// OnEvent observes each processed non-terminal event after it entered
	// the projection batch; OnTerminal observes the terminal event after a
	// successful terminal commit (the WS transport forwards legacy frames).
	OnEvent    func(ctx context.Context, event agentpkg.StreamEvent)
	OnTerminal func(ctx context.Context, event agentpkg.StreamEvent)
	// OnRaw observes every incoming raw event after it was handled
	// (decision transports copy it to their response stream). It receives
	// the execution context, not the finalization authority: response
	// copies stop with the request.
	OnRaw func(ctx context.Context, raw json.RawMessage)
	// OnDecodeError maps an undecodable processed event to a pump error.
	// Nil skips the event, matching historical WS behavior.
	OnDecodeError func(err error) error
	// AfterDrain runs once after the event channel closes when the pump has
	// not failed; its error fails the pump.
	AfterDrain func(ctx context.Context) error
}

// authority owns the pump's commit context: the run's (fenced) execution
// context while it lives, switched once to a bounded detached copy — which
// keeps context values such as the persistence fence — when the stream turns
// terminal or execution is cancelled, so terminal work outlives cancellation
// without ever changing whose authority it runs under.
type authority struct {
	source context.Context
	bound  context.Context
	cancel context.CancelFunc
}

func (a *authority) begin() {
	if a.bound != nil {
		return
	}
	a.bound, a.cancel = context.WithTimeout(context.WithoutCancel(a.source), finalizationTimeout)
}

func (a *authority) current() context.Context {
	if a.bound == nil && a.source.Err() != nil {
		a.begin()
	}
	if a.bound != nil {
		return a.bound
	}
	return a.source
}

func (a *authority) close() {
	if a.cancel != nil {
		a.cancel()
	}
}

// Pump drives agent events into the session runtime with text-delta batching
// and terminal finalization, reporting the first failure after cancelling
// execution. It is the single implementation behind every transport.
//
//nolint:contextcheck // Commit and terminal work intentionally run under the pump's authority context, which detaches from execution cancellation at terminal time.
func Pump(ctx context.Context, manager Manager, handle sessionruntime.RunHandle, eventCh <-chan json.RawMessage, cancel context.CancelFunc, hooks Hooks) error {
	auth := &authority{source: ctx}
	defer auth.close()

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
		if _, err := manager.HandleAgentEvent(auth.current(), handle, event); err != nil {
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
		auth.begin()
		flush()
		if runtimeErr != nil {
			return
		}
		canonicalReady := event.HistoryCommitted
		var finalizeHookErr error
		if hooks.BeforeFinalize != nil {
			canonicalReady, finalizeHookErr = hooks.BeforeFinalize(auth.current(), event)
		}
		finalizationError := ""
		if finalizeHookErr != nil {
			finalizationError = finalizeHookErr.Error()
		}
		if _, err := manager.FinalizeAgentEvent(auth.current(), handle, event, canonicalReady, finalizationError); err != nil {
			fail(fmt.Errorf("finalize session runtime: %w", err))
			return
		}
		if hooks.OnTerminal != nil {
			hooks.OnTerminal(auth.current(), event)
		}
		fail(finalizeHookErr)
	}
	handleProcessed := func(raw json.RawMessage) {
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
			hooks.OnEvent(auth.current(), event)
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
					// The terminal event's own transformation (asset
					// extraction) must already run under the bounded
					// finalization authority, not the cancellable execution
					// context.
					var rawEvent agentpkg.StreamEvent
					if json.Unmarshal(raw, &rawEvent) == nil && rawEvent.IsTerminal() {
						auth.begin()
					}
					for _, processed := range hooks.Transform(auth.current(), raw) {
						handleProcessed(processed)
					}
				} else {
					handleProcessed(raw)
				}
			}
			if hooks.OnRaw != nil {
				hooks.OnRaw(ctx, raw)
			}
		case <-timerC:
			flush()
		}
	}
	flush()
	stopTimer()
	if runtimeErr == nil && hooks.AfterDrain != nil {
		fail(hooks.AfterDrain(auth.current()))
	}
	return runtimeErr
}
