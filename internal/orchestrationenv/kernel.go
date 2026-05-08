package orchestrationenv

import (
	"context"

	"github.com/memohai/memoh/internal/orchestration"
)

// KernelAdapter wraps a Manager so it satisfies orchestration.EnvManager,
// the primitive-typed slice the dispatch path consumes. Keeping the adapter
// next to the Manager (instead of in cmd/agent) means the kernel never has
// to import orchestrationenv directly and the orchestrationenv package
// stays the only place that knows how to translate between the two surfaces.
type KernelAdapter struct {
	manager *Manager
}

// NewKernelAdapter returns an adapter ready to be handed to
// orchestration.Service.SetEnvManager. The manager argument must not be nil.
func NewKernelAdapter(manager *Manager) *KernelAdapter {
	return &KernelAdapter{manager: manager}
}

var _ orchestration.EnvManager = (*KernelAdapter)(nil)

// GetEnvResourceByName resolves a planner-supplied resource_name into the
// minimal projection the dispatch path validates. Capacity is included so
// future scheduling heuristics can reuse the same lookup, but Stage 3-E
// only checks Status and Kind.
func (a *KernelAdapter) GetEnvResourceByName(ctx context.Context, tenantID, name string) (orchestration.EnvResourceRef, error) {
	resource, err := a.manager.GetResourceByName(ctx, tenantID, name)
	if err != nil {
		return orchestration.EnvResourceRef{}, err
	}
	return orchestration.EnvResourceRef{
		ID:       resource.ID,
		TenantID: resource.TenantID,
		Kind:     resource.Kind,
		Name:     resource.Name,
		Status:   resource.Status,
		Capacity: resource.Capacity,
	}, nil
}

// AcquireEnvSession reserves capacity, asks the backend to allocate the
// runtime, and returns the lease tuple the kernel needs to fence subsequent
// writes. The returned RuntimeHandle is opaque to the kernel; the worker
// owns interpretation.
func (a *KernelAdapter) AcquireEnvSession(ctx context.Context, req orchestration.EnvAcquireRequest) (orchestration.EnvSessionLease, error) {
	session, err := a.manager.AcquireSession(ctx, AcquireSessionRequest{
		TenantID:        req.TenantID,
		ResourceID:      req.ResourceID,
		LeaseHolderKind: req.LeaseHolderKind,
		LeaseHolderID:   req.LeaseHolderID,
		LeaseTTL:        req.LeaseTTL,
		RunID:           req.RunID,
		TaskID:          req.TaskID,
		AttemptID:       req.AttemptID,
		Metadata:        req.Metadata,
	})
	if err != nil {
		return orchestration.EnvSessionLease{}, err
	}
	return orchestration.EnvSessionLease{
		SessionID:      session.ID,
		ResourceID:     session.ResourceID,
		LeaseToken:     session.LeaseToken,
		LeaseEpoch:     session.LeaseEpoch,
		LeaseExpiresAt: session.LeaseExpiresAt,
		RuntimeHandle:  session.RuntimeHandle,
	}, nil
}

// CreateEnvBinding records the session→attempt mapping inside a fenced
// transaction. The kernel always writes purpose=primary today; Stage 3-G
// adds secondary bindings for resume_held_env.
func (a *KernelAdapter) CreateEnvBinding(ctx context.Context, req orchestration.EnvCreateBindingRequest) (orchestration.EnvBindingHandle, error) {
	binding, err := a.manager.CreateBinding(ctx, CreateBindingRequest{
		SessionID:  req.SessionID,
		LeaseToken: req.LeaseToken,
		LeaseEpoch: req.LeaseEpoch,
		RunID:      req.RunID,
		TaskID:     req.TaskID,
		AttemptID:  req.AttemptID,
		Purpose:    req.Purpose,
		Metadata:   req.Metadata,
	})
	if err != nil {
		return orchestration.EnvBindingHandle{}, err
	}
	return orchestration.EnvBindingHandle{BindingID: binding.ID}, nil
}

// CaptureEnvSnapshot records a fenced point-in-time snapshot for the
// current session.
func (a *KernelAdapter) CaptureEnvSnapshot(ctx context.Context, req orchestration.EnvCaptureSnapshotRequest) (orchestration.EnvSnapshotRef, error) {
	snapshot, err := a.manager.CaptureSnapshot(ctx, CaptureSnapshotRequest{
		SessionID:   req.SessionID,
		LeaseToken:  req.LeaseToken,
		LeaseEpoch:  req.LeaseEpoch,
		Kind:        req.Kind,
		EffectClass: req.EffectClass,
		RunID:       req.RunID,
		TaskID:      req.TaskID,
		AttemptID:   req.AttemptID,
		Metadata:    req.Metadata,
	})
	if err != nil {
		return orchestration.EnvSnapshotRef{}, err
	}
	return orchestration.EnvSnapshotRef{
		SnapshotID: snapshot.ID,
		Digest:     snapshot.Digest,
	}, nil
}

// ReleaseEnvBinding marks the binding done. The manager decides whether to
// also release the underlying session based on remaining bindings; the
// kernel always pairs a release-binding call with a release-session call so
// the manager's bookkeeping converges to the same end state either way.
func (a *KernelAdapter) ReleaseEnvBinding(ctx context.Context, req orchestration.EnvReleaseBindingRequest) error {
	return a.manager.ReleaseBinding(ctx, ReleaseBindingRequest{
		BindingID:  req.BindingID,
		LeaseToken: req.LeaseToken,
		LeaseEpoch: req.LeaseEpoch,
		Reason:     req.Reason,
	})
}

// HoldEnvBinding pins the session for HITL resume so the next attempt
// re-attaches to the same warm runtime instead of paying the allocate cost
// again. Stage 3-E only exposes the call here; the kernel does not invoke
// it yet because resume_held_env is gated behind Stage 3-G.
func (a *KernelAdapter) HoldEnvBinding(ctx context.Context, req orchestration.EnvHoldBindingRequest) error {
	_, err := a.manager.HoldBinding(ctx, HoldBindingRequest{
		BindingID:           req.BindingID,
		LeaseToken:          req.LeaseToken,
		LeaseEpoch:          req.LeaseEpoch,
		HeldForCheckpointID: req.HeldForCheckpointID,
		Metadata:            req.Metadata,
	})
	return err
}

// ReleaseEnvSession tears down the underlying runtime via the backend and
// transitions the session to released. The reclaim sweep is the safety net
// for sessions that lose the race (worker crashes after dispatch but before
// release).
func (a *KernelAdapter) ReleaseEnvSession(ctx context.Context, req orchestration.EnvReleaseSessionRequest) error {
	return a.manager.ReleaseSession(ctx, ReleaseSessionRequest{
		SessionID:  req.SessionID,
		LeaseToken: req.LeaseToken,
		LeaseEpoch: req.LeaseEpoch,
		Reason:     req.Reason,
	})
}
