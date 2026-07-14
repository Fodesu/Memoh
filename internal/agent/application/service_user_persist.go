package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	chatview "github.com/memohai/memoh/internal/agent/view"
	messagepkg "github.com/memohai/memoh/internal/chat/message"
)

// ApplyUserMessageHookAndPersistUserTurn applies the normal user-message hook
// before writing a user turn ahead of agent execution. Web skill activations
// use this so a denied hook cannot leave a persisted user-only special turn.
func (s *Service) ApplyUserMessageHookAndPersistUserTurn(ctx context.Context, req ChatRequest) (ChatRequest, messagepkg.Message, error) {
	nextReq, err := s.PrepareUserMessageWS(ctx, req)
	if err != nil {
		return req, messagepkg.Message{}, err
	}
	persisted, err := s.persistUserTurn(ctx, nextReq)
	if err != nil {
		return nextReq, messagepkg.Message{}, err
	}
	nextReq.UserMessagePersisted = true
	nextReq.PersistedUserMessageID = persisted.ID
	return nextReq, persisted, nil
}

// PrepareUserMessageWS applies the user-message hook before runtime admission
// without writing history. The resulting request can be used to publish the
// canonical runtime request turn and must not run the hook a second time.
func (s *Service) PrepareUserMessageWS(ctx context.Context, req ChatRequest) (ChatRequest, error) {
	if req.RawQuery == "" {
		req.RawQuery = strings.TrimSpace(req.Query)
	}
	nextReq, err := s.applyUserMessageHook(ctx, req)
	if err != nil {
		return req, err
	}
	nextReq.UserMessageHookApplied = true
	return nextReq, nil
}

// RuntimeRequestUserTurn returns the canonical, non-durable user turn shown
// while an ordinary WebSocket run is active.
func RuntimeRequestUserTurn(req ChatRequest, timestamp time.Time) *chatview.UITurn {
	return &chatview.UITurn{
		Role:              "user",
		Text:              userTurnDisplayText(req),
		UserMessageKind:   strings.TrimSpace(req.UserMessageKind),
		SkillActivation:   req.SkillActivation,
		Attachments:       uiAttachmentsFromChatAttachments(req.Attachments),
		Timestamp:         timestamp,
		Platform:          strings.TrimSpace(req.CurrentChannel),
		SenderUserID:      strings.TrimSpace(req.UserID),
		ExternalMessageID: strings.TrimSpace(req.ExternalMessageID),
	}
}

// persistUserTurn writes a user turn before agent execution. It is used after
// the user-message hook has already accepted the request.
func (s *Service) persistUserTurn(ctx context.Context, req ChatRequest) (messagepkg.Message, error) {
	if s == nil || s.messageService == nil {
		return messagepkg.Message{}, errors.New("message service not configured")
	}
	if strings.TrimSpace(req.BotID) == "" {
		return messagepkg.Message{}, errors.New("bot id is required")
	}
	if strings.TrimSpace(req.ThreadID) == "" {
		return messagepkg.Message{}, errors.New("session id is required")
	}
	modelMessage := ModelMessage{
		Role:    "user",
		Content: newTextContent(persistedUserTurnText(req)),
	}
	modelMessage = normalizeUserMessageContent(modelMessage)
	content, err := json.Marshal(modelMessage)
	if err != nil {
		return messagepkg.Message{}, err
	}
	senderChannelIdentityID, senderUserID := s.resolvePersistSenderIDs(ctx, req)
	sessionMode, runtimeType := s.persistSessionRuntimeSnapshot(ctx, req)
	return s.messageService.Persist(ctx, messagepkg.PersistInput{
		BotID:                   req.BotID,
		SessionID:               req.ThreadID,
		SenderChannelIdentityID: senderChannelIdentityID,
		SenderUserID:            senderUserID,
		ExternalMessageID:       req.ExternalMessageID,
		SourceReplyToMessageID:  req.SourceReplyToMessageID,
		Role:                    "user",
		Content:                 content,
		Metadata:                mergeMetadata(mergeMetadata(buildRouteMetadata(req), buildInteractionMetadata(req)), workspaceTargetMetadata(req.WorkspaceTarget)),
		Assets:                  chatAttachmentsToAssetRefs(req.Attachments),
		EventID:                 req.EventID,
		DisplayText:             userTurnDisplayText(req),
		SessionMode:             sessionMode,
		RuntimeType:             runtimeType,
	})
}

func userTurnDisplayText(req ChatRequest) string {
	if strings.TrimSpace(req.UserVisibleText) != "" || req.UserMessageKind == UserMessageKindSkillActivation {
		return strings.TrimSpace(req.UserVisibleText)
	}
	if strings.TrimSpace(req.RawQuery) != "" {
		return strings.TrimSpace(req.RawQuery)
	}
	return strings.TrimSpace(req.Query)
}

func persistedUserTurnText(req ChatRequest) string {
	if req.UserMessageKind == UserMessageKindSkillActivation {
		return strings.TrimSpace(req.UserVisibleText)
	}
	return req.Query
}
