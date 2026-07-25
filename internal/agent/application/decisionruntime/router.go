package decisionruntime

import (
	"context"
	"errors"
	"log/slog"
	"strings"
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
	ContinueCommittedToolApprovalResponse(context.Context, application.CommittedToolApprovalResponse, chan<- application.StreamEventPayload) error
	CommitUserInputResponse(context.Context, application.UserInputResponseInput) (application.CommittedUserInputResponse, error)
	ContinueCommittedUserInputResponse(context.Context, application.CommittedUserInputResponse, chan<- application.StreamEventPayload) error
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

// RespondOptions carries transport-supplied parameters for one decision
// response. The zero value keeps server-generated defaults, so existing
// callers are unaffected.
type RespondOptions struct {
	// StreamID correlates a native continuation run with a transport-chosen
	// stream identity. The WebSocket protocol lets the client pick the
	// stream ID so its abort frames and event envelopes match the run;
	// leaving it empty selects a server-generated identity.
	StreamID string
	// Hooks attaches the transport's delivery behavior (attachment
	// ingestion, asset linking, legacy frame forwarding) to the shared
	// event pump of a native continuation run.
	Hooks EventPumpHooks
}

func (o RespondOptions) normalized() RespondOptions {
	o.StreamID = strings.TrimSpace(o.StreamID)
	return o
}

func (r *Router) RespondToolApproval(ctx context.Context, input application.ToolApprovalResponseInput, output chan<- application.StreamEventPayload) error {
	return r.RespondToolApprovalWithOptions(ctx, input, output, RespondOptions{})
}

// RespondToolApprovalWithOptions is the transport-neutral decision response
// use case: every transport routes through the same ownership, reconcile,
// and continuation semantics while supplying its own delivery parameters.
func (r *Router) RespondToolApprovalWithOptions(ctx context.Context, input application.ToolApprovalResponseInput, output chan<- application.StreamEventPayload, opts RespondOptions) error {
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
	return r.routeOrContinue(ctx, &prepared, output, opts.normalized())
}

func (r *Router) RespondUserInput(ctx context.Context, input application.UserInputResponseInput, output chan<- application.StreamEventPayload) error {
	return r.RespondUserInputWithOptions(ctx, input, output, RespondOptions{})
}

// RespondUserInputWithOptions mirrors RespondToolApprovalWithOptions for
// ask_user decisions.
func (r *Router) RespondUserInputWithOptions(ctx context.Context, input application.UserInputResponseInput, output chan<- application.StreamEventPayload, opts RespondOptions) error {
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
	return r.routeOrContinue(ctx, &prepared, output, opts.normalized())
}

func (r *Router) routeOrContinue(ctx context.Context, prepared *PreparedDecision, output chan<- application.StreamEventPayload, opts RespondOptions) error {
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
		// The commit layer owns the full non-native protocol: it wakes a live
		// waiter blocked in this process and auto-closes unfenced orphans.
		// Channel-originated runs never register runtime runs, so their
		// waiters are only reachable through this delegation. Without a local
		// waiter a distributed runtime must still fail closed — the waiter may
		// be alive on another server, and this process cannot tell until
		// waiter presence is shared across owners.
		if prepared.hasLocalWaiter || r.manager == nil || !r.manager.IsDistributed() {
			return prepared.commitAndContinue(ctx, output)
		}
		return application.ErrRuntimeDecisionOwnerUnavailable
	}
	if r.manager == nil {
		return prepared.commitAndContinue(ctx, output)
	}
	return r.continuation.Admit(ctx, prepared, output, opts)
}
