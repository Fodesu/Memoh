package orchestration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"golang.org/x/crypto/bcrypt"

	dbembed "github.com/memohai/memoh/db"
	"github.com/memohai/memoh/internal/accounts"
	"github.com/memohai/memoh/internal/auth"
	"github.com/memohai/memoh/internal/bots"
	"github.com/memohai/memoh/internal/config"
	"github.com/memohai/memoh/internal/db"
	"github.com/memohai/memoh/internal/db/postgres/sqlc"
	postgresstore "github.com/memohai/memoh/internal/db/postgres/store"
	"github.com/memohai/memoh/internal/handlers"
	orch "github.com/memohai/memoh/internal/orchestration"
)

type blackboxHarnessOptions struct {
	startScheduler bool
}

type blackboxHarness struct {
	t           *testing.T
	ctx         context.Context
	cancel      context.CancelFunc
	loopWG      sync.WaitGroup
	server      *http.Server
	listener    net.Listener
	serverErrCh chan error
	baseURL     string
	httpClient  *http.Client

	dbName string
	dbCfg  config.PostgresConfig

	appPool  *pgxpool.Pool
	service  *orch.Service
	token    string
	username string
	password string
	secret   string

	plannerOnce   sync.Once
	schedulerOnce sync.Once

	executorOnce sync.Once
}

// #nosec G101 -- fixed test-only JWT secret for local blackbox harness.
const blackboxJWTSecret = "memoh-blackbox-test-secret"

func TestBlackboxRuntimeStartRunDispatchExecuteAndComplete(t *testing.T) {
	h := setupBlackboxHarness(t, blackboxHarnessOptions{
		startScheduler: true,
	})
	defer h.Close()

	h.startAttemptExecutorLoop()

	handle := h.startRun(t, orch.StartRunRequest{
		Goal:           "blackbox happy path run",
		IdempotencyKey: "start-" + uuid.NewString(),
		Input: map[string]any{
			"builtin_workerd": map[string]any{
				"summary": "blackbox happy path completed",
			},
		},
	})

	run := h.waitForRunStatus(t, handle.RunID, orch.LifecycleStatusCompleted, 15*time.Second)
	if run.TerminalReason != "" {
		t.Fatalf("run terminal_reason = %q, want empty", run.TerminalReason)
	}
	task := h.waitForTaskStatus(t, handle.RunID, handle.RootTaskID, orch.TaskStatusCompleted, 5*time.Second)
	if task.LatestResultID == "" {
		t.Fatal("task latest_result_id = empty, want non-empty")
	}
	h.waitForEventType(t, handle.RunID, "run.event.attempt.completed", 5*time.Second)
}

func setupBlackboxHarness(t *testing.T, opts blackboxHarnessOptions) *blackboxHarness {
	t.Helper()

	dbCfg, err := postgresConfigFromTestDSN()
	if err != nil {
		t.Skipf("skip blackbox test: %v", err)
	}

	dbName := "memoh_orch_blackbox_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	adminCfg := dbCfg
	adminCfg.Database = "postgres"
	adminPool, err := openBlackboxPostgres(context.Background(), adminCfg)
	if err != nil {
		t.Skipf("skip blackbox test: open admin db: %v", err)
	}
	if _, err := adminPool.Exec(context.Background(), "CREATE DATABASE "+quoteIdentifier(dbName)); err != nil {
		adminPool.Close()
		t.Skipf("skip blackbox test: create database: %v", err)
	}
	adminPool.Close()

	dbCfg.Database = dbName
	if err := migrateBlackboxDatabase(dbCfg); err != nil {
		dropBlackboxDatabase(t, adminCfg, dbName)
		t.Fatalf("migrate blackbox database: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	appPool, err := openBlackboxPostgres(ctx, dbCfg)
	if err != nil {
		cancel()
		dropBlackboxDatabase(t, adminCfg, dbName)
		t.Fatalf("open app db: %v", err)
	}
	queries := sqlc.New(appPool)
	queryStore := postgresstore.NewQueries(queries)
	accountStore := postgresstore.NewWithQueries(queries)
	createBlackboxAdminUser(t, queries, "admin", "admin123", "test@memoh.local")

	logger := slog.New(slog.DiscardHandler)
	service := orch.NewService(logger, orch.NewPostgresStore(appPool, queries))
	botService := bots.NewService(logger, queryStore)

	listenCfg := net.ListenConfig{}
	listener, err := listenCfg.Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		cancel()
		appPool.Close()
		dropBlackboxDatabase(t, adminCfg, dbName)
		t.Fatalf("listen blackbox server: %v", err)
	}

	e := echo.New()
	e.HideBanner = true
	e.Use(middleware.Recover())
	e.Use(auth.JWTMiddleware(blackboxJWTSecret, func(c echo.Context) bool {
		path := c.Request().URL.Path
		return path == "/auth/login" || path == "/ping"
	}))
	e.GET("/ping", func(c echo.Context) error { return c.String(http.StatusOK, "ok") })
	handlers.NewAuthHandler(logger, accounts.NewService(logger, accountStore), blackboxJWTSecret, 24*time.Hour).Register(e)
	handlers.NewOrchestrationHandler(logger, service, botService).Register(e)

	serverErrCh := make(chan error, 1)
	server := &http.Server{
		Handler:           e,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		err := server.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrCh <- err
			return
		}
		close(serverErrCh)
	}()

	h := &blackboxHarness{
		t:           t,
		ctx:         ctx,
		cancel:      cancel,
		server:      server,
		listener:    listener,
		serverErrCh: serverErrCh,
		baseURL:     "http://" + listener.Addr().String(),
		httpClient:  &http.Client{Timeout: 5 * time.Second},
		dbName:      dbName,
		dbCfg:       dbCfg,
		appPool:     appPool,
		service:     service,
		username:    "admin",
		password:    "admin123",
		secret:      blackboxJWTSecret,
	}
	h.startPlannerLoop()
	if opts.startScheduler {
		h.startSchedulerLoop()
	}
	h.waitForPing(t)
	h.token = h.login(t)
	return h
}

func (h *blackboxHarness) Close() {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = h.server.Shutdown(shutdownCtx)
	h.cancel()
	done := make(chan struct{})
	go func() {
		h.loopWG.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		h.t.Fatalf("runtime loops did not stop before timeout")
	}
	h.appPool.Close()
	dropBlackboxDatabase(h.t, config.PostgresConfig{
		Host:     h.dbCfg.Host,
		Port:     h.dbCfg.Port,
		User:     h.dbCfg.User,
		Password: h.dbCfg.Password,
		Database: "postgres",
		SSLMode:  h.dbCfg.SSLMode,
	}, h.dbName)
}

func (h *blackboxHarness) waitForPing(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		req, reqErr := http.NewRequestWithContext(h.ctx, http.MethodGet, h.baseURL+"/ping", nil)
		if reqErr != nil {
			t.Fatalf("build ping request: %v", reqErr)
		}
		// #nosec G704 -- blackbox harness targets its own local test server via h.baseURL.
		resp, err := h.httpClient.Do(req)
		if err == nil {
			if closeErr := resp.Body.Close(); closeErr != nil {
				t.Fatalf("close ping response body: %v", closeErr)
			}
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		select {
		case err := <-h.serverErrCh:
			if err != nil {
				t.Fatalf("blackbox server exited early: %v", err)
			}
		default:
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("blackbox server did not become ready")
}

func (h *blackboxHarness) startPlannerLoop() {
	h.plannerOnce.Do(func() {
		h.loopWG.Add(1)
		go func() {
			defer h.loopWG.Done()
			h.service.RunPlannerLoop(h.ctx)
		}()
	})
}

func (h *blackboxHarness) startSchedulerLoop() {
	h.schedulerOnce.Do(func() {
		h.loopWG.Add(1)
		go func() {
			defer h.loopWG.Done()
			h.service.RunSchedulerLoop(h.ctx)
		}()
	})
}

func (h *blackboxHarness) startAttemptExecutorLoop() {
	h.executorOnce.Do(func() {
		h.service.SetAttemptExecutorFunc(func(_ context.Context, attempt orch.TaskAttempt) orch.AttemptCompletion {
			return orch.AttemptCompletion{
				AttemptID:        attempt.ID,
				ClaimToken:       attempt.ClaimToken,
				Status:           orch.TaskAttemptStatusCompleted,
				Summary:          "blackbox happy path completed",
				StructuredOutput: map[string]any{"attempt_id": attempt.ID},
			}
		})
		h.loopWG.Add(1)
		go func() {
			defer h.loopWG.Done()
			h.service.RunAttemptExecutorLoop(h.ctx)
		}()
	})
}

func (h *blackboxHarness) login(t *testing.T) string {
	t.Helper()
	body := handlers.LoginRequest{
		Username: h.username,
		Password: h.password,
	}
	var resp handlers.LoginResponse
	h.mustJSON(t, http.MethodPost, "/auth/login", body, "", &resp, http.StatusOK)
	if strings.TrimSpace(resp.AccessToken) == "" {
		t.Fatal("login token = empty")
	}
	return resp.AccessToken
}

func (h *blackboxHarness) startRun(t *testing.T, req orch.StartRunRequest) orch.RunHandle {
	t.Helper()
	var handle orch.RunHandle
	h.mustJSON(t, http.MethodPost, "/orchestration/runs", req, h.token, &handle, http.StatusCreated)
	if handle.RunID == "" || handle.RootTaskID == "" {
		t.Fatalf("invalid run handle: %+v", handle)
	}
	return handle
}

func (h *blackboxHarness) listTasks(t *testing.T, runID string) orch.TaskPage {
	t.Helper()
	var page orch.TaskPage
	h.mustJSON(t, http.MethodGet, "/orchestration/runs/"+runID+"/tasks?limit=100", nil, h.token, &page, http.StatusOK)
	return page
}

func (h *blackboxHarness) getSnapshot(t *testing.T, runID string) orch.RunSnapshot {
	t.Helper()
	var snapshot orch.RunSnapshot
	h.mustJSON(t, http.MethodGet, "/orchestration/runs/"+runID+"/snapshot", nil, h.token, &snapshot, http.StatusOK)
	return snapshot
}

func (h *blackboxHarness) listEvents(t *testing.T, runID string) orch.RunEventPage {
	t.Helper()
	var page orch.RunEventPage
	h.mustJSON(t, http.MethodGet, "/orchestration/runs/"+runID+"/events?limit=500", nil, h.token, &page, http.StatusOK)
	return page
}

func (h *blackboxHarness) waitForRunStatus(t *testing.T, runID, status string, timeout time.Duration) orch.Run {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		snapshot := h.getSnapshot(t, runID)
		if snapshot.Run.LifecycleStatus == status {
			return snapshot.Run
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("run %s did not reach status %q within %s", runID, status, timeout)
	return orch.Run{}
}

func (h *blackboxHarness) waitForTaskStatus(t *testing.T, runID, taskID, status string, timeout time.Duration) orch.Task {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		page := h.listTasks(t, runID)
		for _, task := range page.Items {
			if task.ID == taskID && task.Status == status {
				return task
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("task %s did not reach status %q within %s", taskID, status, timeout)
	return orch.Task{}
}

func (h *blackboxHarness) waitForEventType(t *testing.T, runID, eventType string, timeout time.Duration) orch.RunEvent {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		page := h.listEvents(t, runID)
		for _, event := range page.Items {
			if event.Type == eventType {
				return event
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("run %s did not emit event %q within %s", runID, eventType, timeout)
	return orch.RunEvent{}
}

func (h *blackboxHarness) mustJSON(t *testing.T, method, path string, payload any, token string, dest any, wantStatus int) {
	t.Helper()
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal %s %s payload: %v", method, path, err)
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(h.ctx, method, h.baseURL+path, body)
	if err != nil {
		t.Fatalf("build %s %s request: %v", method, path, err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if strings.TrimSpace(token) != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	// #nosec G704 -- blackbox harness issues requests only to its own local test server.
	resp, err := h.httpClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s request: %v", method, path, err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Fatalf("close %s %s response body: %v", method, path, closeErr)
		}
	}()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s %s response: %v", method, path, err)
	}
	if resp.StatusCode != wantStatus {
		t.Fatalf("%s %s status = %d, want %d, body = %s", method, path, resp.StatusCode, wantStatus, strings.TrimSpace(string(raw)))
	}
	if dest == nil || len(raw) == 0 {
		return
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		t.Fatalf("decode %s %s response: %v; body=%s", method, path, err, strings.TrimSpace(string(raw)))
	}
}

func postgresConfigFromTestDSN() (config.PostgresConfig, error) {
	dsn := strings.TrimSpace(os.Getenv("TEST_POSTGRES_DSN"))
	if dsn == "" {
		return config.PostgresConfig{}, errors.New("TEST_POSTGRES_DSN is not set")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return config.PostgresConfig{}, fmt.Errorf("parse TEST_POSTGRES_DSN: %w", err)
	}
	return config.PostgresConfig{
		Host:     cfg.ConnConfig.Host,
		Port:     int(cfg.ConnConfig.Port),
		User:     cfg.ConnConfig.User,
		Password: cfg.ConnConfig.Password,
		Database: cfg.ConnConfig.Database,
		SSLMode:  chooseString(cfg.ConnConfig.RuntimeParams["sslmode"], "disable"),
	}, nil
}

func migrateBlackboxDatabase(dbCfg config.PostgresConfig) error {
	sub, err := fs.Sub(dbembed.MigrationsFS, "migrations")
	if err != nil {
		return err
	}
	return db.RunMigrate(slog.New(slog.DiscardHandler), dbCfg, sub, "up", nil)
}

func openBlackboxPostgres(ctx context.Context, dbCfg config.PostgresConfig) (*pgxpool.Pool, error) {
	return db.Open(ctx, config.Config{
		Database: config.DatabaseConfig{Driver: string(db.DriverPostgres)},
		Postgres: dbCfg,
	})
}

func createBlackboxAdminUser(t *testing.T, queries *sqlc.Queries, username, password, email string) {
	t.Helper()
	ctx := context.Background()
	count, err := queries.CountAccounts(ctx)
	if err != nil {
		t.Fatalf("CountAccounts() error = %v", err)
	}
	if count > 0 {
		return
	}
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash admin password: %v", err)
	}
	user, err := queries.CreateUser(ctx, sqlc.CreateUserParams{
		IsActive: true,
		Metadata: []byte("{}"),
	})
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
	_, err = queries.CreateAccount(ctx, sqlc.CreateAccountParams{
		UserID:       user.ID,
		Username:     pgtype.Text{String: username, Valid: true},
		Email:        pgtype.Text{String: email, Valid: true},
		PasswordHash: pgtype.Text{String: string(hashed), Valid: true},
		Role:         "admin",
		DisplayName:  pgtype.Text{String: username, Valid: true},
		AvatarUrl:    pgtype.Text{},
		IsActive:     true,
		DataRoot:     pgtype.Text{String: "data", Valid: true},
	})
	if err != nil {
		t.Fatalf("CreateAccount() error = %v", err)
	}
}

func dropBlackboxDatabase(t *testing.T, adminCfg config.PostgresConfig, dbName string) {
	t.Helper()
	pool, err := openBlackboxPostgres(context.Background(), adminCfg)
	if err != nil {
		t.Fatalf("open admin db for cleanup: %v", err)
	}
	defer pool.Close()
	if _, err := pool.Exec(context.Background(), `
SELECT pg_terminate_backend(pid)
FROM pg_stat_activity
WHERE datname = $1
  AND pid <> pg_backend_pid()
`, dbName); err != nil {
		t.Fatalf("terminate blackbox db sessions: %v", err)
	}
	if _, err := pool.Exec(context.Background(), "DROP DATABASE IF EXISTS "+quoteIdentifier(dbName)); err != nil {
		t.Fatalf("drop blackbox database: %v", err)
	}
}

func quoteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func chooseString(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}
