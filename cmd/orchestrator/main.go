package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	agentpkg "github.com/memohai/memoh/internal/agent"
	agenttools "github.com/memohai/memoh/internal/agent/tools"
	"github.com/memohai/memoh/internal/boot"
	"github.com/memohai/memoh/internal/config"
	ctr "github.com/memohai/memoh/internal/container"
	containerprovider "github.com/memohai/memoh/internal/container/provider"
	"github.com/memohai/memoh/internal/db"
	dbsqlc "github.com/memohai/memoh/internal/db/postgres/sqlc"
	postgresstore "github.com/memohai/memoh/internal/db/postgres/store"
	"github.com/memohai/memoh/internal/logger"
	"github.com/memohai/memoh/internal/models"
	"github.com/memohai/memoh/internal/orchestration"
	"github.com/memohai/memoh/internal/orchestrationblackboard"
	"github.com/memohai/memoh/internal/orchestrationbus"
	"github.com/memohai/memoh/internal/orchestrationenv"
	envbrowser "github.com/memohai/memoh/internal/orchestrationenv/backend/browser"
	envcontainer "github.com/memohai/memoh/internal/orchestrationenv/backend/container"
	"github.com/memohai/memoh/internal/orchestrationexec"
	"github.com/memohai/memoh/internal/orchestrationfacts"
	"github.com/memohai/memoh/internal/orchestrationoutbox"
	"github.com/memohai/memoh/internal/settings"
	"github.com/memohai/memoh/internal/workspace"
)

const (
	envReclaimInterval         = 30 * time.Second
	envReclaimMaxRows    int32 = 64
	orchestratorLeaseTTL       = 30
	shutdownTimeout            = 15 * time.Second
)

type containerEnvRuntime interface {
	ctr.ImageService
	ctr.ContainerService
	ctr.WorkloadService
	ctr.SnapshotService
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "orchestrator: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithCancel(signalCtx)
	defer cancel()

	cfg, err := config.Load(os.Getenv("CONFIG_PATH"))
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	logger.Init(cfg.Log.Level, cfg.Log.Format)
	log := logger.L.With(slog.String("component", "orchestrator"))

	pool, err := db.Open(ctx, cfg)
	if err != nil {
		return fmt.Errorf("db connect: %w", err)
	}
	if pool == nil {
		return errors.New("orchestrator requires postgres database driver")
	}
	defer pool.Close()
	queries := dbsqlc.New(pool)
	svc := orchestration.NewService(log, pool, queries)
	leaseTTLSeconds := envInt("ORCHESTRATOR_LEASE_TTL_SECONDS", orchestratorLeaseTTL)
	lease, err := registerOrchestrator(ctx, svc, log, leaseTTLSeconds)
	if err != nil {
		return err
	}

	rc, err := boot.ProvideRuntimeConfig(cfg)
	if err != nil {
		return fmt.Errorf("runtime config: %w", err)
	}
	containerSvc, cleanupContainer, err := containerprovider.ProvideService(ctx, log, cfg, rc.ContainerBackend)
	if err != nil {
		return fmt.Errorf("container service: %w", err)
	}
	if cleanupContainer != nil {
		defer cleanupContainer()
	}
	localSvc := workspace.NewLocalService(log, cfg.Local, cfg.Workspace.DataRoot)
	defer localSvc.Close()
	runtimeSvc := workspace.NewRuntimeRouter(containerSvc, localSvc)
	manager := workspace.NewManager(log, runtimeSvc, nil, cfg.Workspace, cfg.Containerd.Namespace, pool)

	agent := agentpkg.New(agentpkg.Deps{
		BridgeProvider: manager,
		Logger:         log,
	})
	agent.SetToolProviders([]agenttools.ToolProvider{
		agenttools.NewContainerProvider(log, manager, nil, config.DefaultDataMount),
	})
	storeQueries := postgresstore.NewQueries(queries)
	llmRuntime := orchestrationexec.NewRuntime(
		log,
		queries,
		settings.NewService(log, storeQueries, nil, nil),
		models.NewService(log, storeQueries),
		agent,
		rc.TimezoneLocation,
	)
	svc.SetStartRunPlanner(llmRuntime)
	svc.SetReplanner(llmRuntime)

	bus, err := orchestrationbus.New(ctx, log, cfg.NATS)
	if err != nil {
		return fmt.Errorf("orchestration bus: %w", err)
	}
	defer func() { _ = bus.Close() }()
	svc.SetEventBus(bus)

	blackboard, err := orchestrationblackboard.New(ctx, log, orchestrationblackboard.FactoryConfig{
		URL:             cfg.NATS.URL,
		Token:           cfg.NATS.Token,
		User:            cfg.NATS.User,
		Password:        cfg.NATS.Password,
		CredentialsFile: cfg.NATS.CredentialsFile,
		Replicas:        cfg.NATS.EffectiveStreamReplicas(),
		ConnectionName:  "memoh-orchestrator-blackboard",
	})
	if err != nil {
		return fmt.Errorf("orchestration blackboard: %w", err)
	}
	defer func() { _ = blackboard.Close() }()
	svc.SetBlackboardStore(blackboard)

	envManager, err := buildEnvManager(log, cfg, pool, queries, containerSvc)
	if err != nil {
		return err
	}
	if envManager != nil {
		svc.SetEnvManager(orchestrationenv.NewKernelAdapter(envManager))
		if ttl := envLeaseTTLFromEnv(); ttl > 0 {
			svc.SetEnvLeaseTTL(ttl)
		}
	}

	outbox := orchestrationoutbox.New(log, queries, bus, orchestrationoutbox.Config{})
	svc.SetEventCommittedHook(outbox.Notify)
	facts := orchestrationfacts.New(log, queries, bus)

	var wg sync.WaitGroup
	startLoop := func(name string, fn func(context.Context)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			log.Info("orchestration loop started", slog.String("loop", name))
			fn(ctx)
			log.Info("orchestration loop stopped", slog.String("loop", name))
		}()
	}
	startLoop("heartbeat", func(ctx context.Context) {
		runOrchestratorHeartbeatLoop(ctx, svc, log, lease.ID, lease.LeaseToken, leaseTTLSeconds, cancel)
	})
	startLoop("planner", svc.RunPlannerLoop)
	startLoop("scheduler", svc.RunSchedulerLoop)
	startLoop("recovery", svc.RunRecoveryLoop)
	startLoop("verification_recovery", svc.RunVerificationRecoveryLoop)
	startLoop("outbox", outbox.Run)
	startLoop("fact_consumer", func(ctx context.Context) {
		if err := facts.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Warn("orchestration fact consumer exited", slog.Any("error", err))
		}
	})
	if envManager != nil {
		startLoop("env_reclaim", func(ctx context.Context) {
			runEnvReclaimLoop(ctx, log, envManager)
		})
	}

	<-ctx.Done()
	cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		wg.Wait()
	}()
	select {
	case <-done:
		return nil
	case <-time.After(shutdownTimeout):
		return errors.New("timed out waiting for orchestrator loops to stop")
	}
}

func buildEnvManager(log *slog.Logger, cfg config.Config, pool *pgxpool.Pool, queries *dbsqlc.Queries, containerSvc ctr.Service) (*orchestrationenv.Manager, error) {
	registry := orchestrationenv.NewBackendRegistry()
	if rt, ok := containerSvc.(containerEnvRuntime); ok {
		backend, err := envcontainer.New(rt, envcontainer.Options{})
		if err != nil {
			return nil, fmt.Errorf("orchestration env: container backend: %w", err)
		}
		registry.Register(backend)
	} else {
		log.Info("orchestration env: container service does not satisfy env runtime surface; skipping container backend")
	}
	if browserBase := strings.TrimSpace(cfg.BrowserGateway.BaseURL()); browserBase != "" {
		gateway, err := envbrowser.NewHTTPGateway(browserBase, &http.Client{Timeout: 30 * time.Second})
		if err != nil {
			log.Warn("orchestration env: browser gateway not available; skipping browser backend", slog.Any("error", err))
		} else {
			backend, err := envbrowser.New(gateway, envbrowser.Options{})
			if err != nil {
				return nil, fmt.Errorf("orchestration env: browser backend: %w", err)
			}
			registry.Register(backend)
		}
	}
	manager, err := orchestrationenv.NewManager(orchestrationenv.Config{
		Pool:     pool,
		Queries:  queries,
		Backends: registry,
		Logger:   log.With(slog.String("component", "orchestration_env")),
	})
	if err != nil {
		return nil, fmt.Errorf("orchestration env manager: %w", err)
	}
	return manager, nil
}

func runEnvReclaimLoop(ctx context.Context, log *slog.Logger, manager *orchestrationenv.Manager) {
	ticker := time.NewTicker(envReclaimInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			result, err := manager.ReclaimExpiredSessions(ctx, envReclaimMaxRows)
			if err != nil && !errors.Is(err, context.Canceled) {
				log.Warn("orchestration env reclaim sweep failed", slog.Any("error", err))
				continue
			}
			if result.ExpiredSessions > 0 || result.ReleasedBindings > 0 || result.BackendErrors > 0 {
				log.Info("orchestration env reclaim sweep",
					slog.Int("scanned", result.ScannedSessions),
					slog.Int("expired", result.ExpiredSessions),
					slog.Int("released_bindings", result.ReleasedBindings),
					slog.Int("backend_errors", result.BackendErrors),
				)
			}
		}
	}
}

func envLeaseTTLFromEnv() time.Duration {
	raw := strings.TrimSpace(os.Getenv("MEMOH_ORCHESTRATION_ENV_LEASE_TTL_SECONDS"))
	if raw == "" {
		return 0
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

func registerOrchestrator(ctx context.Context, svc *orchestration.Service, log *slog.Logger, leaseTTLSeconds int) (*orchestration.WorkerLease, error) {
	workerID := strings.TrimSpace(os.Getenv("ORCHESTRATOR_ID"))
	if workerID == "" {
		if hostname, err := os.Hostname(); err == nil && strings.TrimSpace(hostname) != "" {
			workerID = "orchestrator-" + strings.TrimSpace(hostname)
		} else {
			workerID = "orchestrator-" + uuid.NewString()
		}
	}
	lease, err := svc.RegisterWorker(ctx, orchestration.WorkerRegistration{
		WorkerID:        workerID,
		ExecutorID:      orchestration.DefaultOrchestratorExecutorID,
		DisplayName:     orchestration.DefaultOrchestratorDisplayName,
		Capabilities:    map[string]any{"control_plane": true},
		LeaseTTLSeconds: leaseTTLSeconds,
	})
	if err != nil {
		return nil, fmt.Errorf("orchestrator registration failed: %w", err)
	}
	log.Info("orchestrator registered", slog.String("worker_id", workerID))
	return lease, nil
}

func runOrchestratorHeartbeatLoop(ctx context.Context, svc *orchestration.Service, log *slog.Logger, workerID, leaseToken string, leaseTTLSeconds int, cancel context.CancelFunc) {
	ticker := time.NewTicker(heartbeatInterval(leaseTTLSeconds))
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := svc.HeartbeatWorker(ctx, workerID, leaseToken, leaseTTLSeconds); err != nil {
				if errors.Is(err, orchestration.ErrWorkerLeaseConflict) {
					log.Error("orchestrator lease lost; stopping control-plane", slog.String("worker_id", workerID))
					cancel()
					return
				}
				log.Warn("orchestrator heartbeat failed", slog.String("worker_id", workerID), slog.Any("error", err))
			}
		}
	}
}

func heartbeatInterval(leaseTTLSeconds int) time.Duration {
	if leaseTTLSeconds <= 2 {
		return time.Second
	}
	return time.Duration(leaseTTLSeconds/2) * time.Second
}

func envInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}
