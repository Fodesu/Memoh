package browser_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/memohai/memoh/internal/orchestrationenv"
	envbrowser "github.com/memohai/memoh/internal/orchestrationenv/backend/browser"
)

// fakeGateway records every gateway call so tests can assert the
// exact request shape the backend issues. failCreate / failClose
// inject targeted failures.
type fakeGateway struct {
	mu sync.Mutex

	createCalls []envbrowser.CreateSessionRequest
	closeCalls  []closeCall

	failCreate error
	failClose  error
	response   *envbrowser.CreateSessionResponse
}

type closeCall struct {
	SessionID    string
	SessionToken string //nolint:gosec // test field mirroring gateway field name
}

func (g *fakeGateway) CreateSession(_ context.Context, req envbrowser.CreateSessionRequest) (*envbrowser.CreateSessionResponse, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.createCalls = append(g.createCalls, req)
	if g.failCreate != nil {
		return nil, g.failCreate
	}
	if g.response != nil {
		return g.response, nil
	}
	return &envbrowser.CreateSessionResponse{
		ID:                "gw-sess-" + req.BotID,
		WSEndpoint:        "ws://browser-gateway/ws/" + req.BotID,
		SessionToken:      "token-" + req.BotID,
		PlaywrightVersion: "1.50.0",
		Core:              req.Core,
	}, nil
}

func (g *fakeGateway) CloseSession(_ context.Context, sessionID, sessionToken string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.closeCalls = append(g.closeCalls, closeCall{SessionID: sessionID, SessionToken: sessionToken})
	return g.failClose
}

func TestBackendKindIsBrowser(t *testing.T) {
	b, err := envbrowser.New(&fakeGateway{}, envbrowser.Options{})
	require.NoError(t, err)
	require.Equal(t, orchestrationenv.KindBrowser, b.Kind())
}

func TestBackendAllocateCallsGatewayWithSessionScope(t *testing.T) {
	gw := &fakeGateway{}
	b, err := envbrowser.New(gw, envbrowser.Options{})
	require.NoError(t, err)

	res, err := b.Allocate(context.Background(), orchestrationenv.AllocateRequest{
		ResourceID:   "res-1",
		ResourceKind: orchestrationenv.KindBrowser,
		ResourceConfig: map[string]any{
			"core":           "firefox",
			"context_config": map[string]any{"viewport": map[string]any{"width": 1920, "height": 1080}},
		},
		SessionID: "sess-1",
		TenantID:  "tenant-A",
	})
	require.NoError(t, err)
	require.Len(t, gw.createCalls, 1)
	call := gw.createCalls[0]
	require.Equal(t, "envs-sess-1", call.BotID, "bot_id must be derived from env session id")
	require.Equal(t, "firefox", call.Core)
	require.NotNil(t, call.ContextConfig)
	require.Equal(t, "browser", res.RuntimeHandle["backend"])
	require.Equal(t, "gw-sess-envs-sess-1", res.RuntimeHandle["gateway_session_id"])
	require.Equal(t, "token-envs-sess-1", res.RuntimeHandle["gateway_session_token"])
	require.Equal(t, "ws://browser-gateway/ws/envs-sess-1", res.RuntimeHandle["ws_endpoint"])
	require.Equal(t, "1.50.0", res.RuntimeHandle["playwright_version"])
	require.Equal(t, "firefox", res.RuntimeHandle["core"])
	require.Equal(t, "sess-1", res.RuntimeHandle["env_session_id"])
}

func TestBackendAllocateRespectsDefaultCore(t *testing.T) {
	gw := &fakeGateway{}
	b, err := envbrowser.New(gw, envbrowser.Options{})
	require.NoError(t, err)

	_, err = b.Allocate(context.Background(), orchestrationenv.AllocateRequest{
		SessionID:      "sess-1",
		ResourceConfig: map[string]any{},
	})
	require.NoError(t, err)
	require.Equal(t, "chromium", gw.createCalls[0].Core, "default core must be chromium")
}

func TestBackendAllocateSurfacesGatewayError(t *testing.T) {
	gw := &fakeGateway{failCreate: errors.New("gateway down")}
	b, err := envbrowser.New(gw, envbrowser.Options{})
	require.NoError(t, err)

	_, err = b.Allocate(context.Background(), orchestrationenv.AllocateRequest{
		SessionID: "sess-1",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "gateway down")
}

func TestBackendSnapshotReturnsUnsupportedRef(t *testing.T) {
	b, err := envbrowser.New(&fakeGateway{}, envbrowser.Options{})
	require.NoError(t, err)

	res, err := b.Snapshot(context.Background(), orchestrationenv.SnapshotRequestBackend{
		SessionID: "sess-1",
		RuntimeHandle: map[string]any{
			"gateway_session_id": "gw-1",
			"ws_endpoint":        "ws://x",
		},
		Kind: orchestrationenv.SnapshotKindPostAction,
	})
	require.NoError(t, err)
	require.Equal(t, true, res.RuntimeRef["unsupported"])
	require.Equal(t, "gw-1", res.RuntimeRef["gateway_session_id"])
	require.Equal(t, orchestrationenv.SnapshotKindPostAction, res.RuntimeRef["snapshot_kind"])
}

func TestBackendReleaseCallsCloseSession(t *testing.T) {
	gw := &fakeGateway{}
	b, err := envbrowser.New(gw, envbrowser.Options{})
	require.NoError(t, err)

	require.NoError(t, b.Release(context.Background(), orchestrationenv.ReleaseRequestBackend{
		SessionID: "sess-1",
		RuntimeHandle: map[string]any{
			"gateway_session_id":    "gw-1",
			"gateway_session_token": "token-1",
		},
	}))
	require.Equal(t, []closeCall{{SessionID: "gw-1", SessionToken: "token-1"}}, gw.closeCalls)
}

func TestBackendReleaseSkipsWhenHandleEmpty(t *testing.T) {
	gw := &fakeGateway{}
	b, err := envbrowser.New(gw, envbrowser.Options{})
	require.NoError(t, err)

	require.NoError(t, b.Release(context.Background(), orchestrationenv.ReleaseRequestBackend{
		SessionID:     "sess-1",
		RuntimeHandle: map[string]any{},
	}))
	require.Empty(t, gw.closeCalls, "missing gateway_session_id must skip the gateway call")
}

func TestBackendReleaseSurfacesCloseError(t *testing.T) {
	gw := &fakeGateway{failClose: errors.New("gateway timeout")}
	b, err := envbrowser.New(gw, envbrowser.Options{})
	require.NoError(t, err)

	err = b.Release(context.Background(), orchestrationenv.ReleaseRequestBackend{
		SessionID: "sess-1",
		RuntimeHandle: map[string]any{
			"gateway_session_id":    "gw-1",
			"gateway_session_token": "token-1",
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "gateway timeout")
}

// HTTPGateway round-trip test to make sure the on-the-wire
// expectations match what the Bun gateway documents.
func TestHTTPGatewayCreateAndCloseSession(t *testing.T) {
	var (
		createBody []byte
		deleteURL  string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/session":
			body := make([]byte, r.ContentLength)
			_, _ = r.Body.Read(body)
			createBody = body
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"gw-1","ws_endpoint":"ws://x","session_token":"tok","playwright_version":"1.50","core":"chromium","context_config":{}}`))
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/session/"):
			deleteURL = r.URL.String()
			_, _ = w.Write([]byte(`{"success":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	gw, err := envbrowser.NewHTTPGateway(srv.URL, srv.Client())
	require.NoError(t, err)

	resp, err := gw.CreateSession(context.Background(), envbrowser.CreateSessionRequest{
		BotID: "envs-sess-1",
		Core:  "chromium",
	})
	require.NoError(t, err)
	require.Equal(t, "gw-1", resp.ID)
	require.Equal(t, "ws://x", resp.WSEndpoint)
	require.Contains(t, string(createBody), "envs-sess-1")

	require.NoError(t, gw.CloseSession(context.Background(), "gw-1", "tok"))
	require.Contains(t, deleteURL, "/session/gw-1")
	require.Contains(t, deleteURL, "token=tok")
}

func TestHTTPGatewayRejectsErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	gw, err := envbrowser.NewHTTPGateway(srv.URL, srv.Client())
	require.NoError(t, err)

	_, err = gw.CreateSession(context.Background(), envbrowser.CreateSessionRequest{BotID: "x"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "HTTP 500")
}
