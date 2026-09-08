package application

import (
	"context"
	"errors"
	"strings"

	sdk "github.com/felinics/twilight/sdk"

	sessionruntime "github.com/felinics/memoh/internal/agent/runtime/session"
	"github.com/felinics/memoh/internal/agent/turn"
	messagepkg "github.com/felinics/memoh/internal/chat/message"
)

// queueStepCoordinator orders queue consumption after history persistence.
// History remains a fenced PostgreSQL transaction; queue state lives in the
// session runtime backend (memory or Redis). Queue state is transient and is
// never included in the history transaction.
type queueStepCoordinator struct {
	service              *Service
	req                  ChatRequest
	persister            messagepkg.AgentStepPersister
	replacementPersister messagepkg.AgentReplacementPersister
	run                  sessionruntime.RunHandle
	steerEnabled         bool
	pendingSteer         *sessionruntime.SteerClaimRef
}

type queueStepOutcome struct {
	historyCommitted     bool
	persisted            []messagepkg.Message
	appliedSteerItemID   string
	claimedSteer         *sessionruntime.SteerItem
	continueAfterFinal   bool
	replacementFinalized bool
}

func newQueueStepCoordinator(s *Service, req ChatRequest) *queueStepCoordinator {
	if req.QueueInjectCh == nil && req.TurnReplacement == nil {
		return nil
	}
	if s == nil || s.sessionManager == nil || s.messageService == nil || req.RunHandle.RunID == "" || req.RunHandle.OwnerID == "" || req.RunHandle.FencingToken <= 0 {
		return nil
	}
	persister, ok := s.messageService.(messagepkg.AgentStepPersister)
	if !ok {
		return nil
	}
	replacementPersister, _ := s.messageService.(messagepkg.AgentReplacementPersister)
	if req.TurnReplacement != nil && replacementPersister == nil {
		return nil
	}
	q := &queueStepCoordinator{
		service: s, req: req, persister: persister,
		replacementPersister: replacementPersister,
		run:                  req.RunHandle,
		steerEnabled:         req.QueueInjectCh != nil,
	}
	return q
}

func (q *queueStepCoordinator) persistHistory(ctx context.Context, step messagepkg.AgentStep) ([]messagepkg.Message, error) {
	if len(step.Messages) == 0 {
		return nil, nil
	}
	if q.req.TurnReplacement != nil {
		return q.replacementPersister.PersistAgentReplacementStep(ctx, step)
	}
	return q.persister.PersistAgentStep(ctx, step)
}

func (q *queueStepCoordinator) releaseSteerClaim(ctx context.Context) {
	if q == nil || q.pendingSteer == nil || q.service == nil || q.service.sessionManager == nil {
		return
	}
	_ = q.service.sessionManager.ReleaseSteer(ctx, sessionruntime.Key{BotID: q.run.BotID, SessionID: q.run.SessionID}, *q.pendingSteer)
}

func (q *queueStepCoordinator) commit(
	ctx context.Context,
	kind queueStepKind,
	agentStep messagepkg.AgentStep,
	previouslyPersisted []messagepkg.Message,
) (queueStepOutcome, error) {
	var outcome queueStepOutcome
	if q == nil || q.service == nil || q.service.sessionManager == nil {
		return outcome, errors.New("live queue step transaction is unavailable")
	}
	persisted, err := q.persistHistory(context.WithoutCancel(ctx), agentStep)
	if err != nil {
		q.releaseSteerClaim(ctx)
		return outcome, err
	}
	outcome.persisted = persisted
	outcome.historyCommitted = true
	if q.pendingSteer != nil {
		if err := q.service.sessionManager.ApplySteer(ctx, sessionruntime.Key{BotID: q.run.BotID, SessionID: q.run.SessionID}, *q.pendingSteer); err != nil {
			// The steer text is already part of the committed step. Releasing the
			// claim here would let the next commit inject it a second time; leave
			// it claimed and fail the step, so terminal cleanup rejects the item.
			return outcome, err
		}
		outcome.appliedSteerItemID = string(q.pendingSteer.ItemID)
		q.pendingSteer = nil
	}
	if kind == queueStepDeferredDecision {
		// The loop parks after this step and its inject channel is never read
		// again. A claim taken here would sit unapplied across the decision, and
		// across any owner change while the run waits. The continuation's first
		// committed step claims instead, so the steer enters the request after
		// the decision result exactly as a tool-loop claim would.
		return outcome, nil
	}

	var item sessionruntime.SteerItem
	var claim sessionruntime.SteerClaimRef
	var claimed bool
	if q.steerEnabled {
		item, claim, claimed, err = q.service.sessionManager.ClaimNextSteer(ctx, q.run, kind == queueStepFinal)
	}
	if err != nil {
		return outcome, err
	}
	if claimed {
		q.pendingSteer = &claim
		outcome.claimedSteer = &item
		if kind == queueStepFinal {
			outcome.continueAfterFinal = true
		} else {
			text := QueuePayloadText(item.Payload)
			if q.req.QueueInjectCh == nil {
				q.releaseSteerClaim(ctx)
				return outcome, errors.New("steer queue injection channel is unavailable")
			}
			select {
			case q.req.QueueInjectCh <- turn.InjectMessage{Text: text, HeaderifiedText: text}:
			default:
				q.releaseSteerClaim(ctx)
				return outcome, errors.New("steer queue injection channel is full")
			}
		}
	} else if q.req.TurnReplacement != nil && kind == queueStepFinal {
		allPersisted := append([]messagepkg.Message(nil), previouslyPersisted...)
		allPersisted = append(allPersisted, persisted...)
		requestID := strings.TrimSpace(q.req.TurnReplacement.RequestMessageID)
		if requestID == "" {
			requestID = firstUserID(allPersisted)
		}
		assistantID := firstAssistantID(allPersisted)
		if assistantID == "" {
			return outcome, errors.New("replacement assistant message was not persisted")
		}
		err = q.replacementPersister.FinalizeAgentReplacement(ctx, q.req.ThreadID, *q.req.TurnReplacement, requestID, assistantID)
		if err != nil {
			return outcome, err
		}
		outcome.replacementFinalized = true
	}
	return outcome, nil
}

type queueStepKind string

const (
	queueStepToolLoop         queueStepKind = "tool_loop"
	queueStepDeferredDecision queueStepKind = "deferred_decision"
	queueStepFinal            queueStepKind = "final"
)

func classifyQueueStep(step *sdk.StepResult) queueStepKind {
	if step == nil || step.DeferredToolApproval != nil {
		return queueStepDeferredDecision
	}
	if step.FinishReason == sdk.FinishReasonToolCalls && len(step.ToolCalls) > 0 {
		return queueStepToolLoop
	}
	return queueStepFinal
}
