package application

import (
	"context"
	"errors"
	"strings"

	sdk "github.com/felinics/twilight/sdk"

	sessionruntime "github.com/felinics/memoh/internal/agent/runtime/session"
	"github.com/felinics/memoh/internal/agent/turn"
	messagepkg "github.com/felinics/memoh/internal/chat/message"
	dbstore "github.com/felinics/memoh/internal/db/store"
	"github.com/felinics/memoh/internal/runtimefence"
)

// queueStepTransaction is a small adapter. History remains a fenced
// PostgreSQL transaction, while queue state lives entirely in the configured
// session runtime backend (memory or Redis). Queue state is transient and is
// never included in the history transaction.
type queueStepTransaction struct {
	service              *Service
	req                  ChatRequest
	txPersister          messagepkg.AgentStepTxPersister
	replacementPersister messagepkg.AgentReplacementTxPersister
	modelID              string
	run                  sessionruntime.RunHandle
	pendingSteer         *sessionruntime.SteerClaimRef
	pendingSteerItem     *sessionruntime.SteerItem
	pendingSteerDelivery steerDelivery
}

type steerDelivery int

const (
	steerNotPending steerDelivery = iota
	steerDeliveredByInject
	steerDeliveredByNextInputs
)

type queueStepOutcome struct {
	persisted            []messagepkg.Message
	appliedSteerItemID   string
	claimedSteer         *sessionruntime.SteerItem
	continueAfterFinal   bool
	replacementFinalized bool
}

func newQueueStepTransaction(s *Service, req ChatRequest, modelID string) *queueStepTransaction {
	if s == nil || s.sessionManager == nil || s.messageService == nil || s.queries == nil {
		return nil
	}
	txPersister, ok := s.messageService.(messagepkg.AgentStepTxPersister)
	if !ok {
		return nil
	}
	replacementPersister, _ := s.messageService.(messagepkg.AgentReplacementTxPersister)
	if req.TurnReplacement != nil && replacementPersister == nil {
		return nil
	}
	q := &queueStepTransaction{
		service: s, req: req, txPersister: txPersister,
		replacementPersister: replacementPersister, modelID: modelID,
		run: req.RunHandle, pendingSteerDelivery: steerNotPending,
	}
	if req.QueueSteerClaim != nil {
		claim := *req.QueueSteerClaim
		q.pendingSteer = &claim
		q.pendingSteerDelivery = steerDeliveredByInject
	}
	return q
}

func (q *queueStepTransaction) persistHistory(ctx context.Context, step messagepkg.AgentStep) ([]messagepkg.Message, error) {
	if len(step.Messages) == 0 {
		return nil, nil
	}
	var persisted []messagepkg.Message
	err := runtimefence.InTransaction(ctx, q.service.queries, q.req.BotID, q.req.ThreadID, func(queries dbstore.Queries) error {
		var err error
		if q.req.TurnReplacement != nil {
			persisted, err = q.replacementPersister.PersistAgentReplacementStepTx(ctx, queries, step)
		} else {
			persisted, err = q.txPersister.PersistAgentStepTx(ctx, queries, step)
		}
		return err
	})
	return persisted, err
}

func (q *queueStepTransaction) releaseSteerClaim(ctx context.Context) {
	if q == nil || q.pendingSteer == nil || q.service == nil || q.service.sessionManager == nil {
		return
	}
	_ = q.service.sessionManager.ReleaseSteer(ctx, sessionruntime.Key{BotID: q.run.BotID, SessionID: q.run.SessionID}, *q.pendingSteer)
}

func (q *queueStepTransaction) commit(
	ctx context.Context,
	_ int,
	_ string,
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
	if q.pendingSteer != nil {
		if err := q.service.sessionManager.ApplySteer(ctx, sessionruntime.Key{BotID: q.run.BotID, SessionID: q.run.SessionID}, *q.pendingSteer); err != nil {
			q.releaseSteerClaim(ctx)
			return outcome, err
		}
		outcome.appliedSteerItemID = string(q.pendingSteer.ItemID)
		q.pendingSteer = nil
		q.pendingSteerItem = nil
		q.pendingSteerDelivery = steerNotPending
	}

	item, claim, claimed, err := q.service.sessionManager.ClaimNextSteer(ctx, q.run, kind == queueStepFinal)
	if err != nil {
		return outcome, err
	}
	if claimed {
		q.pendingSteer = &claim
		q.pendingSteerItem = &item
		outcome.claimedSteer = &item
		if kind == queueStepFinal {
			q.pendingSteerDelivery = steerDeliveredByNextInputs
			outcome.continueAfterFinal = true
		} else {
			q.pendingSteerDelivery = steerDeliveredByInject
			text := continuationPayloadText(item.Payload)
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
		err = runtimefence.InTransaction(ctx, q.service.queries, q.req.BotID, q.req.ThreadID, func(queries dbstore.Queries) error {
			return q.replacementPersister.FinalizeAgentReplacementTx(ctx, queries, q.req.ThreadID, *q.req.TurnReplacement, requestID, assistantID)
		})
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
