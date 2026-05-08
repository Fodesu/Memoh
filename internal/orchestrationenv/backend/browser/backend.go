package browser

import (
	"context"
	"errors"
	"strings"

	"github.com/memohai/memoh/internal/orchestrationenv"
)

// runtime handle keys the backend stamps in. Keeping them as
// constants means tests and downstream readers (the kernel, the web
// inspector, future Stage 3-G HITL resume) all spell them the same
// way.
const (
	handleKeyBackend     = "backend"
	handleKeyBackendKind = "backend_kind"
	handleKeyGatewayID   = "gateway_session_id"
	handleKeyGatewayTok  = "gateway_session_token"
	handleKeyWSEndpoint  = "ws_endpoint"
	handleKeyCore        = "core"
	handleKeyExpiresAt   = "expires_at"
	handleKeySessionID   = "env_session_id"
)

// Options configure the browser backend. Defaults come from
// New().
type Options struct {
	// Core selects the playwright core (chromium / firefox).
	// Defaults to "chromium" when empty, mirroring the gateway.
	Core string

	// DefaultTTLMs lets the backend ask the gateway for a longer or
	// shorter session lifetime than the gateway's 30-minute default.
	// Zero leaves the gateway to choose.
	DefaultTTLMs int64

	// BotIDPrefix is prepended to env_session_id when calling the
	// gateway. The gateway requires a bot_id; using a deterministic
	// "envs-" prefix keeps env-managed sessions distinguishable
	// from real bot sessions.
	BotIDPrefix string
}

func (o Options) withDefaults() Options {
	if o.Core == "" {
		o.Core = "chromium"
	}
	if o.BotIDPrefix == "" {
		o.BotIDPrefix = "envs-"
	}
	return o
}

// Backend is the orchestrationenv.Backend implementation for the
// browser env runtime. It delegates allocation and release to the
// browser gateway and persists the gateway's identifiers into the
// env session's runtime handle.
type Backend struct {
	gw   Gateway
	opts Options
}

// New constructs a backend wired to the given gateway. opts are
// merged with defaults.
func New(gw Gateway, opts Options) (*Backend, error) {
	if gw == nil {
		return nil, errors.New("browser backend: gateway is required")
	}
	return &Backend{gw: gw, opts: opts.withDefaults()}, nil
}

// Kind reports the env kind this backend handles.
func (*Backend) Kind() string {
	return orchestrationenv.KindBrowser
}

// Allocate creates a remote Playwright session via the gateway and
// returns a runtime handle the worker can use to drive the browser.
// Resource config knobs honoured: core (chromium / firefox),
// ttl_ms (overrides the backend default), context_config (passed
// through to the gateway).
func (b *Backend) Allocate(ctx context.Context, req orchestrationenv.AllocateRequest) (orchestrationenv.AllocateResult, error) {
	core := stringFrom(req.ResourceConfig, "core")
	if core == "" {
		core = b.opts.Core
	}
	ttl := b.opts.DefaultTTLMs
	if v, ok := req.ResourceConfig["ttl_ms"].(float64); ok && v > 0 {
		ttl = int64(v)
	}
	contextConfig, _ := req.ResourceConfig["context_config"].(map[string]any)

	resp, err := b.gw.CreateSession(ctx, CreateSessionRequest{
		BotID:         b.botIDFor(req),
		Core:          core,
		TTLMs:         ttl,
		ContextConfig: contextConfig,
	})
	if err != nil {
		return orchestrationenv.AllocateResult{}, err
	}
	handle := map[string]any{
		handleKeyBackend:     "browser",
		handleKeyBackendKind: orchestrationenv.KindBrowser,
		handleKeyGatewayID:   resp.ID,
		handleKeyGatewayTok:  resp.SessionToken,
		handleKeyWSEndpoint:  resp.WSEndpoint,
		handleKeyCore:        resp.Core,
		handleKeyExpiresAt:   resp.ExpiresAt,
		handleKeySessionID:   req.SessionID,
	}
	if resp.PlaywrightVersion != "" {
		handle["playwright_version"] = resp.PlaywrightVersion
	}
	return orchestrationenv.AllocateResult{
		RuntimeHandle: handle,
	}, nil
}

// Snapshot records a bookkeeping reference. The gateway has no
// snapshot endpoint today, so the result is intentionally empty
// other than identifiers; Stage 3-I will replace this with real
// cookie / storage / screenshot capture.
func (*Backend) Snapshot(_ context.Context, req orchestrationenv.SnapshotRequestBackend) (orchestrationenv.SnapshotResult, error) {
	return orchestrationenv.SnapshotResult{
		RuntimeRef: map[string]any{
			"backend":            "browser",
			"unsupported":        true,
			"gateway_session_id": stringFromHandle(req.RuntimeHandle, handleKeyGatewayID),
			"ws_endpoint":        stringFromHandle(req.RuntimeHandle, handleKeyWSEndpoint),
			"snapshot_kind":      req.Kind,
		},
	}, nil
}

// Release closes the gateway session. A missing handle is treated
// as success — there is nothing to clean up.
func (b *Backend) Release(ctx context.Context, req orchestrationenv.ReleaseRequestBackend) error {
	gatewayID := stringFromHandle(req.RuntimeHandle, handleKeyGatewayID)
	gatewayTok := stringFromHandle(req.RuntimeHandle, handleKeyGatewayTok)
	if gatewayID == "" {
		return nil
	}
	return b.gw.CloseSession(ctx, gatewayID, gatewayTok)
}

// botIDFor returns the bot_id the backend uses against the gateway.
// Uses the env_session_id directly so retries reattach, with the
// configured prefix for namespacing.
func (b *Backend) botIDFor(req orchestrationenv.AllocateRequest) string {
	if strings.TrimSpace(req.SessionID) == "" {
		return b.opts.BotIDPrefix + "anonymous"
	}
	return b.opts.BotIDPrefix + req.SessionID
}

func stringFrom(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func stringFromHandle(handle map[string]any, key string) string {
	if handle == nil {
		return ""
	}
	if v, ok := handle[key].(string); ok {
		return v
	}
	return ""
}
