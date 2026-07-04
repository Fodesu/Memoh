package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type runner struct {
	cfg      Config
	executor queryExecutor
	meta     executorMetadata
	catalog  SeedCatalog
	weighted []WeightedQuery
	stats    *statsCollector
	counter  atomic.Int64
	errors   atomic.Int64
}

func newRunner(cfg Config, executor queryExecutor, catalog SeedCatalog) (*runner, error) {
	if executor == nil {
		return nil, errors.New("query executor must not be nil")
	}
	var weighted []WeightedQuery
	var err error
	if cfg.Workload.Scenario == "mixed_saas_read" {
		weighted, err = normalizeWeights(cfg.Workload.QueryWeights)
		if err != nil {
			return nil, err
		}
	}
	return &runner{
		cfg:      cfg,
		executor: executor,
		meta: executorMetadata{
			Runner:      cfg.Workload.Runner,
			QuerySource: executor.querySource(),
			ScanMode:    executor.scanMode(),
		},
		catalog:  catalog,
		weighted: weighted,
		stats:    newStatsCollector(),
	}, nil
}

func (r *runner) run(ctx context.Context) (BenchmarkResult, error) {
	warmup, err := r.cfg.warmupDuration()
	if err != nil {
		return BenchmarkResult{}, err
	}
	duration, err := r.cfg.workloadDuration()
	if err != nil {
		return BenchmarkResult{}, err
	}
	if warmup > 0 {
		if err := r.runPhase(ctx, warmup, true); err != nil {
			return BenchmarkResult{}, err
		}
	}
	r.counter.Store(0)
	startedAt := time.Now().UTC()
	if err := r.runPhase(ctx, duration, false); err != nil {
		return BenchmarkResult{}, err
	}
	result := r.stats.result(r.cfg, r.catalog.Estimate, startedAt, duration, r.meta)
	if r.cfg.Workload.FailOnError && result.TotalErrors() > 0 {
		return result, fmt.Errorf("benchmark completed with %d query errors", result.TotalErrors())
	}
	return result, nil
}

func (r *runner) runPhase(ctx context.Context, duration time.Duration, warmup bool) error {
	phaseCtx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()

	var wg sync.WaitGroup
	errCh := make(chan error, r.cfg.Workload.Concurrency)
	for workerID := 0; workerID < r.cfg.Workload.Concurrency; workerID++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			if err := r.worker(phaseCtx, workerID, warmup); err != nil {
				errCh <- err
			}
		}(workerID)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		return err
	}
	return nil
}

func (r *runner) worker(ctx context.Context, workerID int, warmup bool) error {
	if workerID < 0 {
		return fmt.Errorf("worker id must be non-negative: %d", workerID)
	}
	workerSeed := uint64(workerID) + 1
	// #nosec G404 -- deterministic pseudo-random sampling is required for repeatable benchmarks.
	rng := rand.New(rand.NewPCG(r.cfg.Workload.RandomSeed, workerSeed))
	for {
		select {
		case <-ctx.Done():
			if ctx.Err() == context.DeadlineExceeded {
				return nil
			}
			return ctx.Err()
		default:
		}
		n := int(r.counter.Add(1))
		queryName := r.nextQuery(n)
		sample := r.nextSample(rng)
		start := time.Now()
		rows, err := r.executor.execQuery(ctx, queryName, sample, rng)
		if errors.Is(err, context.DeadlineExceeded) {
			if phaseDone(ctx) {
				return nil
			}
		}
		if errors.Is(err, context.Canceled) {
			if phaseDone(ctx) {
				return nil
			}
			return err
		}
		if err != nil && !warmup {
			r.errors.Add(1)
		}
		r.stats.add(queryMeasurement{
			Name:     queryName,
			Latency:  time.Since(start),
			Rows:     rows,
			Err:      err,
			Warmup:   warmup,
			WorkerID: workerID,
		})
	}
}

func (r *runner) nextQuery(n int) string {
	if r.cfg.Workload.Scenario != "mixed_saas_read" {
		return r.cfg.Workload.Scenario
	}
	return pickWeightedQuery(r.weighted, n)
}

func (r *runner) nextSample(rng *rand.Rand) SessionSeed {
	useHot := rng.Float64() < r.cfg.Workload.HotTrafficRatio && len(r.catalog.HotSessions) > 0
	var idx int
	switch {
	case useHot:
		idx = r.catalog.HotSessions[rng.IntN(len(r.catalog.HotSessions))]
	case len(r.catalog.ColdSessions) > 0:
		idx = r.catalog.ColdSessions[rng.IntN(len(r.catalog.ColdSessions))]
	default:
		idx = rng.IntN(len(r.catalog.Sessions))
	}
	return r.catalog.Sessions[idx]
}

func selectedCursor(s SessionSeed, rng *rand.Rand) (uuid.UUID, time.Time) {
	messageIDs := s.CursorMessageIDs
	createdAts := s.CursorCreatedAts
	if len(messageIDs) == 0 {
		return s.LatestMessageID, time.Now().UTC()
	}
	idx := rng.IntN(len(messageIDs))
	cursorID := messageIDs[idx]
	var cursorTime time.Time
	if idx < len(createdAts) {
		cursorTime = createdAts[idx]
	}
	if cursorTime.IsZero() {
		cursorTime = time.Now().UTC()
	}
	return cursorID, cursorTime
}

func messageAssetIDs(s SessionSeed) []uuid.UUID {
	if len(s.PageMessageIDs) > 0 {
		return s.PageMessageIDs
	}
	if len(s.CursorMessageIDs) > 0 {
		return s.CursorMessageIDs
	}
	if s.LatestMessageID != uuid.Nil {
		return []uuid.UUID{s.LatestMessageID}
	}
	return nil
}

func pageToolCallIDs(s SessionSeed) []string {
	if len(s.PageToolCallIDs) == 0 {
		return nil
	}
	out := make([]string, 0, len(s.PageToolCallIDs))
	seen := make(map[string]struct{}, len(s.PageToolCallIDs))
	for _, id := range s.PageToolCallIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func selectedExternalMessageID(s SessionSeed, rng *rand.Rand) string {
	if len(s.ExternalMessageIDs) == 0 {
		return s.ExternalMessageID
	}
	return s.ExternalMessageIDs[rng.IntN(len(s.ExternalMessageIDs))]
}

type queryArgError string

func (e queryArgError) Error() string {
	return string(e)
}

func phaseDone(ctx context.Context) bool {
	return errors.Is(ctx.Err(), context.DeadlineExceeded)
}

func writeExplainPlans(ctx context.Context, pool *pgxpool.Pool, cfg Config, queries QuerySet, catalog SeedCatalog) error {
	if !cfg.Output.Explain {
		return nil
	}
	if len(catalog.Sessions) == 0 {
		return errors.New("cannot explain without seed catalog")
	}
	// #nosec G703 -- benchmark explain output directory is controlled by the local operator.
	if err := os.MkdirAll(cfg.Output.ExplainDir, 0o750); err != nil {
		return err
	}
	s := catalog.Sessions[0]
	// #nosec G404 -- deterministic pseudo-random sampling is required for repeatable explain plans.
	explainRNG := rand.New(rand.NewPCG(cfg.Workload.RandomSeed, 1))
	argBuilder := &sqlTemplateExecutor{cfg: cfg}
	for _, name := range knownQueries {
		args, err := argBuilder.argsForQuery(name, s, explainRNG)
		if err != nil {
			return err
		}
		sql := "EXPLAIN (ANALYZE, BUFFERS, WAL, FORMAT JSON) " + queries[name]
		var raw string
		if err := pool.QueryRow(ctx, sql, args...).Scan(&raw); err != nil {
			return fmt.Errorf("explain %s: %w", name, err)
		}
		var decoded any
		if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
			return fmt.Errorf("decode explain %s: %w", name, err)
		}
		pretty, err := json.MarshalIndent(decoded, "", "  ")
		if err != nil {
			return err
		}
		// #nosec G703 -- benchmark explain output directory is controlled by the local operator.
		if err := os.WriteFile(filepath.Join(cfg.Output.ExplainDir, name+".json"), append(pretty, '\n'), 0o600); err != nil {
			return err
		}
	}
	return nil
}
