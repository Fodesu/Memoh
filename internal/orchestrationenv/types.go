package orchestrationenv

import "time"

// Kind names the family of runtime an env resource describes. The
// constants here match the CHECK constraint on
// orchestration_env_resources.kind so manager / backend code can rely
// on a closed set without re-parsing the schema.
const (
	KindContainer = "container"
	KindBrowser   = "browser"
	KindDesktop   = "desktop"
	KindPhone     = "phone"
	KindOther     = "other"
)

// LeaseHolderKind names the actor class that currently holds a session
// lease. Keeping it tight (worker / verifier / orchestrator / human)
// mirrors the writer ownership model the blackboard layer enforces, so
// the same fencing argument carries through to env writes.
const (
	LeaseHolderWorker       = "worker"
	LeaseHolderVerifier     = "verifier"
	LeaseHolderOrchestrator = "orchestrator"
	LeaseHolderHuman        = "human"
)

// Status constants for env_sessions. ResourceStatus / BindingStatus /
// ReservationStatus / SnapshotKind / EffectClass mirror their CHECK
// constraints in 0001_init.up.sql. Keeping these as Go consts means
// callers can compare without string literals scattered through the
// codebase.
const (
	SessionStatusReserved  = "reserved"
	SessionStatusCommitted = "committed"
	SessionStatusAborted   = "aborted"
	SessionStatusHeld      = "held"
	SessionStatusReleased  = "released"
	SessionStatusExpired   = "expired"
	SessionStatusReclaimed = "reclaimed"
)

const (
	ResourceStatusActive   = "active"
	ResourceStatusDisabled = "disabled"
	ResourceStatusArchived = "archived"
)

const (
	BindingStatusActive    = "active"
	BindingStatusHeld      = "held"
	BindingStatusReleased  = "released"
	BindingStatusReclaimed = "reclaimed"
)

const (
	BindingPurposePrimary   = "primary"
	BindingPurposeSecondary = "secondary"
)

const (
	ReservationStatusPending   = "pending"
	ReservationStatusCommitted = "committed"
	ReservationStatusAborted   = "aborted"
	ReservationStatusExpired   = "expired"
)

const (
	SnapshotKindPreAction  = "pre_action"
	SnapshotKindPostAction = "post_action"
	SnapshotKindCheckpoint = "checkpoint"
	SnapshotKindPeriodic   = "periodic"
	SnapshotKindManual     = "manual"
)

const (
	EffectClassEnvLocalRead         = "env_local_read"
	EffectClassEnvLocalMutation     = "env_local_mutation"
	EffectClassExternalRead         = "external_read"
	EffectClassExternalWrite        = "external_write"
	EffectClassExternalIrreversible = "external_irreversible"
)

// Resource is the in-memory projection of orchestration_env_resources.
// Capacity bounds how many concurrently active sessions a resource may
// have; Status gates whether new sessions can be acquired.
type Resource struct {
	ID           string
	TenantID     string
	OwnerSubject string
	Kind         string
	Name         string
	Config       map[string]any
	Capacity     int
	Status       string
	Metadata     map[string]any
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Session is the in-memory projection of orchestration_env_sessions.
// LeaseToken plus LeaseEpoch are the fencing primitives every writer
// must echo back; the manager rejects writes whose epoch is below the
// row's current epoch.
type Session struct {
	ID              string
	TenantID        string
	ResourceID      string
	Status          string
	LeaseHolderKind string
	LeaseHolderID   string
	LeaseToken      string
	LeaseEpoch      int64
	LeaseAcquiredAt time.Time
	LeaseExpiresAt  *time.Time
	RunID           string
	TaskID          string
	AttemptID       string
	RuntimeHandle   map[string]any
	Metadata        map[string]any
	ReleasedAt      *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Reservation is the in-memory projection of
// orchestration_env_lease_reservations. The manager creates a
// reservation row on every Acquire so the audit trail of admission
// decisions stays uniform whether the request was satisfied
// immediately or queued for capacity (queueing arrives in Stage 4).
type Reservation struct {
	ID                 string
	TenantID           string
	ResourceID         string
	RequesterKind      string
	RequesterID        string
	RunID              string
	TaskID             string
	AttemptID          string
	Priority           int
	Status             string
	CommittedSessionID string
	RequestedAt        time.Time
	ExpiresAt          *time.Time
	CommittedAt        *time.Time
	AbortedAt          *time.Time
	Metadata           map[string]any
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// Binding maps a session to the task/attempt currently using it. A
// binding survives the attempt when status='held' so HITL pause/resume
// flows can re-attach a new attempt to the same warm session.
type Binding struct {
	ID                  string
	TenantID            string
	RunID               string
	TaskID              string
	AttemptID           string
	SessionID           string
	Purpose             string
	Status              string
	HeldForCheckpointID string
	Metadata            map[string]any
	ReleasedAt          *time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// Snapshot captures point-in-time runtime state for a session. The
// manager persists the metadata; backends own the actual bytes (often
// referenced via RuntimeRef so the manager stays storage-agnostic).
type Snapshot struct {
	ID          string
	TenantID    string
	SessionID   string
	RunID       string
	TaskID      string
	AttemptID   string
	Kind        string
	EffectClass string
	RuntimeRef  map[string]any
	Digest      string
	SizeBytes   int64
	Metadata    map[string]any
	CreatedAt   time.Time
}

// AcquireSessionRequest captures the inputs needed to open a session.
// The caller chooses TTL through LeaseTTL; the manager stamps
// LeaseExpiresAt accordingly and refuses requests that would exceed
// the resource's capacity. Backends never see this struct; the
// manager translates it into a Backend.AllocateRequest after the
// Postgres-side reservation has been recorded.
type AcquireSessionRequest struct {
	TenantID        string
	ResourceID      string
	LeaseHolderKind string
	LeaseHolderID   string
	LeaseTTL        time.Duration
	RunID           string
	TaskID          string
	AttemptID       string
	Priority        int
	Metadata        map[string]any
}

// ReleaseSessionRequest is a guarded transition to released. Callers
// must echo the lease_token they received from Acquire so a stale
// holder cannot release someone else's session.
type ReleaseSessionRequest struct {
	SessionID  string
	LeaseToken string
	LeaseEpoch int64
	Reason     string
}

// CreateBindingRequest pairs a session with a task/attempt. The
// manager validates lease_token + lease_epoch before recording the
// binding so callers cannot bind through a stale lease.
type CreateBindingRequest struct {
	SessionID  string
	LeaseToken string
	LeaseEpoch int64
	RunID      string
	TaskID     string
	AttemptID  string
	Purpose    string
	Metadata   map[string]any
}

// HoldBindingRequest marks a binding as held for HITL resume. The
// session row also moves to 'held' so subsequent acquire requests do
// not race the held capacity slot.
type HoldBindingRequest struct {
	BindingID           string
	LeaseToken          string
	LeaseEpoch          int64
	HeldForCheckpointID string
	Metadata            map[string]any
}

// ResumeBindingRequest re-attaches a held binding to a fresh attempt.
// The manager bumps lease_epoch and rotates lease_token; any stale
// writer carrying the old credentials is fenced out from that point
// forward.
type ResumeBindingRequest struct {
	BindingID        string
	NewAttemptID     string
	NewLeaseHolderID string
	LeaseTTL         time.Duration
	Metadata         map[string]any
}

// ReleaseBindingRequest marks a binding done. The manager decides
// whether to also release the underlying session (when no other
// active bindings remain) or leave it for explicit release.
type ReleaseBindingRequest struct {
	BindingID  string
	LeaseToken string
	LeaseEpoch int64
	Reason     string
}

// CaptureSnapshotRequest asks the backend for a fresh snapshot of the
// session and persists the metadata. EffectClass is optional and only
// recorded when the snapshot was taken alongside a specific action so
// drift detection can later correlate.
type CaptureSnapshotRequest struct {
	SessionID   string
	LeaseToken  string
	LeaseEpoch  int64
	Kind        string
	EffectClass string
	AttemptID   string
	TaskID      string
	RunID       string
	Metadata    map[string]any
}

// RenewSessionLeaseRequest is the heartbeat path. It bumps
// LeaseExpiresAt without rotating the token or epoch so in-flight
// writers stay valid.
type RenewSessionLeaseRequest struct {
	SessionID  string
	LeaseToken string
	LeaseEpoch int64
	LeaseTTL   time.Duration
}

// RegisterResourceRequest adds or updates a resource template. It is
// the only manager method that does not deal with sessions or
// bindings.
type RegisterResourceRequest struct {
	TenantID     string
	OwnerSubject string
	Kind         string
	Name         string
	Config       map[string]any
	Capacity     int
	Status       string
	Metadata     map[string]any
}

// UpdateResourceRequest rewrites the mutable fields of a resource
// template.
type UpdateResourceRequest struct {
	ID       string
	Name     string
	Config   map[string]any
	Capacity int
	Status   string
	Metadata map[string]any
}

// ReclaimResult summarises a single ReclaimExpiredSessions sweep so
// callers (the env reclaim loop, the admin endpoint, support
// tooling) can decide whether to retry or surface the totals.
type ReclaimResult struct {
	ScannedSessions  int
	ExpiredSessions  int
	ReleasedBindings int
	BackendErrors    int
}
