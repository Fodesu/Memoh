package decisionruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/memohai/memoh/internal/agent/application"
	agentpkg "github.com/memohai/memoh/internal/agent/runtime/native"
	sessionruntime "github.com/memohai/memoh/internal/agent/runtime/session"
	conversation "github.com/memohai/memoh/internal/agent/view"
	"github.com/memohai/memoh/internal/runtimefence"
)

const (
	streamBufferSize            = 64
	textBatchWindow             = 20 * time.Millisecond
	textBatchBytes              = 4 * 1024
	terminalFinalizationTimeout = 10 * time.Second
)

type resolver interface {
	CommandResolver
	AllocateRuntimePersistenceFence(context.Context, string, string) (runtimefence.Fence, error)
	ActivateRuntimePersistenceFenceWithOptions(context.Context, runtimefence.Fence, runtimefence.ActivationOptions) error
	PrepareToolApprovalResponseTarget(context.Context, application.ToolApprovalResponseInput) (runtimefence.PreservedDecision, error)
	PrepareUserInputResponseTarget(context.Context, application.UserInputResponseInput) (runtimefence.PreservedDecision, error)
	CommitToolApprovalResponse(context.Context, application.ToolApprovalResponseInput) (application.CommittedToolApprovalResponse, error)
	ContinueCommittedToolApprovalResponse(context.Context, application.CommittedToolApprovalResponse, chan<- application.WSStreamEvent) error
	CommitUserInputResponse(context.Context, application.UserInputResponseInput) (application.CommittedUserInputResponse, error)
	ContinueCommittedUserInputResponse(context.Context, application.CommittedUserInputResponse, chan<- application.WSStreamEvent) error
	ReconcileToolApprovalResponse(context.Context, application.ToolApprovalResponseInput) (bool, error)
	ReconcileUserInputResponse(context.Context, application.UserInputResponseInput) (bool, error)
	DeferSessionCompaction(botID, sessionID, streamID string) func()
}

// CommandResolver applies owner-local decision commands after runtime routing.
type CommandResolver interface {
	CommitToolApprovalResponse(context.Context, application.ToolApprovalResponseInput) (application.CommittedToolApprovalResponse, error)
	ContinueCommittedToolApprovalResponse(context.Context, application.CommittedToolApprovalResponse, chan<- application.WSStreamEvent) error
	CommitUserInputResponse(context.Context, application.UserInputResponseInput) (application.CommittedUserInputResponse, error)
	ContinueCommittedUserInputResponse(context.Context, application.CommittedUserInputResponse, chan<- application.WSStreamEvent) error
}

type commandReconciler interface {
	ReconcileToolApprovalResponse(context.Context, application.ToolApprovalResponseInput) (bool, error)
	ReconcileUserInputResponse(context.Context, application.UserInputResponseInput) (bool, error)
}

type runtimeManager interface {
	SetCommandHandler(func(context.Context, sessionruntime.Command) error)
	SetCommandReconciler(func(context.Context, sessionruntime.Command) (bool, error))
	DispatchActiveCommand(context.Context, string, string, string, string, []byte) (bool, error)
	IsDistributed() bool
	ValidateRunOwnership(context.Context, sessionruntime.RunHandle) error
	StartRunWithOptions(context.Context, sessionruntime.RunStartOptions) (sessionruntime.RunHandle, error)
	HandleAgentEvent(context.Context, sessionruntime.RunHandle, agentpkg.StreamEvent) ([]conversation.UIMessage, error)
	FinalizeAgentEvent(context.Context, sessionruntime.RunHandle, agentpkg.StreamEvent, bool, string) ([]conversation.UIMessage, error)
	FinishRun(context.Context, sessionruntime.RunHandle, string, string) error
}

// Router gives every decision-response transport the same runtime ownership
// semantics. It routes responses to an active owner and creates a fenced
// continuation only when no active run can accept the decision.
type Router struct {
	logger   *slog.Logger
	manager  runtimeManager
	resolver resolver
}

// NewRouter constructs the shared decision-response application service.
func NewRouter(logger *slog.Logger, manager *sessionruntime.Manager, resolver *application.Service) *Router {
	return newRouter(logger, manager, resolver)
}

func newRouter(logger *slog.Logger, manager runtimeManager, decisionResolver resolver) *Router {
	if logger == nil {
		logger = slog.Default()
	}
	router := &Router{
		logger:   logger.With(slog.String("service", "decision_runtime")),
		manager:  manager,
		resolver: decisionResolver,
	}
	router.bindCommandHandlers()
	return router
}

func (r *Router) RespondToolApproval(ctx context.Context, input application.ToolApprovalResponseInput, output chan<- application.WSStreamEvent) error {
	if r == nil || r.resolver == nil {
		return errors.New("decision runtime router is not configured")
	}
	prepared, err := r.resolver.PrepareToolApprovalResponseTarget(ctx, input)
	if err != nil {
		return err
	}
	input.BotID = prepared.BotID
	input.ThreadID = prepared.SessionID
	input.ApprovalID = prepared.ID
	input.ExplicitID = prepared.ID

	routed := input
	routed.ReplyExternalMessageID = ""
	routed.ChatToken = ""
	routed.SuppressActivePromptAttach = true
	routed.ResolveOnly = true
	payload, err := json.Marshal(routed)
	if err != nil {
		return fmt.Errorf("encode tool approval response: %w", err)
	}
	var committed application.CommittedToolApprovalResponse
	return r.routeOrContinue(ctx, prepared, sessionruntime.CommandToolApprovalResponse, payload, output, func(reconcileCtx context.Context) (bool, error) {
		return r.resolver.ReconcileToolApprovalResponse(reconcileCtx, routed)
	}, func(commitCtx context.Context) error {
		var commitErr error
		committed, commitErr = r.resolver.CommitToolApprovalResponse(commitCtx, input)
		return commitErr
	}, func(runCtx context.Context, eventCh chan<- application.WSStreamEvent) error {
		return r.resolver.ContinueCommittedToolApprovalResponse(runCtx, committed, eventCh)
	})
}

func (r *Router) RespondUserInput(ctx context.Context, input application.UserInputResponseInput, output chan<- application.WSStreamEvent) error {
	if r == nil || r.resolver == nil {
		return errors.New("decision runtime router is not configured")
	}
	prepared, err := r.resolver.PrepareUserInputResponseTarget(ctx, input)
	if err != nil {
		return err
	}
	input.BotID = prepared.BotID
	input.ThreadID = prepared.SessionID
	input.UserInputID = prepared.ID
	input.ExplicitID = prepared.ID

	routed := input
	routed.ReplyExternalMessageID = ""
	routed.ChatToken = ""
	routed.SuppressActivePromptAttach = true
	routed.ResolveOnly = true
	payload, err := json.Marshal(routed)
	if err != nil {
		return fmt.Errorf("encode user input response: %w", err)
	}
	var committed application.CommittedUserInputResponse
	return r.routeOrContinue(ctx, prepared, sessionruntime.CommandUserInputResponse, payload, output, func(reconcileCtx context.Context) (bool, error) {
		return r.resolver.ReconcileUserInputResponse(reconcileCtx, routed)
	}, func(commitCtx context.Context) error {
		var commitErr error
		committed, commitErr = r.resolver.CommitUserInputResponse(commitCtx, input)
		return commitErr
	}, func(runCtx context.Context, eventCh chan<- application.WSStreamEvent) error {
		return r.resolver.ContinueCommittedUserInputResponse(runCtx, committed, eventCh)
	})
}

type continuation func(context.Context, chan<- application.WSStreamEvent) error

type decisionCommit func(context.Context) error

type decisionReconciler func(context.Context) (bool, error)

func (r *Router) routeOrContinue(ctx context.Context, prepared runtimefence.PreservedDecision, commandType string, payload []byte, output chan<- application.WSStreamEvent, reconcile decisionReconciler, commit decisionCommit, run continuation) error {
	if strings.TrimSpace(prepared.BotID) == "" || strings.TrimSpace(prepared.SessionID) == "" || strings.TrimSpace(prepared.ID) == "" {
		return errors.New("prepared decision is missing canonical scope")
	}
	if r.manager != nil {
		handled, err := r.manager.DispatchActiveCommand(ctx, prepared.BotID, prepared.SessionID, commandType, prepared.ID, payload)
		if handled {
			return err
		}
	}
	if reconcile != nil {
		if reconciled, err := reconcile(ctx); reconciled {
			return err
		}
	}
	if r.manager == nil {
		if err := commit(ctx); err != nil {
			return err
		}
		return run(ctx, output)
	}
	return r.runContinuation(ctx, prepared, commandType, payload, output, reconcile, commit, run)
}
