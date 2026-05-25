package orchestration

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	dbsqlc "github.com/memohai/memoh/internal/db/postgres/sqlc"
)

type Store interface {
	BeginTx(context.Context) (Tx, error)
	Queries() Queries
}

type Tx interface {
	Queries
	Commit(context.Context) error
	Rollback(context.Context) error
}

type Queries interface {
	DatabaseNow(ctx context.Context) (time.Time, error)
	AdvanceOrchestrationRunPlannerEpoch(ctx context.Context, id pgtype.UUID) (dbsqlc.OrchestrationRun, error)
	AllocateOrchestrationRunEventSeqs(ctx context.Context, arg dbsqlc.AllocateOrchestrationRunEventSeqsParams) (int64, error)
	AttachOrchestrationTaskAttemptSession(ctx context.Context, arg dbsqlc.AttachOrchestrationTaskAttemptSessionParams) (dbsqlc.OrchestrationTaskAttempt, error)
	AttachOrchestrationTaskVerificationSession(ctx context.Context, arg dbsqlc.AttachOrchestrationTaskVerificationSessionParams) (dbsqlc.OrchestrationTaskVerification, error)
	CancelOrchestrationTaskVerification(ctx context.Context, arg dbsqlc.CancelOrchestrationTaskVerificationParams) (dbsqlc.OrchestrationTaskVerification, error)
	ClaimNextCreatedOrchestrationTaskAttempt(ctx context.Context, arg dbsqlc.ClaimNextCreatedOrchestrationTaskAttemptParams) (dbsqlc.OrchestrationTaskAttempt, error)
	ClaimNextCreatedOrchestrationTaskVerification(ctx context.Context, arg dbsqlc.ClaimNextCreatedOrchestrationTaskVerificationParams) (dbsqlc.OrchestrationTaskVerification, error)
	ClaimNextOrchestrationPlanningIntent(ctx context.Context, arg dbsqlc.ClaimNextOrchestrationPlanningIntentParams) (dbsqlc.OrchestrationPlanningIntent, error)
	CompleteOrchestrationIdempotencyRecord(ctx context.Context, arg dbsqlc.CompleteOrchestrationIdempotencyRecordParams) (dbsqlc.OrchestrationIdempotencyRecord, error)
	CompleteOrchestrationPlanningIntent(ctx context.Context, arg dbsqlc.CompleteOrchestrationPlanningIntentParams) (dbsqlc.OrchestrationPlanningIntent, error)
	CountActiveOrchestrationPlanningIntentsByRun(ctx context.Context, runID pgtype.UUID) (int64, error)
	CountFailedOrchestrationPlanningIntentsByRun(ctx context.Context, runID pgtype.UUID) (int64, error)
	CountActiveOrchestrationTaskAttemptsByRun(ctx context.Context, runID pgtype.UUID) (int64, error)
	CountActiveOrchestrationTaskAttemptsByTask(ctx context.Context, taskID pgtype.UUID) (int64, error)
	CountCompletedFinalOrchestrationTasksByRun(ctx context.Context, runID pgtype.UUID) (int64, error)
	CountFailedOrchestrationTasksByRun(ctx context.Context, runID pgtype.UUID) (int64, error)
	CountNonTerminalOrchestrationTasksByRun(ctx context.Context, runID pgtype.UUID) (int64, error)
	CountOpenRunBlockingCheckpointsByRun(ctx context.Context, runID pgtype.UUID) (int64, error)
	CreateOrchestrationArtifact(ctx context.Context, arg dbsqlc.CreateOrchestrationArtifactParams) (dbsqlc.OrchestrationArtifact, error)
	CreateOrchestrationEvent(ctx context.Context, arg dbsqlc.CreateOrchestrationEventParams) (dbsqlc.OrchestrationEvent, error)
	CreateOrchestrationHumanCheckpoint(ctx context.Context, arg dbsqlc.CreateOrchestrationHumanCheckpointParams) (dbsqlc.OrchestrationHumanCheckpoint, error)
	CreateOrchestrationInputManifest(ctx context.Context, arg dbsqlc.CreateOrchestrationInputManifestParams) (dbsqlc.OrchestrationInputManifest, error)
	CreateOrchestrationPlanningIntent(ctx context.Context, arg dbsqlc.CreateOrchestrationPlanningIntentParams) (dbsqlc.OrchestrationPlanningIntent, error)
	CreateOrchestrationProjectionSnapshot(ctx context.Context, arg dbsqlc.CreateOrchestrationProjectionSnapshotParams) (dbsqlc.OrchestrationProjectionSnapshot, error)
	CreateOrchestrationRun(ctx context.Context, arg dbsqlc.CreateOrchestrationRunParams) (dbsqlc.OrchestrationRun, error)
	CreateOrchestrationTask(ctx context.Context, arg dbsqlc.CreateOrchestrationTaskParams) (dbsqlc.OrchestrationTask, error)
	CreateOrchestrationTaskAttempt(ctx context.Context, arg dbsqlc.CreateOrchestrationTaskAttemptParams) (dbsqlc.OrchestrationTaskAttempt, error)
	CreateOrchestrationTaskDependency(ctx context.Context, arg dbsqlc.CreateOrchestrationTaskDependencyParams) (dbsqlc.OrchestrationTaskDependency, error)
	CreateOrchestrationTaskResult(ctx context.Context, arg dbsqlc.CreateOrchestrationTaskResultParams) (dbsqlc.OrchestrationTaskResult, error)
	CreateOrchestrationTaskVerification(ctx context.Context, arg dbsqlc.CreateOrchestrationTaskVerificationParams) (dbsqlc.OrchestrationTaskVerification, error)
	FailOrchestrationPlanningIntent(ctx context.Context, arg dbsqlc.FailOrchestrationPlanningIntentParams) (dbsqlc.OrchestrationPlanningIntent, error)
	GetNextOrchestrationTaskAttemptNo(ctx context.Context, taskID pgtype.UUID) (int32, error)
	GetOrchestrationHumanCheckpointByIDForUpdate(ctx context.Context, id pgtype.UUID) (dbsqlc.OrchestrationHumanCheckpoint, error)
	GetOrchestrationIdempotencyRecordForUpdate(ctx context.Context, arg dbsqlc.GetOrchestrationIdempotencyRecordForUpdateParams) (dbsqlc.OrchestrationIdempotencyRecord, error)
	GetOrchestrationInputManifestByID(ctx context.Context, id pgtype.UUID) (dbsqlc.OrchestrationInputManifest, error)
	GetOrchestrationProjectionSnapshotAtOrBeforeSeq(ctx context.Context, arg dbsqlc.GetOrchestrationProjectionSnapshotAtOrBeforeSeqParams) (dbsqlc.OrchestrationProjectionSnapshot, error)
	GetOrchestrationRunByID(ctx context.Context, id pgtype.UUID) (dbsqlc.OrchestrationRun, error)
	GetOrchestrationRunByIDForUpdate(ctx context.Context, id pgtype.UUID) (dbsqlc.OrchestrationRun, error)
	GetOrchestrationTaskAttemptByID(ctx context.Context, id pgtype.UUID) (dbsqlc.OrchestrationTaskAttempt, error)
	GetOrchestrationTaskAttemptByIDForUpdate(ctx context.Context, id pgtype.UUID) (dbsqlc.OrchestrationTaskAttempt, error)
	GetOrchestrationTaskByID(ctx context.Context, id pgtype.UUID) (dbsqlc.OrchestrationTask, error)
	GetOrchestrationTaskByIDForUpdate(ctx context.Context, id pgtype.UUID) (dbsqlc.OrchestrationTask, error)
	GetOrchestrationTaskResultByID(ctx context.Context, id pgtype.UUID) (dbsqlc.OrchestrationTaskResult, error)
	GetOrchestrationTaskVerificationByIDForUpdate(ctx context.Context, id pgtype.UUID) (dbsqlc.OrchestrationTaskVerification, error)
	GetOrchestrationWorkerByIDForUpdate(ctx context.Context, id string) (dbsqlc.OrchestrationWorker, error)
	HeartbeatOrchestrationTaskAttempt(ctx context.Context, arg dbsqlc.HeartbeatOrchestrationTaskAttemptParams) (dbsqlc.OrchestrationTaskAttempt, error)
	HeartbeatOrchestrationTaskVerification(ctx context.Context, arg dbsqlc.HeartbeatOrchestrationTaskVerificationParams) (dbsqlc.OrchestrationTaskVerification, error)
	HeartbeatOrchestrationWorker(ctx context.Context, arg dbsqlc.HeartbeatOrchestrationWorkerParams) (dbsqlc.OrchestrationWorker, error)
	ListActiveOrchestrationTaskDependenciesByPredecessor(ctx context.Context, predecessorTaskID pgtype.UUID) ([]dbsqlc.OrchestrationTaskDependency, error)
	ListActiveOrchestrationTaskDependenciesBySuccessor(ctx context.Context, successorTaskID pgtype.UUID) ([]dbsqlc.OrchestrationTaskDependency, error)
	ListCurrentOrchestrationArtifactsByRun(ctx context.Context, runID pgtype.UUID) ([]dbsqlc.OrchestrationArtifact, error)
	ListCurrentOrchestrationCheckpointsByRun(ctx context.Context, runID pgtype.UUID) ([]dbsqlc.OrchestrationHumanCheckpoint, error)
	ListCurrentOrchestrationTaskAttemptsByRun(ctx context.Context, runID pgtype.UUID) ([]dbsqlc.OrchestrationTaskAttempt, error)
	ListCurrentOrchestrationTaskDependenciesByRun(ctx context.Context, runID pgtype.UUID) ([]dbsqlc.OrchestrationTaskDependency, error)
	ListCurrentOrchestrationTaskResultsByRun(ctx context.Context, runID pgtype.UUID) ([]dbsqlc.OrchestrationTaskResult, error)
	ListCurrentOrchestrationTaskVerificationsByRun(ctx context.Context, runID pgtype.UUID) ([]dbsqlc.OrchestrationTaskVerification, error)
	ListCurrentOrchestrationTasksByRun(ctx context.Context, runID pgtype.UUID) ([]dbsqlc.OrchestrationTask, error)
	ListDependencyBlockedOrchestrationTasks(ctx context.Context) ([]dbsqlc.OrchestrationTask, error)
	ListExpiredOrchestrationTaskAttempts(ctx context.Context) ([]dbsqlc.OrchestrationTaskAttempt, error)
	ListExpiredOrchestrationTaskVerifications(ctx context.Context) ([]dbsqlc.OrchestrationTaskVerification, error)
	ListOrchestrationArtifactsByTask(ctx context.Context, taskID pgtype.UUID) ([]dbsqlc.OrchestrationArtifact, error)
	ListOrchestrationRunEvents(ctx context.Context, arg dbsqlc.ListOrchestrationRunEventsParams) ([]dbsqlc.OrchestrationEvent, error)
	ListOrchestrationRunsByBot(ctx context.Context, arg dbsqlc.ListOrchestrationRunsByBotParams) ([]dbsqlc.OrchestrationRun, error)
	ListTerminalizableRunningOrchestrationRuns(ctx context.Context, limitCount int32) ([]dbsqlc.OrchestrationRun, error)
	ListOrchestrationWorkersByIDs(ctx context.Context, ids []string) ([]dbsqlc.OrchestrationWorker, error)
	ListSchedulableOrchestrationTasks(ctx context.Context) ([]dbsqlc.OrchestrationTask, error)
	MarkOrchestrationHumanCheckpointCancelled(ctx context.Context, id pgtype.UUID) (dbsqlc.OrchestrationHumanCheckpoint, error)
	MarkOrchestrationHumanCheckpointSuperseded(ctx context.Context, arg dbsqlc.MarkOrchestrationHumanCheckpointSupersededParams) (dbsqlc.OrchestrationHumanCheckpoint, error)
	MarkOrchestrationRunCancelled(ctx context.Context, arg dbsqlc.MarkOrchestrationRunCancelledParams) (dbsqlc.OrchestrationRun, error)
	MarkOrchestrationRunCancelling(ctx context.Context, id pgtype.UUID) (dbsqlc.OrchestrationRun, error)
	MarkOrchestrationRunCompleted(ctx context.Context, id pgtype.UUID) (dbsqlc.OrchestrationRun, error)
	MarkOrchestrationRunFailed(ctx context.Context, arg dbsqlc.MarkOrchestrationRunFailedParams) (dbsqlc.OrchestrationRun, error)
	MarkOrchestrationRunPlanningActive(ctx context.Context, id pgtype.UUID) (dbsqlc.OrchestrationRun, error)
	MarkOrchestrationRunPlanningIdle(ctx context.Context, id pgtype.UUID) (dbsqlc.OrchestrationRun, error)
	MarkOrchestrationRunRunning(ctx context.Context, id pgtype.UUID) (dbsqlc.OrchestrationRun, error)
	MarkOrchestrationRunWaitingHuman(ctx context.Context, id pgtype.UUID) (dbsqlc.OrchestrationRun, error)
	MarkOrchestrationTaskAttemptBinding(ctx context.Context, arg dbsqlc.MarkOrchestrationTaskAttemptBindingParams) (dbsqlc.OrchestrationTaskAttempt, error)
	MarkOrchestrationTaskAttemptCompleted(ctx context.Context, arg dbsqlc.MarkOrchestrationTaskAttemptCompletedParams) (dbsqlc.OrchestrationTaskAttempt, error)
	MarkOrchestrationTaskAttemptFailed(ctx context.Context, arg dbsqlc.MarkOrchestrationTaskAttemptFailedParams) (dbsqlc.OrchestrationTaskAttempt, error)
	MarkOrchestrationTaskAttemptLost(ctx context.Context, arg dbsqlc.MarkOrchestrationTaskAttemptLostParams) (dbsqlc.OrchestrationTaskAttempt, error)
	MarkOrchestrationTaskAttemptParked(ctx context.Context, arg dbsqlc.MarkOrchestrationTaskAttemptParkedParams) (dbsqlc.OrchestrationTaskAttempt, error)
	MarkOrchestrationTaskAttemptRunning(ctx context.Context, arg dbsqlc.MarkOrchestrationTaskAttemptRunningParams) (dbsqlc.OrchestrationTaskAttempt, error)
	MarkOrchestrationTaskBlocked(ctx context.Context, arg dbsqlc.MarkOrchestrationTaskBlockedParams) (dbsqlc.OrchestrationTask, error)
	MarkOrchestrationTaskCancelled(ctx context.Context, arg dbsqlc.MarkOrchestrationTaskCancelledParams) (dbsqlc.OrchestrationTask, error)
	MarkOrchestrationTaskCompleted(ctx context.Context, arg dbsqlc.MarkOrchestrationTaskCompletedParams) (dbsqlc.OrchestrationTask, error)
	MarkOrchestrationTaskDependencySuperseded(ctx context.Context, arg dbsqlc.MarkOrchestrationTaskDependencySupersededParams) (dbsqlc.OrchestrationTaskDependency, error)
	MarkOrchestrationTaskDispatching(ctx context.Context, id pgtype.UUID) (dbsqlc.OrchestrationTask, error)
	MarkOrchestrationTaskCreatedForResume(ctx context.Context, arg dbsqlc.MarkOrchestrationTaskCreatedForResumeParams) (dbsqlc.OrchestrationTask, error)
	MarkOrchestrationTaskFailed(ctx context.Context, arg dbsqlc.MarkOrchestrationTaskFailedParams) (dbsqlc.OrchestrationTask, error)
	MarkOrchestrationTaskReadyForRetry(ctx context.Context, arg dbsqlc.MarkOrchestrationTaskReadyForRetryParams) (dbsqlc.OrchestrationTask, error)
	MarkOrchestrationTaskReadyFromCheckpoint(ctx context.Context, id pgtype.UUID) (dbsqlc.OrchestrationTask, error)
	MarkOrchestrationTaskRunning(ctx context.Context, id pgtype.UUID) (dbsqlc.OrchestrationTask, error)
	MarkOrchestrationTaskSuperseded(ctx context.Context, arg dbsqlc.MarkOrchestrationTaskSupersededParams) (dbsqlc.OrchestrationTask, error)
	MarkOrchestrationTaskVerificationCompleted(ctx context.Context, arg dbsqlc.MarkOrchestrationTaskVerificationCompletedParams) (dbsqlc.OrchestrationTaskVerification, error)
	MarkOrchestrationTaskVerificationFailed(ctx context.Context, arg dbsqlc.MarkOrchestrationTaskVerificationFailedParams) (dbsqlc.OrchestrationTaskVerification, error)
	MarkOrchestrationTaskVerificationLost(ctx context.Context, arg dbsqlc.MarkOrchestrationTaskVerificationLostParams) (dbsqlc.OrchestrationTaskVerification, error)
	MarkOrchestrationTaskVerificationRunning(ctx context.Context, arg dbsqlc.MarkOrchestrationTaskVerificationRunningParams) (dbsqlc.OrchestrationTaskVerification, error)
	MarkOrchestrationTaskVerifying(ctx context.Context, arg dbsqlc.MarkOrchestrationTaskVerifyingParams) (dbsqlc.OrchestrationTask, error)
	MarkOrchestrationTaskWaitingHuman(ctx context.Context, arg dbsqlc.MarkOrchestrationTaskWaitingHumanParams) (dbsqlc.OrchestrationTask, error)
	PreemptRunningOrchestrationTaskAttemptFailed(ctx context.Context, arg dbsqlc.PreemptRunningOrchestrationTaskAttemptFailedParams) (dbsqlc.OrchestrationTaskAttempt, error)
	ReleaseOrchestrationTaskAttemptClaim(ctx context.Context, arg dbsqlc.ReleaseOrchestrationTaskAttemptClaimParams) (dbsqlc.OrchestrationTaskAttempt, error)
	ReleaseOrchestrationTaskVerificationClaim(ctx context.Context, arg dbsqlc.ReleaseOrchestrationTaskVerificationClaimParams) (dbsqlc.OrchestrationTaskVerification, error)
	RequeueOrchestrationTaskVerification(ctx context.Context, arg dbsqlc.RequeueOrchestrationTaskVerificationParams) (dbsqlc.OrchestrationTaskVerification, error)
	ResolveOrchestrationHumanCheckpoint(ctx context.Context, arg dbsqlc.ResolveOrchestrationHumanCheckpointParams) (dbsqlc.OrchestrationHumanCheckpoint, error)
	RetireCreatedOrchestrationTaskAttemptFailed(ctx context.Context, arg dbsqlc.RetireCreatedOrchestrationTaskAttemptFailedParams) (dbsqlc.OrchestrationTaskAttempt, error)
	RetireOrchestrationTaskAttemptFailed(ctx context.Context, arg dbsqlc.RetireOrchestrationTaskAttemptFailedParams) (dbsqlc.OrchestrationTaskAttempt, error)
	TryCreateOrchestrationIdempotencyRecord(ctx context.Context, arg dbsqlc.TryCreateOrchestrationIdempotencyRecordParams) (dbsqlc.OrchestrationIdempotencyRecord, error)
	UpsertOrchestrationWorker(ctx context.Context, arg dbsqlc.UpsertOrchestrationWorkerParams) (dbsqlc.OrchestrationWorker, error)
}

type postgresStore struct {
	pool    *pgxpool.Pool
	queries *dbsqlc.Queries
}

func NewPostgresStore(pool *pgxpool.Pool, queries *dbsqlc.Queries) Store {
	return postgresStore{pool: pool, queries: queries}
}

func (s postgresStore) BeginTx(ctx context.Context) (Tx, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	return postgresTx{Tx: tx, Queries: s.queries.WithTx(tx)}, nil
}

func (s postgresStore) Queries() Queries { return postgresQueries{s.queries, s.pool} }

type postgresTx struct {
	pgx.Tx
	*dbsqlc.Queries
}

type postgresQueries struct {
	*dbsqlc.Queries
	pool *pgxpool.Pool
}

func (s postgresStore) DatabaseNow(ctx context.Context) (time.Time, error) {
	return postgresDatabaseNow(ctx, s.pool)
}

func (tx postgresTx) DatabaseNow(ctx context.Context) (time.Time, error) {
	return postgresDatabaseNow(ctx, tx.Tx)
}

func (q postgresQueries) DatabaseNow(ctx context.Context) (time.Time, error) {
	return postgresDatabaseNow(ctx, q.pool)
}

func postgresDatabaseNow(
	ctx context.Context,
	db interface {
		QueryRow(context.Context, string, ...any) pgx.Row
	},
) (time.Time, error) {
	var observedAt time.Time
	if err := db.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&observedAt); err != nil {
		return time.Time{}, fmt.Errorf("query database clock: %w", err)
	}
	return observedAt.UTC(), nil
}
