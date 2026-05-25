package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	pgsqlc "github.com/memohai/memoh/internal/db/postgres/sqlc"
	sqlitesqlc "github.com/memohai/memoh/internal/db/sqlite/sqlc"
	"github.com/memohai/memoh/internal/orchestration"
)

type OrchestrationStore struct {
	db      *sql.DB
	queries *sqlitesqlc.Queries
}

func NewOrchestrationStore(store *Store) orchestration.Store {
	if store == nil {
		return nil
	}
	return &OrchestrationStore{db: store.db, queries: store.queries}
}

func (s *OrchestrationStore) BeginTx(ctx context.Context) (orchestration.Tx, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &orchestrationTx{Tx: tx, queries: sqlitesqlc.New(tx)}, nil
}

func (s *OrchestrationStore) Queries() orchestration.Queries {
	return orchestrationQueries{queries: s.queries}
}

type orchestrationTx struct {
	*sql.Tx
	queries *sqlitesqlc.Queries
}

func (tx *orchestrationTx) Commit(_ context.Context) error   { return tx.Tx.Commit() }
func (tx *orchestrationTx) Rollback(_ context.Context) error { return tx.Tx.Rollback() }
func (tx *orchestrationTx) DatabaseNow(ctx context.Context) (time.Time, error) {
	return orchestrationQueries{queries: tx.queries}.DatabaseNow(ctx)
}

type orchestrationQueries struct {
	queries *sqlitesqlc.Queries
}

func (q orchestrationQueries) sqliteQueries() *sqlitesqlc.Queries { return q.queries }

func (q orchestrationQueries) DatabaseNow(ctx context.Context) (time.Time, error) {
	out, err := q.queries.GetDatabaseClockTimestamp(ctx)
	if err != nil {
		return time.Time{}, fmt.Errorf("query database clock: %w", err)
	}
	raw := fmt.Sprint(out)
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("parse database clock %q", raw)
}

func (q orchestrationQueries) AdvanceOrchestrationRunPlannerEpoch(ctx context.Context, id pgtype.UUID) (pgsqlc.OrchestrationRun, error) {
	out, err := q.sqliteQueries().AdvanceOrchestrationRunPlannerEpoch(ctx, uuidString(id))
	if err != nil {
		return pgsqlc.OrchestrationRun{}, err
	}
	var result pgsqlc.OrchestrationRun
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationRun{}, err
	}
	return result, nil
}

func (q orchestrationQueries) AllocateOrchestrationRunEventSeqs(ctx context.Context, arg pgsqlc.AllocateOrchestrationRunEventSeqsParams) (int64, error) {
	var sqliteArg sqlitesqlc.AllocateOrchestrationRunEventSeqsParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return 0, err
	}
	out, err := q.sqliteQueries().AllocateOrchestrationRunEventSeqs(ctx, sqliteArg)
	if err != nil {
		return 0, err
	}
	return out, nil
}

func (q orchestrationQueries) AttachOrchestrationTaskAttemptSession(ctx context.Context, arg pgsqlc.AttachOrchestrationTaskAttemptSessionParams) (pgsqlc.OrchestrationTaskAttempt, error) {
	var sqliteArg sqlitesqlc.AttachOrchestrationTaskAttemptSessionParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationTaskAttempt{}, err
	}
	out, err := q.sqliteQueries().AttachOrchestrationTaskAttemptSession(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationTaskAttempt{}, err
	}
	var result pgsqlc.OrchestrationTaskAttempt
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationTaskAttempt{}, err
	}
	return result, nil
}

func (q orchestrationQueries) AttachOrchestrationTaskVerificationSession(ctx context.Context, arg pgsqlc.AttachOrchestrationTaskVerificationSessionParams) (pgsqlc.OrchestrationTaskVerification, error) {
	var sqliteArg sqlitesqlc.AttachOrchestrationTaskVerificationSessionParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationTaskVerification{}, err
	}
	out, err := q.sqliteQueries().AttachOrchestrationTaskVerificationSession(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationTaskVerification{}, err
	}
	var result pgsqlc.OrchestrationTaskVerification
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationTaskVerification{}, err
	}
	return result, nil
}

func (q orchestrationQueries) CancelOrchestrationTaskVerification(ctx context.Context, arg pgsqlc.CancelOrchestrationTaskVerificationParams) (pgsqlc.OrchestrationTaskVerification, error) {
	var sqliteArg sqlitesqlc.CancelOrchestrationTaskVerificationParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationTaskVerification{}, err
	}
	out, err := q.sqliteQueries().CancelOrchestrationTaskVerification(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationTaskVerification{}, err
	}
	var result pgsqlc.OrchestrationTaskVerification
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationTaskVerification{}, err
	}
	return result, nil
}

func (q orchestrationQueries) ClaimNextCreatedOrchestrationTaskAttempt(ctx context.Context, arg pgsqlc.ClaimNextCreatedOrchestrationTaskAttemptParams) (pgsqlc.OrchestrationTaskAttempt, error) {
	var sqliteArg sqlitesqlc.ClaimNextCreatedOrchestrationTaskAttemptParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationTaskAttempt{}, err
	}
	sqliteArg.WorkerProfiles = stringsJSON(arg.WorkerProfiles)
	out, err := q.sqliteQueries().ClaimNextCreatedOrchestrationTaskAttempt(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationTaskAttempt{}, err
	}
	var result pgsqlc.OrchestrationTaskAttempt
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationTaskAttempt{}, err
	}
	return result, nil
}

func (q orchestrationQueries) ClaimNextCreatedOrchestrationTaskVerification(ctx context.Context, arg pgsqlc.ClaimNextCreatedOrchestrationTaskVerificationParams) (pgsqlc.OrchestrationTaskVerification, error) {
	var sqliteArg sqlitesqlc.ClaimNextCreatedOrchestrationTaskVerificationParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationTaskVerification{}, err
	}
	sqliteArg.VerifierProfiles = stringsJSON(arg.VerifierProfiles)
	out, err := q.sqliteQueries().ClaimNextCreatedOrchestrationTaskVerification(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationTaskVerification{}, err
	}
	var result pgsqlc.OrchestrationTaskVerification
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationTaskVerification{}, err
	}
	return result, nil
}

func (q orchestrationQueries) ClaimNextOrchestrationPlanningIntent(ctx context.Context, arg pgsqlc.ClaimNextOrchestrationPlanningIntentParams) (pgsqlc.OrchestrationPlanningIntent, error) {
	var sqliteArg sqlitesqlc.ClaimNextOrchestrationPlanningIntentParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationPlanningIntent{}, err
	}
	out, err := q.sqliteQueries().ClaimNextOrchestrationPlanningIntent(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationPlanningIntent{}, err
	}
	var result pgsqlc.OrchestrationPlanningIntent
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationPlanningIntent{}, err
	}
	return result, nil
}

func (q orchestrationQueries) CompleteOrchestrationIdempotencyRecord(ctx context.Context, arg pgsqlc.CompleteOrchestrationIdempotencyRecordParams) (pgsqlc.OrchestrationIdempotencyRecord, error) {
	var sqliteArg sqlitesqlc.CompleteOrchestrationIdempotencyRecordParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationIdempotencyRecord{}, err
	}
	out, err := q.sqliteQueries().CompleteOrchestrationIdempotencyRecord(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationIdempotencyRecord{}, err
	}
	var result pgsqlc.OrchestrationIdempotencyRecord
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationIdempotencyRecord{}, err
	}
	return result, nil
}

func (q orchestrationQueries) CompleteOrchestrationPlanningIntent(ctx context.Context, arg pgsqlc.CompleteOrchestrationPlanningIntentParams) (pgsqlc.OrchestrationPlanningIntent, error) {
	var sqliteArg sqlitesqlc.CompleteOrchestrationPlanningIntentParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationPlanningIntent{}, err
	}
	out, err := q.sqliteQueries().CompleteOrchestrationPlanningIntent(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationPlanningIntent{}, err
	}
	var result pgsqlc.OrchestrationPlanningIntent
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationPlanningIntent{}, err
	}
	return result, nil
}

func (q orchestrationQueries) CountActiveOrchestrationPlanningIntentsByRun(ctx context.Context, runID pgtype.UUID) (int64, error) {
	out, err := q.sqliteQueries().CountActiveOrchestrationPlanningIntentsByRun(ctx, uuidString(runID))
	if err != nil {
		return 0, err
	}
	return out, nil
}

func (q orchestrationQueries) CountFailedOrchestrationPlanningIntentsByRun(ctx context.Context, runID pgtype.UUID) (int64, error) {
	out, err := q.sqliteQueries().CountFailedOrchestrationPlanningIntentsByRun(ctx, uuidString(runID))
	if err != nil {
		return 0, err
	}
	return out, nil
}

func (q orchestrationQueries) CountActiveOrchestrationTaskAttemptsByRun(ctx context.Context, runID pgtype.UUID) (int64, error) {
	out, err := q.sqliteQueries().CountActiveOrchestrationTaskAttemptsByRun(ctx, uuidString(runID))
	if err != nil {
		return 0, err
	}
	return out, nil
}

func (q orchestrationQueries) CountActiveOrchestrationTaskAttemptsByTask(ctx context.Context, taskID pgtype.UUID) (int64, error) {
	out, err := q.sqliteQueries().CountActiveOrchestrationTaskAttemptsByTask(ctx, uuidString(taskID))
	if err != nil {
		return 0, err
	}
	return out, nil
}

func (q orchestrationQueries) CountCompletedFinalOrchestrationTasksByRun(ctx context.Context, runID pgtype.UUID) (int64, error) {
	out, err := q.sqliteQueries().CountCompletedFinalOrchestrationTasksByRun(ctx, uuidString(runID))
	if err != nil {
		return 0, err
	}
	return out, nil
}

func (q orchestrationQueries) CountFailedOrchestrationTasksByRun(ctx context.Context, runID pgtype.UUID) (int64, error) {
	out, err := q.sqliteQueries().CountFailedOrchestrationTasksByRun(ctx, uuidString(runID))
	if err != nil {
		return 0, err
	}
	return out, nil
}

func (q orchestrationQueries) CountNonTerminalOrchestrationTasksByRun(ctx context.Context, runID pgtype.UUID) (int64, error) {
	out, err := q.sqliteQueries().CountNonTerminalOrchestrationTasksByRun(ctx, uuidString(runID))
	if err != nil {
		return 0, err
	}
	return out, nil
}

func (q orchestrationQueries) CountOpenRunBlockingCheckpointsByRun(ctx context.Context, runID pgtype.UUID) (int64, error) {
	out, err := q.sqliteQueries().CountOpenRunBlockingCheckpointsByRun(ctx, uuidString(runID))
	if err != nil {
		return 0, err
	}
	return out, nil
}

func (q orchestrationQueries) CreateOrchestrationArtifact(ctx context.Context, arg pgsqlc.CreateOrchestrationArtifactParams) (pgsqlc.OrchestrationArtifact, error) {
	var sqliteArg sqlitesqlc.CreateOrchestrationArtifactParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationArtifact{}, err
	}
	out, err := q.sqliteQueries().CreateOrchestrationArtifact(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationArtifact{}, err
	}
	var result pgsqlc.OrchestrationArtifact
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationArtifact{}, err
	}
	return result, nil
}

func (q orchestrationQueries) CreateOrchestrationEvent(ctx context.Context, arg pgsqlc.CreateOrchestrationEventParams) (pgsqlc.OrchestrationEvent, error) {
	var sqliteArg sqlitesqlc.CreateOrchestrationEventParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationEvent{}, err
	}
	out, err := q.sqliteQueries().CreateOrchestrationEvent(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationEvent{}, err
	}
	var result pgsqlc.OrchestrationEvent
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationEvent{}, err
	}
	return result, nil
}

func (q orchestrationQueries) CreateOrchestrationHumanCheckpoint(ctx context.Context, arg pgsqlc.CreateOrchestrationHumanCheckpointParams) (pgsqlc.OrchestrationHumanCheckpoint, error) {
	var sqliteArg sqlitesqlc.CreateOrchestrationHumanCheckpointParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationHumanCheckpoint{}, err
	}
	out, err := q.sqliteQueries().CreateOrchestrationHumanCheckpoint(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationHumanCheckpoint{}, err
	}
	var result pgsqlc.OrchestrationHumanCheckpoint
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationHumanCheckpoint{}, err
	}
	return result, nil
}

func (q orchestrationQueries) CreateOrchestrationInputManifest(ctx context.Context, arg pgsqlc.CreateOrchestrationInputManifestParams) (pgsqlc.OrchestrationInputManifest, error) {
	var sqliteArg sqlitesqlc.CreateOrchestrationInputManifestParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationInputManifest{}, err
	}
	out, err := q.sqliteQueries().CreateOrchestrationInputManifest(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationInputManifest{}, err
	}
	var result pgsqlc.OrchestrationInputManifest
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationInputManifest{}, err
	}
	return result, nil
}

func (q orchestrationQueries) CreateOrchestrationPlanningIntent(ctx context.Context, arg pgsqlc.CreateOrchestrationPlanningIntentParams) (pgsqlc.OrchestrationPlanningIntent, error) {
	var sqliteArg sqlitesqlc.CreateOrchestrationPlanningIntentParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationPlanningIntent{}, err
	}
	out, err := q.sqliteQueries().CreateOrchestrationPlanningIntent(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationPlanningIntent{}, err
	}
	var result pgsqlc.OrchestrationPlanningIntent
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationPlanningIntent{}, err
	}
	return result, nil
}

func (q orchestrationQueries) CreateOrchestrationProjectionSnapshot(ctx context.Context, arg pgsqlc.CreateOrchestrationProjectionSnapshotParams) (pgsqlc.OrchestrationProjectionSnapshot, error) {
	var sqliteArg sqlitesqlc.CreateOrchestrationProjectionSnapshotParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationProjectionSnapshot{}, err
	}
	out, err := q.sqliteQueries().CreateOrchestrationProjectionSnapshot(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationProjectionSnapshot{}, err
	}
	var result pgsqlc.OrchestrationProjectionSnapshot
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationProjectionSnapshot{}, err
	}
	return result, nil
}

func (q orchestrationQueries) CreateOrchestrationRun(ctx context.Context, arg pgsqlc.CreateOrchestrationRunParams) (pgsqlc.OrchestrationRun, error) {
	var sqliteArg sqlitesqlc.CreateOrchestrationRunParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationRun{}, err
	}
	out, err := q.sqliteQueries().CreateOrchestrationRun(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationRun{}, err
	}
	var result pgsqlc.OrchestrationRun
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationRun{}, err
	}
	return result, nil
}

func (q orchestrationQueries) CreateOrchestrationTask(ctx context.Context, arg pgsqlc.CreateOrchestrationTaskParams) (pgsqlc.OrchestrationTask, error) {
	var sqliteArg sqlitesqlc.CreateOrchestrationTaskParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationTask{}, err
	}
	out, err := q.sqliteQueries().CreateOrchestrationTask(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationTask{}, err
	}
	var result pgsqlc.OrchestrationTask
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationTask{}, err
	}
	return result, nil
}

func (q orchestrationQueries) CreateOrchestrationTaskAttempt(ctx context.Context, arg pgsqlc.CreateOrchestrationTaskAttemptParams) (pgsqlc.OrchestrationTaskAttempt, error) {
	var sqliteArg sqlitesqlc.CreateOrchestrationTaskAttemptParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationTaskAttempt{}, err
	}
	out, err := q.sqliteQueries().CreateOrchestrationTaskAttempt(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationTaskAttempt{}, err
	}
	var result pgsqlc.OrchestrationTaskAttempt
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationTaskAttempt{}, err
	}
	return result, nil
}

func (q orchestrationQueries) CreateOrchestrationTaskDependency(ctx context.Context, arg pgsqlc.CreateOrchestrationTaskDependencyParams) (pgsqlc.OrchestrationTaskDependency, error) {
	var sqliteArg sqlitesqlc.CreateOrchestrationTaskDependencyParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationTaskDependency{}, err
	}
	out, err := q.sqliteQueries().CreateOrchestrationTaskDependency(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationTaskDependency{}, err
	}
	var result pgsqlc.OrchestrationTaskDependency
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationTaskDependency{}, err
	}
	return result, nil
}

func (q orchestrationQueries) CreateOrchestrationTaskResult(ctx context.Context, arg pgsqlc.CreateOrchestrationTaskResultParams) (pgsqlc.OrchestrationTaskResult, error) {
	var sqliteArg sqlitesqlc.CreateOrchestrationTaskResultParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationTaskResult{}, err
	}
	out, err := q.sqliteQueries().CreateOrchestrationTaskResult(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationTaskResult{}, err
	}
	var result pgsqlc.OrchestrationTaskResult
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationTaskResult{}, err
	}
	return result, nil
}

func (q orchestrationQueries) CreateOrchestrationTaskVerification(ctx context.Context, arg pgsqlc.CreateOrchestrationTaskVerificationParams) (pgsqlc.OrchestrationTaskVerification, error) {
	var sqliteArg sqlitesqlc.CreateOrchestrationTaskVerificationParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationTaskVerification{}, err
	}
	out, err := q.sqliteQueries().CreateOrchestrationTaskVerification(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationTaskVerification{}, err
	}
	var result pgsqlc.OrchestrationTaskVerification
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationTaskVerification{}, err
	}
	return result, nil
}

func (q orchestrationQueries) FailOrchestrationPlanningIntent(ctx context.Context, arg pgsqlc.FailOrchestrationPlanningIntentParams) (pgsqlc.OrchestrationPlanningIntent, error) {
	var sqliteArg sqlitesqlc.FailOrchestrationPlanningIntentParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationPlanningIntent{}, err
	}
	out, err := q.sqliteQueries().FailOrchestrationPlanningIntent(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationPlanningIntent{}, err
	}
	var result pgsqlc.OrchestrationPlanningIntent
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationPlanningIntent{}, err
	}
	return result, nil
}

func (q orchestrationQueries) GetNextOrchestrationTaskAttemptNo(ctx context.Context, taskID pgtype.UUID) (int32, error) {
	out, err := q.sqliteQueries().GetNextOrchestrationTaskAttemptNo(ctx, uuidString(taskID))
	if err != nil {
		return 0, err
	}
	if out > math.MaxInt32 {
		return 0, fmt.Errorf("next orchestration task attempt number %d overflows int32", out)
	}
	value := int32(out) // #nosec G115 -- guarded by MaxInt32 check above.
	return value, nil
}

func (q orchestrationQueries) GetOrchestrationHumanCheckpointByIDForUpdate(ctx context.Context, id pgtype.UUID) (pgsqlc.OrchestrationHumanCheckpoint, error) {
	out, err := q.sqliteQueries().GetOrchestrationHumanCheckpointByIDForUpdate(ctx, uuidString(id))
	if err != nil {
		return pgsqlc.OrchestrationHumanCheckpoint{}, err
	}
	var result pgsqlc.OrchestrationHumanCheckpoint
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationHumanCheckpoint{}, err
	}
	return result, nil
}

func (q orchestrationQueries) GetOrchestrationIdempotencyRecordForUpdate(ctx context.Context, arg pgsqlc.GetOrchestrationIdempotencyRecordForUpdateParams) (pgsqlc.OrchestrationIdempotencyRecord, error) {
	var sqliteArg sqlitesqlc.GetOrchestrationIdempotencyRecordForUpdateParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationIdempotencyRecord{}, err
	}
	out, err := q.sqliteQueries().GetOrchestrationIdempotencyRecordForUpdate(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationIdempotencyRecord{}, err
	}
	var result pgsqlc.OrchestrationIdempotencyRecord
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationIdempotencyRecord{}, err
	}
	return result, nil
}

func (q orchestrationQueries) GetOrchestrationInputManifestByID(ctx context.Context, id pgtype.UUID) (pgsqlc.OrchestrationInputManifest, error) {
	out, err := q.sqliteQueries().GetOrchestrationInputManifestByID(ctx, uuidString(id))
	if err != nil {
		return pgsqlc.OrchestrationInputManifest{}, err
	}
	var result pgsqlc.OrchestrationInputManifest
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationInputManifest{}, err
	}
	return result, nil
}

func (q orchestrationQueries) GetOrchestrationProjectionSnapshotAtOrBeforeSeq(ctx context.Context, arg pgsqlc.GetOrchestrationProjectionSnapshotAtOrBeforeSeqParams) (pgsqlc.OrchestrationProjectionSnapshot, error) {
	var sqliteArg sqlitesqlc.GetOrchestrationProjectionSnapshotAtOrBeforeSeqParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationProjectionSnapshot{}, err
	}
	out, err := q.sqliteQueries().GetOrchestrationProjectionSnapshotAtOrBeforeSeq(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationProjectionSnapshot{}, err
	}
	var result pgsqlc.OrchestrationProjectionSnapshot
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationProjectionSnapshot{}, err
	}
	return result, nil
}

func (q orchestrationQueries) GetOrchestrationRunByID(ctx context.Context, id pgtype.UUID) (pgsqlc.OrchestrationRun, error) {
	out, err := q.sqliteQueries().GetOrchestrationRunByID(ctx, uuidString(id))
	if err != nil {
		return pgsqlc.OrchestrationRun{}, err
	}
	var result pgsqlc.OrchestrationRun
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationRun{}, err
	}
	return result, nil
}

func (q orchestrationQueries) GetOrchestrationRunByIDForUpdate(ctx context.Context, id pgtype.UUID) (pgsqlc.OrchestrationRun, error) {
	out, err := q.sqliteQueries().GetOrchestrationRunByIDForUpdate(ctx, uuidString(id))
	if err != nil {
		return pgsqlc.OrchestrationRun{}, err
	}
	var result pgsqlc.OrchestrationRun
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationRun{}, err
	}
	return result, nil
}

func (q orchestrationQueries) GetOrchestrationTaskAttemptByID(ctx context.Context, id pgtype.UUID) (pgsqlc.OrchestrationTaskAttempt, error) {
	out, err := q.sqliteQueries().GetOrchestrationTaskAttemptByID(ctx, uuidString(id))
	if err != nil {
		return pgsqlc.OrchestrationTaskAttempt{}, err
	}
	var result pgsqlc.OrchestrationTaskAttempt
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationTaskAttempt{}, err
	}
	return result, nil
}

func (q orchestrationQueries) GetOrchestrationTaskAttemptByIDForUpdate(ctx context.Context, id pgtype.UUID) (pgsqlc.OrchestrationTaskAttempt, error) {
	out, err := q.sqliteQueries().GetOrchestrationTaskAttemptByIDForUpdate(ctx, uuidString(id))
	if err != nil {
		return pgsqlc.OrchestrationTaskAttempt{}, err
	}
	var result pgsqlc.OrchestrationTaskAttempt
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationTaskAttempt{}, err
	}
	return result, nil
}

func (q orchestrationQueries) GetOrchestrationTaskByID(ctx context.Context, id pgtype.UUID) (pgsqlc.OrchestrationTask, error) {
	out, err := q.sqliteQueries().GetOrchestrationTaskByID(ctx, uuidString(id))
	if err != nil {
		return pgsqlc.OrchestrationTask{}, err
	}
	var result pgsqlc.OrchestrationTask
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationTask{}, err
	}
	return result, nil
}

func (q orchestrationQueries) GetOrchestrationTaskByIDForUpdate(ctx context.Context, id pgtype.UUID) (pgsqlc.OrchestrationTask, error) {
	out, err := q.sqliteQueries().GetOrchestrationTaskByIDForUpdate(ctx, uuidString(id))
	if err != nil {
		return pgsqlc.OrchestrationTask{}, err
	}
	var result pgsqlc.OrchestrationTask
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationTask{}, err
	}
	return result, nil
}

func (q orchestrationQueries) GetOrchestrationTaskResultByID(ctx context.Context, id pgtype.UUID) (pgsqlc.OrchestrationTaskResult, error) {
	out, err := q.sqliteQueries().GetOrchestrationTaskResultByID(ctx, uuidString(id))
	if err != nil {
		return pgsqlc.OrchestrationTaskResult{}, err
	}
	var result pgsqlc.OrchestrationTaskResult
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationTaskResult{}, err
	}
	return result, nil
}

func (q orchestrationQueries) GetOrchestrationTaskVerificationByIDForUpdate(ctx context.Context, id pgtype.UUID) (pgsqlc.OrchestrationTaskVerification, error) {
	out, err := q.sqliteQueries().GetOrchestrationTaskVerificationByIDForUpdate(ctx, uuidString(id))
	if err != nil {
		return pgsqlc.OrchestrationTaskVerification{}, err
	}
	var result pgsqlc.OrchestrationTaskVerification
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationTaskVerification{}, err
	}
	return result, nil
}

func (q orchestrationQueries) GetOrchestrationWorkerByIDForUpdate(ctx context.Context, id string) (pgsqlc.OrchestrationWorker, error) {
	out, err := q.sqliteQueries().GetOrchestrationWorkerByIDForUpdate(ctx, id)
	if err != nil {
		return pgsqlc.OrchestrationWorker{}, err
	}
	var result pgsqlc.OrchestrationWorker
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationWorker{}, err
	}
	return result, nil
}

func (q orchestrationQueries) HeartbeatOrchestrationTaskAttempt(ctx context.Context, arg pgsqlc.HeartbeatOrchestrationTaskAttemptParams) (pgsqlc.OrchestrationTaskAttempt, error) {
	var sqliteArg sqlitesqlc.HeartbeatOrchestrationTaskAttemptParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationTaskAttempt{}, err
	}
	out, err := q.sqliteQueries().HeartbeatOrchestrationTaskAttempt(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationTaskAttempt{}, err
	}
	var result pgsqlc.OrchestrationTaskAttempt
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationTaskAttempt{}, err
	}
	return result, nil
}

func (q orchestrationQueries) HeartbeatOrchestrationTaskVerification(ctx context.Context, arg pgsqlc.HeartbeatOrchestrationTaskVerificationParams) (pgsqlc.OrchestrationTaskVerification, error) {
	var sqliteArg sqlitesqlc.HeartbeatOrchestrationTaskVerificationParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationTaskVerification{}, err
	}
	out, err := q.sqliteQueries().HeartbeatOrchestrationTaskVerification(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationTaskVerification{}, err
	}
	var result pgsqlc.OrchestrationTaskVerification
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationTaskVerification{}, err
	}
	return result, nil
}

func (q orchestrationQueries) HeartbeatOrchestrationWorker(ctx context.Context, arg pgsqlc.HeartbeatOrchestrationWorkerParams) (pgsqlc.OrchestrationWorker, error) {
	var sqliteArg sqlitesqlc.HeartbeatOrchestrationWorkerParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationWorker{}, err
	}
	out, err := q.sqliteQueries().HeartbeatOrchestrationWorker(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationWorker{}, err
	}
	var result pgsqlc.OrchestrationWorker
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationWorker{}, err
	}
	return result, nil
}

func (q orchestrationQueries) ListActiveOrchestrationTaskDependenciesByPredecessor(ctx context.Context, predecessorTaskID pgtype.UUID) ([]pgsqlc.OrchestrationTaskDependency, error) {
	out, err := q.sqliteQueries().ListActiveOrchestrationTaskDependenciesByPredecessor(ctx, uuidString(predecessorTaskID))
	if err != nil {
		return nil, err
	}
	var result []pgsqlc.OrchestrationTaskDependency
	if err := convertValue(out, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (q orchestrationQueries) ListActiveOrchestrationTaskDependenciesBySuccessor(ctx context.Context, successorTaskID pgtype.UUID) ([]pgsqlc.OrchestrationTaskDependency, error) {
	out, err := q.sqliteQueries().ListActiveOrchestrationTaskDependenciesBySuccessor(ctx, uuidString(successorTaskID))
	if err != nil {
		return nil, err
	}
	var result []pgsqlc.OrchestrationTaskDependency
	if err := convertValue(out, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (q orchestrationQueries) ListCurrentOrchestrationArtifactsByRun(ctx context.Context, runID pgtype.UUID) ([]pgsqlc.OrchestrationArtifact, error) {
	out, err := q.sqliteQueries().ListCurrentOrchestrationArtifactsByRun(ctx, uuidString(runID))
	if err != nil {
		return nil, err
	}
	var result []pgsqlc.OrchestrationArtifact
	if err := convertValue(out, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (q orchestrationQueries) ListCurrentOrchestrationCheckpointsByRun(ctx context.Context, runID pgtype.UUID) ([]pgsqlc.OrchestrationHumanCheckpoint, error) {
	out, err := q.sqliteQueries().ListCurrentOrchestrationCheckpointsByRun(ctx, uuidString(runID))
	if err != nil {
		return nil, err
	}
	var result []pgsqlc.OrchestrationHumanCheckpoint
	if err := convertValue(out, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (q orchestrationQueries) ListCurrentOrchestrationTaskAttemptsByRun(ctx context.Context, runID pgtype.UUID) ([]pgsqlc.OrchestrationTaskAttempt, error) {
	out, err := q.sqliteQueries().ListCurrentOrchestrationTaskAttemptsByRun(ctx, uuidString(runID))
	if err != nil {
		return nil, err
	}
	var result []pgsqlc.OrchestrationTaskAttempt
	if err := convertValue(out, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (q orchestrationQueries) ListCurrentOrchestrationTaskDependenciesByRun(ctx context.Context, runID pgtype.UUID) ([]pgsqlc.OrchestrationTaskDependency, error) {
	out, err := q.sqliteQueries().ListCurrentOrchestrationTaskDependenciesByRun(ctx, uuidString(runID))
	if err != nil {
		return nil, err
	}
	var result []pgsqlc.OrchestrationTaskDependency
	if err := convertValue(out, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (q orchestrationQueries) ListCurrentOrchestrationTaskResultsByRun(ctx context.Context, runID pgtype.UUID) ([]pgsqlc.OrchestrationTaskResult, error) {
	out, err := q.sqliteQueries().ListCurrentOrchestrationTaskResultsByRun(ctx, uuidString(runID))
	if err != nil {
		return nil, err
	}
	var result []pgsqlc.OrchestrationTaskResult
	if err := convertValue(out, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (q orchestrationQueries) ListCurrentOrchestrationTaskVerificationsByRun(ctx context.Context, runID pgtype.UUID) ([]pgsqlc.OrchestrationTaskVerification, error) {
	out, err := q.sqliteQueries().ListCurrentOrchestrationTaskVerificationsByRun(ctx, uuidString(runID))
	if err != nil {
		return nil, err
	}
	var result []pgsqlc.OrchestrationTaskVerification
	if err := convertValue(out, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (q orchestrationQueries) ListCurrentOrchestrationTasksByRun(ctx context.Context, runID pgtype.UUID) ([]pgsqlc.OrchestrationTask, error) {
	out, err := q.sqliteQueries().ListCurrentOrchestrationTasksByRun(ctx, uuidString(runID))
	if err != nil {
		return nil, err
	}
	var result []pgsqlc.OrchestrationTask
	if err := convertValue(out, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (q orchestrationQueries) ListDependencyBlockedOrchestrationTasks(ctx context.Context) ([]pgsqlc.OrchestrationTask, error) {
	out, err := q.sqliteQueries().ListDependencyBlockedOrchestrationTasks(ctx)
	if err != nil {
		return nil, err
	}
	var result []pgsqlc.OrchestrationTask
	if err := convertValue(out, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (q orchestrationQueries) ListExpiredOrchestrationTaskAttempts(ctx context.Context) ([]pgsqlc.OrchestrationTaskAttempt, error) {
	out, err := q.sqliteQueries().ListExpiredOrchestrationTaskAttempts(ctx)
	if err != nil {
		return nil, err
	}
	var result []pgsqlc.OrchestrationTaskAttempt
	if err := convertValue(out, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (q orchestrationQueries) ListExpiredOrchestrationTaskVerifications(ctx context.Context) ([]pgsqlc.OrchestrationTaskVerification, error) {
	out, err := q.sqliteQueries().ListExpiredOrchestrationTaskVerifications(ctx)
	if err != nil {
		return nil, err
	}
	var result []pgsqlc.OrchestrationTaskVerification
	if err := convertValue(out, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (q orchestrationQueries) ListOrchestrationArtifactsByTask(ctx context.Context, taskID pgtype.UUID) ([]pgsqlc.OrchestrationArtifact, error) {
	out, err := q.sqliteQueries().ListOrchestrationArtifactsByTask(ctx, uuidString(taskID))
	if err != nil {
		return nil, err
	}
	var result []pgsqlc.OrchestrationArtifact
	if err := convertValue(out, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (q orchestrationQueries) ListOrchestrationRunEvents(ctx context.Context, arg pgsqlc.ListOrchestrationRunEventsParams) ([]pgsqlc.OrchestrationEvent, error) {
	var sqliteArg sqlitesqlc.ListOrchestrationRunEventsParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return nil, err
	}
	out, err := q.sqliteQueries().ListOrchestrationRunEvents(ctx, sqliteArg)
	if err != nil {
		return nil, err
	}
	var result []pgsqlc.OrchestrationEvent
	if err := convertValue(out, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (q orchestrationQueries) ListOrchestrationRunsByBot(ctx context.Context, arg pgsqlc.ListOrchestrationRunsByBotParams) ([]pgsqlc.OrchestrationRun, error) {
	var sqliteArg sqlitesqlc.ListOrchestrationRunsByBotParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return nil, err
	}
	out, err := q.sqliteQueries().ListOrchestrationRunsByBot(ctx, sqliteArg)
	if err != nil {
		return nil, err
	}
	var result []pgsqlc.OrchestrationRun
	if err := convertValue(out, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (q orchestrationQueries) ListTerminalizableRunningOrchestrationRuns(ctx context.Context, limitCount int32) ([]pgsqlc.OrchestrationRun, error) {
	out, err := q.sqliteQueries().ListTerminalizableRunningOrchestrationRuns(ctx, int64(limitCount))
	if err != nil {
		return nil, err
	}
	var result []pgsqlc.OrchestrationRun
	if err := convertValue(out, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (q orchestrationQueries) ListOrchestrationWorkersByIDs(ctx context.Context, ids []string) ([]pgsqlc.OrchestrationWorker, error) {
	out, err := q.sqliteQueries().ListOrchestrationWorkersByIDs(ctx, stringsJSON(ids))
	if err != nil {
		return nil, err
	}
	var result []pgsqlc.OrchestrationWorker
	if err := convertValue(out, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (q orchestrationQueries) ListSchedulableOrchestrationTasks(ctx context.Context) ([]pgsqlc.OrchestrationTask, error) {
	out, err := q.sqliteQueries().ListSchedulableOrchestrationTasks(ctx)
	if err != nil {
		return nil, err
	}
	var result []pgsqlc.OrchestrationTask
	if err := convertValue(out, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (q orchestrationQueries) MarkOrchestrationHumanCheckpointCancelled(ctx context.Context, id pgtype.UUID) (pgsqlc.OrchestrationHumanCheckpoint, error) {
	out, err := q.sqliteQueries().MarkOrchestrationHumanCheckpointCancelled(ctx, uuidString(id))
	if err != nil {
		return pgsqlc.OrchestrationHumanCheckpoint{}, err
	}
	var result pgsqlc.OrchestrationHumanCheckpoint
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationHumanCheckpoint{}, err
	}
	return result, nil
}

func (q orchestrationQueries) MarkOrchestrationHumanCheckpointSuperseded(ctx context.Context, arg pgsqlc.MarkOrchestrationHumanCheckpointSupersededParams) (pgsqlc.OrchestrationHumanCheckpoint, error) {
	var sqliteArg sqlitesqlc.MarkOrchestrationHumanCheckpointSupersededParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationHumanCheckpoint{}, err
	}
	out, err := q.sqliteQueries().MarkOrchestrationHumanCheckpointSuperseded(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationHumanCheckpoint{}, err
	}
	var result pgsqlc.OrchestrationHumanCheckpoint
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationHumanCheckpoint{}, err
	}
	return result, nil
}

func (q orchestrationQueries) MarkOrchestrationRunCancelled(ctx context.Context, arg pgsqlc.MarkOrchestrationRunCancelledParams) (pgsqlc.OrchestrationRun, error) {
	var sqliteArg sqlitesqlc.MarkOrchestrationRunCancelledParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationRun{}, err
	}
	out, err := q.sqliteQueries().MarkOrchestrationRunCancelled(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationRun{}, err
	}
	var result pgsqlc.OrchestrationRun
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationRun{}, err
	}
	return result, nil
}

func (q orchestrationQueries) MarkOrchestrationRunCancelling(ctx context.Context, id pgtype.UUID) (pgsqlc.OrchestrationRun, error) {
	out, err := q.sqliteQueries().MarkOrchestrationRunCancelling(ctx, uuidString(id))
	if err != nil {
		return pgsqlc.OrchestrationRun{}, err
	}
	var result pgsqlc.OrchestrationRun
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationRun{}, err
	}
	return result, nil
}

func (q orchestrationQueries) MarkOrchestrationRunCompleted(ctx context.Context, id pgtype.UUID) (pgsqlc.OrchestrationRun, error) {
	out, err := q.sqliteQueries().MarkOrchestrationRunCompleted(ctx, uuidString(id))
	if err != nil {
		return pgsqlc.OrchestrationRun{}, err
	}
	var result pgsqlc.OrchestrationRun
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationRun{}, err
	}
	return result, nil
}

func (q orchestrationQueries) MarkOrchestrationRunFailed(ctx context.Context, arg pgsqlc.MarkOrchestrationRunFailedParams) (pgsqlc.OrchestrationRun, error) {
	var sqliteArg sqlitesqlc.MarkOrchestrationRunFailedParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationRun{}, err
	}
	out, err := q.sqliteQueries().MarkOrchestrationRunFailed(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationRun{}, err
	}
	var result pgsqlc.OrchestrationRun
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationRun{}, err
	}
	return result, nil
}

func (q orchestrationQueries) MarkOrchestrationRunPlanningActive(ctx context.Context, id pgtype.UUID) (pgsqlc.OrchestrationRun, error) {
	out, err := q.sqliteQueries().MarkOrchestrationRunPlanningActive(ctx, uuidString(id))
	if err != nil {
		return pgsqlc.OrchestrationRun{}, err
	}
	var result pgsqlc.OrchestrationRun
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationRun{}, err
	}
	return result, nil
}

func (q orchestrationQueries) MarkOrchestrationRunPlanningIdle(ctx context.Context, id pgtype.UUID) (pgsqlc.OrchestrationRun, error) {
	out, err := q.sqliteQueries().MarkOrchestrationRunPlanningIdle(ctx, uuidString(id))
	if err != nil {
		return pgsqlc.OrchestrationRun{}, err
	}
	var result pgsqlc.OrchestrationRun
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationRun{}, err
	}
	return result, nil
}

func (q orchestrationQueries) MarkOrchestrationRunRunning(ctx context.Context, id pgtype.UUID) (pgsqlc.OrchestrationRun, error) {
	out, err := q.sqliteQueries().MarkOrchestrationRunRunning(ctx, uuidString(id))
	if err != nil {
		return pgsqlc.OrchestrationRun{}, err
	}
	var result pgsqlc.OrchestrationRun
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationRun{}, err
	}
	return result, nil
}

func (q orchestrationQueries) MarkOrchestrationRunWaitingHuman(ctx context.Context, id pgtype.UUID) (pgsqlc.OrchestrationRun, error) {
	out, err := q.sqliteQueries().MarkOrchestrationRunWaitingHuman(ctx, uuidString(id))
	if err != nil {
		return pgsqlc.OrchestrationRun{}, err
	}
	var result pgsqlc.OrchestrationRun
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationRun{}, err
	}
	return result, nil
}

func (q orchestrationQueries) MarkOrchestrationTaskAttemptBinding(ctx context.Context, arg pgsqlc.MarkOrchestrationTaskAttemptBindingParams) (pgsqlc.OrchestrationTaskAttempt, error) {
	var sqliteArg sqlitesqlc.MarkOrchestrationTaskAttemptBindingParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationTaskAttempt{}, err
	}
	out, err := q.sqliteQueries().MarkOrchestrationTaskAttemptBinding(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationTaskAttempt{}, err
	}
	var result pgsqlc.OrchestrationTaskAttempt
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationTaskAttempt{}, err
	}
	return result, nil
}

func (q orchestrationQueries) MarkOrchestrationTaskAttemptCompleted(ctx context.Context, arg pgsqlc.MarkOrchestrationTaskAttemptCompletedParams) (pgsqlc.OrchestrationTaskAttempt, error) {
	var sqliteArg sqlitesqlc.MarkOrchestrationTaskAttemptCompletedParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationTaskAttempt{}, err
	}
	out, err := q.sqliteQueries().MarkOrchestrationTaskAttemptCompleted(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationTaskAttempt{}, err
	}
	var result pgsqlc.OrchestrationTaskAttempt
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationTaskAttempt{}, err
	}
	return result, nil
}

func (q orchestrationQueries) MarkOrchestrationTaskAttemptFailed(ctx context.Context, arg pgsqlc.MarkOrchestrationTaskAttemptFailedParams) (pgsqlc.OrchestrationTaskAttempt, error) {
	var sqliteArg sqlitesqlc.MarkOrchestrationTaskAttemptFailedParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationTaskAttempt{}, err
	}
	out, err := q.sqliteQueries().MarkOrchestrationTaskAttemptFailed(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationTaskAttempt{}, err
	}
	var result pgsqlc.OrchestrationTaskAttempt
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationTaskAttempt{}, err
	}
	return result, nil
}

func (q orchestrationQueries) MarkOrchestrationTaskAttemptParked(ctx context.Context, arg pgsqlc.MarkOrchestrationTaskAttemptParkedParams) (pgsqlc.OrchestrationTaskAttempt, error) {
	var sqliteArg sqlitesqlc.MarkOrchestrationTaskAttemptParkedParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationTaskAttempt{}, err
	}
	out, err := q.sqliteQueries().MarkOrchestrationTaskAttemptParked(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationTaskAttempt{}, err
	}
	var result pgsqlc.OrchestrationTaskAttempt
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationTaskAttempt{}, err
	}
	return result, nil
}

func (q orchestrationQueries) MarkOrchestrationTaskAttemptLost(ctx context.Context, arg pgsqlc.MarkOrchestrationTaskAttemptLostParams) (pgsqlc.OrchestrationTaskAttempt, error) {
	var sqliteArg sqlitesqlc.MarkOrchestrationTaskAttemptLostParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationTaskAttempt{}, err
	}
	out, err := q.sqliteQueries().MarkOrchestrationTaskAttemptLost(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationTaskAttempt{}, err
	}
	var result pgsqlc.OrchestrationTaskAttempt
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationTaskAttempt{}, err
	}
	return result, nil
}

func (q orchestrationQueries) MarkOrchestrationTaskAttemptRunning(ctx context.Context, arg pgsqlc.MarkOrchestrationTaskAttemptRunningParams) (pgsqlc.OrchestrationTaskAttempt, error) {
	var sqliteArg sqlitesqlc.MarkOrchestrationTaskAttemptRunningParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationTaskAttempt{}, err
	}
	out, err := q.sqliteQueries().MarkOrchestrationTaskAttemptRunning(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationTaskAttempt{}, err
	}
	var result pgsqlc.OrchestrationTaskAttempt
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationTaskAttempt{}, err
	}
	return result, nil
}

func (q orchestrationQueries) MarkOrchestrationTaskBlocked(ctx context.Context, arg pgsqlc.MarkOrchestrationTaskBlockedParams) (pgsqlc.OrchestrationTask, error) {
	var sqliteArg sqlitesqlc.MarkOrchestrationTaskBlockedParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationTask{}, err
	}
	out, err := q.sqliteQueries().MarkOrchestrationTaskBlocked(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationTask{}, err
	}
	var result pgsqlc.OrchestrationTask
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationTask{}, err
	}
	return result, nil
}

func (q orchestrationQueries) MarkOrchestrationTaskCancelled(ctx context.Context, arg pgsqlc.MarkOrchestrationTaskCancelledParams) (pgsqlc.OrchestrationTask, error) {
	var sqliteArg sqlitesqlc.MarkOrchestrationTaskCancelledParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationTask{}, err
	}
	out, err := q.sqliteQueries().MarkOrchestrationTaskCancelled(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationTask{}, err
	}
	var result pgsqlc.OrchestrationTask
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationTask{}, err
	}
	return result, nil
}

func (q orchestrationQueries) MarkOrchestrationTaskCompleted(ctx context.Context, arg pgsqlc.MarkOrchestrationTaskCompletedParams) (pgsqlc.OrchestrationTask, error) {
	var sqliteArg sqlitesqlc.MarkOrchestrationTaskCompletedParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationTask{}, err
	}
	out, err := q.sqliteQueries().MarkOrchestrationTaskCompleted(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationTask{}, err
	}
	var result pgsqlc.OrchestrationTask
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationTask{}, err
	}
	return result, nil
}

func (q orchestrationQueries) MarkOrchestrationTaskDependencySuperseded(ctx context.Context, arg pgsqlc.MarkOrchestrationTaskDependencySupersededParams) (pgsqlc.OrchestrationTaskDependency, error) {
	var sqliteArg sqlitesqlc.MarkOrchestrationTaskDependencySupersededParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationTaskDependency{}, err
	}
	out, err := q.sqliteQueries().MarkOrchestrationTaskDependencySuperseded(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationTaskDependency{}, err
	}
	var result pgsqlc.OrchestrationTaskDependency
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationTaskDependency{}, err
	}
	return result, nil
}

func (q orchestrationQueries) MarkOrchestrationTaskDispatching(ctx context.Context, id pgtype.UUID) (pgsqlc.OrchestrationTask, error) {
	out, err := q.sqliteQueries().MarkOrchestrationTaskDispatching(ctx, uuidString(id))
	if err != nil {
		return pgsqlc.OrchestrationTask{}, err
	}
	var result pgsqlc.OrchestrationTask
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationTask{}, err
	}
	return result, nil
}

func (q orchestrationQueries) MarkOrchestrationTaskCreatedForResume(ctx context.Context, arg pgsqlc.MarkOrchestrationTaskCreatedForResumeParams) (pgsqlc.OrchestrationTask, error) {
	var sqliteArg sqlitesqlc.MarkOrchestrationTaskCreatedForResumeParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationTask{}, err
	}
	out, err := q.sqliteQueries().MarkOrchestrationTaskCreatedForResume(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationTask{}, err
	}
	var result pgsqlc.OrchestrationTask
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationTask{}, err
	}
	return result, nil
}

func (q orchestrationQueries) MarkOrchestrationTaskFailed(ctx context.Context, arg pgsqlc.MarkOrchestrationTaskFailedParams) (pgsqlc.OrchestrationTask, error) {
	var sqliteArg sqlitesqlc.MarkOrchestrationTaskFailedParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationTask{}, err
	}
	out, err := q.sqliteQueries().MarkOrchestrationTaskFailed(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationTask{}, err
	}
	var result pgsqlc.OrchestrationTask
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationTask{}, err
	}
	return result, nil
}

func (q orchestrationQueries) MarkOrchestrationTaskReadyForRetry(ctx context.Context, arg pgsqlc.MarkOrchestrationTaskReadyForRetryParams) (pgsqlc.OrchestrationTask, error) {
	var sqliteArg sqlitesqlc.MarkOrchestrationTaskReadyForRetryParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationTask{}, err
	}
	out, err := q.sqliteQueries().MarkOrchestrationTaskReadyForRetry(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationTask{}, err
	}
	var result pgsqlc.OrchestrationTask
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationTask{}, err
	}
	return result, nil
}

func (q orchestrationQueries) MarkOrchestrationTaskReadyFromCheckpoint(ctx context.Context, id pgtype.UUID) (pgsqlc.OrchestrationTask, error) {
	out, err := q.sqliteQueries().MarkOrchestrationTaskReadyFromCheckpoint(ctx, uuidString(id))
	if err != nil {
		return pgsqlc.OrchestrationTask{}, err
	}
	var result pgsqlc.OrchestrationTask
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationTask{}, err
	}
	return result, nil
}

func (q orchestrationQueries) MarkOrchestrationTaskRunning(ctx context.Context, id pgtype.UUID) (pgsqlc.OrchestrationTask, error) {
	out, err := q.sqliteQueries().MarkOrchestrationTaskRunning(ctx, uuidString(id))
	if err != nil {
		return pgsqlc.OrchestrationTask{}, err
	}
	var result pgsqlc.OrchestrationTask
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationTask{}, err
	}
	return result, nil
}

func (q orchestrationQueries) MarkOrchestrationTaskSuperseded(ctx context.Context, arg pgsqlc.MarkOrchestrationTaskSupersededParams) (pgsqlc.OrchestrationTask, error) {
	var sqliteArg sqlitesqlc.MarkOrchestrationTaskSupersededParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationTask{}, err
	}
	out, err := q.sqliteQueries().MarkOrchestrationTaskSuperseded(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationTask{}, err
	}
	var result pgsqlc.OrchestrationTask
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationTask{}, err
	}
	return result, nil
}

func (q orchestrationQueries) MarkOrchestrationTaskVerificationCompleted(ctx context.Context, arg pgsqlc.MarkOrchestrationTaskVerificationCompletedParams) (pgsqlc.OrchestrationTaskVerification, error) {
	var sqliteArg sqlitesqlc.MarkOrchestrationTaskVerificationCompletedParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationTaskVerification{}, err
	}
	out, err := q.sqliteQueries().MarkOrchestrationTaskVerificationCompleted(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationTaskVerification{}, err
	}
	var result pgsqlc.OrchestrationTaskVerification
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationTaskVerification{}, err
	}
	return result, nil
}

func (q orchestrationQueries) MarkOrchestrationTaskVerificationFailed(ctx context.Context, arg pgsqlc.MarkOrchestrationTaskVerificationFailedParams) (pgsqlc.OrchestrationTaskVerification, error) {
	var sqliteArg sqlitesqlc.MarkOrchestrationTaskVerificationFailedParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationTaskVerification{}, err
	}
	out, err := q.sqliteQueries().MarkOrchestrationTaskVerificationFailed(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationTaskVerification{}, err
	}
	var result pgsqlc.OrchestrationTaskVerification
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationTaskVerification{}, err
	}
	return result, nil
}

func (q orchestrationQueries) MarkOrchestrationTaskVerificationLost(ctx context.Context, arg pgsqlc.MarkOrchestrationTaskVerificationLostParams) (pgsqlc.OrchestrationTaskVerification, error) {
	var sqliteArg sqlitesqlc.MarkOrchestrationTaskVerificationLostParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationTaskVerification{}, err
	}
	out, err := q.sqliteQueries().MarkOrchestrationTaskVerificationLost(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationTaskVerification{}, err
	}
	var result pgsqlc.OrchestrationTaskVerification
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationTaskVerification{}, err
	}
	return result, nil
}

func (q orchestrationQueries) MarkOrchestrationTaskVerificationRunning(ctx context.Context, arg pgsqlc.MarkOrchestrationTaskVerificationRunningParams) (pgsqlc.OrchestrationTaskVerification, error) {
	var sqliteArg sqlitesqlc.MarkOrchestrationTaskVerificationRunningParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationTaskVerification{}, err
	}
	out, err := q.sqliteQueries().MarkOrchestrationTaskVerificationRunning(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationTaskVerification{}, err
	}
	var result pgsqlc.OrchestrationTaskVerification
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationTaskVerification{}, err
	}
	return result, nil
}

func (q orchestrationQueries) MarkOrchestrationTaskVerifying(ctx context.Context, arg pgsqlc.MarkOrchestrationTaskVerifyingParams) (pgsqlc.OrchestrationTask, error) {
	var sqliteArg sqlitesqlc.MarkOrchestrationTaskVerifyingParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationTask{}, err
	}
	out, err := q.sqliteQueries().MarkOrchestrationTaskVerifying(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationTask{}, err
	}
	var result pgsqlc.OrchestrationTask
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationTask{}, err
	}
	return result, nil
}

func (q orchestrationQueries) MarkOrchestrationTaskWaitingHuman(ctx context.Context, arg pgsqlc.MarkOrchestrationTaskWaitingHumanParams) (pgsqlc.OrchestrationTask, error) {
	var sqliteArg sqlitesqlc.MarkOrchestrationTaskWaitingHumanParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationTask{}, err
	}
	out, err := q.sqliteQueries().MarkOrchestrationTaskWaitingHuman(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationTask{}, err
	}
	var result pgsqlc.OrchestrationTask
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationTask{}, err
	}
	return result, nil
}

func (q orchestrationQueries) PreemptRunningOrchestrationTaskAttemptFailed(ctx context.Context, arg pgsqlc.PreemptRunningOrchestrationTaskAttemptFailedParams) (pgsqlc.OrchestrationTaskAttempt, error) {
	var sqliteArg sqlitesqlc.PreemptRunningOrchestrationTaskAttemptFailedParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationTaskAttempt{}, err
	}
	out, err := q.sqliteQueries().PreemptRunningOrchestrationTaskAttemptFailed(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationTaskAttempt{}, err
	}
	var result pgsqlc.OrchestrationTaskAttempt
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationTaskAttempt{}, err
	}
	return result, nil
}

func (q orchestrationQueries) ReleaseOrchestrationTaskAttemptClaim(ctx context.Context, arg pgsqlc.ReleaseOrchestrationTaskAttemptClaimParams) (pgsqlc.OrchestrationTaskAttempt, error) {
	var sqliteArg sqlitesqlc.ReleaseOrchestrationTaskAttemptClaimParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationTaskAttempt{}, err
	}
	out, err := q.sqliteQueries().ReleaseOrchestrationTaskAttemptClaim(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationTaskAttempt{}, err
	}
	var result pgsqlc.OrchestrationTaskAttempt
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationTaskAttempt{}, err
	}
	return result, nil
}

func (q orchestrationQueries) ReleaseOrchestrationTaskVerificationClaim(ctx context.Context, arg pgsqlc.ReleaseOrchestrationTaskVerificationClaimParams) (pgsqlc.OrchestrationTaskVerification, error) {
	var sqliteArg sqlitesqlc.ReleaseOrchestrationTaskVerificationClaimParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationTaskVerification{}, err
	}
	out, err := q.sqliteQueries().ReleaseOrchestrationTaskVerificationClaim(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationTaskVerification{}, err
	}
	var result pgsqlc.OrchestrationTaskVerification
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationTaskVerification{}, err
	}
	return result, nil
}

func (q orchestrationQueries) RequeueOrchestrationTaskVerification(ctx context.Context, arg pgsqlc.RequeueOrchestrationTaskVerificationParams) (pgsqlc.OrchestrationTaskVerification, error) {
	var sqliteArg sqlitesqlc.RequeueOrchestrationTaskVerificationParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationTaskVerification{}, err
	}
	out, err := q.sqliteQueries().RequeueOrchestrationTaskVerification(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationTaskVerification{}, err
	}
	var result pgsqlc.OrchestrationTaskVerification
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationTaskVerification{}, err
	}
	return result, nil
}

func (q orchestrationQueries) ResolveOrchestrationHumanCheckpoint(ctx context.Context, arg pgsqlc.ResolveOrchestrationHumanCheckpointParams) (pgsqlc.OrchestrationHumanCheckpoint, error) {
	var sqliteArg sqlitesqlc.ResolveOrchestrationHumanCheckpointParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationHumanCheckpoint{}, err
	}
	out, err := q.sqliteQueries().ResolveOrchestrationHumanCheckpoint(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationHumanCheckpoint{}, err
	}
	var result pgsqlc.OrchestrationHumanCheckpoint
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationHumanCheckpoint{}, err
	}
	return result, nil
}

func (q orchestrationQueries) RetireCreatedOrchestrationTaskAttemptFailed(ctx context.Context, arg pgsqlc.RetireCreatedOrchestrationTaskAttemptFailedParams) (pgsqlc.OrchestrationTaskAttempt, error) {
	var sqliteArg sqlitesqlc.RetireCreatedOrchestrationTaskAttemptFailedParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationTaskAttempt{}, err
	}
	out, err := q.sqliteQueries().RetireCreatedOrchestrationTaskAttemptFailed(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationTaskAttempt{}, err
	}
	var result pgsqlc.OrchestrationTaskAttempt
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationTaskAttempt{}, err
	}
	return result, nil
}

func (q orchestrationQueries) RetireOrchestrationTaskAttemptFailed(ctx context.Context, arg pgsqlc.RetireOrchestrationTaskAttemptFailedParams) (pgsqlc.OrchestrationTaskAttempt, error) {
	var sqliteArg sqlitesqlc.RetireOrchestrationTaskAttemptFailedParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationTaskAttempt{}, err
	}
	out, err := q.sqliteQueries().RetireOrchestrationTaskAttemptFailed(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationTaskAttempt{}, err
	}
	var result pgsqlc.OrchestrationTaskAttempt
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationTaskAttempt{}, err
	}
	return result, nil
}

func (q orchestrationQueries) TryCreateOrchestrationIdempotencyRecord(ctx context.Context, arg pgsqlc.TryCreateOrchestrationIdempotencyRecordParams) (pgsqlc.OrchestrationIdempotencyRecord, error) {
	var sqliteArg sqlitesqlc.TryCreateOrchestrationIdempotencyRecordParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationIdempotencyRecord{}, err
	}
	out, err := q.sqliteQueries().TryCreateOrchestrationIdempotencyRecord(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationIdempotencyRecord{}, err
	}
	var result pgsqlc.OrchestrationIdempotencyRecord
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationIdempotencyRecord{}, err
	}
	return result, nil
}

func (q orchestrationQueries) UpsertOrchestrationWorker(ctx context.Context, arg pgsqlc.UpsertOrchestrationWorkerParams) (pgsqlc.OrchestrationWorker, error) {
	var sqliteArg sqlitesqlc.UpsertOrchestrationWorkerParams
	if err := convertValue(arg, &sqliteArg); err != nil {
		return pgsqlc.OrchestrationWorker{}, err
	}
	out, err := q.sqliteQueries().UpsertOrchestrationWorker(ctx, sqliteArg)
	if err != nil {
		return pgsqlc.OrchestrationWorker{}, err
	}
	var result pgsqlc.OrchestrationWorker
	if err := convertValue(out, &result); err != nil {
		return pgsqlc.OrchestrationWorker{}, err
	}
	return result, nil
}

func (tx *orchestrationTx) AdvanceOrchestrationRunPlannerEpoch(ctx context.Context, id pgtype.UUID) (pgsqlc.OrchestrationRun, error) {
	return orchestrationQueries{queries: tx.queries}.AdvanceOrchestrationRunPlannerEpoch(ctx, id)
}

func (tx *orchestrationTx) AllocateOrchestrationRunEventSeqs(ctx context.Context, arg pgsqlc.AllocateOrchestrationRunEventSeqsParams) (int64, error) {
	return orchestrationQueries{queries: tx.queries}.AllocateOrchestrationRunEventSeqs(ctx, arg)
}

func (tx *orchestrationTx) AttachOrchestrationTaskAttemptSession(ctx context.Context, arg pgsqlc.AttachOrchestrationTaskAttemptSessionParams) (pgsqlc.OrchestrationTaskAttempt, error) {
	return orchestrationQueries{queries: tx.queries}.AttachOrchestrationTaskAttemptSession(ctx, arg)
}

func (tx *orchestrationTx) AttachOrchestrationTaskVerificationSession(ctx context.Context, arg pgsqlc.AttachOrchestrationTaskVerificationSessionParams) (pgsqlc.OrchestrationTaskVerification, error) {
	return orchestrationQueries{queries: tx.queries}.AttachOrchestrationTaskVerificationSession(ctx, arg)
}

func (tx *orchestrationTx) CancelOrchestrationTaskVerification(ctx context.Context, arg pgsqlc.CancelOrchestrationTaskVerificationParams) (pgsqlc.OrchestrationTaskVerification, error) {
	return orchestrationQueries{queries: tx.queries}.CancelOrchestrationTaskVerification(ctx, arg)
}

func (tx *orchestrationTx) ClaimNextCreatedOrchestrationTaskAttempt(ctx context.Context, arg pgsqlc.ClaimNextCreatedOrchestrationTaskAttemptParams) (pgsqlc.OrchestrationTaskAttempt, error) {
	return orchestrationQueries{queries: tx.queries}.ClaimNextCreatedOrchestrationTaskAttempt(ctx, arg)
}

func (tx *orchestrationTx) ClaimNextCreatedOrchestrationTaskVerification(ctx context.Context, arg pgsqlc.ClaimNextCreatedOrchestrationTaskVerificationParams) (pgsqlc.OrchestrationTaskVerification, error) {
	return orchestrationQueries{queries: tx.queries}.ClaimNextCreatedOrchestrationTaskVerification(ctx, arg)
}

func (tx *orchestrationTx) ClaimNextOrchestrationPlanningIntent(ctx context.Context, arg pgsqlc.ClaimNextOrchestrationPlanningIntentParams) (pgsqlc.OrchestrationPlanningIntent, error) {
	return orchestrationQueries{queries: tx.queries}.ClaimNextOrchestrationPlanningIntent(ctx, arg)
}

func (tx *orchestrationTx) CompleteOrchestrationIdempotencyRecord(ctx context.Context, arg pgsqlc.CompleteOrchestrationIdempotencyRecordParams) (pgsqlc.OrchestrationIdempotencyRecord, error) {
	return orchestrationQueries{queries: tx.queries}.CompleteOrchestrationIdempotencyRecord(ctx, arg)
}

func (tx *orchestrationTx) CompleteOrchestrationPlanningIntent(ctx context.Context, arg pgsqlc.CompleteOrchestrationPlanningIntentParams) (pgsqlc.OrchestrationPlanningIntent, error) {
	return orchestrationQueries{queries: tx.queries}.CompleteOrchestrationPlanningIntent(ctx, arg)
}

func (tx *orchestrationTx) CountActiveOrchestrationPlanningIntentsByRun(ctx context.Context, runID pgtype.UUID) (int64, error) {
	return orchestrationQueries{queries: tx.queries}.CountActiveOrchestrationPlanningIntentsByRun(ctx, runID)
}

func (tx *orchestrationTx) CountFailedOrchestrationPlanningIntentsByRun(ctx context.Context, runID pgtype.UUID) (int64, error) {
	return orchestrationQueries{queries: tx.queries}.CountFailedOrchestrationPlanningIntentsByRun(ctx, runID)
}

func (tx *orchestrationTx) CountActiveOrchestrationTaskAttemptsByRun(ctx context.Context, runID pgtype.UUID) (int64, error) {
	return orchestrationQueries{queries: tx.queries}.CountActiveOrchestrationTaskAttemptsByRun(ctx, runID)
}

func (tx *orchestrationTx) CountActiveOrchestrationTaskAttemptsByTask(ctx context.Context, taskID pgtype.UUID) (int64, error) {
	return orchestrationQueries{queries: tx.queries}.CountActiveOrchestrationTaskAttemptsByTask(ctx, taskID)
}

func (tx *orchestrationTx) CountCompletedFinalOrchestrationTasksByRun(ctx context.Context, runID pgtype.UUID) (int64, error) {
	return orchestrationQueries{queries: tx.queries}.CountCompletedFinalOrchestrationTasksByRun(ctx, runID)
}

func (tx *orchestrationTx) CountFailedOrchestrationTasksByRun(ctx context.Context, runID pgtype.UUID) (int64, error) {
	return orchestrationQueries{queries: tx.queries}.CountFailedOrchestrationTasksByRun(ctx, runID)
}

func (tx *orchestrationTx) CountNonTerminalOrchestrationTasksByRun(ctx context.Context, runID pgtype.UUID) (int64, error) {
	return orchestrationQueries{queries: tx.queries}.CountNonTerminalOrchestrationTasksByRun(ctx, runID)
}

func (tx *orchestrationTx) CountOpenRunBlockingCheckpointsByRun(ctx context.Context, runID pgtype.UUID) (int64, error) {
	return orchestrationQueries{queries: tx.queries}.CountOpenRunBlockingCheckpointsByRun(ctx, runID)
}

func (tx *orchestrationTx) CreateOrchestrationArtifact(ctx context.Context, arg pgsqlc.CreateOrchestrationArtifactParams) (pgsqlc.OrchestrationArtifact, error) {
	return orchestrationQueries{queries: tx.queries}.CreateOrchestrationArtifact(ctx, arg)
}

func (tx *orchestrationTx) CreateOrchestrationEvent(ctx context.Context, arg pgsqlc.CreateOrchestrationEventParams) (pgsqlc.OrchestrationEvent, error) {
	return orchestrationQueries{queries: tx.queries}.CreateOrchestrationEvent(ctx, arg)
}

func (tx *orchestrationTx) CreateOrchestrationHumanCheckpoint(ctx context.Context, arg pgsqlc.CreateOrchestrationHumanCheckpointParams) (pgsqlc.OrchestrationHumanCheckpoint, error) {
	return orchestrationQueries{queries: tx.queries}.CreateOrchestrationHumanCheckpoint(ctx, arg)
}

func (tx *orchestrationTx) CreateOrchestrationInputManifest(ctx context.Context, arg pgsqlc.CreateOrchestrationInputManifestParams) (pgsqlc.OrchestrationInputManifest, error) {
	return orchestrationQueries{queries: tx.queries}.CreateOrchestrationInputManifest(ctx, arg)
}

func (tx *orchestrationTx) CreateOrchestrationPlanningIntent(ctx context.Context, arg pgsqlc.CreateOrchestrationPlanningIntentParams) (pgsqlc.OrchestrationPlanningIntent, error) {
	return orchestrationQueries{queries: tx.queries}.CreateOrchestrationPlanningIntent(ctx, arg)
}

func (tx *orchestrationTx) CreateOrchestrationProjectionSnapshot(ctx context.Context, arg pgsqlc.CreateOrchestrationProjectionSnapshotParams) (pgsqlc.OrchestrationProjectionSnapshot, error) {
	return orchestrationQueries{queries: tx.queries}.CreateOrchestrationProjectionSnapshot(ctx, arg)
}

func (tx *orchestrationTx) CreateOrchestrationRun(ctx context.Context, arg pgsqlc.CreateOrchestrationRunParams) (pgsqlc.OrchestrationRun, error) {
	return orchestrationQueries{queries: tx.queries}.CreateOrchestrationRun(ctx, arg)
}

func (tx *orchestrationTx) CreateOrchestrationTask(ctx context.Context, arg pgsqlc.CreateOrchestrationTaskParams) (pgsqlc.OrchestrationTask, error) {
	return orchestrationQueries{queries: tx.queries}.CreateOrchestrationTask(ctx, arg)
}

func (tx *orchestrationTx) CreateOrchestrationTaskAttempt(ctx context.Context, arg pgsqlc.CreateOrchestrationTaskAttemptParams) (pgsqlc.OrchestrationTaskAttempt, error) {
	return orchestrationQueries{queries: tx.queries}.CreateOrchestrationTaskAttempt(ctx, arg)
}

func (tx *orchestrationTx) CreateOrchestrationTaskDependency(ctx context.Context, arg pgsqlc.CreateOrchestrationTaskDependencyParams) (pgsqlc.OrchestrationTaskDependency, error) {
	return orchestrationQueries{queries: tx.queries}.CreateOrchestrationTaskDependency(ctx, arg)
}

func (tx *orchestrationTx) CreateOrchestrationTaskResult(ctx context.Context, arg pgsqlc.CreateOrchestrationTaskResultParams) (pgsqlc.OrchestrationTaskResult, error) {
	return orchestrationQueries{queries: tx.queries}.CreateOrchestrationTaskResult(ctx, arg)
}

func (tx *orchestrationTx) CreateOrchestrationTaskVerification(ctx context.Context, arg pgsqlc.CreateOrchestrationTaskVerificationParams) (pgsqlc.OrchestrationTaskVerification, error) {
	return orchestrationQueries{queries: tx.queries}.CreateOrchestrationTaskVerification(ctx, arg)
}

func (tx *orchestrationTx) FailOrchestrationPlanningIntent(ctx context.Context, arg pgsqlc.FailOrchestrationPlanningIntentParams) (pgsqlc.OrchestrationPlanningIntent, error) {
	return orchestrationQueries{queries: tx.queries}.FailOrchestrationPlanningIntent(ctx, arg)
}

func (tx *orchestrationTx) GetNextOrchestrationTaskAttemptNo(ctx context.Context, taskID pgtype.UUID) (int32, error) {
	return orchestrationQueries{queries: tx.queries}.GetNextOrchestrationTaskAttemptNo(ctx, taskID)
}

func (tx *orchestrationTx) GetOrchestrationHumanCheckpointByIDForUpdate(ctx context.Context, id pgtype.UUID) (pgsqlc.OrchestrationHumanCheckpoint, error) {
	return orchestrationQueries{queries: tx.queries}.GetOrchestrationHumanCheckpointByIDForUpdate(ctx, id)
}

func (tx *orchestrationTx) GetOrchestrationIdempotencyRecordForUpdate(ctx context.Context, arg pgsqlc.GetOrchestrationIdempotencyRecordForUpdateParams) (pgsqlc.OrchestrationIdempotencyRecord, error) {
	return orchestrationQueries{queries: tx.queries}.GetOrchestrationIdempotencyRecordForUpdate(ctx, arg)
}

func (tx *orchestrationTx) GetOrchestrationInputManifestByID(ctx context.Context, id pgtype.UUID) (pgsqlc.OrchestrationInputManifest, error) {
	return orchestrationQueries{queries: tx.queries}.GetOrchestrationInputManifestByID(ctx, id)
}

func (tx *orchestrationTx) GetOrchestrationProjectionSnapshotAtOrBeforeSeq(ctx context.Context, arg pgsqlc.GetOrchestrationProjectionSnapshotAtOrBeforeSeqParams) (pgsqlc.OrchestrationProjectionSnapshot, error) {
	return orchestrationQueries{queries: tx.queries}.GetOrchestrationProjectionSnapshotAtOrBeforeSeq(ctx, arg)
}

func (tx *orchestrationTx) GetOrchestrationRunByID(ctx context.Context, id pgtype.UUID) (pgsqlc.OrchestrationRun, error) {
	return orchestrationQueries{queries: tx.queries}.GetOrchestrationRunByID(ctx, id)
}

func (tx *orchestrationTx) GetOrchestrationRunByIDForUpdate(ctx context.Context, id pgtype.UUID) (pgsqlc.OrchestrationRun, error) {
	return orchestrationQueries{queries: tx.queries}.GetOrchestrationRunByIDForUpdate(ctx, id)
}

func (tx *orchestrationTx) GetOrchestrationTaskAttemptByID(ctx context.Context, id pgtype.UUID) (pgsqlc.OrchestrationTaskAttempt, error) {
	return orchestrationQueries{queries: tx.queries}.GetOrchestrationTaskAttemptByID(ctx, id)
}

func (tx *orchestrationTx) GetOrchestrationTaskAttemptByIDForUpdate(ctx context.Context, id pgtype.UUID) (pgsqlc.OrchestrationTaskAttempt, error) {
	return orchestrationQueries{queries: tx.queries}.GetOrchestrationTaskAttemptByIDForUpdate(ctx, id)
}

func (tx *orchestrationTx) GetOrchestrationTaskByID(ctx context.Context, id pgtype.UUID) (pgsqlc.OrchestrationTask, error) {
	return orchestrationQueries{queries: tx.queries}.GetOrchestrationTaskByID(ctx, id)
}

func (tx *orchestrationTx) GetOrchestrationTaskByIDForUpdate(ctx context.Context, id pgtype.UUID) (pgsqlc.OrchestrationTask, error) {
	return orchestrationQueries{queries: tx.queries}.GetOrchestrationTaskByIDForUpdate(ctx, id)
}

func (tx *orchestrationTx) GetOrchestrationTaskResultByID(ctx context.Context, id pgtype.UUID) (pgsqlc.OrchestrationTaskResult, error) {
	return orchestrationQueries{queries: tx.queries}.GetOrchestrationTaskResultByID(ctx, id)
}

func (tx *orchestrationTx) GetOrchestrationTaskVerificationByIDForUpdate(ctx context.Context, id pgtype.UUID) (pgsqlc.OrchestrationTaskVerification, error) {
	return orchestrationQueries{queries: tx.queries}.GetOrchestrationTaskVerificationByIDForUpdate(ctx, id)
}

func (tx *orchestrationTx) GetOrchestrationWorkerByIDForUpdate(ctx context.Context, id string) (pgsqlc.OrchestrationWorker, error) {
	return orchestrationQueries{queries: tx.queries}.GetOrchestrationWorkerByIDForUpdate(ctx, id)
}

func (tx *orchestrationTx) HeartbeatOrchestrationTaskAttempt(ctx context.Context, arg pgsqlc.HeartbeatOrchestrationTaskAttemptParams) (pgsqlc.OrchestrationTaskAttempt, error) {
	return orchestrationQueries{queries: tx.queries}.HeartbeatOrchestrationTaskAttempt(ctx, arg)
}

func (tx *orchestrationTx) HeartbeatOrchestrationTaskVerification(ctx context.Context, arg pgsqlc.HeartbeatOrchestrationTaskVerificationParams) (pgsqlc.OrchestrationTaskVerification, error) {
	return orchestrationQueries{queries: tx.queries}.HeartbeatOrchestrationTaskVerification(ctx, arg)
}

func (tx *orchestrationTx) HeartbeatOrchestrationWorker(ctx context.Context, arg pgsqlc.HeartbeatOrchestrationWorkerParams) (pgsqlc.OrchestrationWorker, error) {
	return orchestrationQueries{queries: tx.queries}.HeartbeatOrchestrationWorker(ctx, arg)
}

func (tx *orchestrationTx) ListActiveOrchestrationTaskDependenciesByPredecessor(ctx context.Context, predecessorTaskID pgtype.UUID) ([]pgsqlc.OrchestrationTaskDependency, error) {
	return orchestrationQueries{queries: tx.queries}.ListActiveOrchestrationTaskDependenciesByPredecessor(ctx, predecessorTaskID)
}

func (tx *orchestrationTx) ListActiveOrchestrationTaskDependenciesBySuccessor(ctx context.Context, successorTaskID pgtype.UUID) ([]pgsqlc.OrchestrationTaskDependency, error) {
	return orchestrationQueries{queries: tx.queries}.ListActiveOrchestrationTaskDependenciesBySuccessor(ctx, successorTaskID)
}

func (tx *orchestrationTx) ListCurrentOrchestrationArtifactsByRun(ctx context.Context, runID pgtype.UUID) ([]pgsqlc.OrchestrationArtifact, error) {
	return orchestrationQueries{queries: tx.queries}.ListCurrentOrchestrationArtifactsByRun(ctx, runID)
}

func (tx *orchestrationTx) ListCurrentOrchestrationCheckpointsByRun(ctx context.Context, runID pgtype.UUID) ([]pgsqlc.OrchestrationHumanCheckpoint, error) {
	return orchestrationQueries{queries: tx.queries}.ListCurrentOrchestrationCheckpointsByRun(ctx, runID)
}

func (tx *orchestrationTx) ListCurrentOrchestrationTaskAttemptsByRun(ctx context.Context, runID pgtype.UUID) ([]pgsqlc.OrchestrationTaskAttempt, error) {
	return orchestrationQueries{queries: tx.queries}.ListCurrentOrchestrationTaskAttemptsByRun(ctx, runID)
}

func (tx *orchestrationTx) ListCurrentOrchestrationTaskDependenciesByRun(ctx context.Context, runID pgtype.UUID) ([]pgsqlc.OrchestrationTaskDependency, error) {
	return orchestrationQueries{queries: tx.queries}.ListCurrentOrchestrationTaskDependenciesByRun(ctx, runID)
}

func (tx *orchestrationTx) ListCurrentOrchestrationTaskResultsByRun(ctx context.Context, runID pgtype.UUID) ([]pgsqlc.OrchestrationTaskResult, error) {
	return orchestrationQueries{queries: tx.queries}.ListCurrentOrchestrationTaskResultsByRun(ctx, runID)
}

func (tx *orchestrationTx) ListCurrentOrchestrationTaskVerificationsByRun(ctx context.Context, runID pgtype.UUID) ([]pgsqlc.OrchestrationTaskVerification, error) {
	return orchestrationQueries{queries: tx.queries}.ListCurrentOrchestrationTaskVerificationsByRun(ctx, runID)
}

func (tx *orchestrationTx) ListCurrentOrchestrationTasksByRun(ctx context.Context, runID pgtype.UUID) ([]pgsqlc.OrchestrationTask, error) {
	return orchestrationQueries{queries: tx.queries}.ListCurrentOrchestrationTasksByRun(ctx, runID)
}

func (tx *orchestrationTx) ListDependencyBlockedOrchestrationTasks(ctx context.Context) ([]pgsqlc.OrchestrationTask, error) {
	return orchestrationQueries{queries: tx.queries}.ListDependencyBlockedOrchestrationTasks(ctx)
}

func (tx *orchestrationTx) ListExpiredOrchestrationTaskAttempts(ctx context.Context) ([]pgsqlc.OrchestrationTaskAttempt, error) {
	return orchestrationQueries{queries: tx.queries}.ListExpiredOrchestrationTaskAttempts(ctx)
}

func (tx *orchestrationTx) ListExpiredOrchestrationTaskVerifications(ctx context.Context) ([]pgsqlc.OrchestrationTaskVerification, error) {
	return orchestrationQueries{queries: tx.queries}.ListExpiredOrchestrationTaskVerifications(ctx)
}

func (tx *orchestrationTx) ListOrchestrationArtifactsByTask(ctx context.Context, taskID pgtype.UUID) ([]pgsqlc.OrchestrationArtifact, error) {
	return orchestrationQueries{queries: tx.queries}.ListOrchestrationArtifactsByTask(ctx, taskID)
}

func (tx *orchestrationTx) ListOrchestrationRunEvents(ctx context.Context, arg pgsqlc.ListOrchestrationRunEventsParams) ([]pgsqlc.OrchestrationEvent, error) {
	return orchestrationQueries{queries: tx.queries}.ListOrchestrationRunEvents(ctx, arg)
}

func (tx *orchestrationTx) ListOrchestrationRunsByBot(ctx context.Context, arg pgsqlc.ListOrchestrationRunsByBotParams) ([]pgsqlc.OrchestrationRun, error) {
	return orchestrationQueries{queries: tx.queries}.ListOrchestrationRunsByBot(ctx, arg)
}

func (tx *orchestrationTx) ListTerminalizableRunningOrchestrationRuns(ctx context.Context, limitCount int32) ([]pgsqlc.OrchestrationRun, error) {
	return orchestrationQueries{queries: tx.queries}.ListTerminalizableRunningOrchestrationRuns(ctx, limitCount)
}

func (tx *orchestrationTx) ListOrchestrationWorkersByIDs(ctx context.Context, ids []string) ([]pgsqlc.OrchestrationWorker, error) {
	return orchestrationQueries{queries: tx.queries}.ListOrchestrationWorkersByIDs(ctx, ids)
}

func (tx *orchestrationTx) ListSchedulableOrchestrationTasks(ctx context.Context) ([]pgsqlc.OrchestrationTask, error) {
	return orchestrationQueries{queries: tx.queries}.ListSchedulableOrchestrationTasks(ctx)
}

func (tx *orchestrationTx) MarkOrchestrationHumanCheckpointCancelled(ctx context.Context, id pgtype.UUID) (pgsqlc.OrchestrationHumanCheckpoint, error) {
	return orchestrationQueries{queries: tx.queries}.MarkOrchestrationHumanCheckpointCancelled(ctx, id)
}

func (tx *orchestrationTx) MarkOrchestrationHumanCheckpointSuperseded(ctx context.Context, arg pgsqlc.MarkOrchestrationHumanCheckpointSupersededParams) (pgsqlc.OrchestrationHumanCheckpoint, error) {
	return orchestrationQueries{queries: tx.queries}.MarkOrchestrationHumanCheckpointSuperseded(ctx, arg)
}

func (tx *orchestrationTx) MarkOrchestrationRunCancelled(ctx context.Context, arg pgsqlc.MarkOrchestrationRunCancelledParams) (pgsqlc.OrchestrationRun, error) {
	return orchestrationQueries{queries: tx.queries}.MarkOrchestrationRunCancelled(ctx, arg)
}

func (tx *orchestrationTx) MarkOrchestrationRunCancelling(ctx context.Context, id pgtype.UUID) (pgsqlc.OrchestrationRun, error) {
	return orchestrationQueries{queries: tx.queries}.MarkOrchestrationRunCancelling(ctx, id)
}

func (tx *orchestrationTx) MarkOrchestrationRunCompleted(ctx context.Context, id pgtype.UUID) (pgsqlc.OrchestrationRun, error) {
	return orchestrationQueries{queries: tx.queries}.MarkOrchestrationRunCompleted(ctx, id)
}

func (tx *orchestrationTx) MarkOrchestrationRunFailed(ctx context.Context, arg pgsqlc.MarkOrchestrationRunFailedParams) (pgsqlc.OrchestrationRun, error) {
	return orchestrationQueries{queries: tx.queries}.MarkOrchestrationRunFailed(ctx, arg)
}

func (tx *orchestrationTx) MarkOrchestrationRunPlanningActive(ctx context.Context, id pgtype.UUID) (pgsqlc.OrchestrationRun, error) {
	return orchestrationQueries{queries: tx.queries}.MarkOrchestrationRunPlanningActive(ctx, id)
}

func (tx *orchestrationTx) MarkOrchestrationRunPlanningIdle(ctx context.Context, id pgtype.UUID) (pgsqlc.OrchestrationRun, error) {
	return orchestrationQueries{queries: tx.queries}.MarkOrchestrationRunPlanningIdle(ctx, id)
}

func (tx *orchestrationTx) MarkOrchestrationRunRunning(ctx context.Context, id pgtype.UUID) (pgsqlc.OrchestrationRun, error) {
	return orchestrationQueries{queries: tx.queries}.MarkOrchestrationRunRunning(ctx, id)
}

func (tx *orchestrationTx) MarkOrchestrationRunWaitingHuman(ctx context.Context, id pgtype.UUID) (pgsqlc.OrchestrationRun, error) {
	return orchestrationQueries{queries: tx.queries}.MarkOrchestrationRunWaitingHuman(ctx, id)
}

func (tx *orchestrationTx) MarkOrchestrationTaskAttemptBinding(ctx context.Context, arg pgsqlc.MarkOrchestrationTaskAttemptBindingParams) (pgsqlc.OrchestrationTaskAttempt, error) {
	return orchestrationQueries{queries: tx.queries}.MarkOrchestrationTaskAttemptBinding(ctx, arg)
}

func (tx *orchestrationTx) MarkOrchestrationTaskAttemptCompleted(ctx context.Context, arg pgsqlc.MarkOrchestrationTaskAttemptCompletedParams) (pgsqlc.OrchestrationTaskAttempt, error) {
	return orchestrationQueries{queries: tx.queries}.MarkOrchestrationTaskAttemptCompleted(ctx, arg)
}

func (tx *orchestrationTx) MarkOrchestrationTaskAttemptFailed(ctx context.Context, arg pgsqlc.MarkOrchestrationTaskAttemptFailedParams) (pgsqlc.OrchestrationTaskAttempt, error) {
	return orchestrationQueries{queries: tx.queries}.MarkOrchestrationTaskAttemptFailed(ctx, arg)
}

func (tx *orchestrationTx) MarkOrchestrationTaskAttemptLost(ctx context.Context, arg pgsqlc.MarkOrchestrationTaskAttemptLostParams) (pgsqlc.OrchestrationTaskAttempt, error) {
	return orchestrationQueries{queries: tx.queries}.MarkOrchestrationTaskAttemptLost(ctx, arg)
}

func (tx *orchestrationTx) MarkOrchestrationTaskAttemptParked(ctx context.Context, arg pgsqlc.MarkOrchestrationTaskAttemptParkedParams) (pgsqlc.OrchestrationTaskAttempt, error) {
	return orchestrationQueries{queries: tx.queries}.MarkOrchestrationTaskAttemptParked(ctx, arg)
}

func (tx *orchestrationTx) MarkOrchestrationTaskAttemptRunning(ctx context.Context, arg pgsqlc.MarkOrchestrationTaskAttemptRunningParams) (pgsqlc.OrchestrationTaskAttempt, error) {
	return orchestrationQueries{queries: tx.queries}.MarkOrchestrationTaskAttemptRunning(ctx, arg)
}

func (tx *orchestrationTx) MarkOrchestrationTaskBlocked(ctx context.Context, arg pgsqlc.MarkOrchestrationTaskBlockedParams) (pgsqlc.OrchestrationTask, error) {
	return orchestrationQueries{queries: tx.queries}.MarkOrchestrationTaskBlocked(ctx, arg)
}

func (tx *orchestrationTx) MarkOrchestrationTaskCancelled(ctx context.Context, arg pgsqlc.MarkOrchestrationTaskCancelledParams) (pgsqlc.OrchestrationTask, error) {
	return orchestrationQueries{queries: tx.queries}.MarkOrchestrationTaskCancelled(ctx, arg)
}

func (tx *orchestrationTx) MarkOrchestrationTaskCompleted(ctx context.Context, arg pgsqlc.MarkOrchestrationTaskCompletedParams) (pgsqlc.OrchestrationTask, error) {
	return orchestrationQueries{queries: tx.queries}.MarkOrchestrationTaskCompleted(ctx, arg)
}

func (tx *orchestrationTx) MarkOrchestrationTaskDependencySuperseded(ctx context.Context, arg pgsqlc.MarkOrchestrationTaskDependencySupersededParams) (pgsqlc.OrchestrationTaskDependency, error) {
	return orchestrationQueries{queries: tx.queries}.MarkOrchestrationTaskDependencySuperseded(ctx, arg)
}

func (tx *orchestrationTx) MarkOrchestrationTaskDispatching(ctx context.Context, id pgtype.UUID) (pgsqlc.OrchestrationTask, error) {
	return orchestrationQueries{queries: tx.queries}.MarkOrchestrationTaskDispatching(ctx, id)
}

func (tx *orchestrationTx) MarkOrchestrationTaskCreatedForResume(ctx context.Context, arg pgsqlc.MarkOrchestrationTaskCreatedForResumeParams) (pgsqlc.OrchestrationTask, error) {
	return orchestrationQueries{queries: tx.queries}.MarkOrchestrationTaskCreatedForResume(ctx, arg)
}

func (tx *orchestrationTx) MarkOrchestrationTaskFailed(ctx context.Context, arg pgsqlc.MarkOrchestrationTaskFailedParams) (pgsqlc.OrchestrationTask, error) {
	return orchestrationQueries{queries: tx.queries}.MarkOrchestrationTaskFailed(ctx, arg)
}

func (tx *orchestrationTx) MarkOrchestrationTaskReadyForRetry(ctx context.Context, arg pgsqlc.MarkOrchestrationTaskReadyForRetryParams) (pgsqlc.OrchestrationTask, error) {
	return orchestrationQueries{queries: tx.queries}.MarkOrchestrationTaskReadyForRetry(ctx, arg)
}

func (tx *orchestrationTx) MarkOrchestrationTaskReadyFromCheckpoint(ctx context.Context, id pgtype.UUID) (pgsqlc.OrchestrationTask, error) {
	return orchestrationQueries{queries: tx.queries}.MarkOrchestrationTaskReadyFromCheckpoint(ctx, id)
}

func (tx *orchestrationTx) MarkOrchestrationTaskRunning(ctx context.Context, id pgtype.UUID) (pgsqlc.OrchestrationTask, error) {
	return orchestrationQueries{queries: tx.queries}.MarkOrchestrationTaskRunning(ctx, id)
}

func (tx *orchestrationTx) MarkOrchestrationTaskSuperseded(ctx context.Context, arg pgsqlc.MarkOrchestrationTaskSupersededParams) (pgsqlc.OrchestrationTask, error) {
	return orchestrationQueries{queries: tx.queries}.MarkOrchestrationTaskSuperseded(ctx, arg)
}

func (tx *orchestrationTx) MarkOrchestrationTaskVerificationCompleted(ctx context.Context, arg pgsqlc.MarkOrchestrationTaskVerificationCompletedParams) (pgsqlc.OrchestrationTaskVerification, error) {
	return orchestrationQueries{queries: tx.queries}.MarkOrchestrationTaskVerificationCompleted(ctx, arg)
}

func (tx *orchestrationTx) MarkOrchestrationTaskVerificationFailed(ctx context.Context, arg pgsqlc.MarkOrchestrationTaskVerificationFailedParams) (pgsqlc.OrchestrationTaskVerification, error) {
	return orchestrationQueries{queries: tx.queries}.MarkOrchestrationTaskVerificationFailed(ctx, arg)
}

func (tx *orchestrationTx) MarkOrchestrationTaskVerificationLost(ctx context.Context, arg pgsqlc.MarkOrchestrationTaskVerificationLostParams) (pgsqlc.OrchestrationTaskVerification, error) {
	return orchestrationQueries{queries: tx.queries}.MarkOrchestrationTaskVerificationLost(ctx, arg)
}

func (tx *orchestrationTx) MarkOrchestrationTaskVerificationRunning(ctx context.Context, arg pgsqlc.MarkOrchestrationTaskVerificationRunningParams) (pgsqlc.OrchestrationTaskVerification, error) {
	return orchestrationQueries{queries: tx.queries}.MarkOrchestrationTaskVerificationRunning(ctx, arg)
}

func (tx *orchestrationTx) MarkOrchestrationTaskVerifying(ctx context.Context, arg pgsqlc.MarkOrchestrationTaskVerifyingParams) (pgsqlc.OrchestrationTask, error) {
	return orchestrationQueries{queries: tx.queries}.MarkOrchestrationTaskVerifying(ctx, arg)
}

func (tx *orchestrationTx) MarkOrchestrationTaskWaitingHuman(ctx context.Context, arg pgsqlc.MarkOrchestrationTaskWaitingHumanParams) (pgsqlc.OrchestrationTask, error) {
	return orchestrationQueries{queries: tx.queries}.MarkOrchestrationTaskWaitingHuman(ctx, arg)
}

func (tx *orchestrationTx) PreemptRunningOrchestrationTaskAttemptFailed(ctx context.Context, arg pgsqlc.PreemptRunningOrchestrationTaskAttemptFailedParams) (pgsqlc.OrchestrationTaskAttempt, error) {
	return orchestrationQueries{queries: tx.queries}.PreemptRunningOrchestrationTaskAttemptFailed(ctx, arg)
}

func (tx *orchestrationTx) ReleaseOrchestrationTaskAttemptClaim(ctx context.Context, arg pgsqlc.ReleaseOrchestrationTaskAttemptClaimParams) (pgsqlc.OrchestrationTaskAttempt, error) {
	return orchestrationQueries{queries: tx.queries}.ReleaseOrchestrationTaskAttemptClaim(ctx, arg)
}

func (tx *orchestrationTx) ReleaseOrchestrationTaskVerificationClaim(ctx context.Context, arg pgsqlc.ReleaseOrchestrationTaskVerificationClaimParams) (pgsqlc.OrchestrationTaskVerification, error) {
	return orchestrationQueries{queries: tx.queries}.ReleaseOrchestrationTaskVerificationClaim(ctx, arg)
}

func (tx *orchestrationTx) RequeueOrchestrationTaskVerification(ctx context.Context, arg pgsqlc.RequeueOrchestrationTaskVerificationParams) (pgsqlc.OrchestrationTaskVerification, error) {
	return orchestrationQueries{queries: tx.queries}.RequeueOrchestrationTaskVerification(ctx, arg)
}

func (tx *orchestrationTx) ResolveOrchestrationHumanCheckpoint(ctx context.Context, arg pgsqlc.ResolveOrchestrationHumanCheckpointParams) (pgsqlc.OrchestrationHumanCheckpoint, error) {
	return orchestrationQueries{queries: tx.queries}.ResolveOrchestrationHumanCheckpoint(ctx, arg)
}

func (tx *orchestrationTx) RetireCreatedOrchestrationTaskAttemptFailed(ctx context.Context, arg pgsqlc.RetireCreatedOrchestrationTaskAttemptFailedParams) (pgsqlc.OrchestrationTaskAttempt, error) {
	return orchestrationQueries{queries: tx.queries}.RetireCreatedOrchestrationTaskAttemptFailed(ctx, arg)
}

func (tx *orchestrationTx) RetireOrchestrationTaskAttemptFailed(ctx context.Context, arg pgsqlc.RetireOrchestrationTaskAttemptFailedParams) (pgsqlc.OrchestrationTaskAttempt, error) {
	return orchestrationQueries{queries: tx.queries}.RetireOrchestrationTaskAttemptFailed(ctx, arg)
}

func (tx *orchestrationTx) TryCreateOrchestrationIdempotencyRecord(ctx context.Context, arg pgsqlc.TryCreateOrchestrationIdempotencyRecordParams) (pgsqlc.OrchestrationIdempotencyRecord, error) {
	return orchestrationQueries{queries: tx.queries}.TryCreateOrchestrationIdempotencyRecord(ctx, arg)
}

func (tx *orchestrationTx) UpsertOrchestrationWorker(ctx context.Context, arg pgsqlc.UpsertOrchestrationWorkerParams) (pgsqlc.OrchestrationWorker, error) {
	return orchestrationQueries{queries: tx.queries}.UpsertOrchestrationWorker(ctx, arg)
}

func uuidString(value pgtype.UUID) string { return value.String() }

func stringsJSON(values []string) string { raw, _ := json.Marshal(values); return string(raw) }
