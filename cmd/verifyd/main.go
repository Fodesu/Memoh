package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	agentpkg "github.com/memohai/memoh/internal/agent"
	agenttools "github.com/memohai/memoh/internal/agent/tools"
	"github.com/memohai/memoh/internal/boot"
	"github.com/memohai/memoh/internal/config"
	containerprovider "github.com/memohai/memoh/internal/container/provider"
	"github.com/memohai/memoh/internal/db"
	dbsqlc "github.com/memohai/memoh/internal/db/postgres/sqlc"
	postgresstore "github.com/memohai/memoh/internal/db/postgres/store"
	"github.com/memohai/memoh/internal/logger"
	"github.com/memohai/memoh/internal/models"
	"github.com/memohai/memoh/internal/orchestration"
	"github.com/memohai/memoh/internal/orchestrationbus"
	"github.com/memohai/memoh/internal/orchestrationexec"
	"github.com/memohai/memoh/internal/settings"
	"github.com/memohai/memoh/internal/workspace"
)

type verificationExecutor = orchestration.VerificationExecutor

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "verifyd: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithCancel(signalCtx)
	defer cancel()

	cfgPath := os.Getenv("CONFIG_PATH")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	logger.Init(cfg.Log.Level, cfg.Log.Format)
	log := logger.L.With(slog.String("component", "verifyd"))

	pool, err := db.Open(ctx, cfg)
	if err != nil {
		return fmt.Errorf("db connect: %w", err)
	}
	defer pool.Close()

	queries := dbsqlc.New(pool)
	svc := orchestration.NewService(log, pool, queries)
	var executor orchestration.VerificationWorkExecutor = svc
	var workerLeases orchestration.WorkerLeaseRuntime = svc

	bus, err := orchestrationbus.New(ctx, log, cfg.NATS)
	if err != nil {
		log.Warn("orchestration bus unavailable, verification facts will not be published", slog.Any("error", err))
		bus = orchestrationbus.NewInMemoryBus(0)
	}
	defer func() { _ = bus.Close() }()
	facts := newVerificationFactEmitter(log, bus)
	llmRuntime, cleanupLLMRuntime, err := buildLLMRuntime(ctx, log, cfg, pool, queries)
	if err != nil {
		log.Warn("llm runtime unavailable, verifier will use builtin fallback", slog.Any("error", err))
	}
	if cleanupLLMRuntime != nil {
		defer cleanupLLMRuntime()
	}

	workerID := strings.TrimSpace(os.Getenv("VERIFIER_ID"))
	if workerID == "" {
		workerID = "verifyd-" + uuid.NewString()
	}
	executorID := strings.TrimSpace(os.Getenv("VERIFIER_EXECUTOR_ID"))
	if executorID == "" {
		executorID = orchestration.DefaultVerifierExecutorID
	}
	verifierProfiles := orchestration.NormalizeExecutionProfiles(envCSV("VERIFIER_PROFILES", []string{orchestration.DefaultVerifierProfile}))
	leaseTTLSeconds := envInt("VERIFIER_LEASE_TTL_SECONDS", 30)
	pollInterval := time.Duration(envInt("VERIFIER_POLL_INTERVAL_MS", 500)) * time.Millisecond

	workerLease, err := svc.RegisterWorker(ctx, orchestration.WorkerRegistration{
		WorkerID:        workerID,
		ExecutorID:      executorID,
		DisplayName:     workerID,
		Capabilities:    orchestration.VerifierProfileCapabilities(verifierProfiles),
		LeaseTTLSeconds: leaseTTLSeconds,
	})
	if err != nil {
		return fmt.Errorf("verifier registration failed: %w", err)
	}
	workerLeaseToken := workerLease.LeaseToken

	go runWorkerHeartbeatLoop(ctx, workerLeases, log, workerID, workerLeaseToken, leaseTTLSeconds, cancel)

	for {
		if ctx.Err() != nil {
			return nil
		}

		verification, err := executor.ClaimNextVerification(ctx, orchestration.VerificationClaim{
			WorkerID:         workerID,
			ExecutorID:       executorID,
			VerifierProfiles: verifierProfiles,
			LeaseToken:       workerLeaseToken,
			LeaseTTLSeconds:  leaseTTLSeconds,
		})
		if err != nil {
			if errors.Is(err, orchestration.ErrWorkerLeaseConflict) {
				log.Error("worker lease lost; stopping verifier", slog.String("worker_id", workerID))
				return nil
			}
			if errors.Is(err, orchestration.ErrNoRunnableVerification) {
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(pollInterval):
					continue
				}
			}
			log.Error("claim verification failed", slog.Any("error", err))
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(pollInterval):
				continue
			}
		}

		facts.emit(ctx, *verification, "verification.claimed", map[string]any{
			"executor_id":       executorID,
			"worker_id":         workerID,
			"verifier_profiles": verifierProfiles,
		})

		if err := sleepWithContext(ctx, time.Duration(envInt("VERIFIER_START_DELAY_MS", 0))*time.Millisecond); err != nil {
			return nil
		}
		claimedFence := orchestration.NewVerificationFence(*verification)
		runningVerification, err := executor.StartClaimedVerification(ctx, claimedFence)
		if err != nil {
			log.Error("start verification failed", slog.String("verification_id", verification.ID), slog.Any("error", err))
			facts.emit(ctx, *verification, "verification.start_failed", map[string]any{
				"error": err.Error(),
			})
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(pollInterval):
				continue
			}
		}

		facts.emit(ctx, *runningVerification, "verification.started", nil)

		leaseLost := runVerification(ctx, executor, log, *runningVerification, leaseTTLSeconds, verifierProfiles, func(execCtx context.Context, verification orchestration.TaskVerification, profiles []string) orchestration.VerificationCompletion {
			completion := executeVerification(execCtx, queries, llmRuntime, verification, profiles)
			facts.emitCompletion(execCtx, verification, completion)
			return completion
		})
		if leaseLost {
			log.Warn("dropping stale verification completion after lease loss", slog.String("verification_id", runningVerification.ID))
			continue
		}
		if ctx.Err() != nil {
			return nil
		}
	}
}

func runVerification(ctx context.Context, svc orchestration.ClaimedVerificationRuntime, log *slog.Logger, verification orchestration.TaskVerification, leaseTTLSeconds int, verifierProfiles []string, execute verificationExecutor) bool {
	return orchestration.RunClaimedVerification(ctx, svc, log, verification, leaseTTLSeconds, verifierProfiles, execute)
}

func runVerificationWithInterval(ctx context.Context, svc orchestration.ClaimedVerificationRuntime, log *slog.Logger, verification orchestration.TaskVerification, leaseTTLSeconds int, heartbeatEvery time.Duration, verifierProfiles []string, execute verificationExecutor) bool {
	if heartbeatEvery <= 0 {
		return orchestration.RunClaimedVerification(ctx, svc, log, verification, leaseTTLSeconds, verifierProfiles, execute)
	}
	return orchestration.RunClaimedVerificationWithInterval(ctx, svc, log, verification, leaseTTLSeconds, heartbeatEvery, verifierProfiles, execute)
}

func runWorkerHeartbeatLoop(ctx context.Context, svc orchestration.WorkerLeaseRuntime, log *slog.Logger, workerID, leaseToken string, leaseTTLSeconds int, cancel context.CancelFunc) {
	ticker := time.NewTicker(heartbeatInterval(leaseTTLSeconds))
	defer ticker.Stop()
	consecutiveFailures := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := svc.HeartbeatWorker(ctx, workerID, leaseToken, leaseTTLSeconds); err != nil {
				log.Warn("worker heartbeat failed", slog.String("worker_id", workerID), slog.Any("error", err))
				if errors.Is(err, orchestration.ErrWorkerLeaseConflict) {
					log.Error("worker lease conflict detected; stopping verifier", slog.String("worker_id", workerID))
					cancel()
					return
				}
				consecutiveFailures++
				if consecutiveFailures >= 3 {
					log.Error("worker heartbeat failed repeatedly; stopping verifier", slog.String("worker_id", workerID))
					cancel()
					return
				}
				continue
			}
			consecutiveFailures = 0
		}
	}
}

func executeVerification(ctx context.Context, queries *dbsqlc.Queries, llmRuntime *orchestrationexec.Runtime, verification orchestration.TaskVerification, _ []string) orchestration.VerificationCompletion {
	if err := sleepWithContext(ctx, time.Duration(envInt("VERIFIER_EXECUTION_DELAY_MS", 0))*time.Millisecond); err != nil {
		return workerShutdownVerificationCompletion(verification)
	}

	if strings.TrimSpace(verification.VerifierProfile) == orchestration.BuiltinBasicVerifierProfile {
		return orchestration.ExecuteBuiltinVerification(ctx, queries, verification)
	}

	runID, err := db.ParseUUID(verification.RunID)
	if err != nil {
		return llmVerificationUnavailable(verification, "invalid_run_id", err.Error())
	}
	runRow, err := queries.GetOrchestrationRunByID(ctx, runID)
	if err != nil {
		return llmVerificationUnavailable(verification, "run_lookup_failed", err.Error())
	}
	sourceMetadata := decodeObject(runRow.SourceMetadata)
	botID := strings.TrimSpace(stringValue(sourceMetadata["bot_id"]))
	if llmRuntime != nil && botID != "" {
		return llmRuntime.ExecuteVerification(ctx, verification)
	}
	if botID == "" {
		return llmVerificationUnavailable(verification, "llm_runtime_unavailable", "llm verifier profile requires run source_metadata.bot_id")
	}
	return llmVerificationUnavailable(verification, "llm_runtime_unavailable", "llm runtime is not configured")
}

func llmVerificationUnavailable(verification orchestration.TaskVerification, failureClass, reason string) orchestration.VerificationCompletion {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "llm verification runtime is not available"
	}
	return orchestration.VerificationCompletion{
		VerificationID: verification.ID,
		ClaimToken:     verification.ClaimToken,
		Status:         orchestration.TaskVerificationStatusFailed,
		Verdict:        orchestration.VerificationVerdictRejected,
		Summary:        reason,
		FailureClass:   failureClass,
		TerminalReason: reason,
	}
}

func heartbeatInterval(leaseTTLSeconds int) time.Duration {
	ttl := time.Duration(leaseTTLSeconds) * time.Second
	if ttl <= 0 {
		ttl = orchestration.TaskVerificationDefaultLeaseTTL
	}
	interval := ttl / 3
	if interval <= 0 {
		return time.Second
	}
	return interval
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envCSV(key string, fallback []string) []string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parts := strings.Split(value, ",")
	items := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		items = append(items, trimmed)
	}
	if len(items) == 0 {
		return fallback
	}
	return items
}

func sleepWithContext(ctx context.Context, duration time.Duration) error {
	if duration <= 0 {
		return nil
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// verificationFactEmitter is verifyd's counterpart to workerd's
// attemptFactEmitter. It pushes verification updates onto the bus; publish
// errors are logged and dropped because Postgres still has the row.
type verificationFactEmitter struct {
	log *slog.Logger
	bus orchestrationbus.AttemptFactPublisher
}

func newVerificationFactEmitter(log *slog.Logger, bus orchestrationbus.AttemptFactPublisher) *verificationFactEmitter {
	return &verificationFactEmitter{log: log, bus: bus}
}

func (e *verificationFactEmitter) emit(ctx context.Context, verification orchestration.TaskVerification, factType string, payload map[string]any) {
	if e == nil || e.bus == nil {
		return
	}
	env := orchestrationbus.AttemptFactEnvelope{
		FactID:     uuid.NewString(),
		RunID:      verification.RunID,
		TaskID:     verification.TaskID,
		AttemptID:  verification.ID,
		ClaimEpoch: int64(verification.ClaimEpoch & math.MaxInt64), //nolint:gosec // claim epoch fits in int64
		ClaimToken: verification.ClaimToken,
		Type:       factType,
		Payload:    payload,
		ObservedAt: time.Now().UTC(),
	}
	if env.ClaimEpoch <= 0 {
		// Recovery paths can emit a fact before the claim has set an epoch.
		// Fall back to attempt_no so envelope.Validate still accepts it.
		env.ClaimEpoch = int64(verification.AttemptNo)
		if env.ClaimEpoch <= 0 {
			env.ClaimEpoch = 1
		}
	}
	publishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if err := e.bus.PublishAttemptFact(publishCtx, env); err != nil {
		e.log.Debug("publish verification fact failed",
			slog.String("verification_id", verification.ID),
			slog.String("type", factType),
			slog.Any("error", err),
		)
	}
}

func (e *verificationFactEmitter) emitCompletion(ctx context.Context, verification orchestration.TaskVerification, completion orchestration.VerificationCompletion) {
	factType := "verification.completed"
	switch completion.Status {
	case orchestration.TaskVerificationStatusFailed:
		factType = "verification.failed"
	case orchestration.TaskVerificationStatusLost:
		factType = "verification.lost"
	}
	payload := map[string]any{
		"status":  completion.Status,
		"verdict": completion.Verdict,
	}
	if completion.FailureClass != "" {
		payload["failure_class"] = completion.FailureClass
	}
	if completion.TerminalReason != "" {
		payload["terminal_reason"] = completion.TerminalReason
	}
	if completion.Summary != "" {
		payload["summary"] = completion.Summary
	}
	e.emit(ctx, verification, factType, payload)
}

func buildLLMRuntime(
	ctx context.Context,
	log *slog.Logger,
	cfg config.Config,
	pool *pgxpool.Pool,
	queries *dbsqlc.Queries,
) (*orchestrationexec.Runtime, func(), error) {
	rc, err := boot.ProvideRuntimeConfig(cfg)
	if err != nil {
		return nil, nil, err
	}
	containerSvc, cleanupContainer, err := containerprovider.ProvideService(ctx, log, cfg, rc.ContainerBackend)
	if err != nil {
		return nil, nil, err
	}
	localSvc := workspace.NewLocalService(log, cfg.Local, cfg.Workspace.DataRoot)
	runtimeSvc := workspace.NewRuntimeRouter(containerSvc, localSvc)
	manager := workspace.NewManager(log, runtimeSvc, nil, cfg.Workspace, cfg.Containerd.Namespace, pool)
	a := agentpkg.New(agentpkg.Deps{
		BridgeProvider: manager,
		Logger:         log,
	})
	a.SetToolProviders([]agenttools.ToolProvider{
		agenttools.NewContainerProvider(log, manager, nil, config.DefaultDataMount),
	})
	storeQueries := postgresstore.NewQueries(queries)
	runtime := orchestrationexec.NewRuntime(
		log,
		queries,
		settings.NewService(log, storeQueries, nil, nil),
		models.NewService(log, storeQueries),
		a,
		rc.TimezoneLocation,
	)
	cleanup := func() {
		localSvc.Close()
		if cleanupContainer != nil {
			cleanupContainer()
		}
	}
	return runtime, cleanup, nil
}

func decodeObject(raw []byte) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil || payload == nil {
		return map[string]any{}
	}
	return payload
}

func stringValue(raw any) string {
	value, _ := raw.(string)
	return value
}

func workerShutdownVerificationCompletion(verification orchestration.TaskVerification) orchestration.VerificationCompletion {
	return orchestration.VerificationCompletion{
		VerificationID: verification.ID,
		ClaimToken:     verification.ClaimToken,
		Status:         orchestration.TaskVerificationStatusFailed,
		Verdict:        orchestration.VerificationVerdictRejected,
		Summary:        "verifier shutdown interrupted verification",
		FailureClass:   "worker_shutdown",
		TerminalReason: "verifier shutdown interrupted verification",
	}
}
