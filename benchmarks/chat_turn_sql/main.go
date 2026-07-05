package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	if err := runMain(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func runMain() error {
	var (
		configPath string
		mode       string
		dsn        string
		scenario   string
		runnerName string
		queriesDir string
		addr       string
		profiles   profileOptions
	)
	flag.StringVar(&configPath, "config", "benchmarks/chat_turn_sql/config.example.toml", "Path to benchmark TOML config")
	flag.StringVar(&mode, "mode", "seed-run", "Mode: estimate, seed, run, seed-run, cleanup, explain, wrk-server")
	flag.StringVar(&dsn, "dsn", "", "PostgreSQL DSN override")
	flag.StringVar(&scenario, "scenario", "", "Scenario override")
	flag.StringVar(&runnerName, "runner", "", `Runner override: "sqlc" for generated production path, "sql" for SQL templates, "http" for Echo handler path`)
	flag.StringVar(&queriesDir, "queries-dir", "benchmarks/chat_turn_sql/queries/postgres", "Directory containing runnable SQL templates")
	flag.StringVar(&addr, "addr", "127.0.0.1:18083", "Listen address for -mode wrk-server")
	flag.StringVar(&profiles.CPUPath, "cpuprofile", "", "Write Go CPU profile for the measured benchmark phase")
	flag.StringVar(&profiles.MemPath, "memprofile", "", "Write Go heap profile after the measured benchmark phase")
	flag.StringVar(&profiles.TracePath, "trace", "", "Write Go runtime trace for the measured benchmark phase")
	flag.Parse()

	cfg, err := loadConfig(configPath)
	if err != nil {
		return err
	}
	if dsn != "" {
		cfg = applyRuntimeOverrides(cfg, dsn, scenario, runnerName, mode)
	} else {
		cfg = applyRuntimeOverrides(cfg, os.Getenv("MEMOH_BENCH_DSN"), scenario, runnerName, mode)
	}
	if err := cfg.validate(); err != nil {
		return err
	}

	switch mode {
	case "estimate":
		return printJSON(estimateSeed(cfg))
	case "seed", "run", "seed-run", "cleanup", "explain", "wrk-server":
	default:
		return fmt.Errorf("unknown mode %q", mode)
	}

	ctx := context.Background()
	pool, err := openPool(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	switch mode {
	case "cleanup":
		return cleanupBenchmarkData(ctx, pool, cfg.Seed.Marker)
	case "seed":
		catalog, err := seedBenchmarkData(ctx, pool, cfg)
		if err != nil {
			return err
		}
		return printJSON(catalog.Estimate)
	case "run":
		catalog, err := loadSeedCatalog(ctx, pool, cfg)
		if err != nil {
			return err
		}
		return runBenchmark(ctx, cfg, pool, queriesDir, catalog, profiles)
	case "seed-run":
		catalog, err := seedBenchmarkData(ctx, pool, cfg)
		if err != nil {
			return err
		}
		return runBenchmark(ctx, cfg, pool, queriesDir, catalog, profiles)
	case "explain":
		catalog, err := loadSeedCatalog(ctx, pool, cfg)
		if err != nil {
			return err
		}
		queries, err := loadQueries(queriesDir)
		if err != nil {
			return err
		}
		cfg.Output.Explain = true
		return writeExplainPlans(ctx, pool, cfg, queries, catalog)
	case "wrk-server":
		catalog, err := loadSeedCatalog(ctx, pool, cfg)
		if err != nil {
			return err
		}
		return serveWRKBenchmark(ctx, cfg, pool, catalog, addr)
	default:
		panic("unreachable")
	}
}

func runBenchmark(ctx context.Context, cfg Config, pool *pgxpool.Pool, queriesDir string, catalog SeedCatalog, profiles profileOptions) error {
	var queries QuerySet
	if cfg.Workload.Runner == runnerSQL || cfg.Output.Explain {
		var err error
		queries, err = loadQueries(queriesDir)
		if err != nil {
			return err
		}
	}
	executor, err := newQueryExecutor(cfg, pool, queries)
	if err != nil {
		return err
	}
	r, err := newRunner(cfg, executor, catalog)
	if err != nil {
		return err
	}
	start := time.Now()
	result, err := runWithProfiles(profiles, func() (BenchmarkResult, error) {
		return r.run(ctx)
	})
	if err := writeJSON(cfg.Output.JSONPath, result); err != nil {
		return err
	}
	if err := writeCSV(cfg.Output.CSVPath, result); err != nil {
		return err
	}
	if err := writeExplainPlans(ctx, pool, cfg, queries, catalog); err != nil {
		return err
	}
	fmt.Printf("completed %s runner=%s in %s\n", cfg.Workload.Scenario, cfg.Workload.Runner, time.Since(start).Round(time.Millisecond))
	for _, q := range result.Queries {
		fmt.Printf("%-32s total=%d ok=%d errors=%d p50=%.3fms p95=%.3fms p99=%.3fms qps=%.2f\n", q.Name, q.TotalCount, q.Count, q.Errors, q.P50Millis, q.P95Millis, q.P99Millis, q.Throughput)
	}
	fmt.Printf("json=%s csv=%s\n", cfg.Output.JSONPath, cfg.Output.CSVPath)
	return err
}

type profileOptions struct {
	CPUPath   string
	MemPath   string
	TracePath string
}

func runWithProfiles(opts profileOptions, fn func() (BenchmarkResult, error)) (result BenchmarkResult, err error) {
	var cpuFile *os.File
	if opts.CPUPath != "" {
		cpuFile, err = createProfileFile(opts.CPUPath)
		if err != nil {
			return BenchmarkResult{}, err
		}
		if err = pprof.StartCPUProfile(cpuFile); err != nil {
			_ = cpuFile.Close()
			return BenchmarkResult{}, err
		}
		defer func() {
			pprof.StopCPUProfile()
			if closeErr := cpuFile.Close(); err == nil {
				err = closeErr
			}
		}()
	}

	var traceFile *os.File
	if opts.TracePath != "" {
		traceFile, err = createProfileFile(opts.TracePath)
		if err != nil {
			return BenchmarkResult{}, err
		}
		if err = trace.Start(traceFile); err != nil {
			_ = traceFile.Close()
			return BenchmarkResult{}, err
		}
		defer func() {
			trace.Stop()
			if closeErr := traceFile.Close(); err == nil {
				err = closeErr
			}
		}()
	}

	result, err = fn()
	if opts.MemPath != "" {
		if profileErr := writeHeapProfile(opts.MemPath); err == nil {
			err = profileErr
		}
	}
	return result, err
}

func writeHeapProfile(path string) error {
	file, err := createProfileFile(path)
	if err != nil {
		return err
	}
	runtime.GC()
	if err := pprof.WriteHeapProfile(file); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func createProfileFile(path string) (*os.File, error) {
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, err
		}
	}
	// #nosec G304 -- local benchmark operators control profile output paths.
	return os.Create(path)
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func applyRuntimeOverrides(cfg Config, dsn, scenario, runnerName, mode string) Config {
	if dsn != "" {
		cfg.DB.DSN = dsn
	}
	if scenario != "" {
		cfg.Workload.Scenario = scenario
	}
	if runnerName != "" {
		cfg.Workload.Runner = runnerName
	}
	if mode == "explain" {
		cfg.Workload.Runner = runnerSQL
	}
	return cfg
}
