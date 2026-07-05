package main

import (
	"context"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/google/uuid"
)

func TestWRKBenchmarkServerRoutesWriteScenarios(t *testing.T) {
	cfg := defaultConfig()
	cfg.Workload.Scenario = "mixed_saas_write"
	cfg.Workload.QueryWeights = defaultWriteQueryWeights()
	cfg.Workload.SerializeSessionWrites = true

	hotSessionID := uuid.New()
	coldSessionID := uuid.New()
	executor := &recordingExecutor{}
	server, err := newWRKBenchmarkServer(cfg, executor, SeedCatalog{
		Marker: "test",
		Sessions: []SessionSeed{
			{SessionID: hotSessionID},
			{SessionID: coldSessionID},
		},
		HotSessions:  []int{0},
		ColdSessions: []int{1},
	})
	if err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(server.routes())
	defer ts.Close()

	resp := postWRKTestRequest(t, ts, "/bench/write_turn_pair?dist=random")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	closeResponseBody(t, resp)

	resp = postWRKTestRequest(t, ts, "/bench/write_user_message?dist=hot")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	closeResponseBody(t, resp)

	calls := executor.callsSnapshot()
	if len(calls) != 2 {
		t.Fatalf("calls = %d", len(calls))
	}
	if calls[0].queryName != queryWriteTurnPair || calls[0].sessionID != coldSessionID {
		t.Fatalf("first call = %#v", calls[0])
	}
	if calls[1].queryName != queryWriteUserMessage || calls[1].sessionID != hotSessionID {
		t.Fatalf("second call = %#v", calls[1])
	}
}

func TestWRKBenchmarkServerRoutesMixedReadScenarios(t *testing.T) {
	cfg := defaultConfig()
	cfg.Workload.Scenario = "mixed_saas_read"
	cfg.Workload.QueryWeights = map[string]int{
		queryAfterPage:      1,
		queryExternalLookup: 1,
	}

	executor := &recordingExecutor{}
	server, err := newWRKBenchmarkServer(cfg, executor, SeedCatalog{
		Marker: "test",
		Sessions: []SessionSeed{
			{SessionID: uuid.New()},
			{SessionID: uuid.New()},
		},
		HotSessions:  []int{0},
		ColdSessions: []int{1},
	})
	if err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(server.routes())
	defer ts.Close()

	resp := postWRKTestRequest(t, ts, "/bench/mixed_saas_read?dist=random")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("first status = %d", resp.StatusCode)
	}
	closeResponseBody(t, resp)

	resp = postWRKTestRequest(t, ts, "/bench/mixed_saas_read?dist=hot")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("second status = %d", resp.StatusCode)
	}
	closeResponseBody(t, resp)

	calls := executor.callsSnapshot()
	if len(calls) != 2 {
		t.Fatalf("calls = %d", len(calls))
	}
	got := map[string]bool{
		calls[0].queryName: true,
		calls[1].queryName: true,
	}
	if !got[queryAfterPage] || !got[queryExternalLookup] {
		t.Fatalf("calls = %#v, want after_page and external_lookup", calls)
	}
}

func postWRKTestRequest(t *testing.T, ts *httptest.Server, path string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, ts.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	// #nosec G704 -- httptest.Server URL is local and controlled by this test.
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func closeResponseBody(t *testing.T, resp *http.Response) {
	t.Helper()
	if err := resp.Body.Close(); err != nil {
		t.Fatalf("close response body: %v", err)
	}
}

func TestWriteOnlyWeightsDefaultsWhenNoWriteWeights(t *testing.T) {
	got := writeOnlyWeights(map[string]int{
		queryChatPageUI: 10,
	})
	if got[queryWriteTurnPair] != 55 || got[queryWriteToolTail] != 35 {
		t.Fatalf("weights = %#v", got)
	}
}

func TestReadOnlyWeightsDefaultsWhenNoReadWeights(t *testing.T) {
	got := readOnlyWeights(map[string]int{
		queryWriteTurnPair: 10,
	})
	if got[queryChatPageUI] != 70 || got[queryBeforePage] != 18 {
		t.Fatalf("weights = %#v", got)
	}
}

type recordedCall struct {
	queryName string
	sessionID uuid.UUID
}

type recordingExecutor struct {
	mu    sync.Mutex
	calls []recordedCall
}

func (e *recordingExecutor) execQuery(_ context.Context, queryName string, s SessionSeed, _ *rand.Rand) (int64, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = append(e.calls, recordedCall{queryName: queryName, sessionID: s.SessionID})
	return 1, nil
}

func (*recordingExecutor) querySource() string {
	return querySourceGeneratedSQLC
}

func (*recordingExecutor) scanMode() string {
	return scanModeSQLCStructScan
}

func (e *recordingExecutor) callsSnapshot() []recordedCall {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]recordedCall, len(e.calls))
	copy(out, e.calls)
	return out
}
