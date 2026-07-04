package main

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"

	"github.com/jackc/pgx/v5/pgxpool"
)

type sqlTemplateExecutor struct {
	cfg     Config
	pool    *pgxpool.Pool
	queries QuerySet
}

func newSQLTemplateExecutor(cfg Config, pool *pgxpool.Pool, queries QuerySet) (*sqlTemplateExecutor, error) {
	if len(queries) == 0 {
		return nil, errors.New("sql runner requires loaded SQL templates")
	}
	return &sqlTemplateExecutor{
		cfg:     cfg,
		pool:    pool,
		queries: queries,
	}, nil
}

func (*sqlTemplateExecutor) querySource() string {
	return querySourceSQLTemplate
}

func (*sqlTemplateExecutor) scanMode() string {
	return scanModeRowDrainOnly
}

func (e *sqlTemplateExecutor) execQuery(ctx context.Context, queryName string, s SessionSeed, rng *rand.Rand) (int64, error) {
	switch queryName {
	case queryChatPageUI:
		return e.execComposite(ctx, []string{queryLatestPage, queryMessageAssets, queryApprovalToolCalls, queryUserInputToolCalls}, s, rng, false)
	case queryLocateWindow:
		return e.execComposite(ctx, []string{queryExternalLookup, queryBeforePage, queryAfterPage, queryMessageAssets, queryApprovalToolCalls, queryUserInputToolCalls}, s, rng, false)
	case queryApprovalResolve:
		return e.execComposite(ctx, []string{queryApprovalLatest, queryApprovalShortID}, s, rng, true)
	case queryUserInputResolve:
		return e.execComposite(ctx, []string{queryUserInputLatest, queryUserInputShortID}, s, rng, true)
	}

	if (queryName == queryApprovalToolCalls || queryName == queryUserInputToolCalls) && len(pageToolCallIDs(s)) == 0 {
		return 0, nil
	}

	sql, ok := e.queries[queryName]
	if !ok {
		return 0, fmt.Errorf("query %s not loaded", queryName)
	}
	args, err := e.argsForQuery(queryName, s, rng)
	if err != nil {
		return 0, err
	}
	rows, err := e.pool.Query(ctx, sql, args...)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var count int64
	for rows.Next() {
		count++
	}
	if err := rows.Err(); err != nil {
		return count, err
	}
	return count, nil
}

func (e *sqlTemplateExecutor) argsForQuery(queryName string, s SessionSeed, rng *rand.Rand) ([]any, error) {
	switch queryName {
	case queryLatestPage:
		return []any{s.SessionID, e.cfg.Workload.PageSize}, nil
	case queryBeforePage:
		cursorID, _ := selectedCursor(s, rng)
		return []any{s.SessionID, e.cfg.Workload.PageSize, cursorID}, nil
	case queryAfterPage:
		cursorID, _ := selectedCursor(s, rng)
		return []any{s.SessionID, e.cfg.Workload.PageSize, cursorID}, nil
	case queryExternalLookup:
		return []any{s.SessionID, selectedExternalMessageID(s, rng)}, nil
	case queryMessageAssets:
		return []any{messageAssetIDs(s)}, nil
	case queryApprovalList, queryApprovalPendingList, queryApprovalLatest:
		return []any{s.BotID, s.SessionID}, nil
	case queryApprovalToolCalls:
		return []any{s.BotID, s.SessionID, pageToolCallIDs(s)}, nil
	case queryApprovalShortID:
		shortID, err := requiredShortID(queryName, s.ApprovalShortID)
		if err != nil {
			return nil, err
		}
		return []any{s.BotID, s.SessionID, shortID}, nil
	case queryApprovalReplyMessage:
		promptID, err := requiredText(queryName, s.ApprovalPromptID)
		if err != nil {
			return nil, err
		}
		return []any{s.BotID, s.SessionID, promptID}, nil
	case queryUserInputList, queryUserInputPendingList, queryUserInputLatest:
		return []any{s.BotID, s.SessionID}, nil
	case queryUserInputToolCalls:
		return []any{s.BotID, s.SessionID, pageToolCallIDs(s)}, nil
	case queryUserInputShortID:
		shortID, err := requiredShortID(queryName, s.UserInputShortID)
		if err != nil {
			return nil, err
		}
		return []any{s.BotID, s.SessionID, shortID}, nil
	case queryUserInputReplyMessage:
		promptID, err := requiredText(queryName, s.UserInputPromptID)
		if err != nil {
			return nil, err
		}
		return []any{s.BotID, s.SessionID, promptID}, nil
	default:
		return nil, fmt.Errorf("unknown query %s", queryName)
	}
}

func (e *sqlTemplateExecutor) execComposite(ctx context.Context, parts []string, s SessionSeed, rng *rand.Rand, skipMissing bool) (int64, error) {
	var total int64
	for _, name := range parts {
		if skipMissing && compositePartMissing(name, s) {
			continue
		}
		rows, err := e.execQuery(ctx, name, s, rng)
		total += rows
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

func compositePartMissing(name string, s SessionSeed) bool {
	switch name {
	case queryApprovalShortID:
		return s.ApprovalShortID <= 0
	case queryUserInputShortID:
		return s.UserInputShortID <= 0
	default:
		return false
	}
}
