package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigAppliesDefaultsAndOverrides(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	raw := []byte(`
[db]
dsn = "postgres://example"

[seed]
marker = "test-marker"
bots = 2
sessions_per_bot = 3
turns_per_session = 10
messages_per_turn = 2

[workload]
scenario = "latest_page"
duration = "1s"
warmup = "0s"
concurrency = 1
page_size = 20
`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DB.DSN != "postgres://example" {
		t.Fatalf("DSN = %q", cfg.DB.DSN)
	}
	if cfg.DB.MaxOpenConns == 0 {
		t.Fatal("expected default max_open_conns")
	}
	if cfg.Seed.Marker != "test-marker" {
		t.Fatalf("marker = %q", cfg.Seed.Marker)
	}
	if cfg.Workload.Scenario != queryLatestPage {
		t.Fatalf("scenario = %q", cfg.Workload.Scenario)
	}
	if cfg.Workload.Runner != runnerSQLC {
		t.Fatalf("runner = %q", cfg.Workload.Runner)
	}
	if !cfg.Workload.FailOnError {
		t.Fatal("expected default fail_on_error")
	}
	if cfg.Workload.HTTPFormat != "ui" {
		t.Fatalf("http_format = %q", cfg.Workload.HTTPFormat)
	}
	if !cfg.Workload.HTTPDecodeJSON {
		t.Fatal("expected default http_decode_json")
	}
}

func TestLoadConfigRejectsUnknownRunner(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	raw := []byte(`
[workload]
runner = "template"
`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(path); err == nil {
		t.Fatal("expected unknown runner error")
	}
}

func TestApplyRuntimeOverridesRunner(t *testing.T) {
	cfg := defaultConfig()
	got := applyRuntimeOverrides(cfg, "postgres://override", queryLatestPage, runnerHTTP, "run")
	if got.DB.DSN != "postgres://override" {
		t.Fatalf("dsn = %q", got.DB.DSN)
	}
	if got.Workload.Scenario != queryLatestPage {
		t.Fatalf("scenario = %q", got.Workload.Scenario)
	}
	if got.Workload.Runner != runnerHTTP {
		t.Fatalf("runner = %q", got.Workload.Runner)
	}
}

func TestApplyRuntimeOverridesExplainForcesSQLRunner(t *testing.T) {
	cfg := defaultConfig()
	got := applyRuntimeOverrides(cfg, "", "", runnerSQLC, "explain")
	if got.Workload.Runner != runnerSQL {
		t.Fatalf("runner = %q", got.Workload.Runner)
	}
}

func TestLoadConfigRejectsUnknownKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	raw := []byte(`
[db]
dsn = "postgres://example"
typo = true
`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(path); err == nil {
		t.Fatal("expected unknown key error")
	}
}

func TestLoadConfigRejectsInvalidExplicitValue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	raw := []byte(`
[workload]
concurrency = 0
`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(path); err == nil {
		t.Fatal("expected invalid concurrency error")
	}
}

func TestHTTPRunnerRejectsUnsupportedSingleScenario(t *testing.T) {
	cfg := defaultConfig()
	cfg.Workload.Runner = runnerHTTP
	cfg.Workload.Scenario = queryMessageAssets
	if err := cfg.validate(); err == nil {
		t.Fatal("expected unsupported http scenario error")
	}
}

func TestHTTPRunnerRejectsUnsupportedMixedScenario(t *testing.T) {
	cfg := defaultConfig()
	cfg.Workload.Runner = runnerHTTP
	cfg.Workload.Scenario = "mixed_saas_read"
	cfg.Workload.QueryWeights = map[string]int{
		queryLatestPage:    1,
		queryMessageAssets: 1,
	}
	if err := cfg.validate(); err == nil {
		t.Fatal("expected unsupported http mixed query error")
	}
}

func TestHTTPRunnerAcceptsListMessagesMixedScenario(t *testing.T) {
	cfg := defaultConfig()
	cfg.Workload.Runner = runnerHTTP
	cfg.Workload.Scenario = "mixed_saas_read"
	cfg.Workload.QueryWeights = map[string]int{
		queryLatestPage:     5,
		queryBeforePage:     3,
		queryLocateWindow:   1,
		queryExternalLookup: 1,
	}
	if err := cfg.validate(); err != nil {
		t.Fatal(err)
	}
}

func TestMixedSaaSWriteDefaultsToWriteWeights(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	raw := []byte(`
[workload]
scenario = "mixed_saas_write"
runner = "sqlc"
`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Workload.QueryWeights[queryWriteTurnPair] != 55 {
		t.Fatalf("write_turn_pair weight = %d", cfg.Workload.QueryWeights[queryWriteTurnPair])
	}
	if cfg.Workload.QueryWeights[queryWriteToolTail] != 35 {
		t.Fatalf("write_tool_tail weight = %d", cfg.Workload.QueryWeights[queryWriteToolTail])
	}
	if cfg.Workload.QueryWeights[queryLatestPage] != 0 {
		t.Fatalf("latest_page weight = %d, want 0", cfg.Workload.QueryWeights[queryLatestPage])
	}
	if !cfg.Workload.SerializeSessionWrites {
		t.Fatal("expected write scenarios to serialize session writes by default")
	}
}

func TestMixedSaaSWriteRejectsReadWeights(t *testing.T) {
	cfg := defaultConfig()
	cfg.Workload.Scenario = "mixed_saas_write"
	cfg.Workload.QueryWeights = map[string]int{
		queryChatPageUI: 1,
	}
	if err := cfg.validate(); err == nil {
		t.Fatal("expected mixed_saas_write to reject read query weights")
	}
}

func TestMixedSaaSReadRejectsWriteWeights(t *testing.T) {
	cfg := defaultConfig()
	cfg.Workload.Scenario = "mixed_saas_read"
	cfg.Workload.QueryWeights = map[string]int{
		queryWriteTurnPair: 1,
	}
	if err := cfg.validate(); err == nil {
		t.Fatal("expected mixed_saas_read to reject write query weights")
	}
}

func TestWriteScenarioRequiresSQLCRunner(t *testing.T) {
	cfg := defaultConfig()
	cfg.Workload.Runner = runnerSQL
	cfg.Workload.Scenario = queryWriteToolTail
	if err := cfg.validate(); err == nil {
		t.Fatal("expected write scenario to reject sql runner")
	}
}

func TestReadScenarioDoesNotSerializeSessionWritesByDefault(t *testing.T) {
	cfg := defaultConfig()
	if cfg.Workload.SerializeSessionWrites {
		t.Fatal("expected read scenario to leave session write serialization disabled")
	}
}

func TestNormalizeWeightsRejectsUnknownQuery(t *testing.T) {
	_, err := normalizeWeights(map[string]int{"unknown": 1})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestNormalizeWeightsOrdersKnownQueries(t *testing.T) {
	weighted, err := normalizeWeights(map[string]int{
		queryBeforePage: 2,
		queryLatestPage: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(weighted) != 2 {
		t.Fatalf("len = %d", len(weighted))
	}
	if weighted[0].Name != queryLatestPage || weighted[0].Cumulative != 3 {
		t.Fatalf("first = %#v", weighted[0])
	}
	if pickWeightedQuery(weighted, 4) != queryBeforePage {
		t.Fatal("expected before_page for slot 4")
	}
}

func TestEstimateSeedIncludesLinearTurnsAndArtifacts(t *testing.T) {
	cfg := defaultConfig()
	cfg.Seed.Bots = 1
	cfg.Seed.SessionsPerBot = 2
	cfg.Seed.TurnsPerSession = 10
	cfg.Seed.BranchFactor = 3
	cfg.Seed.ActiveHeadsPerSess = 3
	cfg.Seed.BranchDepth = 4
	cfg.Seed.MessagesPerTurn = 2
	cfg.Seed.ApprovalEveryNTurns = 5
	cfg.Seed.UserInputEveryNTurns = 6
	cfg.Seed.AssetEveryNMessages = 10
	got := estimateSeed(cfg)
	if got.Sessions != 2 {
		t.Fatalf("sessions = %d", got.Sessions)
	}
	if got.Turns != 20 {
		t.Fatalf("turns = %d", got.Turns)
	}
	if got.Messages != 40 {
		t.Fatalf("messages = %d", got.Messages)
	}
	if got.Heads != 0 {
		t.Fatalf("heads = %d", got.Heads)
	}
	if got.Approvals != 4 {
		t.Fatalf("approvals = %d", got.Approvals)
	}
	if got.UserInputs != 3 {
		t.Fatalf("user inputs = %d", got.UserInputs)
	}
	if got.Assets != 4 {
		t.Fatalf("assets = %d", got.Assets)
	}
}
