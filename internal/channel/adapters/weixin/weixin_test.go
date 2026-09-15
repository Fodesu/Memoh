package weixin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/felinics/memoh/internal/channel"
)

func TestWeixinAdapter_Type(t *testing.T) {
	adapter := NewWeixinAdapter(nil)
	if adapter.Type() != Type {
		t.Errorf("Type() = %v, want %v", adapter.Type(), Type)
	}
}

func TestWeixinAdapter_Descriptor(t *testing.T) {
	adapter := NewWeixinAdapter(nil)
	desc := adapter.Descriptor()

	if desc.Type != Type {
		t.Errorf("desc.Type = %v", desc.Type)
	}
	if desc.DisplayName != "WeChat" {
		t.Errorf("desc.DisplayName = %q", desc.DisplayName)
	}
	if !desc.Capabilities.Text {
		t.Error("should support text")
	}
	if !desc.Capabilities.Media {
		t.Error("should support media")
	}
	if !desc.Capabilities.Attachments {
		t.Error("should support attachments")
	}
	if len(desc.Capabilities.ChatTypes) != 1 || desc.Capabilities.ChatTypes[0] != channel.ConversationTypePrivate {
		t.Errorf("chat types = %v", desc.Capabilities.ChatTypes)
	}

	if _, ok := desc.ConfigSchema.Fields["token"]; !ok {
		t.Error("config schema should have 'token' field")
	}
	if desc.ConfigSchema.Fields["token"].Type != channel.FieldSecret {
		t.Error("token field should be secret")
	}
	if !desc.ConfigSchema.Fields["token"].Required {
		t.Error("token field should be required")
	}
}

func TestWeixinAdapter_Interfaces(_ *testing.T) {
	adapter := NewWeixinAdapter(nil)

	// Adapter
	var _ channel.Adapter = adapter
	// ConfigNormalizer
	var _ channel.ConfigNormalizer = adapter
	// TargetResolver
	var _ channel.TargetResolver = adapter
	// BindingMatcher
	var _ channel.BindingMatcher = adapter
	// Receiver
	var _ channel.Receiver = adapter
	// Sender
	var _ channel.Sender = adapter
	// AttachmentResolver
	var _ channel.AttachmentResolver = adapter
	// ProcessingStatusNotifier
	var _ channel.ProcessingStatusNotifier = adapter
	// ConfigVerifier
	var _ channel.ConfigVerifier = adapter
}

func TestVerifyConfigSuccess(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ilink/bot/msg/notifystart" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer bot-token" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ret":0}`))
	}))
	defer server.Close()

	adapter := NewWeixinAdapter(nil)
	if err := adapter.VerifyConfig(context.Background(), map[string]any{
		"token":   "bot-token",
		"baseUrl": server.URL,
	}); err != nil {
		t.Fatalf("VerifyConfig error = %v", err)
	}
}

func TestVerifyConfigRejectsInvalidToken(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ret":-1,"errmsg":"invalid token"}`))
	}))
	defer server.Close()

	adapter := NewWeixinAdapter(nil)
	err := adapter.VerifyConfig(context.Background(), map[string]any{
		"token":   "bad-token",
		"baseUrl": server.URL,
	})
	if err == nil {
		t.Fatal("expected VerifyConfig to fail")
	}
	if !strings.Contains(err.Error(), "weixin verify credentials") {
		t.Fatalf("unexpected error: %v", err)
	}
}
