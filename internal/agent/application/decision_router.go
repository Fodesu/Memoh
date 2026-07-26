package application

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/memohai/memoh/internal/agent/decision"
	agentpkg "github.com/memohai/memoh/internal/agent/runtime/native"
	sessionruntime "github.com/memohai/memoh/internal/agent/runtime/session"
	conversation "github.com/memohai/memoh/internal/agent/view"
	"github.com/memohai/memoh/internal/runtimefence"
)

const (
	streamBufferSize            = 64
	terminalFinalizationTimeout = 10 * time.Second
)

type resolver interface {
	decisionResolver
	AllocateRuntimePersistenceFence(context.Context, string, string) (runtimefence.Fence, error)
	ActivateRuntimePersistenceFenceWithOptions(context.Context, runtimefence.Fence, runtimefence.ActivationOptions) error
	DeferSessionCompaction(botID, sessionID, streamID string) func()
}

// DecisionTransportResolver is the application surface a transport needs to wire the
// decision response use case. *Service implements it; tests may
// substitute stage-recording fakes that embed it.
type DecisionTransportResolver = resolver

// NewDecisionRouterForTransport builds the router over the transport-facing resolver
// interface so transports and their tests can wire the production routing
// policy around substituted application services.
func NewDecisionRouterForTransport(logger *slog.Logger, manager *sessionruntime.Manager, resolver DecisionTransportResolver) *DecisionRouter {
	return newDecisionRouter(logger, manager, resolver)
}

// DecisionCommandResolver applies owner-local decision commands after runtime routing.
type DecisionCommandResolver interface {
	CommitToolApprovalResponse(context.Context, ToolApprovalResponseInput) (CommittedToolApprovalResponse, error)
	ContinueCommittedToolApprovalResponse(context.Context, CommittedToolApprovalResponse, chan<- StreamEventPayload) error
	CommitUserInputResponse(context.Context, UserInputResponseInput) (CommittedUserInputResponse, error)
	ContinueCommittedUserInputResponse(context.Context, CommittedUserInputResponse, chan<- StreamEventPayload) error
}

type commandReconciler interface {
	ReconcileToolApprovalResponse(context.Context, ToolApprovalResponseInput) (bool, error)
	ReconcileUserInputResponse(context.Context, UserInputResponseInput) (bool, error)
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

// DecisionRouter gives every decision-response transport the same runtime ownership
// semantics. It routes responses to an active owner and creates a fenced
// continuation only when no active run can accept the decision.
type DecisionRouter struct {
	manager      runtimeManager
	coordinator  *DecisionCoordinator
	continuation *DecisionContinuationAdmission
	initErr      error
}

// NewDecisionRouter constructs the shared decision-response application service.
func NewDecisionRouter(logger *slog.Logger, manager *sessionruntime.Manager, resolver *Service) *DecisionRouter {
	return newDecisionRouter(logger, manager, resolver)
}

func newDecisionRouter(logger *slog.Logger, manager runtimeManager, decisionResolver resolver) *DecisionRouter {
	if logger == nil {
		logger = slog.Default()
	}
	coordinator := newDecisionCoordinator(decisionResolver)
	router := &DecisionRouter{
		manager:      manager,
		coordinator:  coordinator,
		continuation: newDecisionContinuationAdmission(logger, manager, decisionResolver),
	}
	router.initErr = coordinator.bindDecisionCommandHandlers(manager)
	return router
}

// DecisionRespondOptions carries transport-supplied parameters for one decision
// response. The zero value keeps server-generated defaults, so existing
// callers are unaffected.
type DecisionRespondOptions struct {
	// StreamID correlates a native continuation run with a transport-chosen
	// stream identity. The WebSocket protocol lets the client pick the
	// stream ID so its abort frames and event envelopes match the run;
	// leaving it empty selects a server-generated identity.
	StreamID string
	// Hooks attaches the transport's delivery behavior (attachment
	// ingestion, asset linking, legacy frame forwarding) to the shared
	// event pump of a native continuation run. Hooks receive the pump's
	// authority context; they never supply one.
	Hooks RunEventPumpHooks
	// OnDecisionCommitted is called after the response has been durably
	// committed, including an idempotent replay confirmed by reconciliation.
	// It does not imply that the agent continuation has completed.
	OnDecisionCommitted func()
}

func (o DecisionRespondOptions) normalized() DecisionRespondOptions {
	o.StreamID = strings.TrimSpace(o.StreamID)
	if o.OnDecisionCommitted != nil {
		onCommitted := o.OnDecisionCommitted
		var once sync.Once
		o.OnDecisionCommitted = func() {
			once.Do(onCommitted)
		}
	}
	return o
}

func (o DecisionRespondOptions) decisionCommitted() {
	if o.OnDecisionCommitted != nil {
		o.OnDecisionCommitted()
	}
}

func (r *DecisionRouter) RespondToolApproval(ctx context.Context, input ToolApprovalResponseInput, output chan<- StreamEventPayload) error {
	return r.RespondToolApprovalWithOptions(ctx, input, output, DecisionRespondOptions{})
}

// RespondToolApprovalWithOptions is the transport-neutral decision response
// use case: every transport routes through the same ownership, reconcile,
// and continuation semantics while supplying its own delivery parameters.
func (r *DecisionRouter) RespondToolApprovalWithOptions(ctx context.Context, input ToolApprovalResponseInput, output chan<- StreamEventPayload, opts DecisionRespondOptions) error {
	if r == nil || r.coordinator == nil {
		return errors.New("decision response router is not configured")
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

func (r *DecisionRouter) RespondUserInput(ctx context.Context, input UserInputResponseInput, output chan<- StreamEventPayload) error {
	return r.RespondUserInputWithOptions(ctx, input, output, DecisionRespondOptions{})
}

// RespondUserInputWithOptions mirrors RespondToolApprovalWithOptions for
// ask_user decisions.
func (r *DecisionRouter) RespondUserInputWithOptions(ctx context.Context, input UserInputResponseInput, output chan<- StreamEventPayload, opts DecisionRespondOptions) error {
	if r == nil || r.coordinator == nil {
		return errors.New("decision response router is not configured")
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

func (r *DecisionRouter) routeOrContinue(ctx context.Context, prepared *PreparedDecision, output chan<- StreamEventPayload, opts DecisionRespondOptions) error {
	if prepared == nil {
		return errors.New("prepared decision is required")
	}
	if err := prepared.validate(); err != nil {
		return err
	}
	if r.manager != nil {
		handled, err := r.manager.DispatchActiveCommand(ctx, prepared.target.BotID, prepared.target.SessionID, prepared.commandType, prepared.target.ID, prepared.payload)
		if handled {
			err = normalizeDecisionResponseError(err)
			if err == nil {
				opts.decisionCommitted()
			}
			return err
		}
	}
	if prepared.reconcile != nil {
		if reconciled, err := prepared.reconcile(ctx); reconciled {
			if err == nil {
				opts.decisionCommitted()
			}
			return err
		}
	}
	if prepared.continuationMode != decision.ContinuationModeDurable {
		// The commit layer owns the non-durable modes: it wakes a local
		// waiter blocked in this process and auto-closes unfenced orphans.
		// Channel-originated runs never register runtime runs, so their
		// waiters are only reachable through this delegation. Without a local
		// waiter a distributed runtime must still fail closed — the waiter may
		// be alive on another server, and this process cannot tell until
		// waiter presence is shared across owners.
		if prepared.hasLocalWaiter || r.manager == nil || !r.manager.IsDistributed() {
			return prepared.commitAndContinue(ctx, output, opts.decisionCommitted)
		}
		return ErrRuntimeDecisionOwnerUnavailable
	}
	if r.manager == nil {
		return prepared.commitAndContinue(ctx, output, opts.decisionCommitted)
	}
	return r.continuation.Admit(ctx, prepared, output, opts)
}
