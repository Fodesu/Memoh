package application

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"

	"github.com/memohai/memoh/internal/agent/decision"
	agentpkg "github.com/memohai/memoh/internal/agent/runtime/native"
	sessionruntime "github.com/memohai/memoh/internal/agent/runtime/session"
	"github.com/memohai/memoh/internal/agent/turn"
	"github.com/memohai/memoh/internal/runtimefence"
)

// DecisionContinuationAdmission owns the runtime admission and event lifecycle
// for a durable native Decision whose original run can no longer continue.
type DecisionContinuationAdmission struct {
	logger   *slog.Logger
	manager  runtimeManager
	resolver resolver
}

func newDecisionContinuationAdmission(logger *slog.Logger, manager runtimeManager, resolver resolver) *DecisionContinuationAdmission {
	if logger == nil {
		logger = slog.Default()
	}
	return &DecisionContinuationAdmission{
		logger:   logger.With(slog.String("service", "native_decision_continuation")),
		manager:  manager,
		resolver: resolver,
	}
}

func (a *DecisionContinuationAdmission) Admit(ctx context.Context, prepared *PreparedDecision, output chan<- StreamEventPayload, opts DecisionRespondOptions) error {
	if a == nil || a.manager == nil || a.resolver == nil {
		return errors.New("native decision continuation is not configured")
	}
	if prepared == nil {
		return errors.New("prepared decision is required")
	}
	if prepared.continuationMode != decision.ContinuationModeDurable {
		return ErrRuntimeDecisionOwnerUnavailable
	}
	streamID := opts.StreamID
	if streamID == "" {
		streamID = "decision-" + uuid.NewString()
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	abortCh := make(chan struct{}, 1)
	injectCh := make(chan turn.InjectMessage, 16)

	var fence runtimefence.Fence
	var err error
	if a.manager.IsDistributed() {
		fence, err = a.resolver.AllocateRuntimePersistenceFence(runCtx, prepared.target.BotID, prepared.target.SessionID)
		if err != nil {
			return err
		}
	}
	fencedCtx := runCtx
	if fence.Valid() {
		fencedCtx = runtimefence.WithContext(runCtx, fence)
	}
	authorityCtx, revokeOwnership := context.WithCancelCause(context.WithoutCancel(fencedCtx))
	var handle sessionruntime.RunHandle
	runtimeCtx := WithTerminalHookAuthority(fencedCtx, agentpkg.TerminalHookAuthority{
		Context: authorityCtx,
		Validate: func(validateCtx context.Context) error {
			return a.manager.ValidateRunOwnership(validateCtx, handle)
		},
	})

	handle, err = a.manager.StartRunWithOptions(runtimeCtx, sessionruntime.RunStartOptions{
		BotID:                prepared.target.BotID,
		SessionID:            prepared.target.SessionID,
		StreamID:             streamID,
		OwnershipCancel:      revokeOwnership,
		AbortCh:              abortCh,
		Cancel:               cancel,
		InjectCh:             injectCh,
		DecisionContinuation: true,
		AdmissionBuilder: func(admissionCtx context.Context, admitted sessionruntime.RunHandle) (sessionruntime.RunAdmissionView, error) {
			if fence.Valid() {
				admissionCtx = runtimefence.WithContext(admissionCtx, fence)
			}
			if err := a.manager.ValidateRunOwnership(admissionCtx, admitted); err != nil {
				return sessionruntime.RunAdmissionView{}, err
			}
			if fence.Valid() {
				if err := a.resolver.ActivateRuntimePersistenceFenceWithOptions(admissionCtx, fence, runtimefence.ActivationOptions{PreserveDecision: &prepared.target}); err != nil {
					return sessionruntime.RunAdmissionView{}, err
				}
			}
			if err := a.manager.ValidateRunOwnership(admissionCtx, admitted); err != nil {
				return sessionruntime.RunAdmissionView{}, err
			}
			if err := prepared.commit(admissionCtx); err != nil {
				return sessionruntime.RunAdmissionView{}, err
			}
			if decision := sessionruntime.ResolvedDecisionFromCommand(prepared.target.Kind, prepared.target.ID, prepared.commandType, prepared.payload); decision != nil {
				return sessionruntime.RunAdmissionView{ResolvedDecision: decision}, nil
			}
			return sessionruntime.RunAdmissionView{}, nil
		},
	})
	if err != nil {
		// A concurrent responder may have installed the active continuation after
		// our first dispatch. Route to it before reporting the admission failure.
		if handled, dispatchErr := a.manager.DispatchActiveCommand(ctx, prepared.target.BotID, prepared.target.SessionID, prepared.commandType, prepared.target.ID, prepared.payload); handled {
			dispatchErr = normalizeDecisionResponseError(dispatchErr)
			if dispatchErr == nil {
				opts.decisionCommitted()
			}
			return dispatchErr
		}
		if reconciled, reconcileErr := prepared.reconcileAfterFailure(ctx); reconciled {
			if reconcileErr == nil {
				opts.decisionCommitted()
			}
			return reconcileErr
		}
		return err
	}
	opts.decisionCommitted()
	releaseCompaction := a.resolver.DeferSessionCompaction(prepared.target.BotID, prepared.target.SessionID, streamID)
	defer releaseCompaction()

	eventCh := make(chan StreamEventPayload, streamBufferSize)
	forwardDone := make(chan error, 1)
	go func() {
		forwardDone <- a.consumeEvents(runtimeCtx, handle, eventCh, output, cancel, opts.Hooks)
	}()

	runnerCtx := WithTerminalEventDeliveryTimeout(runtimeCtx, terminalFinalizationTimeout)
	runnerCtx = WithPersistenceGuard(runnerCtx, func(guardCtx context.Context) error {
		return a.manager.ValidateRunOwnership(guardCtx, handle)
	})
	runErr := func() error {
		defer close(eventCh)
		return prepared.continueRun(runnerCtx, eventCh)
	}()
	if forwardErr := <-forwardDone; forwardErr != nil {
		runErr = forwardErr
	}
	if errors.Is(runErr, sessionruntime.ErrTerminalCommitPending) {
		a.logger.Warn("decision continuation terminal commit deferred for retry", slog.String("stream_id", streamID))
		return runErr
	}

	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(runtimeCtx), terminalFinalizationTimeout)
	defer finishCancel()
	if runErr != nil {
		if finishErr := a.manager.FinishRun(finishCtx, handle, sessionruntime.RunStatusErrored, runErr.Error()); finishErr != nil && !errors.Is(finishErr, sessionruntime.ErrRunOwnershipLost) {
			a.logger.Warn("finish decision continuation after error failed", slog.Any("error", finishErr), slog.String("stream_id", streamID))
		}
		return runErr
	}
	if err := a.manager.FinishRun(finishCtx, handle, "", ""); err != nil && !errors.Is(err, sessionruntime.ErrRunOwnershipLost) {
		return err
	}
	return nil
}
