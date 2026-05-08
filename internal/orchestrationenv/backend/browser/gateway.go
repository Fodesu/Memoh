package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Gateway is the minimal browser-gateway client surface the env
// backend needs. Defining it here, rather than depending on the
// agent tools' BrowserProvider, keeps the env backend testable and
// independent of bot identity.
type Gateway interface {
	CreateSession(ctx context.Context, req CreateSessionRequest) (*CreateSessionResponse, error)
	CloseSession(ctx context.Context, sessionID, sessionToken string) error
}

// CreateSessionRequest is the payload the gateway expects on
// POST /session. The fields mirror the Elysia schema in
// apps/browser/src/modules/session.ts so callers do not need to
// re-derive them.
type CreateSessionRequest struct {
	BotID         string         `json:"bot_id"`
	Core          string         `json:"core,omitempty"`
	TTLMs         int64          `json:"ttl_ms,omitempty"`
	ContextConfig map[string]any `json:"context_config,omitempty"`
}

// CreateSessionResponse mirrors the gateway response. The fields are
// what the worker needs to drive Playwright remotely.
type CreateSessionResponse struct {
	ID                string         `json:"id"`
	WSEndpoint        string         `json:"ws_endpoint"`
	SessionToken      string         `json:"session_token"` //nolint:gosec // gateway-issued auth token, not a secret literal
	PlaywrightVersion string         `json:"playwright_version"`
	Core              string         `json:"core"`
	ContextConfig     map[string]any `json:"context_config"`
	ExpiresAt         string         `json:"expires_at"`
}

// HTTPGateway is the production Gateway that talks to a real browser
// gateway over HTTP. It is intentionally small — heartbeat / status
// calls live on the env Manager's renew path, not here, so this type
// stays a one-shot allocator.
type HTTPGateway struct {
	BaseURL    string
	HTTPClient *http.Client
}

// NewHTTPGateway constructs a gateway pointed at base. The HTTP
// client falls back to a 30s timeout when not provided.
func NewHTTPGateway(base string, client *http.Client) (*HTTPGateway, error) {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return nil, errors.New("browser backend: gateway base url is required")
	}
	if _, err := url.Parse(base); err != nil {
		return nil, fmt.Errorf("browser backend: invalid base url: %w", err)
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &HTTPGateway{BaseURL: base, HTTPClient: client}, nil
}

// CreateSession POSTs to /session and returns the gateway's response.
func (g *HTTPGateway) CreateSession(ctx context.Context, req CreateSessionRequest) (*CreateSessionResponse, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("browser backend: marshal session request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, g.BaseURL+"/session", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("browser backend: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := g.HTTPClient.Do(httpReq) //nolint:gosec // base url comes from operator config, not user input
	if err != nil {
		return nil, fmt.Errorf("browser backend: gateway create call: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("browser backend: read create response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("browser backend: gateway create failed (HTTP %d): %s", resp.StatusCode, string(body))
	}
	out := &CreateSessionResponse{}
	if err := json.Unmarshal(body, out); err != nil {
		return nil, fmt.Errorf("browser backend: decode create response: %w", err)
	}
	if out.ID == "" {
		return nil, errors.New("browser backend: gateway returned empty session id")
	}
	return out, nil
}

// CloseSession DELETEs /session/:id?token=... so a released env
// session no longer counts against the gateway's per-bot limits.
func (g *HTTPGateway) CloseSession(ctx context.Context, sessionID, sessionToken string) error {
	if strings.TrimSpace(sessionID) == "" {
		return nil
	}
	reqURL := fmt.Sprintf("%s/session/%s?token=%s", g.BaseURL, url.PathEscape(sessionID), url.QueryEscape(sessionToken))
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, reqURL, nil)
	if err != nil {
		return fmt.Errorf("browser backend: build delete request: %w", err)
	}
	resp, err := g.HTTPClient.Do(httpReq) //nolint:gosec // base url comes from operator config, not user input
	if err != nil {
		return fmt.Errorf("browser backend: gateway delete call: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("browser backend: gateway delete failed (HTTP %d): %s", resp.StatusCode, string(body))
	}
	return nil
}
