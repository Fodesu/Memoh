package botsetup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/felinics/memoh/internal/db"
	dbsqlc "github.com/felinics/memoh/internal/db/postgres/sqlc"
	dbstore "github.com/felinics/memoh/internal/db/store"
)

// ObservedWrite is one observation of the setup row.
type ObservedWrite struct {
	BotID              string
	Owner              string
	ExpectedVersion    int64
	State              string
	ObservedGeneration int64
	Attempts           int32
	NextAttemptAt      time.Time
	ReleaseLease       bool
}

// Repository is the persistence port; the postgres implementation is thin so
// the reconciler can be tested against an in-memory fake.
type Repository interface {
	// Upsert records a new intent (generation +1 on an existing row) and
	// resets every step: pending for the steps the spec manages, skipped for
	// the rest.
	Upsert(ctx context.Context, botID, requestedBy string, spec Spec) (Setup, error)
	// Retry bumps the generation and resets the steps that are not done.
	Retry(ctx context.Context, botID string) (Setup, error)
	Get(ctx context.Context, botID string) (Setup, error)
	Claim(ctx context.Context, owner string, lease time.Duration, limit int32) ([]Setup, error)
	Renew(ctx context.Context, botID, owner string, lease time.Duration) error
	WriteObserved(ctx context.Context, w ObservedWrite) (Setup, error)
	Release(ctx context.Context, botID, owner string) error
	WriteStep(ctx context.Context, botID string, step Step) error
}

type postgresRepository struct {
	queries dbstore.Queries
}

// txRunner is the optional transaction entry point of the postgres store; the
// dbstore.Queries interface does not expose it, so it is detected at runtime
// and the repository degrades to sequential writes without it (tests).
type txRunner interface {
	InTx(ctx context.Context, fn func(dbstore.Queries) error) error
}

// inTx runs fn inside one transaction when the store supports it.
func (r *postgresRepository) inTx(ctx context.Context, fn func(q dbstore.Queries) error) error {
	if tx, ok := r.queries.(txRunner); ok {
		return tx.InTx(ctx, fn)
	}
	return fn(r.queries)
}

// NewRepository returns the postgres-backed Repository.
func NewRepository(queries dbstore.Queries) Repository {
	return &postgresRepository{queries: queries}
}

func (r *postgresRepository) Upsert(ctx context.Context, botID, requestedBy string, spec Spec) (Setup, error) {
	id, err := db.ParseUUID(botID)
	if err != nil {
		return Setup{}, err
	}
	payload, err := json.Marshal(spec)
	if err != nil {
		return Setup{}, fmt.Errorf("encode setup spec: %w", err)
	}
	params := dbsqlc.UpsertBotSetupIntentParams{BotID: id, Spec: payload}
	if requestedBy != "" {
		if owner, err := db.ParseUUID(requestedBy); err == nil {
			params.RequestedByUserID = owner
		}
	}
	// The intent bump and the step reset land together: a pass that claims
	// the row in between would otherwise see the new generation with the old
	// generation's step statuses.
	var row dbsqlc.BotSetup
	err = r.inTx(ctx, func(q dbstore.Queries) error {
		var err error
		row, err = q.UpsertBotSetupIntent(ctx, params)
		if err != nil {
			return err
		}
		for _, step := range StepOrder {
			status := StatusSkipped
			if spec.Manages(step) {
				status = StatusPending
			}
			if _, err := q.UpsertBotSetupStep(ctx, dbsqlc.UpsertBotSetupStepParams{
				BotID: id, Step: step, Status: status, Generation: row.DesiredGeneration,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return Setup{}, err
	}
	return r.withSteps(ctx, fromRow(row))
}

func (r *postgresRepository) Retry(ctx context.Context, botID string) (Setup, error) {
	id, err := db.ParseUUID(botID)
	if err != nil {
		return Setup{}, err
	}
	var row dbsqlc.BotSetup
	err = r.inTx(ctx, func(q dbstore.Queries) error {
		var err error
		row, err = q.RetryBotSetupIntent(ctx, id)
		if err != nil {
			return err
		}
		return q.ResetBotSetupStepsNotDone(ctx, id)
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Setup{}, ErrNotFound
		}
		return Setup{}, err
	}
	return r.withSteps(ctx, fromRow(row))
}

func (r *postgresRepository) Get(ctx context.Context, botID string) (Setup, error) {
	id, err := db.ParseUUID(botID)
	if err != nil {
		return Setup{}, err
	}
	row, err := r.queries.GetBotSetup(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Setup{}, ErrNotFound
		}
		return Setup{}, err
	}
	return r.withSteps(ctx, fromRow(row))
}

func (r *postgresRepository) Claim(ctx context.Context, owner string, lease time.Duration, limit int32) ([]Setup, error) {
	rows, err := r.queries.ClaimBotSetups(ctx, dbsqlc.ClaimBotSetupsParams{
		LeaseOwner: owner, LeaseSeconds: lease.Seconds(), Lim: limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]Setup, 0, len(rows))
	for _, row := range rows {
		s, err := r.withSteps(ctx, fromRow(row))
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

func (r *postgresRepository) Renew(ctx context.Context, botID, owner string, lease time.Duration) error {
	id, err := db.ParseUUID(botID)
	if err != nil {
		return err
	}
	n, err := r.queries.RenewBotSetupLease(ctx, dbsqlc.RenewBotSetupLeaseParams{LeaseSeconds: lease.Seconds(), BotID: id, LeaseOwner: owner})
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrVersionConflict
	}
	return nil
}

func (r *postgresRepository) WriteObserved(ctx context.Context, w ObservedWrite) (Setup, error) {
	id, err := db.ParseUUID(w.BotID)
	if err != nil {
		return Setup{}, err
	}
	row, err := r.queries.UpdateBotSetupObserved(ctx, dbsqlc.UpdateBotSetupObservedParams{
		State: w.State, ObservedGeneration: w.ObservedGeneration, Attempts: w.Attempts,
		NextAttemptAt: pgtype.Timestamptz{Time: w.NextAttemptAt, Valid: true},
		ReleaseLease:  w.ReleaseLease, BotID: id, LeaseOwner: w.Owner, ExpectedVersion: w.ExpectedVersion,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Setup{}, ErrVersionConflict
		}
		return Setup{}, err
	}
	return r.withSteps(ctx, fromRow(row))
}

func (r *postgresRepository) Release(ctx context.Context, botID, owner string) error {
	id, err := db.ParseUUID(botID)
	if err != nil {
		return err
	}
	_, err = r.queries.ReleaseBotSetupLease(ctx, dbsqlc.ReleaseBotSetupLeaseParams{BotID: id, LeaseOwner: owner})
	return err
}

func (r *postgresRepository) WriteStep(ctx context.Context, botID string, step Step) error {
	id, err := db.ParseUUID(botID)
	if err != nil {
		return err
	}
	_, err = r.queries.UpsertBotSetupStep(ctx, dbsqlc.UpsertBotSetupStepParams{
		BotID: id, Step: step.Step, Status: step.Status, Generation: step.Generation,
		Attempts: step.Attempts, LastError: step.LastError,
	})
	return err
}

func (r *postgresRepository) withSteps(ctx context.Context, s Setup) (Setup, error) {
	id, err := db.ParseUUID(s.BotID)
	if err != nil {
		return Setup{}, err
	}
	rows, err := r.queries.ListBotSetupSteps(ctx, id)
	if err != nil {
		return Setup{}, err
	}
	byName := make(map[string]Step, len(rows))
	for _, row := range rows {
		st := Step{Step: row.Step, Status: row.Status, Generation: row.Generation, Attempts: row.Attempts, LastError: row.LastError}
		if row.UpdatedAt.Valid {
			st.UpdatedAt = row.UpdatedAt.Time
		}
		byName[row.Step] = st
	}
	s.Steps = s.Steps[:0]
	for _, name := range StepOrder {
		if st, ok := byName[name]; ok {
			s.Steps = append(s.Steps, st)
		}
	}
	return s, nil
}

func fromRow(row dbsqlc.BotSetup) Setup {
	s := Setup{
		BotID:              uuidString(row.BotID),
		TeamID:             uuidString(row.TeamID),
		DesiredGeneration:  row.DesiredGeneration,
		RequestedBy:        uuidString(row.RequestedByUserID),
		State:              row.State,
		ObservedGeneration: row.ObservedGeneration,
		Attempts:           row.Attempts,
		LeaseOwner:         row.LeaseOwner,
		Version:            row.Version,
	}
	if len(row.Spec) > 0 {
		_ = json.Unmarshal(row.Spec, &s.Spec)
	}
	if row.NextAttemptAt.Valid {
		s.NextAttemptAt = row.NextAttemptAt.Time
	}
	if row.LeaseUntil.Valid {
		s.LeaseUntil = row.LeaseUntil.Time
	}
	if row.UpdatedAt.Valid {
		s.UpdatedAt = row.UpdatedAt.Time
	}
	return s
}

func uuidString(id pgtype.UUID) string {
	if !id.Valid {
		return ""
	}
	return uuid.UUID(id.Bytes).String()
}
