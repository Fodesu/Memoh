package slack

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"

	"github.com/memohai/memoh/internal/channel"
)

func TestSlackResolveAttachmentDownloadsPrivateURLWithBearerToken(t *testing.T) {
	t.Parallel()

	var gotAuth string
	adapter := NewSlackAdapter(nil)
	client := slack.New(
		"xoxb-test-token",
		slack.OptionHTTPClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.String() != "https://files.slack.test/private/file.txt" {
				return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("not found")), Header: make(http.Header)}, nil
			}
			gotAuth = r.Header.Get("Authorization")
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/plain"}},
				Body:       io.NopCloser(strings.NewReader("slack-private-file")),
			}, nil
		})}),
		slack.OptionRetry(3),
	)

	payload, err := adapter.resolveAttachmentWithClient(context.Background(), client, channel.Attachment{
		URL:         "https://files.slack.test/private/file.txt",
		Name:        "file.txt",
		Mime:        "text/plain",
		Size:        18,
		Type:        channel.AttachmentFile,
		PlatformKey: "F123",
	})
	if err != nil {
		t.Fatalf("ResolveAttachment: %v", err)
	}
	defer func() { _ = payload.Reader.Close() }()

	data, err := io.ReadAll(payload.Reader)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(data) != "slack-private-file" {
		t.Fatalf("unexpected payload: %q", string(data))
	}
	if gotAuth != "Bearer xoxb-test-token" {
		t.Fatalf("unexpected auth header: %q", gotAuth)
	}
}

func TestSlackResolveAttachmentFallsBackToFilesInfo(t *testing.T) {
	t.Parallel()

	var gotFileToken string
	var gotDownloadAuth string
	adapter := NewSlackAdapter(nil)
	client := slack.New(
		"xoxb-test-token",
		slack.OptionAPIURL("https://slack.test/api/"),
		slack.OptionHTTPClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			switch r.URL.String() {
			case "https://slack.test/api/files.info":
				if err := r.ParseForm(); err != nil {
					t.Fatalf("ParseForm: %v", err)
				}
				gotFileToken = r.FormValue("token")
				body, _ := json.Marshal(map[string]any{
					"ok": true,
					"file": map[string]any{
						"id":                   "F123",
						"name":                 "fallback.txt",
						"mimetype":             "text/plain",
						"size":                 13,
						"url_private_download": "https://files.slack.test/download/F123",
					},
				})
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(string(body))),
				}, nil
			case "https://files.slack.test/download/F123":
				gotDownloadAuth = r.Header.Get("Authorization")
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"text/plain"}},
					Body:       io.NopCloser(strings.NewReader("fallback-file")),
				}, nil
			default:
				return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("not found")), Header: make(http.Header)}, nil
			}
		})}),
		slack.OptionRetry(3),
	)

	payload, err := adapter.resolveAttachmentWithClient(context.Background(), client, channel.Attachment{
		PlatformKey: "F123",
		Type:        channel.AttachmentFile,
	})
	if err != nil {
		t.Fatalf("resolveAttachmentWithClient: %v", err)
	}
	defer func() { _ = payload.Reader.Close() }()

	data, err := io.ReadAll(payload.Reader)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(data) != "fallback-file" {
		t.Fatalf("unexpected payload: %q", string(data))
	}
	if gotFileToken != "xoxb-test-token" {
		t.Fatalf("unexpected files.info token: %q", gotFileToken)
	}
	if gotDownloadAuth != "Bearer xoxb-test-token" {
		t.Fatalf("unexpected download auth header: %q", gotDownloadAuth)
	}
	if payload.Name != "fallback.txt" {
		t.Fatalf("unexpected name: %q", payload.Name)
	}
	if payload.Mime != "text/plain" {
		t.Fatalf("unexpected mime: %q", payload.Mime)
	}
}

func TestSlackHandleMessageEventStoresDMChannelID(t *testing.T) {
	t.Parallel()

	adapter := NewSlackAdapter(nil)
	conn := &slackConnection{}
	cfg := channel.ChannelConfig{ID: "cfg-1", BotID: "bot-1"}
	msgCh := make(chan channel.InboundMessage, 1)

	adapter.handleMessageEvent(context.Background(), conn, &slackevents.MessageEvent{
		User:        "U123",
		Text:        "hello",
		TimeStamp:   "1710000000.000100",
		Channel:     "D123",
		ChannelType: "im",
		Message:     &slack.Msg{Text: "hello"},
	}, cfg, func(_ context.Context, _ channel.ChannelConfig, msg channel.InboundMessage) error {
		msgCh <- msg
		return nil
	}, "UBOT")

	select {
	case msg := <-msgCh:
		if got := msg.Sender.Attribute("channel_id"); got != "D123" {
			t.Fatalf("unexpected channel_id: %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for inbound message")
	}
}

func TestSlackHandleMessageEventSkipsChannelIDOutsideDM(t *testing.T) {
	t.Parallel()

	adapter := NewSlackAdapter(nil)
	conn := &slackConnection{}
	cfg := channel.ChannelConfig{ID: "cfg-2", BotID: "bot-1"}
	msgCh := make(chan channel.InboundMessage, 1)

	adapter.handleMessageEvent(context.Background(), conn, &slackevents.MessageEvent{
		User:        "U123",
		Text:        "hello",
		TimeStamp:   "1710000000.000101",
		Channel:     "C123",
		ChannelType: "channel",
		Message:     &slack.Msg{Text: "hello"},
	}, cfg, func(_ context.Context, _ channel.ChannelConfig, msg channel.InboundMessage) error {
		msgCh <- msg
		return nil
	}, "UBOT")

	select {
	case msg := <-msgCh:
		if got := msg.Sender.Attribute("channel_id"); got != "" {
			t.Fatalf("expected empty channel_id, got %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for inbound message")
	}
}

func TestSlackHandleMessageEventResolvesConversationName(t *testing.T) {
	t.Parallel()

	var conversationsInfoCalls int
	adapter := NewSlackAdapter(nil)
	conn := &slackConnection{
		api: slack.New(
			"xoxb-test-token",
			slack.OptionAPIURL("https://slack.test/api/"),
			slack.OptionHTTPClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				switch r.URL.String() {
				case "https://slack.test/api/conversations.info":
					conversationsInfoCalls++
					if err := r.ParseForm(); err != nil {
						t.Fatalf("ParseForm: %v", err)
					}
					body, _ := json.Marshal(map[string]any{
						"ok": true,
						"channel": map[string]any{
							"id":   "C123",
							"name": "general",
						},
					})
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     http.Header{"Content-Type": []string{"application/json"}},
						Body:       io.NopCloser(strings.NewReader(string(body))),
					}, nil
				default:
					return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("not found")), Header: make(http.Header)}, nil
				}
			})}),
			slack.OptionRetry(3),
		),
	}
	cfg := channel.ChannelConfig{ID: "cfg-name", BotID: "bot-1"}
	msgCh := make(chan channel.InboundMessage, 1)

	adapter.handleMessageEvent(context.Background(), conn, &slackevents.MessageEvent{
		User:        "U123",
		Text:        "hello",
		TimeStamp:   "1710000000.000102",
		Channel:     "C123",
		ChannelType: "channel",
		Message:     &slack.Msg{Text: "hello"},
	}, cfg, func(_ context.Context, _ channel.ChannelConfig, msg channel.InboundMessage) error {
		msgCh <- msg
		return nil
	}, "UBOT")

	select {
	case msg := <-msgCh:
		if msg.Conversation.Name != "general" {
			t.Fatalf("unexpected conversation name: %q", msg.Conversation.Name)
		}
		gotMeta, _ := msg.Metadata["channel_name"].(string)
		if gotMeta != "general" {
			t.Fatalf("unexpected metadata channel_name: %q", gotMeta)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for inbound message")
	}
	if conversationsInfoCalls != 1 {
		t.Fatalf("unexpected conversations.info calls: %d", conversationsInfoCalls)
	}
}

func TestSlackLookupConversationNameCachesResolvedNames(t *testing.T) {
	t.Parallel()

	var conversationsInfoCalls int
	adapter := NewSlackAdapter(nil)
	api := slack.New(
		"xoxb-test-token",
		slack.OptionAPIURL("https://slack.test/api/"),
		slack.OptionHTTPClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			switch r.URL.String() {
			case "https://slack.test/api/conversations.info":
				conversationsInfoCalls++
				body, _ := json.Marshal(map[string]any{
					"ok": true,
					"channel": map[string]any{
						"id":   "C123",
						"name": "general",
					},
				})
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(string(body))),
				}, nil
			default:
				return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("not found")), Header: make(http.Header)}, nil
			}
		})}),
		slack.OptionRetry(3),
	)

	first := adapter.lookupConversationName(context.Background(), api, "cfg-cache", "C123")
	second := adapter.lookupConversationName(context.Background(), api, "cfg-cache", "C123")
	if first != "general" || second != "general" {
		t.Fatalf("unexpected cached names: %q / %q", first, second)
	}
	if conversationsInfoCalls != 1 {
		t.Fatalf("unexpected conversations.info calls: %d", conversationsInfoCalls)
	}
}

func TestSlackHandleMessageEventKeepsFlowWhenConversationNameLookupFails(t *testing.T) {
	t.Parallel()

	adapter := NewSlackAdapter(nil)
	conn := &slackConnection{
		api: slack.New(
			"xoxb-test-token",
			slack.OptionAPIURL("https://slack.test/api/"),
			slack.OptionHTTPClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				switch r.URL.String() {
				case "https://slack.test/api/conversations.info":
					body, _ := json.Marshal(map[string]any{
						"ok":    false,
						"error": "missing_scope",
					})
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     http.Header{"Content-Type": []string{"application/json"}},
						Body:       io.NopCloser(strings.NewReader(string(body))),
					}, nil
				default:
					return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("not found")), Header: make(http.Header)}, nil
				}
			})}),
			slack.OptionRetry(3),
		),
	}
	cfg := channel.ChannelConfig{ID: "cfg-name-fail", BotID: "bot-1"}
	msgCh := make(chan channel.InboundMessage, 1)

	adapter.handleMessageEvent(context.Background(), conn, &slackevents.MessageEvent{
		User:        "U123",
		Text:        "hello",
		TimeStamp:   "1710000000.000103",
		Channel:     "C123",
		ChannelType: "channel",
		Message:     &slack.Msg{Text: "hello"},
	}, cfg, func(_ context.Context, _ channel.ChannelConfig, msg channel.InboundMessage) error {
		msgCh <- msg
		return nil
	}, "UBOT")

	select {
	case msg := <-msgCh:
		if msg.Conversation.Name != "" {
			t.Fatalf("expected empty conversation name, got %q", msg.Conversation.Name)
		}
		gotMeta, _ := msg.Metadata["channel_name"].(string)
		if gotMeta != "" {
			t.Fatalf("expected empty metadata channel_name, got %q", gotMeta)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for inbound message")
	}
}

func TestSlackSendReturnsAttachmentUploadFailures(t *testing.T) {
	t.Parallel()

	var postMessageCalls int
	adapter := NewSlackAdapter(nil)
	api := slack.New(
		"xoxb-test-token",
		slack.OptionAPIURL("https://slack.test/api/"),
		slack.OptionHTTPClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			switch r.URL.String() {
			case "https://slack.test/api/files.getUploadURLExternal":
				if err := r.ParseForm(); err != nil {
					t.Fatalf("ParseForm: %v", err)
				}
				body, _ := json.Marshal(map[string]any{
					"ok":         true,
					"upload_url": "https://upload.slack.test/fail",
					"file_id":    "F123",
				})
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(string(body))),
				}, nil
			case "https://upload.slack.test/fail":
				return &http.Response{
					StatusCode: http.StatusInternalServerError,
					Body:       io.NopCloser(strings.NewReader("upload failed")),
					Header:     make(http.Header),
				}, nil
			case "https://slack.test/api/files.completeUploadExternal":
				t.Fatal("completeUploadExternal should not be called after failed upload")
				return nil, nil
			case "https://slack.test/api/chat.postMessage":
				postMessageCalls++
				t.Fatal("chat.postMessage should not be called after failed attachment upload")
				return nil, nil
			default:
				return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("not found")), Header: make(http.Header)}, nil
			}
		})}),
		slack.OptionRetry(3),
	)

	err := adapter.sendSlackMessage(context.Background(), api, "C123", channel.OutboundMessage{
		Message: channel.Message{
			Text: "hello",
			Attachments: []channel.Attachment{{
				Type:   channel.AttachmentFile,
				Base64: "data:text/plain;base64,aGVsbG8=",
				Name:   "hello.txt",
				Mime:   "text/plain",
				Size:   5,
			}},
		},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "UploadToURL") {
		t.Fatalf("unexpected error: %v", err)
	}
	if postMessageCalls != 0 {
		t.Fatalf("unexpected chat.postMessage calls: %d", postMessageCalls)
	}
}

func TestSlackSendAttachmentOnlyReturnsUploadFailures(t *testing.T) {
	t.Parallel()

	api := slack.New(
		"xoxb-test-token",
		slack.OptionAPIURL("https://slack.test/api/"),
		slack.OptionHTTPClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			switch r.URL.String() {
			case "https://slack.test/api/files.getUploadURLExternal":
				body, _ := json.Marshal(map[string]any{
					"ok":         true,
					"upload_url": "https://upload.slack.test/fail-only-attachment",
					"file_id":    "F124",
				})
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(string(body))),
				}, nil
			case "https://upload.slack.test/fail-only-attachment":
				return &http.Response{
					StatusCode: http.StatusInternalServerError,
					Body:       io.NopCloser(strings.NewReader("upload failed")),
					Header:     make(http.Header),
				}, nil
			case "https://slack.test/api/files.completeUploadExternal":
				t.Fatal("completeUploadExternal should not be called after failed attachment upload")
				return nil, nil
			case "https://slack.test/api/chat.postMessage":
				t.Fatal("chat.postMessage should not be called for attachment-only failure")
				return nil, nil
			default:
				return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("not found")), Header: make(http.Header)}, nil
			}
		})}),
		slack.OptionRetry(3),
	)

	adapter := NewSlackAdapter(nil)
	err := adapter.sendSlackMessage(context.Background(), api, "C123", channel.OutboundMessage{
		Message: channel.Message{
			Attachments: []channel.Attachment{{
				Type:   channel.AttachmentFile,
				Base64: "data:text/plain;base64,aGVsbG8=",
				Name:   "hello.txt",
				Mime:   "text/plain",
				Size:   5,
			}},
		},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "UploadToURL") {
		t.Fatalf("unexpected error: %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
