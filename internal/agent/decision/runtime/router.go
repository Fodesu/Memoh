package decisionruntime

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/memohai/memoh/internal/agent/application"
	"github.com/memohai/memoh/internal/agent/decision"
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
	decisionResolver
	AllocateRuntimePersistenceFence(context.Context, string, string) (runtimefence.Fence, error)
	ActivateRuntimePersistenceFenceWithOptions(context.Context, runtimefence.Fence, runtimefence.ActivationOptions) error
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
	RegisterCommandHandler(string, sessionruntime.CommandHandler, sessionruntime.CommandReconciler) error
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
	manager      runtimeManager
	coordinator  *DecisionCoordinator
	continuation *NativeContinuationAdmission
	initErr      error
}

// NewRouter constructs the shared decision-response application service.
func NewRouter(logger *slog.Logger, manager *sessionruntime.Manager, resolver *application.Service) *Router {
	return newRouter(logger, manager, resolver)
}

func newRouter(logger *slog.Logger, manager runtimeManager, decisionResolver resolver) *Router {
	if logger == nil {
		logger = slog.Default()
	}
	coordinator := newDecisionCoordinator(decisionResolver)
	router := &Router{
		manager:      manager,
		coordinator:  coordinator,
		continuation: newNativeContinuationAdmission(logger, manager, decisionResolver),
	}
	router.initErr = coordinator.bindCommandHandlers(manager)
	return router
}

func (r *Router) RespondToolApproval(ctx context.Context, input application.ToolApprovalResponseInput, output chan<- application.WSStreamEvent) error {
	if r == nil || r.coordinator == nil {
		return errors.New("decision runtime router is not configured")
	}
	if r.initErr != nil {
		return r.initErr
	}
	prepared, err := r.coordinator.PrepareToolApproval(ctx, input)
	if err != nil {
		return err
	}
	return r.routeOrContinue(ctx, &prepared, output)
}

func (r *Router) RespondUserInput(ctx context.Context, input application.UserInputResponseInput, output chan<- application.WSStreamEvent) error {
	if r == nil || r.coordinator == nil {
		return errors.New("decision runtime router is not configured")
	}
	if r.initErr != nil {
		return r.initErr
	}
	prepared, err := r.coordinator.PrepareUserInput(ctx, input)
	if err != nil {
		return err
	}
	return r.routeOrContinue(ctx, &prepared, output)
}

func (r *Router) routeOrContinue(ctx context.Context, prepared *PreparedDecision, output chan<- application.WSStreamEvent) error {
	if prepared == nil {
		return errors.New("prepared decision is required")
	}
	if err := prepared.validate(); err != nil {
		return err
	}
	if r.manager != nil {
		handled, err := r.manager.DispatchActiveCommand(ctx, prepared.target.BotID, prepared.target.SessionID, prepared.commandType, prepared.target.ID, prepared.payload)
		if handled {
			return err
		}
	}
	if prepared.reconcile != nil {
		if reconciled, err := prepared.reconcile(ctx); reconciled {
			return err
		}
	}
	if prepared.resumePolicy != decision.ResumePolicyNativeContinuation {
		return application.ErrRuntimeDecisionOwnerUnavailable
	}
	if r.manager == nil {
		return prepared.commitAndContinue(ctx, output)
	}
	return r.continuation.Admit(ctx, prepared, output)
}
