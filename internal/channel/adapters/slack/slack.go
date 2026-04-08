package slack

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"

	"github.com/memohai/memoh/internal/channel"
	"github.com/memohai/memoh/internal/channel/adapters/common"
	"github.com/memohai/memoh/internal/media"
)

const (
	inboundDedupTTL = time.Minute
	slackMaxLength  = 40000
	channelNameTTL  = 5 * time.Minute
)

// assetOpener reads stored asset bytes by content hash.
type assetOpener interface {
	Open(ctx context.Context, botID, contentHash string) (io.ReadCloser, media.Asset, error)
}

type slackConnection struct {
	api    *slack.Client
	sm     *socketmode.Client
	cancel context.CancelFunc
}

type cachedSlackChannelName struct {
	name     string
	cachedAt time.Time
}

type SlackAdapter struct {
	logger       *slog.Logger
	mu           sync.RWMutex
	connections  map[string]*slackConnection       // keyed by config ID
	seenMessages map[string]time.Time              // keyed by configID:messageTS
	channelNames map[string]cachedSlackChannelName // keyed by configID:channelID
	assets       assetOpener
}

func NewSlackAdapter(log *slog.Logger) *SlackAdapter {
	if log == nil {
		log = slog.Default()
	}
	return &SlackAdapter{
		logger:       log.With(slog.String("adapter", "slack")),
		connections:  make(map[string]*slackConnection),
		seenMessages: make(map[string]time.Time),
		channelNames: make(map[string]cachedSlackChannelName),
	}
}

// SetAssetOpener configures the asset opener for reading stored attachments by content hash.
func (a *SlackAdapter) SetAssetOpener(opener assetOpener) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.assets = opener
}

func (*SlackAdapter) Type() channel.ChannelType {
	return Type
}

func (*SlackAdapter) Descriptor() channel.Descriptor {
	return channel.Descriptor{
		Type:        Type,
		DisplayName: "Slack",
		Capabilities: channel.ChannelCapabilities{
			Text:           true,
			Markdown:       true,
			Reply:          true,
			Attachments:    true,
			Media:          true,
			Streaming:      true,
			BlockStreaming: true,
			Reactions:      true,
			Threads:        true,
			Edit:           true,
		},
		ConfigSchema: channel.ConfigSchema{
			Version: 1,
			Fields: map[string]channel.FieldSchema{
				"botToken": {
					Type:        channel.FieldSecret,
					Required:    true,
					Title:       "Bot Token",
					Description: "Slack Bot User OAuth Token (xoxb-...)",
				},
				"appToken": {
					Type:        channel.FieldSecret,
					Required:    true,
					Title:       "App-Level Token",
					Description: "Slack App-Level Token for Socket Mode (xapp-...)",
				},
			},
		},
		UserConfigSchema: channel.ConfigSchema{
			Version: 1,
			Fields: map[string]channel.FieldSchema{
				"user_id":    {Type: channel.FieldString},
				"channel_id": {Type: channel.FieldString},
				"username":   {Type: channel.FieldString},
			},
		},
		TargetSpec: channel.TargetSpec{
			Format: "channel_id | user_id",
			Hints: []channel.TargetHint{
				{Label: "Channel ID", Example: "C0123456789"},
				{Label: "User ID", Example: "U0123456789"},
			},
		},
	}
}

func (a *SlackAdapter) newAPIClient(cfg Config, options ...slack.Option) *slack.Client {
	opts := []slack.Option{
		slack.OptionRetry(3),
	}
	opts = append(opts, options...)
	return slack.New(cfg.BotToken, opts...)
}

func newSocketModeClient(cfg Config) (*slack.Client, *socketmode.Client) {
	api := slack.New(
		cfg.BotToken,
		slack.OptionRetry(3),
		slack.OptionAppLevelToken(cfg.AppToken),
	)
	return api, socketmode.New(api)
}

func (a *SlackAdapter) getOrCreateConnection(channelCfg channel.ChannelConfig, cfg Config) (*slackConnection, error) {
	a.mu.RLock()
	conn, ok := a.connections[channelCfg.ID]
	a.mu.RUnlock()
	if ok {
		return conn, nil
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if c, ok := a.connections[channelCfg.ID]; ok {
		return c, nil
	}

	api, sm := newSocketModeClient(cfg)

	conn = &slackConnection{
		api: api,
		sm:  sm,
	}
	a.connections[channelCfg.ID] = conn
	return conn, nil
}

func (a *SlackAdapter) Connect(ctx context.Context, cfg channel.ChannelConfig, handler channel.InboundHandler) (channel.Connection, error) {
	if a.logger != nil {
		a.logger.Info("start", slog.String("config_id", cfg.ID))
	}

	slackCfg, err := parseConfig(cfg.Credentials)
	if err != nil {
		return nil, err
	}

	conn, err := a.getOrCreateConnection(cfg, slackCfg)
	if err != nil {
		return nil, err
	}

	// Discover self identity for filtering bot's own messages
	authResp, err := conn.api.AuthTest()
	if err != nil {
		return nil, fmt.Errorf("slack auth test: %w", err)
	}
	selfUserID := authResp.UserID

	smCtx, cancel := context.WithCancel(ctx)
	conn.cancel = cancel

	go func() {
		for {
			select {
			case <-smCtx.Done():
				return
			case evt, ok := <-conn.sm.Events:
				if !ok {
					return
				}
				a.handleSocketModeEvent(smCtx, conn, evt, cfg, handler, selfUserID)
			}
		}
	}()

	go func() {
		if err := conn.sm.RunContext(smCtx); err != nil && a.logger != nil {
			if !errors.Is(err, context.Canceled) {
				a.logger.Error("socket mode run error", slog.String("config_id", cfg.ID), slog.Any("error", err))
			}
		}
	}()

	stop := func(_ context.Context) error {
		if a.logger != nil {
			a.logger.Info("stop", slog.String("config_id", cfg.ID))
		}
		cancel()
		a.clearConnection(cfg.ID)
		return nil
	}

	return channel.NewConnection(cfg, stop), nil
}

func (a *SlackAdapter) handleSocketModeEvent(
	ctx context.Context,
	conn *slackConnection,
	evt socketmode.Event,
	cfg channel.ChannelConfig,
	handler channel.InboundHandler,
	selfUserID string,
) {
	switch evt.Type {
	case socketmode.EventTypeEventsAPI:
		eventsAPIEvent, ok := evt.Data.(slackevents.EventsAPIEvent)
		if !ok {
			return
		}
		if evt.Request != nil {
			conn.sm.Ack(*evt.Request)
		}

		if eventsAPIEvent.Type != slackevents.CallbackEvent {
			return
		}

		switch ev := eventsAPIEvent.InnerEvent.Data.(type) {
		case *slackevents.MessageEvent:
			a.handleMessageEvent(ctx, conn, ev, cfg, handler, selfUserID)
		case *slackevents.AppMentionEvent:
			a.handleAppMentionEvent(ctx, conn, ev, cfg, handler)
		}

	case socketmode.EventTypeConnecting:
		if a.logger != nil {
			a.logger.Info("connecting to Slack Socket Mode", slog.String("config_id", cfg.ID))
		}

	case socketmode.EventTypeConnected:
		if a.logger != nil {
			a.logger.Info("connected to Slack Socket Mode", slog.String("config_id", cfg.ID))
		}

	case socketmode.EventTypeConnectionError:
		if a.logger != nil {
			a.logger.Error("Slack Socket Mode connection error", slog.String("config_id", cfg.ID))
		}

	case socketmode.EventTypeInteractive:
		if evt.Request != nil {
			conn.sm.Ack(*evt.Request)
		}

	case socketmode.EventTypeSlashCommand:
		if evt.Request != nil {
			conn.sm.Ack(*evt.Request)
		}
	}
}

func (a *SlackAdapter) handleMessageEvent(
	ctx context.Context,
	conn *slackConnection,
	ev *slackevents.MessageEvent,
	cfg channel.ChannelConfig,
	handler channel.InboundHandler,
	selfUserID string,
) {
	if ev.BotID != "" || ev.User == "" || ev.User == selfUserID {
		return
	}

	// Skip message subtypes that aren't regular messages
	if ev.SubType != "" && ev.SubType != "file_share" {
		return
	}

	text := strings.TrimSpace(ev.Text)
	attachments := a.collectAttachments(ev.Message)
	if text == "" && len(attachments) == 0 {
		return
	}

	if a.isDuplicateInbound(cfg.ID, ev.TimeStamp) {
		return
	}

	chatType := "channel"
	switch ev.ChannelType {
	case "im":
		chatType = "direct"
	case "mpim":
		chatType = "group"
	case "group":
		chatType = "private_channel"
	}

	// Resolve user display name
	displayName := a.resolveUserDisplayName(conn.api, ev.User)

	isMentioned := strings.Contains(ev.Text, "<@"+selfUserID+">")

	threadID := ev.ThreadTimeStamp
	if ev.Message != nil && strings.TrimSpace(ev.Message.ThreadTimestamp) != "" {
		threadID = strings.TrimSpace(ev.Message.ThreadTimestamp)
	}
	conversationName := a.lookupConversationName(ctx, conn.api, cfg.ID, ev.Channel)

	msg := channel.InboundMessage{
		Channel: Type,
		Message: channel.Message{
			ID:          ev.TimeStamp,
			Format:      channel.MessageFormatPlain,
			Text:        text,
			Attachments: attachments,
		},
		BotID:       cfg.BotID,
		ReplyTarget: ev.Channel,
		Sender: channel.Identity{
			SubjectID:   ev.User,
			DisplayName: displayName,
			Attributes:  slackIdentityAttributes(ev.User, "", ev.ChannelType, ev.Channel),
		},
		Conversation: channel.Conversation{
			ID:       ev.Channel,
			Type:     chatType,
			Name:     conversationName,
			ThreadID: threadID,
		},
		ReceivedAt: time.Now().UTC(),
		Source:     "slack",
		Metadata: map[string]any{
			"channel_type": ev.ChannelType,
			"channel_name": conversationName,
			"is_mentioned": isMentioned,
			"thread_ts":    threadID,
			"subtype":      ev.SubType,
		},
	}

	if a.logger != nil {
		a.logger.Info("inbound received",
			slog.String("config_id", cfg.ID),
			slog.String("chat_type", chatType),
			slog.String("user_id", ev.User),
			slog.String("text", common.SummarizeText(text)),
		)
	}

	go func() {
		if err := handler(ctx, cfg, msg); err != nil && a.logger != nil {
			a.logger.Error("handle inbound failed", slog.String("config_id", cfg.ID), slog.Any("error", err))
		}
	}()
}

func (a *SlackAdapter) handleAppMentionEvent(
	ctx context.Context,
	conn *slackConnection,
	ev *slackevents.AppMentionEvent,
	cfg channel.ChannelConfig,
	handler channel.InboundHandler,
) {
	if ev.BotID != "" || ev.User == "" {
		return
	}

	text := strings.TrimSpace(ev.Text)
	if text == "" {
		return
	}

	if a.isDuplicateInbound(cfg.ID, ev.TimeStamp) {
		return
	}

	displayName := a.resolveUserDisplayName(conn.api, ev.User)

	threadID := ev.ThreadTimeStamp
	conversationName := a.lookupConversationName(ctx, conn.api, cfg.ID, ev.Channel)

	msg := channel.InboundMessage{
		Channel: Type,
		Message: channel.Message{
			ID:     ev.TimeStamp,
			Format: channel.MessageFormatPlain,
			Text:   text,
		},
		BotID:       cfg.BotID,
		ReplyTarget: ev.Channel,
		Sender: channel.Identity{
			SubjectID:   ev.User,
			DisplayName: displayName,
			Attributes: map[string]string{
				"user_id": ev.User,
			},
		},
		Conversation: channel.Conversation{
			ID:       ev.Channel,
			Type:     "channel",
			Name:     conversationName,
			ThreadID: threadID,
		},
		ReceivedAt: time.Now().UTC(),
		Source:     "slack",
		Metadata: map[string]any{
			"channel_name": conversationName,
			"is_mentioned": true,
			"thread_ts":    threadID,
		},
	}

	if a.logger != nil {
		a.logger.Info("app mention received",
			slog.String("config_id", cfg.ID),
			slog.String("user_id", ev.User),
			slog.String("text", common.SummarizeText(text)),
		)
	}

	go func() {
		if err := handler(ctx, cfg, msg); err != nil && a.logger != nil {
			a.logger.Error("handle inbound failed", slog.String("config_id", cfg.ID), slog.Any("error", err))
		}
	}()
}

func (a *SlackAdapter) Send(ctx context.Context, cfg channel.ChannelConfig, msg channel.OutboundMessage) error {
	slackCfg, err := parseConfig(cfg.Credentials)
	if err != nil {
		return err
	}

	target := strings.TrimSpace(msg.Target)
	if target == "" {
		return errors.New("slack target is required")
	}

	return a.sendSlackMessage(ctx, a.newAPIClient(slackCfg), target, msg)
}

func (a *SlackAdapter) sendSlackMessage(ctx context.Context, api *slack.Client, channelID string, msg channel.OutboundMessage) error {
	text := truncateSlackText(msg.Message.Text)
	threadTS := ""
	if msg.Message.Reply != nil && msg.Message.Reply.MessageID != "" {
		threadTS = msg.Message.Reply.MessageID
	}

	opts := []slack.MsgOption{
		slack.MsgOptionText(text, false),
	}

	if threadTS != "" {
		opts = append(opts, slack.MsgOptionTS(threadTS))
	}

	if len(msg.Message.Attachments) > 0 {
		for _, att := range msg.Message.Attachments {
			if err := a.uploadAttachment(ctx, api, channelID, threadTS, att); err != nil {
				if a.logger != nil {
					a.logger.Error("upload attachment failed", slog.Any("error", err))
				}
				return err
			}
		}
	}

	if text == "" && len(msg.Message.Attachments) > 0 {
		return nil
	}

	if text == "" {
		return errors.New("cannot send empty message")
	}

	_, _, err := api.PostMessageContext(ctx, channelID, opts...)
	return err
}

func (a *SlackAdapter) uploadAttachment(ctx context.Context, api *slack.Client, channelID string, threadTS string, att channel.Attachment) error {
	name := att.Name
	if name == "" {
		name = "attachment"
		ext := mimeExtension(att.Mime)
		if ext != "" {
			name += ext
		}
	}

	var reader io.Reader

	var botID string
	if att.Metadata != nil {
		if bid, ok := att.Metadata["bot_id"].(string); ok && bid != "" {
			botID = bid
		}
	}

	a.mu.RLock()
	opener := a.assets
	a.mu.RUnlock()

	if att.ContentHash != "" && botID != "" && opener != nil {
		if rc, _, err := opener.Open(ctx, botID, att.ContentHash); err == nil {
			data, _ := io.ReadAll(rc)
			_ = rc.Close()
			if len(data) > 0 {
				reader = bytes.NewReader(data)
			}
		}
	}

	if reader == nil && att.Base64 != "" {
		data, err := base64DataURLToBytes(att.Base64)
		if err == nil {
			reader = bytes.NewReader(data)
		}
	}

	if reader == nil && att.URL != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, att.URL, nil)
		if err == nil {
			resp, doErr := http.DefaultClient.Do(req) //nolint:gosec
			if doErr == nil {
				defer func() { _ = resp.Body.Close() }()
				if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
					data, _ := io.ReadAll(resp.Body)
					reader = bytes.NewReader(data)
				}
			}
		}
	}

	if reader == nil {
		return errors.New("slack attachment requires accessible content_hash, base64, or url")
	}

	fileSize := int(att.Size)
	if fileSize == 0 {
		if br, ok := reader.(*bytes.Reader); ok {
			fileSize = br.Len()
		}
	}

	_, err := api.UploadFileContext(ctx, slack.UploadFileParameters{
		Channel:         channelID,
		ThreadTimestamp: threadTS,
		Filename:        name,
		Reader:          reader,
		FileSize:        fileSize,
	})
	return err
}

func (a *SlackAdapter) ResolveAttachment(ctx context.Context, cfg channel.ChannelConfig, attachment channel.Attachment) (channel.AttachmentPayload, error) {
	slackCfg, err := parseConfig(cfg.Credentials)
	if err != nil {
		return channel.AttachmentPayload{}, err
	}

	return a.resolveAttachmentWithClient(ctx, a.newAPIClient(slackCfg), attachment)
}

func (a *SlackAdapter) resolveAttachmentWithClient(ctx context.Context, api *slack.Client, attachment channel.Attachment) (channel.AttachmentPayload, error) {
	downloadURL := strings.TrimSpace(attachment.URL)
	if downloadURL == "" {
		fileID := strings.TrimSpace(attachment.PlatformKey)
		if fileID == "" {
			return channel.AttachmentPayload{}, errors.New("slack attachment requires url or platform_key")
		}
		file, _, _, err := api.GetFileInfoContext(ctx, fileID, 0, 0)
		if err != nil {
			return channel.AttachmentPayload{}, fmt.Errorf("slack get file info: %w", err)
		}
		if file == nil {
			return channel.AttachmentPayload{}, errors.New("slack file info response is empty")
		}
		downloadURL = strings.TrimSpace(file.URLPrivateDownload)
		if downloadURL == "" {
			downloadURL = strings.TrimSpace(file.URLPrivate)
		}
		if strings.TrimSpace(attachment.Name) == "" {
			attachment.Name = strings.TrimSpace(file.Name)
		}
		if strings.TrimSpace(attachment.Mime) == "" {
			attachment.Mime = strings.TrimSpace(file.Mimetype)
		}
		if attachment.Size <= 0 {
			attachment.Size = int64(file.Size)
		}
	}

	if downloadURL == "" {
		return channel.AttachmentPayload{}, errors.New("slack attachment download URL is empty")
	}

	var buf bytes.Buffer
	if err := api.GetFileContext(ctx, downloadURL, &buf); err != nil {
		return channel.AttachmentPayload{}, fmt.Errorf("slack download file: %w", err)
	}

	return channel.AttachmentPayload{
		Reader: io.NopCloser(bytes.NewReader(buf.Bytes())),
		Mime:   strings.TrimSpace(attachment.Mime),
		Name:   strings.TrimSpace(attachment.Name),
		Size:   int64(buf.Len()),
	}, nil
}

func truncateSlackText(text string) string {
	if utf8.RuneCountInString(text) <= slackMaxLength {
		return text
	}
	runes := []rune(text)
	return string(runes[:slackMaxLength-3]) + "..."
}

func base64DataURLToBytes(dataURL string) ([]byte, error) {
	parts := strings.SplitN(dataURL, ",", 2)
	if len(parts) != 2 {
		return nil, errors.New("invalid data URL")
	}
	return base64.StdEncoding.DecodeString(parts[1])
}

func mimeExtension(mime string) string {
	switch mime {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "video/mp4":
		return ".mp4"
	case "video/webm":
		return ".webm"
	case "audio/mpeg", "audio/mp3":
		return ".mp3"
	case "audio/ogg":
		return ".ogg"
	case "audio/wav":
		return ".wav"
	case "application/pdf":
		return ".pdf"
	case "text/plain":
		return ".txt"
	default:
		return ""
	}
}

func (a *SlackAdapter) OpenStream(_ context.Context, cfg channel.ChannelConfig, target string, opts channel.StreamOptions) (channel.OutboundStream, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return nil, errors.New("slack target is required")
	}

	slackCfg, err := parseConfig(cfg.Credentials)
	if err != nil {
		return nil, err
	}

	return &slackOutboundStream{
		adapter: a,
		cfg:     cfg,
		target:  target,
		reply:   opts.Reply,
		api:     a.newAPIClient(slackCfg),
	}, nil
}

func (a *SlackAdapter) ProcessingStarted(_ context.Context, _ channel.ChannelConfig, _ channel.InboundMessage, _ channel.ProcessingStatusInfo) (channel.ProcessingStatusHandle, error) {
	// Slack does not have a public typing indicator API for bots
	return channel.ProcessingStatusHandle{}, nil
}

func (*SlackAdapter) ProcessingCompleted(_ context.Context, _ channel.ChannelConfig, _ channel.InboundMessage, _ channel.ProcessingStatusInfo, _ channel.ProcessingStatusHandle) error {
	return nil
}

func (*SlackAdapter) ProcessingFailed(_ context.Context, _ channel.ChannelConfig, _ channel.InboundMessage, _ channel.ProcessingStatusInfo, _ channel.ProcessingStatusHandle, _ error) error {
	return nil
}

func (a *SlackAdapter) React(_ context.Context, cfg channel.ChannelConfig, target string, messageID string, emoji string) error {
	slackCfg, err := parseConfig(cfg.Credentials)
	if err != nil {
		return err
	}

	// Slack reactions use emoji names without colons
	emoji = strings.Trim(emoji, ":")

	return a.newAPIClient(slackCfg).AddReaction(emoji, slack.ItemRef{
		Channel:   target,
		Timestamp: messageID,
	})
}

func (a *SlackAdapter) Unreact(_ context.Context, cfg channel.ChannelConfig, target string, messageID string, emoji string) error {
	slackCfg, err := parseConfig(cfg.Credentials)
	if err != nil {
		return err
	}

	emoji = strings.Trim(emoji, ":")

	return a.newAPIClient(slackCfg).RemoveReaction(emoji, slack.ItemRef{
		Channel:   target,
		Timestamp: messageID,
	})
}

func (a *SlackAdapter) DiscoverSelf(_ context.Context, credentials map[string]any) (map[string]any, string, error) {
	cfg, err := parseConfig(credentials)
	if err != nil {
		return nil, "", err
	}

	api := slack.New(cfg.BotToken)
	resp, err := api.AuthTest()
	if err != nil {
		return nil, "", fmt.Errorf("slack auth test: %w", err)
	}

	identity := map[string]any{
		"user_id":  resp.UserID,
		"bot_id":   resp.BotID,
		"team_id":  resp.TeamID,
		"username": resp.User,
		"team":     resp.Team,
	}

	return identity, resp.UserID, nil
}

func (*SlackAdapter) NormalizeConfig(raw map[string]any) (map[string]any, error) {
	return normalizeConfig(raw)
}

func (*SlackAdapter) NormalizeUserConfig(raw map[string]any) (map[string]any, error) {
	return normalizeUserConfig(raw)
}

func (*SlackAdapter) NormalizeTarget(raw string) string {
	return normalizeTarget(raw)
}

func (*SlackAdapter) ResolveTarget(userConfig map[string]any) (string, error) {
	return resolveTarget(userConfig)
}

func (*SlackAdapter) MatchBinding(config map[string]any, criteria channel.BindingCriteria) bool {
	return matchBinding(config, criteria)
}

func (*SlackAdapter) BuildUserConfig(identity channel.Identity) map[string]any {
	return buildUserConfig(identity)
}

func (a *SlackAdapter) isDuplicateInbound(token, messageTS string) bool {
	if strings.TrimSpace(token) == "" || strings.TrimSpace(messageTS) == "" {
		return false
	}

	now := time.Now().UTC()
	expireBefore := now.Add(-inboundDedupTTL)

	a.mu.Lock()
	defer a.mu.Unlock()

	for key, seenAt := range a.seenMessages {
		if seenAt.Before(expireBefore) {
			delete(a.seenMessages, key)
		}
	}

	seenKey := token + ":" + messageTS
	if _, ok := a.seenMessages[seenKey]; ok {
		return true
	}
	a.seenMessages[seenKey] = now
	return false
}

func (a *SlackAdapter) clearConnection(appToken string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if conn, ok := a.connections[appToken]; ok {
		if conn.cancel != nil {
			conn.cancel()
		}
		delete(a.connections, appToken)
	}
}

func (a *SlackAdapter) resolveUserDisplayName(api *slack.Client, userID string) string {
	displayName := strings.TrimSpace(userID)
	if api == nil || displayName == "" {
		return displayName
	}
	userInfo, err := api.GetUserInfo(userID)
	if err != nil {
		return displayName
	}
	displayName = strings.TrimSpace(userInfo.Profile.DisplayName)
	if displayName == "" {
		displayName = strings.TrimSpace(userInfo.RealName)
	}
	if displayName == "" {
		displayName = strings.TrimSpace(userInfo.Name)
	}
	if displayName == "" {
		displayName = strings.TrimSpace(userID)
	}
	return displayName
}

func (a *SlackAdapter) collectAttachments(msg *slack.Msg) []channel.Attachment {
	if msg == nil || len(msg.Files) == 0 {
		return nil
	}

	attachments := make([]channel.Attachment, 0, len(msg.Files))
	for _, file := range msg.Files {
		attachment := channel.Attachment{
			Type:           channel.AttachmentFile,
			URL:            strings.TrimSpace(file.URLPrivateDownload),
			PlatformKey:    strings.TrimSpace(file.ID),
			SourcePlatform: Type.String(),
			Name:           strings.TrimSpace(file.Name),
			Size:           int64(file.Size),
			Mime:           strings.TrimSpace(file.Mimetype),
		}

		switch {
		case strings.HasPrefix(file.Mimetype, "image/"):
			attachment.Type = channel.AttachmentImage
		case strings.HasPrefix(file.Mimetype, "video/"):
			attachment.Type = channel.AttachmentVideo
		case strings.HasPrefix(file.Mimetype, "audio/"):
			attachment.Type = channel.AttachmentAudio
		}

		attachments = append(attachments, attachment)
	}

	return attachments
}

func slackIdentityAttributes(userID, username, channelType, channelID string) map[string]string {
	attrs := map[string]string{}
	if value := strings.TrimSpace(userID); value != "" {
		attrs["user_id"] = value
	}
	if value := strings.TrimSpace(username); value != "" {
		attrs["username"] = value
	}
	if strings.TrimSpace(channelType) == "im" {
		if value := strings.TrimSpace(channelID); value != "" {
			attrs["channel_id"] = value
		}
	}
	return attrs
}

func (a *SlackAdapter) lookupConversationName(ctx context.Context, api *slack.Client, configID, channelID string) string {
	configID = strings.TrimSpace(configID)
	channelID = strings.TrimSpace(channelID)
	if api == nil || configID == "" || channelID == "" {
		return ""
	}

	cacheKey := configID + ":" + channelID
	expireBefore := time.Now().UTC().Add(-channelNameTTL)

	a.mu.RLock()
	cached, ok := a.channelNames[cacheKey]
	a.mu.RUnlock()
	if ok && cached.cachedAt.After(expireBefore) {
		return cached.name
	}

	name, err := a.fetchConversationName(ctx, api, channelID)
	if err != nil {
		if a.logger != nil {
			a.logger.Debug("resolve slack conversation name failed",
				slog.String("channel_id", channelID),
				slog.Any("error", err),
			)
		}
		return ""
	}
	if name == "" {
		return ""
	}

	a.mu.Lock()
	a.channelNames[cacheKey] = cachedSlackChannelName{name: name, cachedAt: time.Now().UTC()}
	a.mu.Unlock()
	return name
}

func (a *SlackAdapter) fetchConversationName(ctx context.Context, api *slack.Client, channelID string) (string, error) {
	info, err := api.GetConversationInfoContext(ctx, &slack.GetConversationInfoInput{
		ChannelID: channelID,
	})
	if err != nil {
		return "", err
	}
	if info == nil {
		return "", nil
	}

	name := strings.TrimSpace(info.Name)
	if name == "" {
		name = strings.TrimSpace(info.NameNormalized)
	}
	return name, nil
}
