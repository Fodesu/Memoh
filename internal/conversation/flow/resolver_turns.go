package flow

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/memohai/memoh/internal/conversation"
	dbpkg "github.com/memohai/memoh/internal/db"
	dbsqlc "github.com/memohai/memoh/internal/db/postgres/sqlc"
)

func sessionTurnKey(botID, sessionID string) string {
	return strings.TrimSpace(botID) + ":" + strings.TrimSpace(sessionID)
}

func (r *Resolver) enterSessionTurn(_ context.Context, botID, sessionID string) func() {
	botID = strings.TrimSpace(botID)
	sessionID = strings.TrimSpace(sessionID)
	if botID == "" || sessionID == "" {
		return func() {}
	}

	key := sessionTurnKey(botID, sessionID)
	r.sessionTurnMu.Lock()
	if r.sessionTurnLocks == nil {
		r.sessionTurnLocks = make(map[string]*sync.Mutex)
	}
	lock := r.sessionTurnLocks[key]
	if lock == nil {
		lock = &sync.Mutex{}
		r.sessionTurnLocks[key] = lock
	}
	r.sessionTurnMu.Unlock()

	lock.Lock()
	var once sync.Once
	return func() {
		once.Do(func() {
			lock.Unlock()
		})
	}
}

type turnStore interface {
	CreateHistoryTurn(context.Context, dbsqlc.CreateHistoryTurnParams) (dbsqlc.BotHistoryTurn, error)
	GetSessionByID(context.Context, pgtype.UUID) (dbsqlc.BotSession, error)
	GetVisibleUserMessageTurnForRewrite(context.Context, dbsqlc.GetVisibleUserMessageTurnForRewriteParams) (dbsqlc.GetVisibleUserMessageTurnForRewriteRow, error)
	UpdateSessionHeadTurn(context.Context, dbsqlc.UpdateSessionHeadTurnParams) (dbsqlc.BotSession, error)
	UpdateSessionHeadTurnIfCurrent(context.Context, dbsqlc.UpdateSessionHeadTurnIfCurrentParams) (dbsqlc.BotSession, error)
}

func (r *Resolver) pinPersistTurn(ctx context.Context, req conversation.ChatRequest) (conversation.ChatRequest, error) {
	if r == nil || r.queries == nil || strings.TrimSpace(req.PersistTurnID) != "" {
		return req, nil
	}
	botID := strings.TrimSpace(req.BotID)
	sessionID := strings.TrimSpace(req.SessionID)
	if botID == "" || sessionID == "" {
		return req, nil
	}
	store, ok := r.queries.(turnStore)
	if !ok {
		return req, nil
	}
	pgBotID, err := dbpkg.ParseUUID(botID)
	if err != nil {
		return req, fmt.Errorf("pin persist turn: invalid bot id: %w", err)
	}
	pgSessionID, err := dbpkg.ParseUUID(sessionID)
	if err != nil {
		return req, fmt.Errorf("pin persist turn: invalid session id: %w", err)
	}
	parentTurnID, baseHeadTurnID, contextHeadTurnPinned, err := r.resolvePersistParentTurn(ctx, store, pgSessionID, req)
	if err != nil {
		return req, err
	}
	turn, err := store.CreateHistoryTurn(ctx, dbsqlc.CreateHistoryTurnParams{
		BotID:          pgBotID,
		OwnerSessionID: pgSessionID,
		ParentTurnID:   parentTurnID,
	})
	if err != nil {
		return req, fmt.Errorf("pin persist turn: create history turn: %w", err)
	}
	req.PersistTurnID = turn.ID.String()
	if baseHeadTurnID.Valid {
		req.PersistBaseHeadTurnID = baseHeadTurnID.String()
	}
	req.PersistBaseHeadTurnPinned = true
	if parentTurnID.Valid {
		req.ContextHeadTurnID = parentTurnID.String()
	}
	req.ContextHeadTurnPinned = contextHeadTurnPinned
	return req, nil
}

func (*Resolver) resolvePersistParentTurn(ctx context.Context, store turnStore, sessionID pgtype.UUID, req conversation.ChatRequest) (pgtype.UUID, pgtype.UUID, bool, error) {
	sess, err := store.GetSessionByID(ctx, sessionID)
	if err != nil {
		return pgtype.UUID{}, pgtype.UUID{}, false, fmt.Errorf("pin persist turn: get session: %w", err)
	}
	baseHeadTurnID := sess.HeadTurnID

	rewriteTargetID := strings.TrimSpace(req.RewriteTargetMessageID)
	if rewriteTargetID != "" {
		pgMessageID, err := dbpkg.ParseUUID(rewriteTargetID)
		if err != nil {
			return pgtype.UUID{}, pgtype.UUID{}, false, fmt.Errorf("pin persist turn: invalid rewrite target message id: %w", err)
		}
		row, err := store.GetVisibleUserMessageTurnForRewrite(ctx, dbsqlc.GetVisibleUserMessageTurnForRewriteParams{
			MessageID: pgMessageID,
			SessionID: sessionID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return pgtype.UUID{}, pgtype.UUID{}, false, errors.New("pin persist turn: rewrite target is not a visible user message")
			}
			return pgtype.UUID{}, pgtype.UUID{}, false, fmt.Errorf("pin persist turn: resolve rewrite target: %w", err)
		}
		return row.ParentTurnID, baseHeadTurnID, true, nil
	}

	return baseHeadTurnID, baseHeadTurnID, false, nil
}

func (r *Resolver) updateSessionHeadTurn(ctx context.Context, req conversation.ChatRequest, turnID string) error {
	if r == nil || r.queries == nil {
		return nil
	}
	sessionID := strings.TrimSpace(req.SessionID)
	turnID = strings.TrimSpace(turnID)
	if sessionID == "" || turnID == "" {
		return nil
	}
	store, ok := r.queries.(turnStore)
	if !ok {
		return nil
	}
	pgSessionID, err := dbpkg.ParseUUID(sessionID)
	if err != nil {
		return fmt.Errorf("update session head: invalid session id: %w", err)
	}
	pgTurnID, err := dbpkg.ParseUUID(turnID)
	if err != nil {
		return fmt.Errorf("update session head: invalid turn id: %w", err)
	}
	expectedText := strings.TrimSpace(req.PersistBaseHeadTurnID)
	useCompareAndSet := req.PersistBaseHeadTurnPinned
	if !useCompareAndSet && strings.TrimSpace(req.PersistTurnID) != "" {
		expectedText = strings.TrimSpace(req.PersistTurnID)
		useCompareAndSet = true
	}
	if useCompareAndSet {
		var expectedHeadTurnID pgtype.UUID
		if expectedText != "" {
			parsed, err := dbpkg.ParseUUID(expectedText)
			if err != nil {
				return fmt.Errorf("update session head: invalid expected turn id: %w", err)
			}
			expectedHeadTurnID = parsed
		}
		if _, err := store.UpdateSessionHeadTurnIfCurrent(ctx, dbsqlc.UpdateSessionHeadTurnIfCurrentParams{
			ID:                 pgSessionID,
			HeadTurnID:         pgTurnID,
			ExpectedHeadTurnID: expectedHeadTurnID,
		}); err != nil {
			return fmt.Errorf("update session head: expected head %q no longer current: %w", expectedText, err)
		}
		return nil
	}
	if _, err := store.UpdateSessionHeadTurn(ctx, dbsqlc.UpdateSessionHeadTurnParams{
		ID:         pgSessionID,
		HeadTurnID: pgTurnID,
	}); err != nil {
		return fmt.Errorf("update session head: %w", err)
	}
	return nil
}
