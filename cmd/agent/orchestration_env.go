package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/fx"

	"github.com/memohai/memoh/internal/config"
	ctr "github.com/memohai/memoh/internal/container"
	dbsqlc "github.com/memohai/memoh/internal/db/postgres/sqlc"
	"github.com/memohai/memoh/internal/orchestration"
	"github.com/memohai/memoh/internal/orchestrationenv"
	envbrowser "github.com/memohai/memoh/internal/orchestrationenv/backend/browser"
	envcontainer "github.com/memohai/memoh/internal/orchestrationenv/backend/container"
)

// containerEnvRuntime is the wider container surface the env container
// backend expects. The kernel-facing ctr.Service interface intentionally
// omits image-pull and snapshot operations; we narrow the original concrete
// service to this combined interface via a runtime type assertion so the env
// backend gets a Runtime without us forking the container abstraction.
type containerEnvRuntime interface {
	ctr.ImageService
	ctr.ContainerService
	ctr.WorkloadService
	ctr.SnapshotService
}

// envReclaimInterval bounds how often the reclaim sweep runs. Sessions
// already expire on their lease TTL; the loop exists to drain orphaned
// rows when a worker crashes between dispatch and release. Half a minute is
// the same cadence the verification recovery loop uses.
const envReclaimInterval = 30 * time.Second

// envReclaimMaxRowsPerSweep keeps a single sweep bounded so a backlog of
// expired sessions does not pin a Postgres connection forever. Producing
// fresh batches per tick is cheaper than holding a long transaction open.
const envReclaimMaxRowsPerSweep int32 = 64

// provideOrchestrationEnvBackends builds a backend registry populated with
// the kinds the deployment can actually serve. A missing container service
// or browser gateway simply omits its backend; the kernel will then refuse
// dispatches that demand the unavailable kind, surfacing the misconfiguration
// at runtime instead of silently allocating against the wrong runtime.
func provideOrchestrationEnvBackends(log *slog.Logger, cfg config.Config, containerService ctr.Service) (*orchestrationenv.BackendRegistry, error) {
	registry := orchestrationenv.NewBackendRegistry()

	if rt, ok := containerService.(containerEnvRuntime); ok {
		backend, err := envcontainer.New(rt, envcontainer.Options{})
		if err != nil {
			return nil, fmt.Errorf("orchestration env: container backend: %w", err)
		}
		registry.Register(backend)
	} else {
		log.Info("orchestration env: container service does not satisfy the env runtime surface; skipping container backend")
	}

	browserBase := strings.TrimSpace(cfg.BrowserGateway.BaseURL())
	if browserBase != "" {
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
	return registry, nil
}

// provideOrchestrationEnvManager constructs the durable env manager. It
// returns nil when Postgres is unavailable so the FX graph still wires up
// in single-process / SQLite test deployments where env-bound tasks are
// not exercised.
func provideOrchestrationEnvManager(log *slog.Logger, pool *pgxpool.Pool, queries *dbsqlc.Queries, registry *orchestrationenv.BackendRegistry) (*orchestrationenv.Manager, error) {
	if pool == nil || queries == nil {
		log.Info("orchestration env: postgres pool not configured; env manager disabled")
		return nil, nil
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

// provideOrchestrationEnvKernelAdapter exposes the manager via the primitive
// orchestration.EnvManager interface the kernel consumes. Returning a
// dedicated adapter type (rather than the bare manager) keeps the kernel
// import-clean — the kernel never has to know orchestrationenv exists.
func provideOrchestrationEnvKernelAdapter(manager *orchestrationenv.Manager) *orchestrationenv.KernelAdapter {
	if manager == nil {
		return nil
	}
	return orchestrationenv.NewKernelAdapter(manager)
}

// wireOrchestrationEnvManager hands the env manager adapter to the
// orchestration kernel and applies the configured lease TTL. We register
// the adapter even when the manager is nil-checked away so tests that
// re-use the FX module without env support still wire a concrete service.
func wireOrchestrationEnvManager(orchestrationService *orchestration.Service, adapter *orchestrationenv.KernelAdapter) {
	if orchestrationService == nil || adapter == nil {
		return
	}
	orchestrationService.SetEnvManager(adapter)
	if ttl := envLeaseTTLFromEnv(); ttl > 0 {
		orchestrationService.SetEnvLeaseTTL(ttl)
	}
}

// startOrchestrationEnvReclaim runs the reclaim sweep on a steady cadence
// for the process lifetime. Reclaim is the safety net for sessions whose
// holder crashed between dispatch and release; lease TTL and the per-call
// release path remain the primary cleanup paths.
func startOrchestrationEnvReclaim(lc fx.Lifecycle, log *slog.Logger, manager *orchestrationenv.Manager) {
	if manager == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	lc.Append(fx.Hook{
		OnStart: func(_ context.Context) error {
			go runEnvReclaimLoop(ctx, log, manager, done)
			return nil
		},
		OnStop: func(stopCtx context.Context) error {
			cancel()
			select {
			case <-done:
				return nil
			case <-stopCtx.Done():
				log.Warn("orchestration env reclaim loop did not stop in time", slog.Any("error", stopCtx.Err()))
				return stopCtx.Err()
			}
		},
	})
}

func runEnvReclaimLoop(ctx context.Context, log *slog.Logger, manager *orchestrationenv.Manager, done chan struct{}) {
	defer close(done)
	ticker := time.NewTicker(envReclaimInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			result, err := manager.ReclaimExpiredSessions(ctx, envReclaimMaxRowsPerSweep)
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

// envLeaseTTLFromEnv reads MEMOH_ORCHESTRATION_ENV_LEASE_TTL_SECONDS so
// operators can override the default without rebuilding. Invalid values
// fall back to whatever orchestration.Service has already configured.
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
