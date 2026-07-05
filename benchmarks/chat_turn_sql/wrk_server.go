package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type wrkBenchmarkServer struct {
	cfg           Config
	executor      queryExecutor
	catalog       SeedCatalog
	readWeighted  []WeightedQuery
	writeWeighted []WeightedQuery
	counter       atomic.Int64
	writeLocks    map[uuid.UUID]*sync.Mutex
}

func serveWRKBenchmark(ctx context.Context, cfg Config, pool *pgxpool.Pool, catalog SeedCatalog, addr string) error {
	if cfg.Workload.Runner != runnerSQLC {
		return fmt.Errorf("wrk-server requires runner %q, got %q", runnerSQLC, cfg.Workload.Runner)
	}
	if !isMixedWorkloadScenario(cfg.Workload.Scenario) && !isKnownQuery(cfg.Workload.Scenario) {
		return fmt.Errorf("wrk-server does not support scenario %q", cfg.Workload.Scenario)
	}
	if strings.TrimSpace(addr) == "" {
		return errors.New("wrk-server address must not be empty")
	}
	server, err := newWRKBenchmarkServer(cfg, newSQLCExecutor(cfg, pool), catalog)
	if err != nil {
		return err
	}

	httpServer := &http.Server{
		Addr:              addr,
		Handler:           server.routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		errCh <- httpServer.Shutdown(shutdownCtx)
	}()

	fmt.Printf("wrk benchmark server listening on http://%s\n", addr)
	fmt.Println("example:")
	fmt.Printf("  MEMOH_WRK_SCENARIO=mixed_saas_read MEMOH_WRK_DIST=random wrk -t4 -c16 -d30s --latency -s benchmarks/chat_turn_sql/wrk_read.lua http://%s\n", addr)
	fmt.Printf("  MEMOH_WRK_SCENARIO=mixed_saas_write MEMOH_WRK_DIST=random wrk -t4 -c16 -d30s --latency -s benchmarks/chat_turn_sql/wrk_write.lua http://%s\n", addr)

	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	select {
	case err := <-errCh:
		return err
	default:
		return nil
	}
}

func newWRKBenchmarkServer(cfg Config, executor queryExecutor, catalog SeedCatalog) (*wrkBenchmarkServer, error) {
	if executor == nil {
		return nil, errors.New("query executor must not be nil")
	}
	if len(catalog.Sessions) == 0 {
		return nil, errors.New("wrk-server requires at least one seeded session")
	}
	readWeighted, err := normalizeWeights(readOnlyWeights(cfg.Workload.QueryWeights))
	if err != nil {
		return nil, fmt.Errorf("mixed read weights: %w", err)
	}
	writeWeighted, err := normalizeWeights(writeOnlyWeights(cfg.Workload.QueryWeights))
	if err != nil {
		return nil, fmt.Errorf("mixed write weights: %w", err)
	}
	return &wrkBenchmarkServer{
		cfg:           cfg,
		executor:      executor,
		catalog:       catalog,
		readWeighted:  readWeighted,
		writeWeighted: writeWeighted,
		writeLocks:    sessionWriteLocks(catalog),
	}, nil
}

func writeOnlyWeights(weights map[string]int) map[string]int {
	out := map[string]int{}
	for name, weight := range weights {
		if weight > 0 && isWriteScenario(name) {
			out[name] = weight
		}
	}
	if len(out) == 0 {
		return defaultWriteQueryWeights()
	}
	return out
}

func readOnlyWeights(weights map[string]int) map[string]int {
	out := map[string]int{}
	for name, weight := range weights {
		if weight > 0 && !isWriteScenario(name) {
			out[name] = weight
		}
	}
	if len(out) == 0 {
		return defaultConfig().Workload.QueryWeights
	}
	return out
}

func (s *wrkBenchmarkServer) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/bench/", s.handleBench)
	return mux
}

func (s *wrkBenchmarkServer) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSONResponse(w, http.StatusOK, map[string]any{
		"ok":              true,
		"benchmark":       benchmarkName,
		"marker":          s.catalog.Marker,
		"scenario":        s.cfg.Workload.Scenario,
		"sessions":        len(s.catalog.Sessions),
		"hot_sessions":    len(s.catalog.HotSessions),
		"cold_sessions":   len(s.catalog.ColdSessions),
		"serialize_write": s.cfg.Workload.SerializeSessionWrites,
	})
}

func (s *wrkBenchmarkServer) handleBench(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSONResponse(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}

	requestNo := s.counter.Add(1)
	scenario := strings.Trim(strings.TrimPrefix(r.URL.Path, "/bench/"), "/")
	if scenario == "" || scenario == "write" {
		scenario = strings.TrimSpace(r.URL.Query().Get("scenario"))
	}
	if scenario == "" {
		scenario = s.cfg.Workload.Scenario
	}
	queryName, err := s.queryForScenario(scenario, requestNo)
	if err != nil {
		writeJSONResponse(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	sample, err := s.sampleForRequest(requestNo, r.URL.Query().Get("dist"))
	if err != nil {
		writeJSONResponse(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}

	unlock := s.lockWriteSession(queryName, sample)
	defer unlock()

	start := time.Now()
	// #nosec G404 -- deterministic pseudo-random sampling is required for repeatable benchmarks.
	rng := rand.New(rand.NewPCG(s.cfg.Workload.RandomSeed, requestNumberSeed(requestNo)+1))
	rows, err := s.executor.execQuery(r.Context(), queryName, sample, rng)
	if err != nil {
		writeJSONResponse(w, http.StatusInternalServerError, map[string]any{
			"scenario": scenario,
			"query":    queryName,
			"error":    err.Error(),
		})
		return
	}
	writeJSONResponse(w, http.StatusOK, map[string]any{
		"scenario":   scenario,
		"query":      queryName,
		"rows":       rows,
		"latency_us": time.Since(start).Microseconds(),
	})
}

func (s *wrkBenchmarkServer) queryForScenario(scenario string, requestNo int64) (string, error) {
	switch scenario {
	case "mixed_saas_write":
		return pickWeightedQueryForRequest(s.writeWeighted, requestNo), nil
	case "mixed_saas_read":
		return pickWeightedQueryForRequest(s.readWeighted, requestNo), nil
	default:
		if isKnownQuery(scenario) {
			return scenario, nil
		}
		return "", fmt.Errorf("unsupported wrk scenario %q", scenario)
	}
}

func (s *wrkBenchmarkServer) sampleForRequest(requestNo int64, dist string) (SessionSeed, error) {
	dist = strings.TrimSpace(strings.ToLower(dist))
	if dist == "" {
		dist = "configured"
	}
	seed := splitmix64(requestNumberSeed(requestNo) + s.cfg.Workload.RandomSeed + 0x9e3779b97f4a7c15)
	switch dist {
	case "hot":
		return s.sampleFromIndexes(s.catalog.HotSessions, seed)
	case "random", "cold":
		if len(s.catalog.ColdSessions) > 0 {
			return s.sampleFromIndexes(s.catalog.ColdSessions, seed)
		}
		return s.sampleFromAll(seed)
	case "configured":
		if len(s.catalog.HotSessions) > 0 && unitFloat(seed) < s.cfg.Workload.HotTrafficRatio {
			return s.sampleFromIndexes(s.catalog.HotSessions, splitmix64(seed))
		}
		if len(s.catalog.ColdSessions) > 0 {
			return s.sampleFromIndexes(s.catalog.ColdSessions, splitmix64(seed))
		}
		return s.sampleFromAll(seed)
	default:
		return SessionSeed{}, fmt.Errorf("unknown wrk distribution %q", dist)
	}
}

func (s *wrkBenchmarkServer) sampleFromIndexes(indexes []int, seed uint64) (SessionSeed, error) {
	if len(indexes) == 0 {
		return SessionSeed{}, errors.New("requested distribution has no sessions")
	}
	idx := indexes[indexFromSeed(seed, len(indexes))]
	if idx < 0 || idx >= len(s.catalog.Sessions) {
		return SessionSeed{}, fmt.Errorf("catalog session index out of range: %d", idx)
	}
	return s.catalog.Sessions[idx], nil
}

func (s *wrkBenchmarkServer) sampleFromAll(seed uint64) (SessionSeed, error) {
	if len(s.catalog.Sessions) == 0 {
		return SessionSeed{}, errors.New("catalog has no sessions")
	}
	return s.catalog.Sessions[indexFromSeed(seed, len(s.catalog.Sessions))], nil
}

func requestNumberSeed(requestNo int64) uint64 {
	if requestNo <= 0 {
		return 0
	}
	return uint64(requestNo)
}

func pickWeightedQueryForRequest(weighted []WeightedQuery, requestNo int64) string {
	if len(weighted) == 0 {
		return queryChatPageUI
	}
	total := weighted[len(weighted)-1].Cumulative
	if total <= 0 {
		return weighted[0].Name
	}
	if requestNo < 0 {
		requestNo = 0
	}
	slot := requestNo % int64(total)
	for _, item := range weighted {
		if slot < int64(item.Cumulative) {
			return item.Name
		}
	}
	return weighted[len(weighted)-1].Name
}

func indexFromSeed(seed uint64, length int) int {
	if length <= 1 {
		return 0
	}
	slot := seed % uint64(length)
	idx := 0
	for slot > 0 {
		idx++
		slot--
	}
	return idx
}

func (s *wrkBenchmarkServer) lockWriteSession(queryName string, sample SessionSeed) func() {
	if !s.cfg.Workload.SerializeSessionWrites || !isWriteScenario(queryName) {
		return func() {}
	}
	lock := s.writeLocks[sample.SessionID]
	if lock == nil {
		return func() {}
	}
	lock.Lock()
	return lock.Unlock
}

func splitmix64(x uint64) uint64 {
	x += 0x9e3779b97f4a7c15
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return x ^ (x >> 31)
}

func unitFloat(v uint64) float64 {
	const inv53 = 1.0 / (1 << 53)
	return float64(v>>11) * inv53
}

func writeJSONResponse(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
