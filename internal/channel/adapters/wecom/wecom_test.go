package wecom

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"

	"github.com/felinics/memoh/internal/channel"
)

func startSubscribeServer(t *testing.T, errCode int) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		var subscribeFrame WSFrame
		if err := conn.ReadJSON(&subscribeFrame); err != nil {
			return
		}
		_ = conn.WriteJSON(WSFrame{
			Headers: WSHeaders{ReqID: subscribeFrame.Headers.ReqID},
			ErrCode: errCode,
			ErrMsg:  "denied",
		})
		_, _, _ = conn.ReadMessage()
	}))
}

func TestDiscoverSelf(t *testing.T) {
	t.Parallel()

	server := startSubscribeServer(t, 0)
	defer server.Close()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	adapter := NewWeComAdapter(nil)
	identity, externalID, err := adapter.DiscoverSelf(context.Background(), map[string]any{
		"botId":  "bot_123",
		"secret": "sec",
		"wsUrl":  wsURL,
	})
	if err != nil {
		t.Fatalf("DiscoverSelf error = %v", err)
	}
	if externalID != "bot_123" {
		t.Fatalf("unexpected external id: %q", externalID)
	}
	if identity["bot_id"] != "bot_123" {
		t.Fatalf("unexpected bot_id: %v", identity["bot_id"])
	}
	if identity["aibot_id"] != "bot_123" {
		t.Fatalf("unexpected aibot_id: %v", identity["aibot_id"])
	}
	if _, ok := identity["name"]; ok {
		t.Fatalf("unexpected name field: %v", identity["name"])
	}
	if _, ok := identity["display_name"]; ok {
		t.Fatalf("unexpected display_name field: %v", identity["display_name"])
	}
}

func TestDiscoverSelfRejectsFailedHandshake(t *testing.T) {
	t.Parallel()

	server := startSubscribeServer(t, 40014)
	defer server.Close()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	adapter := NewWeComAdapter(nil)
	_, _, err := adapter.DiscoverSelf(context.Background(), map[string]any{
		"botId":  "bot_123",
		"secret": "bad-secret",
		"wsUrl":  wsURL,
	})
	if err == nil {
		t.Fatal("expected DiscoverSelf to fail")
	}
	if !strings.Contains(err.Error(), "wecom verify credentials") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSelfIdentityPolicyRequiresVerificationBeforeEnable(t *testing.T) {
	t.Parallel()

	policy := (&WeComAdapter{}).SelfIdentityPolicy()
	if !policy.RequireDiscoveryOnEnable {
		t.Fatal("expected WeCom to require credential verification before enable")
	}
	if policy.RequiredSelfIdentityKey != "bot_id" {
		t.Fatalf("required identity key = %q, want bot_id", policy.RequiredSelfIdentityKey)
	}
}

func TestOpenStream_FallbackReplyFromSourceMessageID(t *testing.T) {
	adapter := NewWeComAdapter(nil)
	stream, err := adapter.OpenStream(context.Background(), channel.ChannelConfig{}, "chat_id:chat_1", channel.StreamOptions{
		SourceMessageID: "msg_1",
	})
	if err != nil {
		t.Fatalf("OpenStream error = %v", err)
	}
	ws, ok := stream.(*wecomOutboundStream)
	if !ok {
		t.Fatalf("unexpected stream type: %T", stream)
	}
	if ws.reply == nil || ws.reply.MessageID != "msg_1" || ws.reply.Target != "chat_id:chat_1" {
		t.Fatalf("unexpected reply fallback: %+v", ws.reply)
	}
}
