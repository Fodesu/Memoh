package orchestrationenv

import (
	"context"
	"sync"
	"time"
)

// Backend is the kind-specific driver responsible for actual runtime
// allocation. The manager handles all Postgres state and lease
// fencing; backends only need to translate manager intents into
// container / browser / desktop operations.
//
// Implementations must be safe for concurrent use. Backends are
// looked up by Kind at allocation time, so a single registered
// backend serves every resource of that kind.
type Backend interface {
	// Kind returns the resource kind this backend handles. Must match
	// one of the env.Kind* constants.
	Kind() string

	// Allocate creates a runtime instance for the given session and
	// returns the runtime-specific handle the manager persists into
	// orchestration_env_sessions.runtime_handle. Allocate runs after
	// the manager has reserved capacity in Postgres, so it can
	// optimistically allocate without re-checking the resource row.
	Allocate(ctx context.Context, req AllocateRequest) (AllocateResult, error)

	// Snapshot captures runtime state. The manager records the
	// returned RuntimeRef + Digest + SizeBytes into
	// orchestration_env_snapshots. Backends that cannot snapshot
	// should return ErrSnapshotUnsupported wrapped, and the manager
	// will surface that without persisting a snapshot row.
	Snapshot(ctx context.Context, req SnapshotRequestBackend) (SnapshotResult, error)

	// Release tears down the runtime instance. The manager already
	// updated the session row to released by the time this is called,
	// so backends can treat the call as best-effort cleanup.
	Release(ctx context.Context, req ReleaseRequestBackend) error
}

// AllocateRequest carries the runtime-relevant fields the backend
// needs from the resource row plus the session row about to be
// written. The manager keeps ResourceConfig and SessionMetadata as
// already-decoded maps so backends never re-parse JSONB.
type AllocateRequest struct {
	ResourceID     string
	ResourceKind   string
	ResourceName   string
	ResourceConfig map[string]any
	SessionID      string
	TenantID       string
	RunID          string
	TaskID         string
	AttemptID      string
	LeaseTTL       time.Duration
	Metadata       map[string]any
}

// AllocateResult is what the backend hands back after a successful
// runtime allocation. RuntimeHandle is opaque to the manager but is
// persisted verbatim into the session row so future snapshot/release
// calls can rebuild backend state.
type AllocateResult struct {
	RuntimeHandle map[string]any
	Metadata      map[string]any
}

// SnapshotRequestBackend mirrors the manager's CaptureSnapshotRequest
// but pre-resolves the session into a runtime handle the backend
// already understands. Keeping a separate struct lets the manager
// evolve its public surface without breaking backend contracts.
type SnapshotRequestBackend struct {
	SessionID     string
	ResourceKind  string
	RuntimeHandle map[string]any
	Kind          string
	EffectClass   string
}

// SnapshotResult is what the backend returns after capturing a
// snapshot. RuntimeRef stays opaque to the manager; Digest is content
// addressing for diff/dedup downstream.
type SnapshotResult struct {
	RuntimeRef map[string]any
	Digest     string
	SizeBytes  int64
	Metadata   map[string]any
}

// ReleaseRequestBackend tells the backend which runtime instance to
// tear down. The manager has already moved the Postgres state to
// released by the time this is called.
type ReleaseRequestBackend struct {
	SessionID     string
	ResourceKind  string
	RuntimeHandle map[string]any
	Reason        string
}

// BackendRegistry holds the Kind → Backend mapping the manager looks
// up at allocation time. It is concurrency-safe so backends may be
// added during startup wiring without locking the rest of the world.
type BackendRegistry struct {
	mu       sync.RWMutex
	backends map[string]Backend
}

// NewBackendRegistry returns an empty registry. The manager refuses
// to allocate against unregistered kinds, so an empty registry only
// supports resource CRUD until a backend is registered.
func NewBackendRegistry() *BackendRegistry {
	return &BackendRegistry{backends: make(map[string]Backend)}
}

// Register adds or replaces the backend for a given kind. Replacing
// an existing backend is intentional: it lets tests swap in fakes
// without rebuilding the registry.
func (r *BackendRegistry) Register(b Backend) {
	if r == nil || b == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.backends[b.Kind()] = b
}

// Lookup returns the backend for a kind. The bool is false when no
// backend was registered, mirroring map idioms; callers translate the
// miss into ErrBackendUnavailable so the manager surface stays
// uniform.
func (r *BackendRegistry) Lookup(kind string) (Backend, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	b, ok := r.backends[kind]
	return b, ok
}
