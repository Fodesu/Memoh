package decisionruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/memohai/memoh/internal/agent/application"
	"github.com/memohai/memoh/internal/agent/decision"
	sessionruntime "github.com/memohai/memoh/internal/agent/runtime/session"
	"github.com/memohai/memoh/internal/runtimefence"
)

type decisionResolver interface {
	CommandResolver
	PrepareToolApprovalResponseTarget(context.Context, application.ToolApprovalResponseInput) (runtimefence.PreservedDecision, error)
	PrepareUserInputResponseTarget(context.Context, application.UserInputResponseInput) (runtimefence.PreservedDecision, error)
	ReconcileToolApprovalResponse(context.Context, application.ToolApprovalResponseInput) (bool, error)
	ReconcileUserInputResponse(context.Context, application.UserInputResponseInput) (bool, error)
}

type runtimeTargetResolver interface {
	PrepareToolApprovalRuntimeTarget(context.Context, application.ToolApprovalResponseInput) (application.RuntimeDecisionTarget, error)
	PrepareUserInputRuntimeTarget(context.Context, application.UserInputResponseInput) (application.RuntimeDecisionTarget, error)
}

type continuation func(context.Context, chan<- application.StreamEventPayload) error

type decisionCommit func(context.Context) error

type decisionReconciler func(context.Context) (bool, error)

// PreparedDecision is the canonical response operation shared by owner
// routing and native continuation admission.
type PreparedDecision struct {
	target         runtimefence.PreservedDecision
	resumePolicy   decision.ResumePolicy
	hasLocalWaiter bool
	commandType    string
	payload        []byte
	commit         decisionCommit
	continueRun    continuation
	reconcile      decisionReconciler
}

func (p PreparedDecision) validate() error {
	if strings.TrimSpace(p.target.BotID) == "" || strings.TrimSpace(p.target.SessionID) == "" || strings.TrimSpace(p.target.ID) == "" {
		return errors.New("prepared decision is missing canonical scope")
	}
	if strings.TrimSpace(p.commandType) == "" || p.commit == nil || p.continueRun == nil {
		return errors.New("prepared decision is incomplete")
	}
	return nil
}

func (p *PreparedDecision) commitAndContinue(ctx context.Context, output chan<- application.StreamEventPayload) error {
	if p == nil {
		return errors.New("prepared decision is required")
	}
	if err := p.commit(ctx); err != nil {
		return err
	}
	return p.continueRun(ctx, output)
}

// DecisionCoordinator owns validation, canonical command encoding, durable
// commit, and idempotent replay checks for every Decision response transport.
type DecisionCoordinator struct {
	resolver decisionResolver
}

func newDecisionCoordinator(resolver decisionResolver) *DecisionCoordinator {
	return &DecisionCoordinator{resolver: resolver}
}

func (c *DecisionCoordinator) PrepareToolApproval(ctx context.Context, input application.ToolApprovalResponseInput) (PreparedDecision, error) {
	if c == nil || c.resolver == nil {
		return PreparedDecision{}, errors.New("decision coordinator is not configured")
	}
	var runtimeTarget application.RuntimeDecisionTarget
	var err error
	if enriched, ok := c.resolver.(runtimeTargetResolver); ok {
		runtimeTarget, err = enriched.PrepareToolApprovalRuntimeTarget(ctx, input)
	} else {
		runtimeTarget.Decision, err = c.resolver.PrepareToolApprovalResponseTarget(ctx, input)
		runtimeTarget.ResumePolicy = decision.ResumePolicyNativeContinuation
	}
	if err != nil {
		return PreparedDecision{}, err
	}
	target := runtimeTarget.Decision
	input.BotID = target.BotID
	input.ThreadID = target.SessionID
	input.ApprovalID = target.ID
	input.ExplicitID = target.ID

	routed := input
	routed.ReplyExternalMessageID = ""
	routed.ChatToken = ""
	routed.SuppressActivePromptAttach = true
	routed.ResolveOnly = true
	payload, err := json.Marshal(routed)
	if err != nil {
		return PreparedDecision{}, fmt.Errorf("encode tool approval response: %w", err)
	}
	var committed application.CommittedToolApprovalResponse
	prepared := PreparedDecision{
		target: target, resumePolicy: runtimeTarget.ResumePolicy, hasLocalWaiter: runtimeTarget.HasLocalWaiter,
		commandType: sessionruntime.CommandToolApprovalResponse, payload: payload,
		reconcile: func(reconcileCtx context.Context) (bool, error) {
			return c.resolver.ReconcileToolApprovalResponse(reconcileCtx, routed)
		},
		commit: func(commitCtx context.Context) error {
			var commitErr error
			committed, commitErr = c.resolver.CommitToolApprovalResponse(commitCtx, input)
			return commitErr
		},
		continueRun: func(runCtx context.Context, eventCh chan<- application.StreamEventPayload) error {
			return c.resolver.ContinueCommittedToolApprovalResponse(runCtx, committed, eventCh)
		},
	}
	return prepared, prepared.validate()
}

func (c *DecisionCoordinator) PrepareUserInput(ctx context.Context, input application.UserInputResponseInput) (PreparedDecision, error) {
	if c == nil || c.resolver == nil {
		return PreparedDecision{}, errors.New("decision coordinator is not configured")
	}
	var runtimeTarget application.RuntimeDecisionTarget
	var err error
	if enriched, ok := c.resolver.(runtimeTargetResolver); ok {
		runtimeTarget, err = enriched.PrepareUserInputRuntimeTarget(ctx, input)
	} else {
		runtimeTarget.Decision, err = c.resolver.PrepareUserInputResponseTarget(ctx, input)
		runtimeTarget.ResumePolicy = decision.ResumePolicyNativeContinuation
	}
	if err != nil {
		return PreparedDecision{}, err
	}
	target := runtimeTarget.Decision
	input.BotID = target.BotID
	input.ThreadID = target.SessionID
	input.UserInputID = target.ID
	input.ExplicitID = target.ID

	routed := input
	routed.ReplyExternalMessageID = ""
	routed.ChatToken = ""
	routed.SuppressActivePromptAttach = true
	routed.ResolveOnly = true
	payload, err := json.Marshal(routed)
	if err != nil {
		return PreparedDecision{}, fmt.Errorf("encode user input response: %w", err)
	}
	var committed application.CommittedUserInputResponse
	prepared := PreparedDecision{
		target: target, resumePolicy: runtimeTarget.ResumePolicy, hasLocalWaiter: runtimeTarget.HasLocalWaiter,
		commandType: sessionruntime.CommandUserInputResponse, payload: payload,
		reconcile: func(reconcileCtx context.Context) (bool, error) {
			return c.resolver.ReconcileUserInputResponse(reconcileCtx, routed)
		},
		commit: func(commitCtx context.Context) error {
			var commitErr error
			committed, commitErr = c.resolver.CommitUserInputResponse(commitCtx, input)
			return commitErr
		},
		continueRun: func(runCtx context.Context, eventCh chan<- application.StreamEventPayload) error {
			return c.resolver.ContinueCommittedUserInputResponse(runCtx, committed, eventCh)
		},
	}
	return prepared, prepared.validate()
}
