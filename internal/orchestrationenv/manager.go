package orchestrationenv

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/memohai/memoh/internal/db"
	"github.com/memohai/memoh/internal/db/postgres/sqlc"
)

// Clock is the time source the manager uses. Tests inject a stub so
// they can reason about lease expiry deterministically; the default
// clock is time.Now.
type Clock func() time.Time

// Config wires the manager to its dependencies. Pool and Queries
// must operate on the same Postgres database; Backends is consulted
// at allocation time and may be empty for resource-only deployments.
// Logger is optional; nil routes to slog.Default.
type Config struct {
	Pool     *pgxpool.Pool
	Queries  *sqlc.Queries
	Backends *BackendRegistry
	Logger   *slog.Logger
	Clock    Clock
}

// Manager is the package's primary entry point. It owns the durable
// state machine (env_resources / sessions / reservations / bindings /
// snapshots) and delegates runtime allocation to Backend
// implementations. Every state transition validates lease_token plus
// lease_epoch so a stale holder cannot mutate state behind the
// kernel's back.
type Manager struct {
	pool     *pgxpool.Pool
	queries  *sqlc.Queries
	backends *BackendRegistry
	log      *slog.Logger
	now      Clock
}

// NewManager constructs a Manager from the given config. Pool /
// Queries / Backends are required; missing fields fail loud.
func NewManager(cfg Config) (*Manager, error) {
	if cfg.Pool == nil {
		return nil, errors.New("env: pool is required")
	}
	if cfg.Queries == nil {
		return nil, errors.New("env: queries is required")
	}
	if cfg.Backends == nil {
		cfg.Backends = NewBackendRegistry()
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Clock == nil {
		cfg.Clock = time.Now
	}
	return &Manager{
		pool:     cfg.Pool,
		queries:  cfg.Queries,
		backends: cfg.Backends,
		log:      cfg.Logger,
		now:      cfg.Clock,
	}, nil
}

// Backends exposes the underlying registry so wiring code can register
// backends after manager construction. Useful for FX-style graphs
// where the manager and the backends both depend on overlapping
// pieces.
func (m *Manager) Backends() *BackendRegistry {
	return m.backends
}

// RegisterResource creates a new env resource template. Names must
// be unique per tenant; the schema enforces it via a unique index, so
// a duplicate insert surfaces as a unique-violation error wrapped in
// ErrInvalidArgument so the caller can decide whether to retry under
// a different name.
func (m *Manager) RegisterResource(ctx context.Context, req RegisterResourceRequest) (*Resource, error) {
	if err := validateRegisterResource(&req); err != nil {
		return nil, err
	}
	id, idUUID, err := newUUID()
	if err != nil {
		return nil, err
	}
	row, err := m.queries.CreateOrchestrationEnvResource(ctx, sqlc.CreateOrchestrationEnvResourceParams{
		ID:           idUUID,
		TenantID:     req.TenantID,
		OwnerSubject: req.OwnerSubject,
		Kind:         req.Kind,
		Name:         req.Name,
		Config:       encodeObject(req.Config),
		Capacity:     int32(req.Capacity), //nolint:gosec // capacity is small and CHECK > 0
		Status:       req.Status,
		Metadata:     encodeObject(req.Metadata),
	})
	if err != nil {
		if db.IsUniqueViolation(err) {
			return nil, fmt.Errorf("%w: resource name already exists for tenant", ErrInvalidArgument)
		}
		return nil, fmt.Errorf("env: create resource: %w", err)
	}
	resource := projectResource(row)
	_ = id // id mirrors row.ID; kept for symmetry with other helpers.
	return &resource, nil
}

// GetResource looks up a resource by id. Returns ErrResourceNotFound
// for an unknown row so callers can fall through to a default
// resource lookup or surface the gap to the user.
func (m *Manager) GetResource(ctx context.Context, id string) (*Resource, error) {
	pgID, err := db.ParseUUID(id)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid resource id", ErrInvalidArgument)
	}
	row, err := m.queries.GetOrchestrationEnvResourceByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrResourceNotFound
		}
		return nil, fmt.Errorf("env: get resource: %w", err)
	}
	resource := projectResource(row)
	return &resource, nil
}

// GetResourceByName resolves the (tenant_id, name) unique key. Useful
// for kernel code that knows the canonical resource name (e.g.
// "default-container") without having to remember its UUID.
func (m *Manager) GetResourceByName(ctx context.Context, tenantID, name string) (*Resource, error) {
	tenantID = strings.TrimSpace(tenantID)
	name = strings.TrimSpace(name)
	if tenantID == "" || name == "" {
		return nil, fmt.Errorf("%w: tenant_id and name are required", ErrInvalidArgument)
	}
	row, err := m.queries.GetOrchestrationEnvResourceByTenantName(ctx, sqlc.GetOrchestrationEnvResourceByTenantNameParams{
		TenantID: tenantID,
		Name:     name,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrResourceNotFound
		}
		return nil, fmt.Errorf("env: get resource by name: %w", err)
	}
	resource := projectResource(row)
	return &resource, nil
}

// UpdateResource applies admin-controlled fields to a resource template.
// Existing sessions are left untouched; status and capacity only affect
// future admission decisions.
func (m *Manager) UpdateResource(ctx context.Context, req UpdateResourceRequest) (*Resource, error) {
	if err := validateUpdateResource(&req); err != nil {
		return nil, err
	}
	pgID, err := db.ParseUUID(req.ID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid resource id", ErrInvalidArgument)
	}
	row, err := m.queries.UpdateOrchestrationEnvResource(ctx, sqlc.UpdateOrchestrationEnvResourceParams{
		ID:       pgID,
		Name:     req.Name,
		Config:   encodeObject(req.Config),
		Capacity: int32(req.Capacity), //nolint:gosec // capacity is small and CHECK > 0
		Status:   req.Status,
		Metadata: encodeObject(req.Metadata),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrResourceNotFound
		}
		return nil, fmt.Errorf("env: update resource: %w", err)
	}
	resource := projectResource(row)
	return &resource, nil
}

// DeleteResource removes an unused resource template. Resources that
// have ever allocated sessions stay in place so run history and audit
// records keep a stable foreign key; callers can archive those instead.
func (m *Manager) DeleteResource(ctx context.Context, id string) error {
	pgID, err := db.ParseUUID(strings.TrimSpace(id))
	if err != nil {
		return fmt.Errorf("%w: invalid resource id", ErrInvalidArgument)
	}
	_, err = m.GetResource(ctx, id)
	if err != nil {
		return err
	}
	count, err := m.queries.CountOrchestrationEnvSessionsByResource(ctx, pgID)
	if err != nil {
		return fmt.Errorf("env: count resource sessions: %w", err)
	}
	if count > 0 {
		return ErrResourceInUse
	}
	if err := m.queries.DeleteOrchestrationEnvResource(ctx, pgID); err != nil {
		return fmt.Errorf("env: delete resource: %w", err)
	}
	return nil
}

// ListResources returns every resource for a tenant, alphabetised so
// admin UIs render a stable order without further sorting.
func (m *Manager) ListResources(ctx context.Context, tenantID string) ([]Resource, error) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return nil, fmt.Errorf("%w: tenant_id is required", ErrInvalidArgument)
	}
	rows, err := m.queries.ListOrchestrationEnvResourcesByTenant(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("env: list resources: %w", err)
	}
	out := make([]Resource, 0, len(rows))
	for _, row := range rows {
		out = append(out, projectResource(row))
	}
	return out, nil
}

// AcquireSession reserves a slot under the resource's capacity, asks
// the backend to allocate runtime state, and persists both a
// reservation row (for audit) and a session row (for fencing).
//
// The implementation is two-phase against Postgres but single-phase
// from the caller's view:
//
//  1. Begin tx, lock the resource row, count active sessions vs
//     capacity, insert reservation + session rows in pending /
//     reserved state.
//  2. Commit the Postgres tx.
//  3. Call backend.Allocate.
//  4. If backend.Allocate succeeds, mark reservation committed and
//     session committed in a second tx. If it fails, mark
//     reservation aborted and session aborted, then surface the
//     backend error.
//
// Steps 1+2 are wrapped together so capacity is reserved atomically;
// step 3 runs outside the tx so a slow backend does not hold a row
// lock. Step 4 is a small follow-up update. The state machine
// already accounts for an 'aborted' terminal status so a crash
// between steps 3 and 4 leaves a row that the reclaim sweep can
// finalise.
func (m *Manager) AcquireSession(ctx context.Context, req AcquireSessionRequest) (*Session, error) {
	if err := validateAcquireSession(&req); err != nil {
		return nil, err
	}

	resource, err := m.GetResource(ctx, req.ResourceID)
	if err != nil {
		return nil, err
	}
	if resource.Status != ResourceStatusActive {
		return nil, fmt.Errorf("%w: resource %s is %s", ErrInvalidArgument, resource.ID, resource.Status)
	}

	backend, ok := m.backends.Lookup(resource.Kind)
	if !ok {
		return nil, fmt.Errorf("%w: kind %q has no registered backend", ErrBackendUnavailable, resource.Kind)
	}

	resourceUUID, err := db.ParseUUID(resource.ID)
	if err != nil {
		return nil, fmt.Errorf("env: invalid resource id: %w", err)
	}

	leaseToken := uuid.NewString()
	now := m.now().UTC()
	var leaseExpiresAt pgtype.Timestamptz
	if req.LeaseTTL > 0 {
		leaseExpiresAt = timeToPg(now.Add(req.LeaseTTL))
	}

	_ = now
	reservation, session, err := m.reserveCapacity(ctx, req, resource, resourceUUID, leaseToken, leaseExpiresAt)
	if err != nil {
		return nil, err
	}

	allocated, allocateErr := backend.Allocate(ctx, AllocateRequest{
		ResourceID:     resource.ID,
		ResourceKind:   resource.Kind,
		ResourceName:   resource.Name,
		ResourceConfig: resource.Config,
		SessionID:      session.ID,
		TenantID:       session.TenantID,
		RunID:          session.RunID,
		TaskID:         session.TaskID,
		AttemptID:      session.AttemptID,
		LeaseTTL:       req.LeaseTTL,
		Metadata:       req.Metadata,
	})
	if allocateErr != nil {
		if abortErr := m.abortAfterBackendFailure(ctx, reservation.ID, session.ID); abortErr != nil {
			m.log.WarnContext(ctx, "env: abort cleanup failed",
				slog.String("session_id", session.ID),
				slog.String("reservation_id", reservation.ID),
				slog.Any("err", abortErr))
		}
		return nil, fmt.Errorf("env: backend allocate: %w", allocateErr)
	}

	committed, err := m.commitAcquire(ctx, reservation.ID, session, allocated)
	if err != nil {
		// Best-effort: try to release the runtime that was just
		// allocated so we do not leak. The session row is left
		// dirty for the reclaim sweep to finalise.
		_ = backend.Release(ctx, ReleaseRequestBackend{
			SessionID:     session.ID,
			ResourceKind:  resource.Kind,
			RuntimeHandle: allocated.RuntimeHandle,
			Reason:        "commit_failed",
		})
		return nil, err
	}
	return committed, nil
}

// ReleaseSession marks the session released and asks the backend to
// tear down the underlying runtime. Lease fencing is enforced so a
// stale holder cannot release a session re-leased to someone else.
func (m *Manager) ReleaseSession(ctx context.Context, req ReleaseSessionRequest) error {
	if strings.TrimSpace(req.SessionID) == "" {
		return fmt.Errorf("%w: session_id is required", ErrInvalidArgument)
	}
	sessionUUID, err := db.ParseUUID(req.SessionID)
	if err != nil {
		return fmt.Errorf("%w: invalid session id", ErrInvalidArgument)
	}

	session, backend, err := m.lookupSessionAndBackend(ctx, sessionUUID)
	if err != nil {
		return err
	}
	if err := assertLeaseMatches(session, req.LeaseToken, req.LeaseEpoch); err != nil {
		return err
	}
	if isTerminalSessionStatus(session.Status) {
		return ErrSessionTerminal
	}

	releasedAt := m.now().UTC()
	updated, err := m.queries.UpdateOrchestrationEnvSessionStatus(ctx, sqlc.UpdateOrchestrationEnvSessionStatusParams{
		Status:        SessionStatusReleased,
		RuntimeHandle: encodeObject(session.RuntimeHandle),
		Metadata:      mergeMetadata(session.Metadata, map[string]any{"release_reason": req.Reason}),
		ReleasedAt:    timeToPg(releasedAt),
		ID:            sessionUUID,
	})
	if err != nil {
		return fmt.Errorf("env: update session status: %w", err)
	}

	if backend != nil {
		if err := backend.Release(ctx, ReleaseRequestBackend{
			SessionID:     session.ID,
			ResourceKind:  resolveResourceKind(session.RuntimeHandle),
			RuntimeHandle: session.RuntimeHandle,
			Reason:        req.Reason,
		}); err != nil {
			m.log.WarnContext(ctx, "env: backend release returned error",
				slog.String("session_id", session.ID),
				slog.Any("err", err))
		}
	}
	_ = updated
	return nil
}

// RenewSessionLease bumps lease_expires_at without rotating the token
// or epoch. It is the heartbeat path for in-flight workers.
func (m *Manager) RenewSessionLease(ctx context.Context, req RenewSessionLeaseRequest) (*Session, error) {
	if strings.TrimSpace(req.SessionID) == "" {
		return nil, fmt.Errorf("%w: session_id is required", ErrInvalidArgument)
	}
	sessionUUID, err := db.ParseUUID(req.SessionID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid session id", ErrInvalidArgument)
	}
	tx, err := m.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("env: begin renew tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := m.queries.WithTx(tx)

	row, err := qtx.GetOrchestrationEnvSessionByIDForUpdate(ctx, sessionUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrSessionNotFound
		}
		return nil, fmt.Errorf("env: select session: %w", err)
	}
	current := projectSession(row)
	if err := assertLeaseMatches(&current, req.LeaseToken, req.LeaseEpoch); err != nil {
		return nil, err
	}
	if isTerminalSessionStatus(current.Status) {
		return nil, ErrSessionTerminal
	}

	expiresAt := pgtype.Timestamptz{}
	if req.LeaseTTL > 0 {
		expiresAt = timeToPg(m.now().UTC().Add(req.LeaseTTL))
	}
	updated, err := qtx.UpdateOrchestrationEnvSessionLease(ctx, sqlc.UpdateOrchestrationEnvSessionLeaseParams{
		LeaseHolderKind: row.LeaseHolderKind,
		LeaseHolderID:   row.LeaseHolderID,
		LeaseToken:      row.LeaseToken,
		LeaseEpoch:      row.LeaseEpoch,
		LeaseAcquiredAt: row.LeaseAcquiredAt,
		LeaseExpiresAt:  expiresAt,
		AttemptID:       row.AttemptID,
		TaskID:          row.TaskID,
		RunID:           row.RunID,
		ID:              sessionUUID,
	})
	if err != nil {
		return nil, fmt.Errorf("env: update lease: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("env: commit renew: %w", err)
	}
	out := projectSession(updated)
	return &out, nil
}

// CreateBinding records the (session_id, run_id, task_id, attempt_id)
// link. Only one active or held binding per session is allowed; the
// schema enforces it via partial unique index.
func (m *Manager) CreateBinding(ctx context.Context, req CreateBindingRequest) (*Binding, error) {
	return m.createBinding(ctx, m.queries, req)
}

func (m *Manager) CreateBindingInTx(ctx context.Context, qtx *sqlc.Queries, req CreateBindingRequest) (*Binding, error) {
	if qtx == nil {
		return nil, fmt.Errorf("%w: queries is required", ErrInvalidArgument)
	}
	return m.createBinding(ctx, qtx, req)
}

func (*Manager) createBinding(ctx context.Context, qtx *sqlc.Queries, req CreateBindingRequest) (*Binding, error) {
	if err := validateCreateBinding(&req); err != nil {
		return nil, err
	}
	sessionUUID, err := db.ParseUUID(req.SessionID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid session id", ErrInvalidArgument)
	}

	sessionRow, err := qtx.GetOrchestrationEnvSessionByIDForUpdate(ctx, sessionUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrSessionNotFound
		}
		return nil, fmt.Errorf("env: get session: %w", err)
	}
	session := projectSession(sessionRow)
	if err := assertLeaseMatches(&session, req.LeaseToken, req.LeaseEpoch); err != nil {
		return nil, err
	}
	if isTerminalSessionStatus(session.Status) {
		return nil, ErrSessionTerminal
	}

	runUUID, err := db.ParseUUID(req.RunID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid run id", ErrInvalidArgument)
	}
	taskUUID, err := db.ParseUUID(req.TaskID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid task id", ErrInvalidArgument)
	}
	attemptUUID := db.ParseUUIDOrEmpty(req.AttemptID)

	id, idUUID, err := newUUID()
	if err != nil {
		return nil, err
	}
	purpose := req.Purpose
	if purpose == "" {
		purpose = BindingPurposePrimary
	}
	row, err := qtx.CreateOrchestrationEnvBinding(ctx, sqlc.CreateOrchestrationEnvBindingParams{
		ID:                  idUUID,
		TenantID:            session.TenantID,
		RunID:               runUUID,
		TaskID:              taskUUID,
		AttemptID:           attemptUUID,
		SessionID:           sessionUUID,
		Purpose:             purpose,
		Status:              BindingStatusActive,
		HeldForCheckpointID: pgtype.UUID{},
		Metadata:            encodeObject(req.Metadata),
	})
	if err != nil {
		if db.IsUniqueViolation(err) {
			return nil, fmt.Errorf("%w: session already has an active or held binding", ErrInvalidArgument)
		}
		return nil, fmt.Errorf("env: create binding: %w", err)
	}
	_ = id
	binding := projectBinding(row)
	return &binding, nil
}

// GetHeldBindingForTask returns the held primary binding for a task, if any.
func (m *Manager) GetHeldBindingForTask(ctx context.Context, runID, taskID string) (*Binding, bool, error) {
	runUUID, err := db.ParseUUID(runID)
	if err != nil {
		return nil, false, fmt.Errorf("%w: invalid run id", ErrInvalidArgument)
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, false, fmt.Errorf("%w: task_id is required", ErrInvalidArgument)
	}
	rows, err := m.queries.ListActiveOrchestrationEnvBindingsByRun(ctx, runUUID)
	if err != nil {
		return nil, false, fmt.Errorf("env: list active bindings: %w", err)
	}
	for _, row := range rows {
		binding := projectBinding(row)
		if binding.TaskID == taskID && binding.Status == BindingStatusHeld {
			return &binding, true, nil
		}
	}
	return nil, false, nil
}

// HoldBinding marks an active binding as held for HITL resume and
// transitions the underlying session to 'held' so capacity accounting
// keeps it counted.
func (m *Manager) HoldBinding(ctx context.Context, req HoldBindingRequest) (*Binding, error) {
	if strings.TrimSpace(req.BindingID) == "" {
		return nil, fmt.Errorf("%w: binding_id is required", ErrInvalidArgument)
	}
	bindingUUID, err := db.ParseUUID(req.BindingID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid binding id", ErrInvalidArgument)
	}

	tx, err := m.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("env: begin hold tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := m.queries.WithTx(tx)

	bindingRow, err := qtx.GetOrchestrationEnvBindingByID(ctx, bindingUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrBindingNotFound
		}
		return nil, fmt.Errorf("env: get binding: %w", err)
	}
	binding := projectBinding(bindingRow)
	if isTerminalBindingStatus(binding.Status) {
		return nil, ErrBindingTerminal
	}

	sessionRow, err := qtx.GetOrchestrationEnvSessionByIDForUpdate(ctx, bindingRow.SessionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrSessionNotFound
		}
		return nil, fmt.Errorf("env: get session: %w", err)
	}
	session := projectSession(sessionRow)
	if err := assertLeaseMatches(&session, req.LeaseToken, req.LeaseEpoch); err != nil {
		return nil, err
	}

	updatedSession, err := qtx.UpdateOrchestrationEnvSessionStatus(ctx, sqlc.UpdateOrchestrationEnvSessionStatusParams{
		Status:        SessionStatusHeld,
		RuntimeHandle: encodeObject(session.RuntimeHandle),
		Metadata:      encodeObject(session.Metadata),
		ReleasedAt:    pgtype.Timestamptz{},
		ID:            sessionRow.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("env: update session held: %w", err)
	}
	_ = updatedSession

	checkpointUUID := db.ParseUUIDOrEmpty(req.HeldForCheckpointID)
	updatedBinding, err := qtx.UpdateOrchestrationEnvBindingStatus(ctx, sqlc.UpdateOrchestrationEnvBindingStatusParams{
		Status:              BindingStatusHeld,
		HeldForCheckpointID: checkpointUUID,
		Metadata:            mergeMetadata(binding.Metadata, req.Metadata),
		ReleasedAt:          pgtype.Timestamptz{},
		ID:                  bindingUUID,
	})
	if err != nil {
		return nil, fmt.Errorf("env: update binding held: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("env: commit hold: %w", err)
	}
	out := projectBinding(updatedBinding)
	return &out, nil
}

// ResumeBinding re-attaches a held binding to a fresh attempt. The
// session's lease_epoch is bumped and lease_token rotated so any
// stale credentials from the prior attempt are fenced out.
func (m *Manager) ResumeBinding(ctx context.Context, req ResumeBindingRequest) (*Binding, error) {
	if strings.TrimSpace(req.BindingID) == "" {
		return nil, fmt.Errorf("%w: binding_id is required", ErrInvalidArgument)
	}
	if strings.TrimSpace(req.NewAttemptID) == "" {
		return nil, fmt.Errorf("%w: new_attempt_id is required", ErrInvalidArgument)
	}
	bindingUUID, err := db.ParseUUID(req.BindingID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid binding id", ErrInvalidArgument)
	}
	newAttemptUUID, err := db.ParseUUID(req.NewAttemptID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid new_attempt_id", ErrInvalidArgument)
	}

	tx, err := m.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("env: begin resume tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := m.queries.WithTx(tx)

	bindingRow, err := qtx.GetOrchestrationEnvBindingByID(ctx, bindingUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrBindingNotFound
		}
		return nil, fmt.Errorf("env: get binding: %w", err)
	}
	binding := projectBinding(bindingRow)
	if binding.Status != BindingStatusHeld {
		return nil, fmt.Errorf("%w: binding %s is not held", ErrInvalidArgument, binding.ID)
	}

	sessionRow, err := qtx.GetOrchestrationEnvSessionByIDForUpdate(ctx, bindingRow.SessionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrSessionNotFound
		}
		return nil, fmt.Errorf("env: get session: %w", err)
	}
	session := projectSession(sessionRow)
	if session.Status != SessionStatusHeld {
		return nil, fmt.Errorf("%w: session %s is not held", ErrInvalidArgument, session.ID)
	}

	now := m.now().UTC()
	var leaseExpiresAt pgtype.Timestamptz
	if req.LeaseTTL > 0 {
		leaseExpiresAt = timeToPg(now.Add(req.LeaseTTL))
	}
	holderID := req.NewLeaseHolderID
	if strings.TrimSpace(holderID) == "" {
		holderID = sessionRow.LeaseHolderID
	}
	rotated, err := qtx.UpdateOrchestrationEnvSessionLease(ctx, sqlc.UpdateOrchestrationEnvSessionLeaseParams{
		LeaseHolderKind: sessionRow.LeaseHolderKind,
		LeaseHolderID:   holderID,
		LeaseToken:      uuid.NewString(),
		LeaseEpoch:      sessionRow.LeaseEpoch + 1,
		LeaseAcquiredAt: timeToPg(now),
		LeaseExpiresAt:  leaseExpiresAt,
		AttemptID:       newAttemptUUID,
		TaskID:          sessionRow.TaskID,
		RunID:           sessionRow.RunID,
		ID:              sessionRow.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("env: rotate lease: %w", err)
	}
	if _, err := qtx.UpdateOrchestrationEnvSessionStatus(ctx, sqlc.UpdateOrchestrationEnvSessionStatusParams{
		Status:        SessionStatusCommitted,
		RuntimeHandle: rotated.RuntimeHandle,
		Metadata:      rotated.Metadata,
		ReleasedAt:    pgtype.Timestamptz{},
		ID:            sessionRow.ID,
	}); err != nil {
		return nil, fmt.Errorf("env: reactivate session: %w", err)
	}

	updatedBindingRow, err := qtx.UpdateOrchestrationEnvBindingStatus(ctx, sqlc.UpdateOrchestrationEnvBindingStatusParams{
		Status:              BindingStatusActive,
		HeldForCheckpointID: pgtype.UUID{},
		Metadata:            mergeMetadata(binding.Metadata, req.Metadata),
		ReleasedAt:          pgtype.Timestamptz{},
		ID:                  bindingUUID,
	})
	if err != nil {
		return nil, fmt.Errorf("env: update binding resume: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("env: commit resume: %w", err)
	}
	out := projectBinding(updatedBindingRow)
	return &out, nil
}

// ReleaseBinding marks a binding released. The caller is responsible
// for calling ReleaseSession separately if it also wants the runtime
// torn down; keeping the two calls independent lets sibling bindings
// continue to share a session when that pattern lands.
func (m *Manager) ReleaseBinding(ctx context.Context, req ReleaseBindingRequest) error {
	if strings.TrimSpace(req.BindingID) == "" {
		return fmt.Errorf("%w: binding_id is required", ErrInvalidArgument)
	}
	bindingUUID, err := db.ParseUUID(req.BindingID)
	if err != nil {
		return fmt.Errorf("%w: invalid binding id", ErrInvalidArgument)
	}
	bindingRow, err := m.queries.GetOrchestrationEnvBindingByID(ctx, bindingUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrBindingNotFound
		}
		return fmt.Errorf("env: get binding: %w", err)
	}
	binding := projectBinding(bindingRow)
	if isTerminalBindingStatus(binding.Status) {
		return ErrBindingTerminal
	}

	sessionRow, err := m.queries.GetOrchestrationEnvSessionByID(ctx, bindingRow.SessionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrSessionNotFound
		}
		return fmt.Errorf("env: get session: %w", err)
	}
	session := projectSession(sessionRow)
	if err := assertLeaseMatches(&session, req.LeaseToken, req.LeaseEpoch); err != nil {
		return err
	}

	releasedAt := m.now().UTC()
	if _, err := m.queries.UpdateOrchestrationEnvBindingStatus(ctx, sqlc.UpdateOrchestrationEnvBindingStatusParams{
		Status:              BindingStatusReleased,
		HeldForCheckpointID: pgtype.UUID{},
		Metadata:            mergeMetadata(binding.Metadata, map[string]any{"release_reason": req.Reason}),
		ReleasedAt:          timeToPg(releasedAt),
		ID:                  bindingUUID,
	}); err != nil {
		return fmt.Errorf("env: release binding: %w", err)
	}
	return nil
}

// CaptureSnapshot delegates to the backend and persists the metadata.
// Callers that pre-built a snapshot (e.g., backend-internal periodic
// snapshot) can still record it via this entry point by registering
// a backend that returns the desired result without re-doing work.
func (m *Manager) CaptureSnapshot(ctx context.Context, req CaptureSnapshotRequest) (*Snapshot, error) {
	if strings.TrimSpace(req.SessionID) == "" {
		return nil, fmt.Errorf("%w: session_id is required", ErrInvalidArgument)
	}
	if strings.TrimSpace(req.Kind) == "" {
		return nil, fmt.Errorf("%w: kind is required", ErrInvalidArgument)
	}
	sessionUUID, err := db.ParseUUID(req.SessionID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid session id", ErrInvalidArgument)
	}
	session, backend, err := m.lookupSessionAndBackend(ctx, sessionUUID)
	if err != nil {
		return nil, err
	}
	if err := assertLeaseMatches(session, req.LeaseToken, req.LeaseEpoch); err != nil {
		return nil, err
	}

	result, err := backend.Snapshot(ctx, SnapshotRequestBackend{
		SessionID:     session.ID,
		ResourceKind:  resolveResourceKind(session.RuntimeHandle),
		RuntimeHandle: session.RuntimeHandle,
		Kind:          req.Kind,
		EffectClass:   req.EffectClass,
	})
	if err != nil {
		return nil, fmt.Errorf("env: backend snapshot: %w", err)
	}

	id, idUUID, err := newUUID()
	if err != nil {
		return nil, err
	}
	row, err := m.queries.CreateOrchestrationEnvSnapshot(ctx, sqlc.CreateOrchestrationEnvSnapshotParams{
		ID:          idUUID,
		TenantID:    session.TenantID,
		SessionID:   sessionUUID,
		RunID:       db.ParseUUIDOrEmpty(req.RunID),
		TaskID:      db.ParseUUIDOrEmpty(req.TaskID),
		AttemptID:   db.ParseUUIDOrEmpty(req.AttemptID),
		Kind:        req.Kind,
		EffectClass: req.EffectClass,
		RuntimeRef:  encodeObject(result.RuntimeRef),
		Digest:      result.Digest,
		SizeBytes:   result.SizeBytes,
		Metadata:    mergeMetadata(req.Metadata, result.Metadata),
	})
	if err != nil {
		return nil, fmt.Errorf("env: create snapshot: %w", err)
	}
	_ = id
	snap := projectSnapshot(row)
	return &snap, nil
}

// GetSession is a convenience read for handlers and tests.
func (m *Manager) GetSession(ctx context.Context, id string) (*Session, error) {
	pgID, err := db.ParseUUID(id)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid session id", ErrInvalidArgument)
	}
	row, err := m.queries.GetOrchestrationEnvSessionByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrSessionNotFound
		}
		return nil, fmt.Errorf("env: get session: %w", err)
	}
	out := projectSession(row)
	return &out, nil
}

// GetBinding is a convenience read.
func (m *Manager) GetBinding(ctx context.Context, id string) (*Binding, error) {
	pgID, err := db.ParseUUID(id)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid binding id", ErrInvalidArgument)
	}
	row, err := m.queries.GetOrchestrationEnvBindingByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrBindingNotFound
		}
		return nil, fmt.Errorf("env: get binding: %w", err)
	}
	out := projectBinding(row)
	return &out, nil
}

// ListSessionSnapshots returns snapshots in insertion order. Useful
// for inspector views and verifier replay.
func (m *Manager) ListSessionSnapshots(ctx context.Context, sessionID string) ([]Snapshot, error) {
	pgID, err := db.ParseUUID(sessionID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid session id", ErrInvalidArgument)
	}
	rows, err := m.queries.ListOrchestrationEnvSnapshotsBySession(ctx, pgID)
	if err != nil {
		return nil, fmt.Errorf("env: list snapshots: %w", err)
	}
	out := make([]Snapshot, 0, len(rows))
	for _, row := range rows {
		out = append(out, projectSnapshot(row))
	}
	return out, nil
}

// ReclaimExpiredSessions sweeps sessions whose lease expired and
// transitions them to expired plus their bindings to reclaimed. The
// backend Release call is best-effort; failures only bump
// ReclaimResult.BackendErrors so the caller can decide whether to
// alert.
func (m *Manager) ReclaimExpiredSessions(ctx context.Context, maxRows int32) (ReclaimResult, error) {
	if maxRows <= 0 {
		maxRows = 64
	}
	rows, err := m.queries.ListExpiredOrchestrationEnvSessions(ctx, sqlc.ListExpiredOrchestrationEnvSessionsParams{
		Now:     timeToPg(m.now().UTC()),
		MaxRows: maxRows,
	})
	if err != nil {
		return ReclaimResult{}, fmt.Errorf("env: list expired: %w", err)
	}
	result := ReclaimResult{ScannedSessions: len(rows)}
	for _, row := range rows {
		session := projectSession(row)
		bindings, err := m.queries.ListOrchestrationEnvBindingsBySession(ctx, row.ID)
		if err != nil {
			m.log.WarnContext(ctx, "env: list bindings during reclaim",
				slog.String("session_id", session.ID),
				slog.Any("err", err))
			continue
		}
		releasedAt := timeToPg(m.now().UTC())
		for _, bindingRow := range bindings {
			binding := projectBinding(bindingRow)
			if isTerminalBindingStatus(binding.Status) {
				continue
			}
			if _, err := m.queries.UpdateOrchestrationEnvBindingStatus(ctx, sqlc.UpdateOrchestrationEnvBindingStatusParams{
				Status:              BindingStatusReclaimed,
				HeldForCheckpointID: pgtype.UUID{},
				Metadata:            encodeObject(binding.Metadata),
				ReleasedAt:          releasedAt,
				ID:                  bindingRow.ID,
			}); err != nil {
				m.log.WarnContext(ctx, "env: update binding reclaim",
					slog.String("binding_id", binding.ID),
					slog.Any("err", err))
				continue
			}
			result.ReleasedBindings++
		}
		if _, err := m.queries.UpdateOrchestrationEnvSessionStatus(ctx, sqlc.UpdateOrchestrationEnvSessionStatusParams{
			Status:        SessionStatusExpired,
			RuntimeHandle: encodeObject(session.RuntimeHandle),
			Metadata:      mergeMetadata(session.Metadata, map[string]any{"expire_reason": "lease_expired"}),
			ReleasedAt:    releasedAt,
			ID:            row.ID,
		}); err != nil {
			m.log.WarnContext(ctx, "env: update session expired",
				slog.String("session_id", session.ID),
				slog.Any("err", err))
			continue
		}
		result.ExpiredSessions++

		if backend, ok := m.backends.Lookup(resolveResourceKind(session.RuntimeHandle)); ok {
			if err := backend.Release(ctx, ReleaseRequestBackend{
				SessionID:     session.ID,
				ResourceKind:  resolveResourceKind(session.RuntimeHandle),
				RuntimeHandle: session.RuntimeHandle,
				Reason:        "lease_expired",
			}); err != nil {
				result.BackendErrors++
				m.log.WarnContext(ctx, "env: backend release during reclaim",
					slog.String("session_id", session.ID),
					slog.Any("err", err))
			}
		}
	}
	return result, nil
}
