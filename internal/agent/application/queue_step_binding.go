package application

import (
	"context"
	"errors"
	"sync"

	"github.com/felinics/memoh/internal/agent/runtime/native"
	"github.com/felinics/memoh/internal/agent/turn"
)

// bindQueueContinuation installs the live queue step boundary used by an
// initially admitted turn onto an application-owned native continuation. Queue
// items are transient; history remains persisted by the normal message
// service, while claims are fenced by the session runtime backend.
func (s *Service) bindQueueContinuation(
	ctx context.Context,
	req *ChatRequest,
	cfg *native.RunConfig,
	rc resolvedContext,
) (*agentStepCommitter, func(), error) {
	noop := func() {}
	if s == nil || req == nil || cfg == nil || s.sessionManager == nil ||
		req.RunHandle.RunID == "" || req.RunHandle.OwnerID == "" || req.RunHandle.FencingToken <= 0 {
		return nil, noop, nil
	}

	stepIndex, err := s.sessionManager.ContinuationStepIndex(req.RunHandle)
	if err != nil {
		return nil, noop, err
	}
	req.StepIndexOffset = stepIndex
	cfg.StepIndexOffset = stepIndex

	queueInput := make(chan turn.InjectMessage, 16)
	nativeInput := make(chan native.InjectMessage, 16)
	done := make(chan struct{})
	var stopOnce sync.Once
	stop := func() { stopOnce.Do(func() { close(done) }) }
	existingInput := cfg.InjectCh
	go func() {
		defer close(nativeInput)
		for {
			select {
			case <-done:
				return
			case msg, ok := <-existingInput:
				if !ok {
					existingInput = nil
					continue
				}
				select {
				case nativeInput <- msg:
				case <-done:
					return
				}
			case msg := <-queueInput:
				nativeMessage := native.InjectMessage{Text: msg.Text, HeaderifiedText: msg.HeaderifiedText}
				select {
				case nativeInput <- nativeMessage:
				case <-done:
					return
				}
			}
		}
	}()

	req.QueueInjectCh = queueInput
	cfg.InjectCh = nativeInput
	committer := s.newAgentStepCommitter(ctx, *req, rc)
	if committer == nil {
		stop()
		return nil, noop, errors.New("live queue step committer is unavailable for decision continuation")
	}
	committer.bindContinuation(cfg)
	return committer, stop, nil
}
