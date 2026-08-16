//go:build integration

package db_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	memohdb "github.com/memohai/memoh/internal/db"
	postgresstore "github.com/memohai/memoh/internal/db/postgres/store"
)

const (
	packageTeamID   = "00000000-0000-0000-0000-000000000001"
	packageUserID   = "10000000-0000-4000-8000-000000000001"
	packageBotOneID = "20000000-0000-4000-8000-000000000001"
	packageRevision = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

func TestRemoteRuntimeDeletionRespectsPackageInstallation(t *testing.T) {
	ctx := context.Background()
	pool := teamScopedPool(t)
	seedPackageBot(t, pool)
	targetID := seedRemoteRuntimeTarget(t, pool)

	if _, err := pool.Exec(ctx, `
		INSERT INTO bot_skill_package_installations
			(id, bot_id, workspace_target_id, registry_id, package_id, revision)
		VALUES ($1, $2, $3, 'openai', 'documents', $4)`,
		"30000000-0000-4000-8000-000000000001", packageBotOneID, targetID, packageRevision); err != nil {
		t.Fatalf("seed Package: %v", err)
	}

	store, err := postgresstore.New(pool)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	if err := store.DeleteMount(ctx, packageBotOneID, targetID); !errors.Is(err, memohdb.ErrNotFound) {
		t.Fatalf("DeleteMount() with installed Package error = %v, want deletion to be rejected", err)
	}

	if _, err := pool.Exec(ctx, `
		DELETE FROM bot_skill_package_installations
		WHERE bot_id = $1 AND workspace_target_id = $2`, packageBotOneID, targetID); err != nil {
		t.Fatalf("remove Package: %v", err)
	}
	if err := store.DeleteMount(ctx, packageBotOneID, targetID); err != nil {
		t.Fatalf("DeleteMount() after Package removal: %v", err)
	}
}

func seedRemoteRuntimeTarget(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	const runtimeID = "50000000-0000-4000-8000-000000000001"
	const targetID = "60000000-0000-4000-8000-000000000001"
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO user_runtimes (id, user_id, name, api_token) VALUES ($1, $2, 'package-runtime', 'token')`, runtimeID, packageUserID); err != nil {
		t.Fatalf("seed Remote Runtime: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO bot_remote_runtime_bindings (id, bot_id, runtime_id) VALUES ($1, $2, $3)`, targetID, packageBotOneID, runtimeID); err != nil {
		t.Fatalf("seed target: %v", err)
	}
	return targetID
}

func teamScopedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	_ = freshMigratedDB(t)
	cfg, err := pgxpool.ParseConfig(teamMigrationDSN(t))
	if err != nil {
		t.Fatalf("parse test DSN: %v", err)
	}
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, `SELECT set_config('memoh.team_id', $1, false)`, packageTeamID)
		return err
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("create team pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func seedPackageBot(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO users (id, username) VALUES ($1, 'package-owner')`, []any{packageUserID}},
		{`INSERT INTO team_members (team_id, user_id) VALUES ($1, $2)`, []any{packageTeamID, packageUserID}},
		{`INSERT INTO bots (id, team_id, owner_user_id, name) VALUES ($3, $2, $1, 'bot-one')`, []any{packageUserID, packageTeamID, packageBotOneID}},
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seed Package fixture: %v", err)
		}
	}
}
