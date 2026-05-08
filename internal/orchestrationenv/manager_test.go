package orchestrationenv_test

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	dbembed "github.com/memohai/memoh/db"
	"github.com/memohai/memoh/internal/config"
	"github.com/memohai/memoh/internal/db"
	"github.com/memohai/memoh/internal/db/postgres/sqlc"
	orchestrationenv "github.com/memohai/memoh/internal/orchestrationenv"
)

// setupEnvIntegrationManager spins up a dedicated postgres database
// per test, runs the orchestration migrations, and returns a Manager
// wired to a noop container backend so the tests exercise the full
// reserve / commit / hold / resume / release path without depending
// on containerd.
//
// The skip-on-missing-DSN convention matches every other integration
// test in this tree so CI can run unit tests on machines that have no
// postgres available.
func setupEnvIntegrationManager(t *testing.T) (*orchestrationenv.Manager, *pgxpool.Pool, func()) {
	t.Helper()

	dbCfg, err := envIntegrationPostgresConfigFromTestDSN()
	if err != nil {
		t.Skipf("skip integration test: %v", err)
	}

	ctx := context.Background()
	dbName := "memoh_env_integration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	adminCfg := dbCfg
	adminCfg.Database = "postgres"
	adminPool, err := db.OpenPostgres(ctx, adminCfg)
	if err != nil {
		t.Skipf("skip integration test: open admin database: %v", err)
	}
	if _, err := adminPool.Exec(ctx, "CREATE DATABASE "+envIntegrationQuote(dbName)); err != nil {
		adminPool.Close()
		t.Skipf("skip integration test: create database: %v", err)
	}
	adminPool.Close()

	dbCfg.Database = dbName
	if err := envIntegrationMigrate(dbCfg); err != nil {
		envIntegrationDrop(t, adminCfg, dbName)
		t.Fatalf("migrate env integration database: %v", err)
	}

	pool, err := db.OpenPostgres(ctx, dbCfg)
	if err != nil {
		envIntegrationDrop(t, adminCfg, dbName)
		t.Fatalf("open env integration database: %v", err)
	}

	registry := orchestrationenv.NewBackendRegistry()
	registry.Register(orchestrationenv.NewNoopBackend(orchestrationenv.KindContainer))
	registry.Register(orchestrationenv.NewNoopBackend(orchestrationenv.KindBrowser))
	manager, err := orchestrationenv.NewManager(orchestrationenv.Config{
		Pool:     pool,
		Queries:  sqlc.New(pool),
		Backends: registry,
		Logger:   slog.New(slog.DiscardHandler),
	})
	require.NoError(t, err)

	cleanup := func() {
		pool.Close()
		envIntegrationDrop(t, adminCfg, dbName)
	}
	return manager, pool, cleanup
}

func envIntegrationPostgresConfigFromTestDSN() (config.PostgresConfig, error) {
	dsn := strings.TrimSpace(os.Getenv("TEST_POSTGRES_DSN"))
	if dsn == "" {
		return config.PostgresConfig{}, errors.New("TEST_POSTGRES_DSN is not set")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return config.PostgresConfig{}, err
	}
	sslmode := cfg.ConnConfig.RuntimeParams["sslmode"]
	if strings.TrimSpace(sslmode) == "" {
		sslmode = "disable"
	}
	return config.PostgresConfig{
		Host:     cfg.ConnConfig.Host,
		Port:     int(cfg.ConnConfig.Port),
		User:     cfg.ConnConfig.User,
		Password: cfg.ConnConfig.Password,
		Database: cfg.ConnConfig.Database,
		SSLMode:  sslmode,
	}, nil
}

func envIntegrationMigrate(dbCfg config.PostgresConfig) error {
	sub, err := fs.Sub(dbembed.MigrationsFS, "postgres/migrations")
	if err != nil {
		return err
	}
	return db.RunMigrate(slog.New(slog.DiscardHandler), dbCfg, sub, "up", nil)
}

func envIntegrationDrop(t *testing.T, adminCfg config.PostgresConfig, dbName string) {
	t.Helper()
	pool, err := db.OpenPostgres(context.Background(), adminCfg)
	if err != nil {
		t.Fatalf("open admin database for cleanup: %v", err)
	}
	defer pool.Close()
	if _, err := pool.Exec(context.Background(), `
SELECT pg_terminate_backend(pid)
FROM pg_stat_activity
WHERE datname = $1
  AND pid <> pg_backend_pid()
`, dbName); err != nil {
		t.Fatalf("terminate env integration database sessions: %v", err)
	}
	if _, err := pool.Exec(context.Background(), "DROP DATABASE IF EXISTS "+envIntegrationQuote(dbName)); err != nil {
		t.Fatalf("drop env integration database: %v", err)
	}
}

func envIntegrationQuote(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

// seedRunAndTask inserts a minimal orchestration_run + orchestration_task
// pair so binding tests have something to FK against. The two inserts
// run in a single transaction so the deferrable
// orchestration_runs_root_task_fk constraint is satisfied at commit
// time even though the run row references a task that does not yet
// exist when its row is written.
func seedRunAndTask(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string) (runID, taskID, attemptID string) {
	t.Helper()
	runUUID := uuid.New()
	taskUUID := uuid.New()
	attemptUUID := uuid.New()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin seed tx: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
INSERT INTO orchestration_runs
(id, tenant_id, owner_subject, lifecycle_status, planning_status, root_task_id, created_by)
VALUES ($1, $2, 'system', 'created', 'idle', $3, 'system')
`, runUUID, tenantID, taskUUID); err != nil {
		t.Fatalf("seed orchestration_run: %v", err)
	}

	if _, err := tx.Exec(ctx, `
INSERT INTO orchestration_tasks
(id, run_id, status, planner_epoch, kind, goal)
VALUES ($1, $2, 'created', 1, 'agent_task', 'env-test')
`, taskUUID, runUUID); err != nil {
		t.Fatalf("seed orchestration_task: %v", err)
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit seed tx: %v", err)
	}

	return runUUID.String(), taskUUID.String(), attemptUUID.String()
}

// --- tests ---

func TestIntegrationManagerRegisterResource(t *testing.T) {
	manager, _, cleanup := setupEnvIntegrationManager(t)
	defer cleanup()
	ctx := context.Background()

	res, err := manager.RegisterResource(ctx, orchestrationenv.RegisterResourceRequest{
		TenantID: "tenant-A",
		Kind:     orchestrationenv.KindContainer,
		Name:     "alpine-default",
		Capacity: 2,
		Config:   map[string]any{"image": "alpine:3.20"},
		Metadata: map[string]any{"region": "local"},
	})
	require.NoError(t, err)
	require.Equal(t, "alpine-default", res.Name)
	require.Equal(t, orchestrationenv.ResourceStatusActive, res.Status)
	require.Equal(t, "alpine:3.20", res.Config["image"])

	got, err := manager.GetResourceByName(ctx, "tenant-A", "alpine-default")
	require.NoError(t, err)
	require.Equal(t, res.ID, got.ID)

	_, err = manager.RegisterResource(ctx, orchestrationenv.RegisterResourceRequest{
		TenantID: "tenant-A",
		Kind:     orchestrationenv.KindContainer,
		Name:     "alpine-default",
		Capacity: 1,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, orchestrationenv.ErrInvalidArgument)
}

func TestIntegrationManagerAcquireAndRelease(t *testing.T) {
	manager, _, cleanup := setupEnvIntegrationManager(t)
	defer cleanup()
	ctx := context.Background()

	res, err := manager.RegisterResource(ctx, orchestrationenv.RegisterResourceRequest{
		TenantID: "tenant-A",
		Kind:     orchestrationenv.KindContainer,
		Name:     "default",
		Capacity: 1,
	})
	require.NoError(t, err)

	session, err := manager.AcquireSession(ctx, orchestrationenv.AcquireSessionRequest{
		TenantID:        "tenant-A",
		ResourceID:      res.ID,
		LeaseHolderKind: orchestrationenv.LeaseHolderWorker,
		LeaseHolderID:   "worker-1",
		LeaseTTL:        time.Minute,
	})
	require.NoError(t, err)
	require.Equal(t, orchestrationenv.SessionStatusCommitted, session.Status)
	require.NotEmpty(t, session.LeaseToken)
	require.Equal(t, int64(1), session.LeaseEpoch)
	require.Equal(t, "noop", session.RuntimeHandle["backend"])

	// capacity = 1 → second acquire fails fast.
	_, err = manager.AcquireSession(ctx, orchestrationenv.AcquireSessionRequest{
		TenantID:        "tenant-A",
		ResourceID:      res.ID,
		LeaseHolderKind: orchestrationenv.LeaseHolderWorker,
		LeaseHolderID:   "worker-2",
		LeaseTTL:        time.Minute,
	})
	require.ErrorIs(t, err, orchestrationenv.ErrCapacityExceeded)

	require.NoError(t, manager.ReleaseSession(ctx, orchestrationenv.ReleaseSessionRequest{
		SessionID:  session.ID,
		LeaseToken: session.LeaseToken,
		LeaseEpoch: session.LeaseEpoch,
		Reason:     "test_complete",
	}))

	released, err := manager.GetSession(ctx, session.ID)
	require.NoError(t, err)
	require.Equal(t, orchestrationenv.SessionStatusReleased, released.Status)
	require.NotNil(t, released.ReleasedAt)

	// after release the slot frees up.
	again, err := manager.AcquireSession(ctx, orchestrationenv.AcquireSessionRequest{
		TenantID:        "tenant-A",
		ResourceID:      res.ID,
		LeaseHolderKind: orchestrationenv.LeaseHolderWorker,
		LeaseHolderID:   "worker-3",
		LeaseTTL:        time.Minute,
	})
	require.NoError(t, err)
	require.NotEqual(t, session.ID, again.ID)
}

func TestIntegrationManagerStaleLeaseRejected(t *testing.T) {
	manager, _, cleanup := setupEnvIntegrationManager(t)
	defer cleanup()
	ctx := context.Background()

	res, err := manager.RegisterResource(ctx, orchestrationenv.RegisterResourceRequest{
		TenantID: "tenant-A", Kind: orchestrationenv.KindContainer, Name: "r", Capacity: 1,
	})
	require.NoError(t, err)

	session, err := manager.AcquireSession(ctx, orchestrationenv.AcquireSessionRequest{
		TenantID:        "tenant-A",
		ResourceID:      res.ID,
		LeaseHolderKind: orchestrationenv.LeaseHolderWorker,
		LeaseHolderID:   "worker-1",
		LeaseTTL:        time.Minute,
	})
	require.NoError(t, err)

	err = manager.ReleaseSession(ctx, orchestrationenv.ReleaseSessionRequest{
		SessionID:  session.ID,
		LeaseToken: "wrong-token",
		LeaseEpoch: session.LeaseEpoch,
	})
	require.ErrorIs(t, err, orchestrationenv.ErrStaleLease)

	err = manager.ReleaseSession(ctx, orchestrationenv.ReleaseSessionRequest{
		SessionID:  session.ID,
		LeaseToken: session.LeaseToken,
		LeaseEpoch: 999,
	})
	require.ErrorIs(t, err, orchestrationenv.ErrStaleLease)
}

func TestIntegrationManagerHoldAndResumeBumpsEpoch(t *testing.T) {
	manager, pool, cleanup := setupEnvIntegrationManager(t)
	defer cleanup()
	ctx := context.Background()

	runID, taskID, attemptID := seedRunAndTask(t, ctx, pool, "tenant-A")

	res, err := manager.RegisterResource(ctx, orchestrationenv.RegisterResourceRequest{
		TenantID: "tenant-A", Kind: orchestrationenv.KindContainer, Name: "r", Capacity: 1,
	})
	require.NoError(t, err)

	session, err := manager.AcquireSession(ctx, orchestrationenv.AcquireSessionRequest{
		TenantID:        "tenant-A",
		ResourceID:      res.ID,
		LeaseHolderKind: orchestrationenv.LeaseHolderWorker,
		LeaseHolderID:   "worker-1",
		LeaseTTL:        time.Minute,
		RunID:           runID,
		TaskID:          taskID,
		AttemptID:       attemptID,
	})
	require.NoError(t, err)

	binding, err := manager.CreateBinding(ctx, orchestrationenv.CreateBindingRequest{
		SessionID:  session.ID,
		LeaseToken: session.LeaseToken,
		LeaseEpoch: session.LeaseEpoch,
		RunID:      session.RunID,
		TaskID:     session.TaskID,
		AttemptID:  session.AttemptID,
	})
	require.NoError(t, err)
	require.Equal(t, orchestrationenv.BindingStatusActive, binding.Status)

	held, err := manager.HoldBinding(ctx, orchestrationenv.HoldBindingRequest{
		BindingID:  binding.ID,
		LeaseToken: session.LeaseToken,
		LeaseEpoch: session.LeaseEpoch,
		Metadata:   map[string]any{"reason": "hitl"},
	})
	require.NoError(t, err)
	require.Equal(t, orchestrationenv.BindingStatusHeld, held.Status)

	heldSession, err := manager.GetSession(ctx, session.ID)
	require.NoError(t, err)
	require.Equal(t, orchestrationenv.SessionStatusHeld, heldSession.Status)

	newAttempt := uuid.NewString()
	resumed, err := manager.ResumeBinding(ctx, orchestrationenv.ResumeBindingRequest{
		BindingID:        binding.ID,
		NewAttemptID:     newAttempt,
		NewLeaseHolderID: "worker-2",
		LeaseTTL:         time.Minute,
	})
	require.NoError(t, err)
	require.Equal(t, orchestrationenv.BindingStatusActive, resumed.Status)

	resumedSession, err := manager.GetSession(ctx, session.ID)
	require.NoError(t, err)
	require.Equal(t, orchestrationenv.SessionStatusCommitted, resumedSession.Status)
	require.Equal(t, int64(2), resumedSession.LeaseEpoch, "resume must bump the epoch")
	require.NotEqual(t, session.LeaseToken, resumedSession.LeaseToken, "resume must rotate the token")
	require.Equal(t, "worker-2", resumedSession.LeaseHolderID)
	require.Equal(t, newAttempt, resumedSession.AttemptID)

	// stale credentials from the held attempt are now fenced out.
	err = manager.ReleaseSession(ctx, orchestrationenv.ReleaseSessionRequest{
		SessionID:  session.ID,
		LeaseToken: session.LeaseToken,
		LeaseEpoch: session.LeaseEpoch,
	})
	require.ErrorIs(t, err, orchestrationenv.ErrStaleLease)
}

func TestIntegrationManagerCaptureAndListSnapshots(t *testing.T) {
	manager, _, cleanup := setupEnvIntegrationManager(t)
	defer cleanup()
	ctx := context.Background()

	res, err := manager.RegisterResource(ctx, orchestrationenv.RegisterResourceRequest{
		TenantID: "tenant-A", Kind: orchestrationenv.KindContainer, Name: "r", Capacity: 1,
	})
	require.NoError(t, err)

	session, err := manager.AcquireSession(ctx, orchestrationenv.AcquireSessionRequest{
		TenantID:        "tenant-A",
		ResourceID:      res.ID,
		LeaseHolderKind: orchestrationenv.LeaseHolderWorker,
		LeaseHolderID:   "worker-1",
		LeaseTTL:        time.Minute,
	})
	require.NoError(t, err)

	pre, err := manager.CaptureSnapshot(ctx, orchestrationenv.CaptureSnapshotRequest{
		SessionID:   session.ID,
		LeaseToken:  session.LeaseToken,
		LeaseEpoch:  session.LeaseEpoch,
		Kind:        orchestrationenv.SnapshotKindPreAction,
		EffectClass: orchestrationenv.EffectClassEnvLocalMutation,
	})
	require.NoError(t, err)
	require.Equal(t, orchestrationenv.SnapshotKindPreAction, pre.Kind)

	post, err := manager.CaptureSnapshot(ctx, orchestrationenv.CaptureSnapshotRequest{
		SessionID:   session.ID,
		LeaseToken:  session.LeaseToken,
		LeaseEpoch:  session.LeaseEpoch,
		Kind:        orchestrationenv.SnapshotKindPostAction,
		EffectClass: orchestrationenv.EffectClassEnvLocalMutation,
	})
	require.NoError(t, err)
	require.NotEqual(t, pre.ID, post.ID)

	snaps, err := manager.ListSessionSnapshots(ctx, session.ID)
	require.NoError(t, err)
	require.Len(t, snaps, 2)
	require.Equal(t, orchestrationenv.SnapshotKindPreAction, snaps[0].Kind)
	require.Equal(t, orchestrationenv.SnapshotKindPostAction, snaps[1].Kind)
}

func TestIntegrationManagerReclaimExpiredSessions(t *testing.T) {
	manager, _, cleanup := setupEnvIntegrationManager(t)
	defer cleanup()
	ctx := context.Background()

	res, err := manager.RegisterResource(ctx, orchestrationenv.RegisterResourceRequest{
		TenantID: "tenant-A", Kind: orchestrationenv.KindContainer, Name: "r", Capacity: 2,
	})
	require.NoError(t, err)

	short, err := manager.AcquireSession(ctx, orchestrationenv.AcquireSessionRequest{
		TenantID:        "tenant-A",
		ResourceID:      res.ID,
		LeaseHolderKind: orchestrationenv.LeaseHolderWorker,
		LeaseHolderID:   "worker-1",
		LeaseTTL:        10 * time.Millisecond,
	})
	require.NoError(t, err)
	long, err := manager.AcquireSession(ctx, orchestrationenv.AcquireSessionRequest{
		TenantID:        "tenant-A",
		ResourceID:      res.ID,
		LeaseHolderKind: orchestrationenv.LeaseHolderWorker,
		LeaseHolderID:   "worker-2",
		LeaseTTL:        time.Hour,
	})
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	result, err := manager.ReclaimExpiredSessions(ctx, 10)
	require.NoError(t, err)
	require.GreaterOrEqual(t, result.ScannedSessions, 1)
	require.GreaterOrEqual(t, result.ExpiredSessions, 1)

	expired, err := manager.GetSession(ctx, short.ID)
	require.NoError(t, err)
	require.Equal(t, orchestrationenv.SessionStatusExpired, expired.Status)

	stillCommitted, err := manager.GetSession(ctx, long.ID)
	require.NoError(t, err)
	require.Equal(t, orchestrationenv.SessionStatusCommitted, stillCommitted.Status)
}

func TestIntegrationManagerCreateBindingRejectsDuplicate(t *testing.T) {
	manager, pool, cleanup := setupEnvIntegrationManager(t)
	defer cleanup()
	ctx := context.Background()

	runID, taskID, attemptID := seedRunAndTask(t, ctx, pool, "tenant-A")

	res, err := manager.RegisterResource(ctx, orchestrationenv.RegisterResourceRequest{
		TenantID: "tenant-A", Kind: orchestrationenv.KindContainer, Name: "r", Capacity: 1,
	})
	require.NoError(t, err)

	session, err := manager.AcquireSession(ctx, orchestrationenv.AcquireSessionRequest{
		TenantID:        "tenant-A",
		ResourceID:      res.ID,
		LeaseHolderKind: orchestrationenv.LeaseHolderWorker,
		LeaseHolderID:   "worker-1",
		LeaseTTL:        time.Minute,
		RunID:           runID,
		TaskID:          taskID,
		AttemptID:       attemptID,
	})
	require.NoError(t, err)

	_, err = manager.CreateBinding(ctx, orchestrationenv.CreateBindingRequest{
		SessionID:  session.ID,
		LeaseToken: session.LeaseToken,
		LeaseEpoch: session.LeaseEpoch,
		RunID:      session.RunID,
		TaskID:     session.TaskID,
		AttemptID:  session.AttemptID,
	})
	require.NoError(t, err)

	_, err = manager.CreateBinding(ctx, orchestrationenv.CreateBindingRequest{
		SessionID:  session.ID,
		LeaseToken: session.LeaseToken,
		LeaseEpoch: session.LeaseEpoch,
		RunID:      session.RunID,
		TaskID:     session.TaskID,
		AttemptID:  uuid.NewString(),
	})
	require.ErrorIs(t, err, orchestrationenv.ErrInvalidArgument)
}
