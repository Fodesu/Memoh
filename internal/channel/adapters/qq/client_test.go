package qq

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestQQAccessTokenAcceptsStringExpiresIn(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/app/getAppAccessToken" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "token-1",
			"expires_in":   "7200",
		})
	}))
	defer server.Close()

	client := &qqClient{
		appID:        "1024",
		clientSecret: "secret",
		httpClient:   server.Client(),
		tokenURL:     server.URL + "/app/getAppAccessToken",
		msgSeq:       make(map[string]int),
	}

	token, err := client.accessToken(context.Background())
	if err != nil {
		t.Fatalf("access token: %v", err)
	}
	if token != "token-1" {
		t.Fatalf("unexpected token: %q", token)
	}
	if remaining := time.Until(client.expiresAt); remaining < 7100*time.Second || remaining > 7200*time.Second {
		t.Fatalf("unexpected token ttl: %s", remaining)
	}
}

func TestDiscoverSelfRequiresAccessToken(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/app/getAppAccessToken" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "token-1",
			"expires_in":   7200,
		})
	}))
	defer server.Close()

	adapter := NewQQAdapter(nil)
	adapter.httpClient = server.Client()
	adapter.tokenURL = server.URL + "/app/getAppAccessToken"
	identity, externalID, err := adapter.DiscoverSelf(context.Background(), map[string]any{
		"appId":        "1024",
		"clientSecret": "secret",
	})
	if err != nil {
		t.Fatalf("DiscoverSelf error = %v", err)
	}
	if externalID != "1024" {
		t.Fatalf("external id = %q, want 1024", externalID)
	}
	if identity["app_id"] != "1024" {
		t.Fatalf("identity = %#v", identity)
	}
}

func TestDiscoverSelfRejectsTokenError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"invalid secret"}`))
	}))
	defer server.Close()

	adapter := NewQQAdapter(nil)
	adapter.httpClient = server.Client()
	adapter.tokenURL = server.URL + "/app/getAppAccessToken"
	_, _, err := adapter.DiscoverSelf(context.Background(), map[string]any{
		"appId":        "1024",
		"clientSecret": "bad",
	})
	if err == nil {
		t.Fatal("expected DiscoverSelf to fail")
	}
}
