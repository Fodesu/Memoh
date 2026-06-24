package flow

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	sdk "github.com/memohai/twilight-ai/sdk"

	"github.com/memohai/memoh/internal/conversation"
	dbsqlc "github.com/memohai/memoh/internal/db/postgres/sqlc"
	dbstore "github.com/memohai/memoh/internal/db/store"
	messagepkg "github.com/memohai/memoh/internal/message"
)

type recordingMessageService struct {
	persisted     []messagepkg.PersistInput
	activeBot     []string
	activeSession []string
	activeTurn    []string
}

func (s *recordingMessageService) Persist(_ context.Context, input messagepkg.PersistInput) (messagepkg.Message, error) {
	s.persisted = append(s.persisted, input)
	return messagepkg.Message{ID: "message-id", Role: input.Role}, nil
}

func (*recordingMessageService) List(context.Context, string) ([]messagepkg.Message, error) {
	return nil, nil
}

func (*recordingMessageService) ListSince(context.Context, string, time.Time) ([]messagepkg.Message, error) {
	return nil, nil
}

func (s *recordingMessageService) ListActiveSince(_ context.Context, botID string, _ time.Time) ([]messagepkg.Message, error) {
	s.activeBot = append(s.activeBot, botID)
	return nil, nil
}

func (*recordingMessageService) ListLatest(context.Context, string, int32) ([]messagepkg.Message, error) {
	return nil, nil
}

func (*recordingMessageService) ListBefore(context.Context, string, time.Time, string, int32) ([]messagepkg.Message, error) {
	return nil, nil
}

func (*recordingMessageService) ListBySession(context.Context, string) ([]messagepkg.Message, error) {
	return nil, nil
}

func (*recordingMessageService) ListSinceBySession(context.Context, string, time.Time) ([]messagepkg.Message, error) {
	return nil, nil
}

func (s *recordingMessageService) ListActiveSinceBySession(_ context.Context, sessionID string, _ time.Time) ([]messagepkg.Message, error) {
	s.activeSession = append(s.activeSession, sessionID)
	return nil, nil
}

func (s *recordingMessageService) ListActiveSinceByTurn(_ context.Context, turnID string, _ time.Time) ([]messagepkg.Message, error) {
	s.activeTurn = append(s.activeTurn, turnID)
	return nil, nil
}

func (*recordingMessageService) ListLatestBySession(context.Context, string, int32) ([]messagepkg.Message, error) {
	return nil, nil
}

func (*recordingMessageService) ListBeforeBySession(context.Context, string, time.Time, string, int32) ([]messagepkg.Message, error) {
	return nil, nil
}

func (*recordingMessageService) GetSessionTurnGraph(context.Context, string) (messagepkg.SessionTurnGraph, error) {
	return messagepkg.SessionTurnGraph{}, nil
}

func (*recordingMessageService) LocateByExternalIDBySession(context.Context, string, string, int32, int32) (messagepkg.LocateResult, error) {
	return messagepkg.LocateResult{}, nil
}

func (*recordingMessageService) DeleteByBot(context.Context, string) error {
	return nil
}

func (*recordingMessageService) DeleteBySession(context.Context, string) error {
	return nil
}

func (*recordingMessageService) LinkAssets(context.Context, string, []messagepkg.AssetRef) error {
	return nil
}

type fakeTurnStore struct {
	dbstore.Queries
	session        dbsqlc.BotSession
	rewrite        dbsqlc.GetVisibleUserMessageTurnForRewriteRow
	sessionHeadErr error
	createdTurns   []dbsqlc.CreateHistoryTurnParams
	createdHeads   []dbsqlc.CreateSessionTurnHeadParams
	replacedHeads  []dbsqlc.ReplaceSessionTurnHeadParams
	updatedDefault []dbsqlc.UpdateSessionDefaultHeadTurnIfValidParams
	nextTurnByte   byte
	txCount        int
}

func (s *fakeTurnStore) RunInTx(_ context.Context, fn func(dbstore.Queries) error) error {
	s.txCount++
	return fn(s)
}

func (s *fakeTurnStore) CreateHistoryTurn(_ context.Context, arg dbsqlc.CreateHistoryTurnParams) (dbsqlc.BotHistoryTurn, error) {
	s.createdTurns = append(s.createdTurns, arg)
	s.nextTurnByte++
	return dbsqlc.BotHistoryTurn{ID: testUUID(s.nextTurnByte)}, nil
}

func (s *fakeTurnStore) GetSessionByID(context.Context, pgtype.UUID) (dbsqlc.BotSession, error) {
	return s.session, nil
}

func (s *fakeTurnStore) CreateSessionTurnHead(_ context.Context, arg dbsqlc.CreateSessionTurnHeadParams) (dbsqlc.BotSessionTurnHead, error) {
	s.createdHeads = append(s.createdHeads, arg)
	return dbsqlc.BotSessionTurnHead{SessionID: arg.SessionID, HeadTurnID: arg.HeadTurnID}, nil
}

func (s *fakeTurnStore) GetSessionTurnHead(_ context.Context, arg dbsqlc.GetSessionTurnHeadParams) (dbsqlc.BotSessionTurnHead, error) {
	if s.sessionHeadErr != nil {
		return dbsqlc.BotSessionTurnHead{}, s.sessionHeadErr
	}
	return dbsqlc.BotSessionTurnHead{SessionID: arg.SessionID, HeadTurnID: arg.HeadTurnID}, nil
}

func (s *fakeTurnStore) GetVisibleUserMessageTurnForRewrite(context.Context, dbsqlc.GetVisibleUserMessageTurnForRewriteParams) (dbsqlc.GetVisibleUserMessageTurnForRewriteRow, error) {
	return s.rewrite, nil
}

func (s *fakeTurnStore) ReplaceSessionTurnHead(_ context.Context, arg dbsqlc.ReplaceSessionTurnHeadParams) (dbsqlc.BotSessionTurnHead, error) {
	s.replacedHeads = append(s.replacedHeads, arg)
	return dbsqlc.BotSessionTurnHead{SessionID: arg.TargetSessionID, HeadTurnID: arg.NewHeadTurnID}, nil
}

func (s *fakeTurnStore) UpdateSessionDefaultHeadTurnIfValid(_ context.Context, arg dbsqlc.UpdateSessionDefaultHeadTurnIfValidParams) (dbsqlc.BotSession, error) {
	s.updatedDefault = append(s.updatedDefault, arg)
	return dbsqlc.BotSession{ID: arg.ID, DefaultHeadTurnID: arg.DefaultHeadTurnID}, nil
}

func testUUID(lastByte byte) pgtype.UUID {
	id := pgtype.UUID{Valid: true}
	id.Bytes[15] = lastByte
	return id
}

func TestResolveTurnRunParentNormalFirstTurnUsesEmptyContext(t *testing.T) {
	t.Parallel()

	store := &fakeTurnStore{}
	mode, parent, selectedHead, err := (*Resolver)(nil).resolveTurnRunParent(context.Background(), store, testUUID(1), conversation.ChatRequest{}, nil)
	if err != nil {
		t.Fatalf("resolveTurnRunParent() error = %v", err)
	}
	if mode != TurnRunModeNormal {
		t.Fatalf("mode = %q, want %q", mode, TurnRunModeNormal)
	}
	if parent.Valid || selectedHead.HeadTurnID.Valid {
		t.Fatalf("parent=%v selectedHead=%v, want both invalid", parent, selectedHead)
	}
	if scope := contextScopeFromParentTurn(parent); scope.Kind != ContextScopeEmpty {
		t.Fatalf("scope = %#v, want empty", scope)
	}
}

func TestResolveTurnRunParentNormalLaterTurnUsesSessionHead(t *testing.T) {
	t.Parallel()

	head := testUUID(9)
	store := &fakeTurnStore{session: dbsqlc.BotSession{DefaultHeadTurnID: head}}
	mode, parent, selectedHead, err := (*Resolver)(nil).resolveTurnRunParent(context.Background(), store, testUUID(1), conversation.ChatRequest{}, nil)
	if err != nil {
		t.Fatalf("resolveTurnRunParent() error = %v", err)
	}
	if mode != TurnRunModeNormal {
		t.Fatalf("mode = %q, want %q", mode, TurnRunModeNormal)
	}
	if parent != head || selectedHead.HeadTurnID != head {
		t.Fatalf("parent=%v selectedHead=%v, want head=%v", parent, selectedHead, head)
	}
	scope := contextScopeFromParentTurn(parent)
	if scope.Kind != ContextScopeTurnHead || scope.TurnID != head.String() {
		t.Fatalf("scope = %#v, want turn head %s", scope, head.String())
	}
}

func TestResolveTurnRunParentRewriteFirstTurnUsesEmptyContext(t *testing.T) {
	t.Parallel()

	store := &fakeTurnStore{
		session: dbsqlc.BotSession{DefaultHeadTurnID: testUUID(7)},
		rewrite: dbsqlc.GetVisibleUserMessageTurnForRewriteRow{
			ParentTurnID: pgtype.UUID{},
		},
	}
	mode, parent, selectedHead, err := (*Resolver)(nil).resolveTurnRunParent(context.Background(), store, testUUID(1), conversation.ChatRequest{
		RewriteTargetMessageID: "00000000-0000-0000-0000-000000000123",
	}, nil)
	if err != nil {
		t.Fatalf("resolveTurnRunParent() error = %v", err)
	}
	if mode != TurnRunModeRewrite {
		t.Fatalf("mode = %q, want %q", mode, TurnRunModeRewrite)
	}
	if parent.Valid {
		t.Fatalf("parent = %v, want invalid for first-turn rewrite", parent)
	}
	if !selectedHead.HeadTurnID.Valid || selectedHead.HeadTurnID != testUUID(7) {
		t.Fatalf("selectedHead = %v, want current session head", selectedHead)
	}
	if scope := contextScopeFromParentTurn(parent); scope.Kind != ContextScopeEmpty {
		t.Fatalf("scope = %#v, want empty", scope)
	}
}

func TestResolveTurnRunParentRewriteMiddleTurnUsesTargetParent(t *testing.T) {
	t.Parallel()

	currentHead := testUUID(8)
	targetParent := testUUID(4)
	store := &fakeTurnStore{
		session: dbsqlc.BotSession{DefaultHeadTurnID: currentHead},
		rewrite: dbsqlc.GetVisibleUserMessageTurnForRewriteRow{
			ParentTurnID: targetParent,
		},
	}
	mode, parent, selectedHead, err := (*Resolver)(nil).resolveTurnRunParent(context.Background(), store, testUUID(1), conversation.ChatRequest{
		RewriteTargetMessageID: "00000000-0000-0000-0000-000000000123",
	}, nil)
	if err != nil {
		t.Fatalf("resolveTurnRunParent() error = %v", err)
	}
	if mode != TurnRunModeRewrite {
		t.Fatalf("mode = %q, want %q", mode, TurnRunModeRewrite)
	}
	if parent != targetParent {
		t.Fatalf("parent = %v, want target parent %v", parent, targetParent)
	}
	if selectedHead.HeadTurnID != currentHead {
		t.Fatalf("selectedHead = %v, want current head %v", selectedHead, currentHead)
	}
	scope := contextScopeFromParentTurn(parent)
	if scope.Kind != ContextScopeTurnHead || scope.TurnID != targetParent.String() {
		t.Fatalf("scope = %#v, want target parent turn head %s", scope, targetParent.String())
	}
}

func TestResolveTurnRunParentRewriteUsesResolvedTurnAnchor(t *testing.T) {
	t.Parallel()

	currentHead := testUUID(8)
	targetParent := testUUID(4)
	store := &fakeTurnStore{
		session: dbsqlc.BotSession{DefaultHeadTurnID: currentHead},
		rewrite: dbsqlc.GetVisibleUserMessageTurnForRewriteRow{
			ParentTurnID: testUUID(99),
		},
	}
	mode, parent, selectedHead, err := (*Resolver)(nil).resolveTurnRunParent(context.Background(), store, testUUID(1), conversation.ChatRequest{}, &conversation.TurnAnchor{
		Role:               conversation.TurnAnchorRoleUser,
		MessageID:          "message-1",
		TurnID:             testUUID(3).String(),
		ParentTurnID:       targetParent.String(),
		SelectedHeadTurnID: currentHead.String(),
	})
	if err != nil {
		t.Fatalf("resolveTurnRunParent() error = %v", err)
	}
	if mode != TurnRunModeRewrite {
		t.Fatalf("mode = %q, want %q", mode, TurnRunModeRewrite)
	}
	if parent != targetParent {
		t.Fatalf("parent = %v, want anchor parent %v", parent, targetParent)
	}
	if selectedHead.HeadTurnID != currentHead {
		t.Fatalf("selectedHead = %v, want anchor selected head %v", selectedHead, currentHead)
	}
}

func TestResolveTurnRunParentRewriteAnchorRejectsInvalidSelectedHead(t *testing.T) {
	t.Parallel()

	currentHead := testUUID(8)
	targetParent := testUUID(4)
	store := &fakeTurnStore{
		session:        dbsqlc.BotSession{DefaultHeadTurnID: currentHead},
		sessionHeadErr: pgx.ErrNoRows,
	}
	_, _, _, err := (*Resolver)(nil).resolveTurnRunParent(context.Background(), store, testUUID(1), conversation.ChatRequest{}, &conversation.TurnAnchor{
		Role:               conversation.TurnAnchorRoleUser,
		MessageID:          "message-1",
		TurnID:             testUUID(3).String(),
		ParentTurnID:       targetParent.String(),
		SelectedHeadTurnID: currentHead.String(),
	})
	if err == nil {
		t.Fatalf("resolveTurnRunParent() error = nil, want invalid selected head error")
	}
	if !strings.Contains(err.Error(), "selected head is not valid for this session") {
		t.Fatalf("resolveTurnRunParent() error = %v, want invalid selected head", err)
	}
}

func TestApplyVariantTransitionContinueVariantReplacesSelectedHead(t *testing.T) {
	t.Parallel()

	store := &fakeTurnStore{}
	resolver := &Resolver{queries: store}
	sessionID := testUUID(1).String()
	oldHead := testUUID(2)
	newHead := testUUID(3)

	run := TurnRun{
		Mode:          TurnRunModeNormal,
		PersistTurnID: newHead.String(),
		Variant: VariantTransition{
			Action:             VariantTransitionContinueSelected,
			SessionID:          sessionID,
			SelectedHeadTurnID: oldHead.String(),
		},
	}
	err := resolver.applyVariantTransition(context.Background(), &run, "")
	if err != nil {
		t.Fatalf("applyVariantTransition() error = %v", err)
	}
	if store.txCount != 1 {
		t.Fatalf("txCount = %d, want 1", store.txCount)
	}
	if len(store.replacedHeads) != 1 {
		t.Fatalf("replacedHeads = %d, want 1", len(store.replacedHeads))
	}
	if got := store.replacedHeads[0]; got.OldHeadTurnID != oldHead || got.NewHeadTurnID != newHead {
		t.Fatalf("replace params = %#v, want old=%v new=%v", got, oldHead, newHead)
	}
	if len(store.createdHeads) != 0 {
		t.Fatalf("createdHeads = %d, want 0", len(store.createdHeads))
	}
	if len(store.updatedDefault) != 1 || store.updatedDefault[0].DefaultHeadTurnID != newHead {
		t.Fatalf("updatedDefault = %#v, want new head", store.updatedDefault)
	}
}

func TestApplyVariantTransitionCreateVariantCreatesSiblingHead(t *testing.T) {
	t.Parallel()

	store := &fakeTurnStore{}
	resolver := &Resolver{queries: store}
	sessionID := testUUID(1).String()
	selectedHead := testUUID(2)
	newHead := testUUID(3)

	run := TurnRun{
		Mode:          TurnRunModeRewrite,
		PersistTurnID: newHead.String(),
		Variant: VariantTransition{
			Action:             VariantTransitionCreateSibling,
			SessionID:          sessionID,
			SelectedHeadTurnID: selectedHead.String(),
		},
	}
	err := resolver.applyVariantTransition(context.Background(), &run, "")
	if err != nil {
		t.Fatalf("applyVariantTransition() error = %v", err)
	}
	if store.txCount != 1 {
		t.Fatalf("txCount = %d, want 1", store.txCount)
	}
	if len(store.createdHeads) != 1 || store.createdHeads[0].HeadTurnID != newHead {
		t.Fatalf("createdHeads = %#v, want new head", store.createdHeads)
	}
	if len(store.replacedHeads) != 0 {
		t.Fatalf("replacedHeads = %d, want 0", len(store.replacedHeads))
	}
	if len(store.updatedDefault) != 1 || store.updatedDefault[0].DefaultHeadTurnID != newHead {
		t.Fatalf("updatedDefault = %#v, want new head", store.updatedDefault)
	}
}

func TestContinuationTurnRunDoesNotMoveSessionHead(t *testing.T) {
	t.Parallel()

	store := &fakeTurnStore{}
	resolver := &Resolver{queries: store}
	sessionID := testUUID(1).String()
	turnID := testUUID(2)

	run := continuationTurnRun(sessionID, turnID.String())
	err := resolver.applyVariantTransition(context.Background(), &run, "")
	if err != nil {
		t.Fatalf("applyVariantTransition() error = %v", err)
	}
	if store.txCount != 0 {
		t.Fatalf("txCount = %d, want 0", store.txCount)
	}
	if run.PersistTurnID != turnID.String() {
		t.Fatalf("PersistTurnID = %q, want %q", run.PersistTurnID, turnID.String())
	}
	if run.Context.Kind != ContextScopeTurnHead || run.Context.TurnID != turnID.String() {
		t.Fatalf("Context = %#v, want same turn context", run.Context)
	}
	if len(store.createdHeads) != 0 {
		t.Fatalf("createdHeads = %d, want 0", len(store.createdHeads))
	}
	if len(store.replacedHeads) != 0 {
		t.Fatalf("replacedHeads = %d, want 0", len(store.replacedHeads))
	}
	if len(store.updatedDefault) != 0 {
		t.Fatalf("updatedDefault = %d, want 0", len(store.updatedDefault))
	}
}

func TestTurnRunAllowsPipelineContextOnlyForWholeSessionScopes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		run  TurnRun
		want bool
	}{
		{name: "empty", run: TurnRun{Context: TurnContextScope{Kind: ContextScopeEmpty}}, want: false},
		{name: "turn head", run: TurnRun{Context: TurnContextScope{Kind: ContextScopeTurnHead, TurnID: "turn-1"}}, want: false},
		{name: "session head", run: TurnRun{Context: TurnContextScope{Kind: ContextScopeSessionHead}}, want: true},
		{name: "bot history", run: TurnRun{Context: TurnContextScope{Kind: ContextScopeBotHistory}}, want: true},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := turnRunAllowsPipelineContext(tt.run); got != tt.want {
				t.Fatalf("turnRunAllowsPipelineContext() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLoadMessagesForTurnRunUsesExplicitEmptyContext(t *testing.T) {
	t.Parallel()

	messages := &recordingMessageService{}
	resolver := &Resolver{
		messageService: messages,
		logger:         slog.New(slog.DiscardHandler),
	}

	loaded, err := resolver.loadMessagesForTurnRun(context.Background(), conversation.ChatRequest{
		BotID:     "bot-1",
		ChatID:    "bot-1",
		SessionID: "session-1",
	}, TurnRun{Context: TurnContextScope{Kind: ContextScopeEmpty}}, defaultMaxContextMinutes)
	if err != nil {
		t.Fatalf("loadMessagesForTurnRun() error = %v", err)
	}
	if len(loaded) != 0 {
		t.Fatalf("loaded %d messages, want 0", len(loaded))
	}
	if len(messages.activeSession) != 0 || len(messages.activeTurn) != 0 || len(messages.activeBot) != 0 {
		t.Fatalf("empty context should not load history, got session=%v turn=%v bot=%v", messages.activeSession, messages.activeTurn, messages.activeBot)
	}
}

func TestLoadMessagesForTurnRunUsesTurnHeadContext(t *testing.T) {
	t.Parallel()

	messages := &recordingMessageService{}
	resolver := &Resolver{
		messageService: messages,
		logger:         slog.New(slog.DiscardHandler),
	}

	_, err := resolver.loadMessagesForTurnRun(context.Background(), conversation.ChatRequest{
		BotID:     "bot-1",
		ChatID:    "bot-1",
		SessionID: "session-1",
	}, TurnRun{Context: TurnContextScope{Kind: ContextScopeTurnHead, TurnID: "turn-1"}}, defaultMaxContextMinutes)
	if err != nil {
		t.Fatalf("loadMessagesForTurnRun() error = %v", err)
	}
	if len(messages.activeTurn) != 1 || messages.activeTurn[0] != "turn-1" {
		t.Fatalf("turn context calls = %v, want [turn-1]", messages.activeTurn)
	}
	if len(messages.activeSession) != 0 || len(messages.activeBot) != 0 {
		t.Fatalf("turn context should not load fallback history, got session=%v bot=%v", messages.activeSession, messages.activeBot)
	}
}

func TestLoadMessagesForTurnRunRejectsEmptyTurnHead(t *testing.T) {
	t.Parallel()

	messages := &recordingMessageService{}
	resolver := &Resolver{
		messageService: messages,
		logger:         slog.New(slog.DiscardHandler),
	}

	_, err := resolver.loadMessagesForTurnRun(context.Background(), conversation.ChatRequest{
		BotID:     "bot-1",
		ChatID:    "bot-1",
		SessionID: "session-1",
	}, TurnRun{Context: TurnContextScope{Kind: ContextScopeTurnHead}}, defaultMaxContextMinutes)
	if err == nil {
		t.Fatalf("loadMessagesForTurnRun() error = nil, want error")
	}
	if len(messages.activeSession) != 0 || len(messages.activeTurn) != 0 || len(messages.activeBot) != 0 {
		t.Fatalf("empty turn context should not load fallback history, got session=%v turn=%v bot=%v", messages.activeSession, messages.activeTurn, messages.activeBot)
	}
}

func TestLoadMessagesForTurnRunUsesSessionHeadContext(t *testing.T) {
	t.Parallel()

	messages := &recordingMessageService{}
	resolver := &Resolver{
		messageService: messages,
		logger:         slog.New(slog.DiscardHandler),
	}

	_, err := resolver.loadMessagesForTurnRun(context.Background(), conversation.ChatRequest{
		BotID:     "bot-1",
		ChatID:    "bot-1",
		SessionID: "session-1",
	}, TurnRun{Context: TurnContextScope{Kind: ContextScopeSessionHead}}, defaultMaxContextMinutes)
	if err != nil {
		t.Fatalf("loadMessagesForTurnRun() error = %v", err)
	}
	if len(messages.activeSession) != 1 || messages.activeSession[0] != "session-1" {
		t.Fatalf("session context calls = %v, want [session-1]", messages.activeSession)
	}
}

func TestPersistPartialResultDoesNotStoreUserOnlyFailure(t *testing.T) {
	t.Parallel()

	messages := &recordingMessageService{}
	resolver := &Resolver{
		messageService: messages,
		logger:         slog.New(slog.DiscardHandler),
	}
	run := legacyTurnRun(conversation.ChatRequest{BotID: "bot-1", SessionID: "session-1"})

	resolver.persistPartialResult(
		context.Background(),
		conversation.ChatRequest{
			BotID:     "bot-1",
			SessionID: "session-1",
			Query:     "hello",
		},
		&run,
		resolvedContext{},
		nil,
		0,
		false,
		true,
	)

	if len(messages.persisted) != 0 {
		t.Fatalf("expected failed stream not to persist user-only history, got %#v", messages.persisted)
	}
}

func TestPersistTerminalSnapshotSkipsUserOnlySnapshot(t *testing.T) {
	t.Parallel()

	messages := &recordingMessageService{}
	resolver := &Resolver{
		messageService: messages,
		logger:         slog.New(slog.DiscardHandler),
	}
	run := legacyTurnRun(conversation.ChatRequest{BotID: "bot-1", SessionID: "session-1"})

	if err := resolver.persistTerminalSnapshot(
		context.Background(),
		conversation.ChatRequest{
			BotID:     "bot-1",
			SessionID: "session-1",
			Query:     "hello",
		},
		&run,
		resolvedContext{},
		terminalSnapshot{
			sdkMessages: []sdk.Message{sdk.UserMessage("hello")},
		},
	); err != nil {
		t.Fatalf("persistTerminalSnapshot returned error: %v", err)
	}

	if len(messages.persisted) != 0 {
		t.Fatalf("expected user-only terminal snapshot not to persist, got %#v", messages.persisted)
	}
}

func TestPersistTerminalSnapshotSkipsEmptyAssistantSnapshot(t *testing.T) {
	t.Parallel()

	messages := &recordingMessageService{}
	resolver := &Resolver{
		messageService: messages,
		logger:         slog.New(slog.DiscardHandler),
	}
	run := legacyTurnRun(conversation.ChatRequest{BotID: "bot-1", SessionID: "session-1"})

	if err := resolver.persistTerminalSnapshot(
		context.Background(),
		conversation.ChatRequest{
			BotID:     "bot-1",
			SessionID: "session-1",
			Query:     "hello",
		},
		&run,
		resolvedContext{},
		terminalSnapshot{
			sdkMessages: []sdk.Message{sdk.AssistantMessage("")},
		},
	); err != nil {
		t.Fatalf("persistTerminalSnapshot returned error: %v", err)
	}

	if len(messages.persisted) != 0 {
		t.Fatalf("expected empty assistant terminal snapshot not to persist, got %#v", messages.persisted)
	}
}

func TestStoreRoundUsesPinnedTurnForEveryMessage(t *testing.T) {
	t.Parallel()

	messages := &recordingMessageService{}
	resolver := &Resolver{
		messageService: messages,
		logger:         slog.New(slog.DiscardHandler),
	}

	req := conversation.ChatRequest{
		BotID:     "bot-1",
		SessionID: "session-1",
		Query:     "hello",
	}
	run := TurnRun{
		Mode:          TurnRunModeNormal,
		PersistTurnID: "turn-1",
	}
	err := resolver.storeRoundWithOptions(context.Background(), req, &run, []conversation.ModelMessage{
		{Role: "user", Content: conversation.NewTextContent("hello")},
		{Role: "assistant", Content: conversation.NewTextContent("thinking")},
		{Role: "tool", ToolCallID: "tool-1", Content: conversation.NewTextContent("ok")},
		{Role: "assistant", Content: conversation.NewTextContent("done")},
	}, "", storeRoundOptions{AllowPendingToolCalls: true})
	if err != nil {
		t.Fatalf("storeRoundWithOptions() error = %v", err)
	}
	if len(messages.persisted) != 4 {
		t.Fatalf("persisted %d messages, want 4", len(messages.persisted))
	}
	for i, persisted := range messages.persisted {
		if persisted.TurnID != "turn-1" {
			t.Fatalf("persisted[%d].TurnID = %q, want turn-1", i, persisted.TurnID)
		}
	}
}

func TestStoreRoundMaterializesPlannedTurnForEveryMessage(t *testing.T) {
	t.Parallel()

	messages := &recordingMessageService{}
	store := &fakeTurnStore{}
	resolver := &Resolver{
		queries:        store,
		messageService: messages,
		logger:         slog.New(slog.DiscardHandler),
	}

	req := conversation.ChatRequest{
		BotID:     testUUID(1).String(),
		SessionID: testUUID(2).String(),
		Query:     "hello",
	}
	run, err := resolver.prepareTurnRun(context.Background(), req)
	if err != nil {
		t.Fatalf("prepareTurnRun() error = %v", err)
	}
	if len(store.createdTurns) != 0 {
		t.Fatalf("prepareTurnRun created %d turns, want 0", len(store.createdTurns))
	}

	err = resolver.storeRoundWithOptions(context.Background(), req, &run, []conversation.ModelMessage{
		{Role: "user", Content: conversation.NewTextContent("hello")},
		{Role: "assistant", Content: conversation.NewTextContent("done")},
	}, "", storeRoundOptions{})
	if err != nil {
		t.Fatalf("storeRoundWithOptions() error = %v", err)
	}
	if len(store.createdTurns) != 1 {
		t.Fatalf("createdTurns = %d, want 1", len(store.createdTurns))
	}
	if run.PersistTurnID == "" {
		t.Fatalf("run.PersistTurnID is empty after store")
	}
	if len(messages.persisted) != 2 {
		t.Fatalf("persisted %d messages, want 2", len(messages.persisted))
	}
	for i, persisted := range messages.persisted {
		if persisted.TurnID != run.PersistTurnID {
			t.Fatalf("persisted[%d].TurnID = %q, want %q", i, persisted.TurnID, run.PersistTurnID)
		}
	}
}

func TestEnsurePersistTurnIsIdempotentForRun(t *testing.T) {
	t.Parallel()

	store := &fakeTurnStore{}
	resolver := &Resolver{queries: store}
	run := TurnRun{
		Mode: TurnRunModeNormal,
		Turn: TurnPersistencePlan{
			BotID:          testUUID(1).String(),
			OwnerSessionID: testUUID(2).String(),
		},
	}

	first, err := resolver.ensurePersistTurn(context.Background(), &run)
	if err != nil {
		t.Fatalf("ensurePersistTurn() first error = %v", err)
	}
	second, err := resolver.ensurePersistTurn(context.Background(), &run)
	if err != nil {
		t.Fatalf("ensurePersistTurn() second error = %v", err)
	}
	if first == "" || second == "" || first != second {
		t.Fatalf("turn ids first=%q second=%q, want same non-empty id", first, second)
	}
	if len(store.createdTurns) != 1 {
		t.Fatalf("createdTurns = %d, want 1", len(store.createdTurns))
	}
}

func TestPersistTerminalSnapshotStoresAssistantOutput(t *testing.T) {
	t.Parallel()

	messages := &recordingMessageService{}
	resolver := &Resolver{
		messageService: messages,
		logger:         slog.New(slog.DiscardHandler),
	}
	run := legacyTurnRun(conversation.ChatRequest{BotID: "bot-1", SessionID: "session-1"})

	if err := resolver.persistTerminalSnapshot(
		context.Background(),
		conversation.ChatRequest{
			BotID:     "bot-1",
			SessionID: "session-1",
			Query:     "hello",
		},
		&run,
		resolvedContext{},
		terminalSnapshot{
			sdkMessages: []sdk.Message{sdk.AssistantMessage("partial answer")},
		},
	); err != nil {
		t.Fatalf("persistTerminalSnapshot returned error: %v", err)
	}

	if len(messages.persisted) != 2 {
		t.Fatalf("expected user and assistant messages to persist, got %#v", messages.persisted)
	}
	if messages.persisted[0].Role != "user" || messages.persisted[1].Role != "assistant" {
		t.Fatalf("unexpected persisted roles: %q, %q", messages.persisted[0].Role, messages.persisted[1].Role)
	}
}

func TestPersistTerminalSnapshotSkipsUserWhenPipelineContextContainsCurrentMessage(t *testing.T) {
	t.Parallel()

	messages := &recordingMessageService{}
	resolver := &Resolver{
		messageService: messages,
		logger:         slog.New(slog.DiscardHandler),
	}
	run := legacyTurnRun(conversation.ChatRequest{BotID: "bot-1", SessionID: "session-1"})

	if err := resolver.persistTerminalSnapshot(
		context.Background(),
		conversation.ChatRequest{
			BotID:     "bot-1",
			SessionID: "session-1",
			Query:     "---\nmessage-id: tg-1\nchannel: telegram\n---\n@memoh1bot ping",
		},
		&run,
		resolvedContext{
			userMessageAlreadyInContext: true,
		},
		terminalSnapshot{
			sdkMessages: []sdk.Message{sdk.AssistantMessage("pong")},
		},
	); err != nil {
		t.Fatalf("persistTerminalSnapshot returned error: %v", err)
	}

	if len(messages.persisted) != 1 {
		t.Fatalf("expected only assistant output to persist, got %#v", messages.persisted)
	}
	if messages.persisted[0].Role != "assistant" {
		t.Fatalf("unexpected persisted role: %q", messages.persisted[0].Role)
	}
}
