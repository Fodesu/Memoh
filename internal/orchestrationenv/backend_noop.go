package orchestrationenv

import (
	"context"

	"github.com/google/uuid"
)

// NoopBackend is a backend that pretends to allocate runtime
// resources. It is used by tests and by single-process deployments
// that have no real env runtime wired in yet, so the kernel can
// exercise reserve / bind / release flows without depending on
// containerd / browser-gateway.
//
// The fake handles it returns include a synthetic backend_session_id
// so callers that round-trip through Postgres can still observe a
// distinguishable handle per session. Snapshots return an empty
// runtime_ref with a zero digest; callers that care about real
// content addressing must register one of the kind-specific
// backends.
type NoopBackend struct {
	kind string
}

// NewNoopBackend returns a backend that registers itself under the
// given kind. Multiple noop backends with different kinds can coexist
// in the same registry so manager-level tests can simulate
// heterogeneous tenants without pulling in real drivers.
func NewNoopBackend(kind string) *NoopBackend {
	if kind == "" {
		kind = KindOther
	}
	return &NoopBackend{kind: kind}
}

// Kind reports the backend's registered kind.
func (b *NoopBackend) Kind() string {
	return b.kind
}

// Allocate returns a synthetic handle without touching any external
// runtime. The handle carries enough metadata for tests to
// distinguish sessions.
func (b *NoopBackend) Allocate(_ context.Context, req AllocateRequest) (AllocateResult, error) {
	return AllocateResult{
		RuntimeHandle: map[string]any{
			"backend":            "noop",
			"backend_kind":       b.kind,
			"backend_session_id": uuid.NewString(),
			"resource_id":        req.ResourceID,
			"resource_name":      req.ResourceName,
		},
	}, nil
}

// Snapshot returns a deterministic stub snapshot result. Tests use
// the empty digest to assert that the manager records snapshot rows
// even when the backend has nothing useful to capture.
func (*NoopBackend) Snapshot(_ context.Context, req SnapshotRequestBackend) (SnapshotResult, error) {
	return SnapshotResult{
		RuntimeRef: map[string]any{
			"backend":     "noop",
			"snapshot_id": uuid.NewString(),
			"session_id":  req.SessionID,
			"kind":        req.Kind,
			"effect":      req.EffectClass,
		},
	}, nil
}

// Release is a no-op. Real backends would tear down the underlying
// runtime instance here.
func (*NoopBackend) Release(_ context.Context, _ ReleaseRequestBackend) error {
	return nil
}
