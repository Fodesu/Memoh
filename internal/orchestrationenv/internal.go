package orchestrationenv

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/memohai/memoh/internal/db"
	"github.com/memohai/memoh/internal/db/postgres/sqlc"
)

// newUUID returns both the string and pgtype representation in one
// call. Manager methods need both forms — the pgtype goes into sqlc
// params, the string goes into projections and structured logs.
func newUUID() (string, pgtype.UUID, error) {
	id, err := uuid.NewRandom()
	if err != nil {
		return "", pgtype.UUID{}, fmt.Errorf("env: generate uuid: %w", err)
	}
	pg := pgtype.UUID{Bytes: id, Valid: true}
	return id.String(), pg, nil
}

// validateRegisterResource fills in defaults and rejects malformed
// requests before they hit Postgres. Keeping defaults in one place
// avoids each manager method drifting toward subtly different values.
func validateRegisterResource(req *RegisterResourceRequest) error {
	if req == nil {
		return fmt.Errorf("%w: request is nil", ErrInvalidArgument)
	}
	req.TenantID = strings.TrimSpace(req.TenantID)
	req.Kind = strings.TrimSpace(req.Kind)
	req.Name = strings.TrimSpace(req.Name)
	if req.TenantID == "" {
		return fmt.Errorf("%w: tenant_id is required", ErrInvalidArgument)
	}
	if req.Name == "" {
		return fmt.Errorf("%w: name is required", ErrInvalidArgument)
	}
	if !validResourceKind(req.Kind) {
		return fmt.Errorf("%w: kind %q is not supported", ErrInvalidArgument, req.Kind)
	}
	if req.Capacity <= 0 {
		req.Capacity = 1
	}
	if req.Status == "" {
		req.Status = ResourceStatusActive
	}
	if !validResourceStatus(req.Status) {
		return fmt.Errorf("%w: status %q is invalid", ErrInvalidArgument, req.Status)
	}
	return nil
}

// validateUpdateResource normalizes admin edits before they reach SQL.
func validateUpdateResource(req *UpdateResourceRequest) error {
	if req == nil {
		return fmt.Errorf("%w: request is nil", ErrInvalidArgument)
	}
	req.ID = strings.TrimSpace(req.ID)
	req.Name = strings.TrimSpace(req.Name)
	if req.ID == "" {
		return fmt.Errorf("%w: id is required", ErrInvalidArgument)
	}
	if req.Name == "" {
		return fmt.Errorf("%w: name is required", ErrInvalidArgument)
	}
	if req.Capacity <= 0 {
		req.Capacity = 1
	}
	if req.Status == "" {
		req.Status = ResourceStatusActive
	}
	if !validResourceStatus(req.Status) {
		return fmt.Errorf("%w: status %q is invalid", ErrInvalidArgument, req.Status)
	}
	return nil
}

func validateAcquireSession(req *AcquireSessionRequest) error {
	if req == nil {
		return fmt.Errorf("%w: request is nil", ErrInvalidArgument)
	}
	req.TenantID = strings.TrimSpace(req.TenantID)
	req.ResourceID = strings.TrimSpace(req.ResourceID)
	req.LeaseHolderKind = strings.TrimSpace(req.LeaseHolderKind)
	req.LeaseHolderID = strings.TrimSpace(req.LeaseHolderID)
	if req.TenantID == "" {
		return fmt.Errorf("%w: tenant_id is required", ErrInvalidArgument)
	}
	if req.ResourceID == "" {
		return fmt.Errorf("%w: resource_id is required", ErrInvalidArgument)
	}
	if !validLeaseHolder(req.LeaseHolderKind) {
		return fmt.Errorf("%w: lease_holder_kind %q is invalid", ErrInvalidArgument, req.LeaseHolderKind)
	}
	if req.LeaseHolderID == "" {
		return fmt.Errorf("%w: lease_holder_id is required", ErrInvalidArgument)
	}
	return nil
}

func validateCreateBinding(req *CreateBindingRequest) error {
	if req == nil {
		return fmt.Errorf("%w: request is nil", ErrInvalidArgument)
	}
	req.SessionID = strings.TrimSpace(req.SessionID)
	req.RunID = strings.TrimSpace(req.RunID)
	req.TaskID = strings.TrimSpace(req.TaskID)
	if req.SessionID == "" {
		return fmt.Errorf("%w: session_id is required", ErrInvalidArgument)
	}
	if req.RunID == "" || req.TaskID == "" {
		return fmt.Errorf("%w: run_id and task_id are required", ErrInvalidArgument)
	}
	if req.Purpose == "" {
		req.Purpose = BindingPurposePrimary
	}
	if req.Purpose != BindingPurposePrimary && req.Purpose != BindingPurposeSecondary {
		return fmt.Errorf("%w: purpose %q is invalid", ErrInvalidArgument, req.Purpose)
	}
	return nil
}

// validResourceKind checks against the closed set the schema enforces
// so the manager fails fast before wasting a Postgres roundtrip.
func validResourceKind(kind string) bool {
	switch kind {
	case KindContainer, KindBrowser, KindDesktop, KindPhone, KindOther:
		return true
	}
	return false
}

func validResourceStatus(status string) bool {
	switch status {
	case ResourceStatusActive, ResourceStatusDisabled, ResourceStatusArchived:
		return true
	}
	return false
}

func validLeaseHolder(kind string) bool {
	switch kind {
	case LeaseHolderWorker, LeaseHolderVerifier, LeaseHolderOrchestrator, LeaseHolderHuman:
		return true
	}
	return false
}

// assertLeaseMatches enforces the lease fence. Empty lease_token in
// the request is treated as "no fence required" so kernel-internal
// callers can elide it; outside callers must echo what they were
// handed at acquire/resume time.
func assertLeaseMatches(session *Session, token string, epoch int64) error {
	if session == nil {
		return ErrSessionNotFound
	}
	if token == "" && epoch == 0 {
		return nil
	}
	if token != session.LeaseToken || epoch != session.LeaseEpoch {
		return ErrStaleLease
	}
	return nil
}

func isTerminalSessionStatus(status string) bool {
	switch status {
	case SessionStatusReleased, SessionStatusExpired, SessionStatusReclaimed, SessionStatusAborted:
		return true
	}
	return false
}

func isTerminalBindingStatus(status string) bool {
	switch status {
	case BindingStatusReleased, BindingStatusReclaimed:
		return true
	}
	return false
}

// mergeMetadata returns the JSON encoding of base + overlay; overlay
// keys win. Used wherever the manager wants to attach an audit
// breadcrumb (release_reason, expire_reason, ...) to existing row
// metadata without losing the prior values.
func mergeMetadata(base, overlay map[string]any) []byte {
	merged := make(map[string]any, len(base)+len(overlay))
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range overlay {
		merged[k] = v
	}
	return encodeObject(merged)
}

// resolveResourceKind digs the backend kind out of a runtime handle
// the noop backend (and future real backends) stamp in. It is best
// effort; an empty return tells callers to skip backend dispatch.
func resolveResourceKind(handle map[string]any) string {
	if handle == nil {
		return ""
	}
	if v, ok := handle["backend_kind"].(string); ok {
		return v
	}
	if v, ok := handle["resource_kind"].(string); ok {
		return v
	}
	return ""
}

// reserveCapacity opens a tx, locks the resource row implicitly via
// SELECT FOR UPDATE on the session count, and inserts both the
// reservation and the session in 'reserved' / 'pending' state. The
// returned reservation is in 'pending'; the returned session is in
// 'reserved'. commitAcquire flips both forward after backend
// allocation.
func (m *Manager) reserveCapacity(
	ctx context.Context,
	req AcquireSessionRequest,
	resource *Resource,
	resourceUUID pgtype.UUID,
	leaseToken string,
	leaseExpiresAt pgtype.Timestamptz,
) (*Reservation, *Session, error) {
	tx, err := m.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("env: begin acquire tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := m.queries.WithTx(tx)

	count, err := qtx.CountActiveOrchestrationEnvSessionsByResource(ctx, resourceUUID)
	if err != nil {
		return nil, nil, fmt.Errorf("env: count active sessions: %w", err)
	}
	if int(count) >= resource.Capacity {
		return nil, nil, fmt.Errorf("%w: %d/%d sessions in use", ErrCapacityExceeded, count, resource.Capacity)
	}

	_, reservationUUID, err := newUUID()
	if err != nil {
		return nil, nil, err
	}
	_, sessionUUID, err := newUUID()
	if err != nil {
		return nil, nil, err
	}

	reservationRow, err := qtx.CreateOrchestrationEnvLeaseReservation(ctx, sqlc.CreateOrchestrationEnvLeaseReservationParams{
		ID:            reservationUUID,
		TenantID:      req.TenantID,
		ResourceID:    resourceUUID,
		RequesterKind: req.LeaseHolderKind,
		RequesterID:   req.LeaseHolderID,
		RunID:         db.ParseUUIDOrEmpty(req.RunID),
		TaskID:        db.ParseUUIDOrEmpty(req.TaskID),
		AttemptID:     db.ParseUUIDOrEmpty(req.AttemptID),
		Priority:      int32(req.Priority), //nolint:gosec // priority bounded by caller
		Status:        ReservationStatusPending,
		ExpiresAt:     pgtype.Timestamptz{},
		Metadata:      encodeObject(req.Metadata),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("env: create reservation: %w", err)
	}

	sessionRow, err := qtx.CreateOrchestrationEnvSession(ctx, sqlc.CreateOrchestrationEnvSessionParams{
		ID:              sessionUUID,
		TenantID:        req.TenantID,
		ResourceID:      resourceUUID,
		Status:          SessionStatusReserved,
		LeaseHolderKind: req.LeaseHolderKind,
		LeaseHolderID:   req.LeaseHolderID,
		LeaseToken:      leaseToken,
		LeaseEpoch:      1,
		LeaseAcquiredAt: timeToPg(m.now().UTC()),
		LeaseExpiresAt:  leaseExpiresAt,
		RunID:           db.ParseUUIDOrEmpty(req.RunID),
		TaskID:          db.ParseUUIDOrEmpty(req.TaskID),
		AttemptID:       db.ParseUUIDOrEmpty(req.AttemptID),
		RuntimeHandle:   encodeObject(map[string]any{}),
		Metadata:        encodeObject(req.Metadata),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("env: create session: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, nil, fmt.Errorf("env: commit acquire reservation: %w", err)
	}
	reservation := projectReservation(reservationRow)
	session := projectSession(sessionRow)
	return &reservation, &session, nil
}

// commitAcquire moves the reservation to committed and the session to
// committed in a small follow-up tx. The runtime handle from
// backend.Allocate is recorded on the session row so subsequent
// snapshot/release calls can find it.
func (m *Manager) commitAcquire(ctx context.Context, reservationID string, session *Session, allocated AllocateResult) (*Session, error) {
	reservationUUID, err := db.ParseUUID(reservationID)
	if err != nil {
		return nil, fmt.Errorf("env: parse reservation id: %w", err)
	}
	sessionUUID, err := db.ParseUUID(session.ID)
	if err != nil {
		return nil, fmt.Errorf("env: parse session id: %w", err)
	}

	tx, err := m.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("env: begin commit tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := m.queries.WithTx(tx)

	if _, err := qtx.CommitOrchestrationEnvLeaseReservation(ctx, sqlc.CommitOrchestrationEnvLeaseReservationParams{
		CommittedSessionID: sessionUUID,
		ID:                 reservationUUID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("env: reservation %s no longer pending", reservationID)
		}
		return nil, fmt.Errorf("env: commit reservation: %w", err)
	}
	updated, err := qtx.UpdateOrchestrationEnvSessionStatus(ctx, sqlc.UpdateOrchestrationEnvSessionStatusParams{
		Status:        SessionStatusCommitted,
		RuntimeHandle: encodeObject(allocated.RuntimeHandle),
		Metadata:      mergeMetadata(session.Metadata, allocated.Metadata),
		ReleasedAt:    pgtype.Timestamptz{},
		ID:            sessionUUID,
	})
	if err != nil {
		return nil, fmt.Errorf("env: commit session: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("env: commit acquire: %w", err)
	}
	out := projectSession(updated)
	return &out, nil
}

// abortAfterBackendFailure walks back the reservation and session
// rows when backend.Allocate fails after step 1 succeeded. Both rows
// land in 'aborted' so the audit trail is unambiguous.
func (m *Manager) abortAfterBackendFailure(ctx context.Context, reservationID, sessionID string) error {
	reservationUUID, err := db.ParseUUID(reservationID)
	if err != nil {
		return err
	}
	sessionUUID, err := db.ParseUUID(sessionID)
	if err != nil {
		return err
	}
	tx, err := m.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := m.queries.WithTx(tx)

	if _, err := qtx.AbortOrchestrationEnvLeaseReservation(ctx, sqlc.AbortOrchestrationEnvLeaseReservationParams{
		Status: ReservationStatusAborted,
		ID:     reservationUUID,
	}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if _, err := qtx.UpdateOrchestrationEnvSessionStatus(ctx, sqlc.UpdateOrchestrationEnvSessionStatusParams{
		Status:        SessionStatusAborted,
		RuntimeHandle: encodeObject(map[string]any{}),
		Metadata:      encodeObject(map[string]any{"abort_reason": "backend_allocate_failed"}),
		ReleasedAt:    timeToPg(m.now().UTC()),
		ID:            sessionUUID,
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// lookupSessionAndBackend resolves the session row plus the backend
// for its kind. Returns ErrSessionNotFound / ErrBackendUnavailable so
// callers can branch.
func (m *Manager) lookupSessionAndBackend(ctx context.Context, sessionID pgtype.UUID) (*Session, Backend, error) {
	row, err := m.queries.GetOrchestrationEnvSessionByID(ctx, sessionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, ErrSessionNotFound
		}
		return nil, nil, fmt.Errorf("env: get session: %w", err)
	}
	session := projectSession(row)
	kind := resolveResourceKind(session.RuntimeHandle)
	if kind == "" {
		// Fall back to the resource row when the session row has no
		// stamped kind yet (e.g. session was created in the
		// reservation phase before backend.Allocate ran).
		resource, err := m.queries.GetOrchestrationEnvResourceByID(ctx, row.ResourceID)
		if err != nil {
			return &session, nil, fmt.Errorf("env: get resource for session: %w", err)
		}
		kind = resource.Kind
	}
	backend, ok := m.backends.Lookup(kind)
	if !ok {
		return &session, nil, fmt.Errorf("%w: kind %q has no registered backend", ErrBackendUnavailable, kind)
	}
	return &session, backend, nil
}
