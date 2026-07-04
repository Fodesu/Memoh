package main

import (
	"context"
	"fmt"
	"math/rand/v2"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	postgresqlc "github.com/memohai/memoh/internal/db/postgres/sqlc"
	postgresstore "github.com/memohai/memoh/internal/db/postgres/store"
	messagepkg "github.com/memohai/memoh/internal/message"
)

type sqlcExecutor struct {
	cfg            Config
	queries        *postgresqlc.Queries
	messageService *messagepkg.DBService
}

func newSQLCExecutor(cfg Config, pool *pgxpool.Pool) *sqlcExecutor {
	queries := postgresqlc.New(pool)
	return &sqlcExecutor{
		cfg:            cfg,
		queries:        queries,
		messageService: messagepkg.NewService(nil, postgresstore.NewQueries(queries)),
	}
}

func (*sqlcExecutor) querySource() string {
	return querySourceGeneratedSQLC
}

func (*sqlcExecutor) scanMode() string {
	return scanModeSQLCStructScan
}

func (e *sqlcExecutor) execQuery(ctx context.Context, queryName string, s SessionSeed, rng *rand.Rand) (int64, error) {
	maxCount := pageSizeInt32(e.cfg)
	switch queryName {
	case queryChatPageUI:
		return e.execChatPageUI(ctx, s, rng)
	case queryLocateWindow:
		return e.execLocateWindow(ctx, s, rng)
	case queryApprovalResolve:
		return e.execApprovalResolve(ctx, s)
	case queryUserInputResolve:
		return e.execUserInputResolve(ctx, s)
	case queryWriteUserMessage:
		return e.execWriteUserMessage(ctx, s)
	case queryWriteAssistantMessage:
		return e.execWriteAssistantMessage(ctx, s)
	case queryWriteTurnPair:
		return e.execWriteTurnPair(ctx, s)
	case queryLatestPage:
		items, err := e.queries.ListMessagesLatestBySession(ctx, postgresqlc.ListMessagesLatestBySessionParams{
			SessionID: pgUUID(s.SessionID),
			MaxCount:  maxCount,
		})
		return int64(len(items)), err
	case queryBeforePage:
		cursorID, _ := selectedCursor(s, rng)
		items, err := e.queries.ListMessagesBeforeMessageBySession(ctx, postgresqlc.ListMessagesBeforeMessageBySessionParams{
			SessionID:       pgUUID(s.SessionID),
			MaxCount:        maxCount,
			BeforeMessageID: pgUUID(cursorID),
		})
		return int64(len(items)), err
	case queryAfterPage:
		cursorID, _ := selectedCursor(s, rng)
		items, err := e.queries.ListMessagesAfterMessageBySession(ctx, postgresqlc.ListMessagesAfterMessageBySessionParams{
			SessionID:      pgUUID(s.SessionID),
			MaxCount:       maxCount,
			AfterMessageID: pgUUID(cursorID),
		})
		return int64(len(items)), err
	case queryExternalLookup:
		externalID := selectedExternalMessageID(s, rng)
		_, err := e.queries.GetMessageByExternalIDBySession(ctx, postgresqlc.GetMessageByExternalIDBySessionParams{
			SessionID:         pgUUID(s.SessionID),
			ExternalMessageID: pgText(externalID),
		})
		return rowsForOne(err)
	case queryMessageAssets:
		items, err := e.queries.ListMessageAssetsBatch(ctx, pgUUIDs(messageAssetIDs(s)))
		return int64(len(items)), err
	case queryApprovalList:
		items, err := e.queries.ListToolApprovalsBySession(ctx, postgresqlc.ListToolApprovalsBySessionParams{
			BotID:     pgUUID(s.BotID),
			SessionID: pgUUID(s.SessionID),
		})
		return int64(len(items)), err
	case queryApprovalToolCalls:
		toolCallIDs := pageToolCallIDs(s)
		if len(toolCallIDs) == 0 {
			return 0, nil
		}
		items, err := e.queries.ListToolApprovalsBySessionToolCalls(ctx, postgresqlc.ListToolApprovalsBySessionToolCallsParams{
			BotID:       pgUUID(s.BotID),
			SessionID:   pgUUID(s.SessionID),
			ToolCallIds: toolCallIDs,
		})
		return int64(len(items)), err
	case queryApprovalPendingList:
		items, err := e.queries.ListPendingToolApprovalsBySession(ctx, postgresqlc.ListPendingToolApprovalsBySessionParams{
			BotID:     pgUUID(s.BotID),
			SessionID: pgUUID(s.SessionID),
		})
		return int64(len(items)), err
	case queryApprovalLatest:
		_, err := e.queries.GetLatestPendingToolApprovalBySession(ctx, postgresqlc.GetLatestPendingToolApprovalBySessionParams{
			BotID:     pgUUID(s.BotID),
			SessionID: pgUUID(s.SessionID),
		})
		return rowsForOne(err)
	case queryApprovalShortID:
		shortID, err := requiredShortID(queryName, s.ApprovalShortID)
		if err != nil {
			return 0, err
		}
		_, err = e.queries.GetPendingToolApprovalBySessionShortID(ctx, postgresqlc.GetPendingToolApprovalBySessionShortIDParams{
			BotID:     pgUUID(s.BotID),
			SessionID: pgUUID(s.SessionID),
			ShortID:   shortID,
		})
		return rowsForOne(err)
	case queryApprovalReplyMessage:
		promptID, err := requiredText(queryName, s.ApprovalPromptID)
		if err != nil {
			return 0, err
		}
		_, err = e.queries.GetPendingToolApprovalByReplyMessage(ctx, postgresqlc.GetPendingToolApprovalByReplyMessageParams{
			BotID:                   pgUUID(s.BotID),
			SessionID:               pgUUID(s.SessionID),
			PromptExternalMessageID: promptID,
		})
		return rowsForOne(err)
	case queryUserInputList:
		items, err := e.queries.ListUserInputsBySession(ctx, postgresqlc.ListUserInputsBySessionParams{
			BotID:     pgUUID(s.BotID),
			SessionID: pgUUID(s.SessionID),
		})
		return int64(len(items)), err
	case queryUserInputToolCalls:
		toolCallIDs := pageToolCallIDs(s)
		if len(toolCallIDs) == 0 {
			return 0, nil
		}
		items, err := e.queries.ListUserInputsBySessionToolCalls(ctx, postgresqlc.ListUserInputsBySessionToolCallsParams{
			BotID:       pgUUID(s.BotID),
			SessionID:   pgUUID(s.SessionID),
			ToolCallIds: toolCallIDs,
		})
		return int64(len(items)), err
	case queryUserInputPendingList:
		items, err := e.queries.ListPendingUserInputsBySession(ctx, postgresqlc.ListPendingUserInputsBySessionParams{
			BotID:     pgUUID(s.BotID),
			SessionID: pgUUID(s.SessionID),
		})
		return int64(len(items)), err
	case queryUserInputLatest:
		_, err := e.queries.GetLatestPendingUserInputBySession(ctx, postgresqlc.GetLatestPendingUserInputBySessionParams{
			BotID:     pgUUID(s.BotID),
			SessionID: pgUUID(s.SessionID),
		})
		return rowsForOne(err)
	case queryUserInputShortID:
		shortID, err := requiredShortID(queryName, s.UserInputShortID)
		if err != nil {
			return 0, err
		}
		_, err = e.queries.GetPendingUserInputBySessionShortID(ctx, postgresqlc.GetPendingUserInputBySessionShortIDParams{
			BotID:     pgUUID(s.BotID),
			SessionID: pgUUID(s.SessionID),
			ShortID:   shortID,
		})
		return rowsForOne(err)
	case queryUserInputReplyMessage:
		promptID, err := requiredText(queryName, s.UserInputPromptID)
		if err != nil {
			return 0, err
		}
		_, err = e.queries.GetPendingUserInputByReplyMessage(ctx, postgresqlc.GetPendingUserInputByReplyMessageParams{
			BotID:                   pgUUID(s.BotID),
			SessionID:               pgUUID(s.SessionID),
			PromptExternalMessageID: promptID,
		})
		return rowsForOne(err)
	default:
		return 0, fmt.Errorf("unknown query %s", queryName)
	}
}

func (e *sqlcExecutor) execChatPageUI(ctx context.Context, s SessionSeed, rng *rand.Rand) (int64, error) {
	items, err := e.queries.ListMessagesLatestUIBySession(ctx, postgresqlc.ListMessagesLatestUIBySessionParams{
		SessionID: pgUUID(s.SessionID),
		MaxCount:  pageSizeInt32(e.cfg),
	})
	rows := int64(len(items))
	if err != nil {
		return rows, err
	}
	extra, err := e.execQuery(ctx, queryMessageAssets, s, rng)
	rows += extra
	if err != nil {
		return rows, err
	}
	extra, err = e.execQuery(ctx, queryApprovalToolCalls, s, rng)
	rows += extra
	if err != nil {
		return rows, err
	}
	extra, err = e.execQuery(ctx, queryUserInputToolCalls, s, rng)
	rows += extra
	return rows, err
}

func (e *sqlcExecutor) execWriteUserMessage(ctx context.Context, s SessionSeed) (int64, error) {
	_, err := e.messageService.Persist(ctx, e.writeMessageInput(s, "user", ""))
	return rowsForWrite(1, err)
}

func (e *sqlcExecutor) execWriteAssistantMessage(ctx context.Context, s SessionSeed) (int64, error) {
	_, err := e.messageService.Persist(ctx, e.writeMessageInput(s, "assistant", ""))
	return rowsForWrite(1, err)
}

func (e *sqlcExecutor) execWriteTurnPair(ctx context.Context, s SessionSeed) (int64, error) {
	user, err := e.messageService.Persist(ctx, e.writeMessageInput(s, "user", ""))
	if err != nil {
		return 0, err
	}
	if _, err := e.messageService.Persist(ctx, e.writeMessageInput(s, "assistant", user.ID)); err != nil {
		return 1, err
	}
	return 2, nil
}

func (e *sqlcExecutor) writeMessageInput(s SessionSeed, role string, turnRequestMessageID string) messagepkg.PersistInput {
	messageID := uuid.NewString()
	content := jsonRaw(`{"type":"text","content":"bench write"}`)
	displayText := "bench write"
	if role == "assistant" {
		content = jsonText(map[string]any{
			"role":    "assistant",
			"content": []map[string]string{{"type": "text", "text": "bench write assistant"}},
		})
		displayText = "bench write assistant"
	}
	return messagepkg.PersistInput{
		BotID:             s.BotID.String(),
		SessionID:         s.SessionID.String(),
		SenderUserID:      senderUserIDForRole(s, role),
		ExternalMessageID: "bench-write-" + messageID,
		Role:              role,
		Content:           content,
		Metadata: map[string]any{
			"benchmark":        benchmarkName,
			"benchmark_marker": e.cfg.Seed.Marker,
			"benchmark_write":  true,
		},
		Usage:                jsonRaw("{}"),
		SessionMode:          "chat",
		RuntimeType:          "model",
		DisplayText:          displayText,
		TurnRequestMessageID: turnRequestMessageID,
	}
}

func senderUserIDForRole(s SessionSeed, role string) string {
	if role == "user" && s.OwnerUserID != uuid.Nil {
		return s.OwnerUserID.String()
	}
	return ""
}

func rowsForWrite(rows int64, err error) (int64, error) {
	if err != nil {
		return rows, err
	}
	return rows, nil
}

func (e *sqlcExecutor) execLocateWindow(ctx context.Context, s SessionSeed, rng *rand.Rand) (int64, error) {
	externalID := selectedExternalMessageID(s, rng)
	target, err := e.queries.GetMessageByExternalIDBySession(ctx, postgresqlc.GetMessageByExternalIDBySessionParams{
		SessionID:         pgUUID(s.SessionID),
		ExternalMessageID: pgText(externalID),
	})
	rows, err := rowsForOne(err)
	if err != nil {
		return rows, err
	}
	before, err := e.queries.ListMessagesBeforeMessageBySession(ctx, postgresqlc.ListMessagesBeforeMessageBySessionParams{
		SessionID:       pgUUID(s.SessionID),
		MaxCount:        pageSizeInt32(e.cfg),
		BeforeMessageID: target.ID,
	})
	rows += int64(len(before))
	if err != nil {
		return rows, err
	}
	after, err := e.queries.ListMessagesAfterMessageBySession(ctx, postgresqlc.ListMessagesAfterMessageBySessionParams{
		SessionID:      pgUUID(s.SessionID),
		MaxCount:       pageSizeInt32(e.cfg),
		AfterMessageID: target.ID,
	})
	rows += int64(len(after))
	if err != nil {
		return rows, err
	}
	extra, err := e.execQuery(ctx, queryMessageAssets, s, rng)
	rows += extra
	if err != nil {
		return rows, err
	}
	extra, err = e.execQuery(ctx, queryApprovalToolCalls, s, rng)
	rows += extra
	if err != nil {
		return rows, err
	}
	extra, err = e.execQuery(ctx, queryUserInputToolCalls, s, rng)
	rows += extra
	return rows, err
}

func (e *sqlcExecutor) execApprovalResolve(ctx context.Context, s SessionSeed) (int64, error) {
	_, err := e.queries.GetLatestPendingToolApprovalBySession(ctx, postgresqlc.GetLatestPendingToolApprovalBySessionParams{
		BotID:     pgUUID(s.BotID),
		SessionID: pgUUID(s.SessionID),
	})
	rows, err := rowsForOne(err)
	if err != nil {
		return rows, err
	}
	if s.ApprovalShortID > 0 {
		_, err = e.queries.GetPendingToolApprovalBySessionShortID(ctx, postgresqlc.GetPendingToolApprovalBySessionShortIDParams{
			BotID:     pgUUID(s.BotID),
			SessionID: pgUUID(s.SessionID),
			ShortID:   s.ApprovalShortID,
		})
		extra, err := rowsForOne(err)
		rows += extra
		if err != nil {
			return rows, err
		}
	}
	return rows, nil
}

func (e *sqlcExecutor) execUserInputResolve(ctx context.Context, s SessionSeed) (int64, error) {
	_, err := e.queries.GetLatestPendingUserInputBySession(ctx, postgresqlc.GetLatestPendingUserInputBySessionParams{
		BotID:     pgUUID(s.BotID),
		SessionID: pgUUID(s.SessionID),
	})
	rows, err := rowsForOne(err)
	if err != nil {
		return rows, err
	}
	if s.UserInputShortID > 0 {
		_, err = e.queries.GetPendingUserInputBySessionShortID(ctx, postgresqlc.GetPendingUserInputBySessionShortIDParams{
			BotID:     pgUUID(s.BotID),
			SessionID: pgUUID(s.SessionID),
			ShortID:   s.UserInputShortID,
		})
		extra, err := rowsForOne(err)
		rows += extra
		if err != nil {
			return rows, err
		}
	}
	return rows, nil
}

func pageSizeInt32(cfg Config) int32 {
	if cfg.Workload.PageSize > 2147483647 {
		return 2147483647
	}
	// #nosec G115 -- config validation requires a positive int and the upper bound is checked above.
	return int32(cfg.Workload.PageSize)
}

func rowsForOne(err error) (int64, error) {
	if err != nil {
		return 0, err
	}
	return 1, nil
}

func pgUUID(id uuid.UUID) pgtype.UUID {
	if id == uuid.Nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: id, Valid: true}
}

func pgUUIDs(ids []uuid.UUID) []pgtype.UUID {
	out := make([]pgtype.UUID, 0, len(ids))
	for _, id := range ids {
		out = append(out, pgUUID(id))
	}
	return out
}

func pgText(value string) pgtype.Text {
	if value == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: value, Valid: true}
}

func requiredShortID(queryName string, v int32) (int32, error) {
	if v <= 0 {
		return 0, queryArgError(fmt.Sprintf("%s requires a pending short_id; increase pending_ratio or request density", queryName))
	}
	return v, nil
}

func requiredText(queryName, value string) (string, error) {
	if value == "" {
		return "", queryArgError(fmt.Sprintf("%s requires a prompt external id; increase pending_ratio or request density", queryName))
	}
	return value, nil
}
