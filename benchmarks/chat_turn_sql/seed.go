package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SeedCatalog struct {
	Marker       string        `json:"marker"`
	BotIDs       []uuid.UUID   `json:"bot_ids"`
	Sessions     []SessionSeed `json:"sessions"`
	HotSessions  []int         `json:"hot_sessions"`
	ColdSessions []int         `json:"cold_sessions"`
	Estimate     SeedEstimate  `json:"estimate"`
}

type SessionSeed struct {
	BotID              uuid.UUID   `json:"bot_id"`
	OwnerUserID        uuid.UUID   `json:"owner_user_id"`
	RouteID            uuid.UUID   `json:"route_id"`
	SessionID          uuid.UUID   `json:"session_id"`
	LatestTurnID       uuid.UUID   `json:"latest_turn_id"`
	LatestMessageID    uuid.UUID   `json:"latest_message_id"`
	PageMessageIDs     []uuid.UUID `json:"page_message_ids"`
	PageToolCallIDs    []string    `json:"page_tool_call_ids"`
	CursorMessageIDs   []uuid.UUID `json:"cursor_message_ids"`
	CursorCreatedAts   []time.Time `json:"cursor_created_ats"`
	ExternalMessageID  string      `json:"external_message_id"`
	ExternalMessageIDs []string    `json:"external_message_ids"`
	ApprovalRequestID  uuid.UUID   `json:"approval_request_id"`
	ApprovalShortID    int32       `json:"approval_short_id"`
	ApprovalPromptID   string      `json:"approval_prompt_external_id"`
	UserInputRequestID uuid.UUID   `json:"user_input_request_id"`
	UserInputShortID   int32       `json:"user_input_short_id"`
	UserInputPromptID  string      `json:"user_input_prompt_external_id"`
}

type assetSeedRow struct {
	id          uuid.UUID
	messageID   uuid.UUID
	role        string
	ordinal     int
	contentHash string
	name        string
	metadata    json.RawMessage
	createdAt   time.Time
}

type pendingToolCallSeed struct {
	id    string
	name  string
	input json.RawMessage
}

type approvalInsertShape struct {
	columns             []string
	hasOperation        bool
	hasRequestedMessage bool
	hasPromptMessage    bool
	hasPromptExternal   bool
}

type userInputInsertShape struct {
	columns           []string
	hasPromptMessage  bool
	hasPromptExternal bool
}

func seedBenchmarkData(ctx context.Context, pool *pgxpool.Pool, cfg Config) (SeedCatalog, error) {
	if cfg.Seed.CleanupBefore {
		if err := cleanupBenchmarkData(ctx, pool, cfg.Seed.Marker); err != nil {
			return SeedCatalog{}, fmt.Errorf("cleanup before seed: %w", err)
		}
	}
	approvalShape, err := loadApprovalInsertShape(ctx, pool)
	if err != nil {
		return SeedCatalog{}, err
	}
	userInputShape, err := loadUserInputInsertShape(ctx, pool)
	if err != nil {
		return SeedCatalog{}, err
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return SeedCatalog{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	now := time.Now().UTC().Add(-24 * time.Hour)
	markerJSON := jsonText(map[string]any{
		"benchmark":        benchmarkName,
		"benchmark_marker": cfg.Seed.Marker,
	})

	userBatch := newCopyBatcher(ctx, tx, "users", []string{"id", "username", "email", "role", "display_name", "timezone", "metadata", "created_at", "updated_at"}, 5000)
	identityBatch := newCopyBatcher(ctx, tx, "channel_identities", []string{"id", "channel_type", "channel_subject_id", "display_name", "metadata", "created_at", "updated_at"}, 5000)
	botBatch := newCopyBatcher(ctx, tx, "bots", []string{"id", "owner_user_id", "name", "display_name", "timezone", "metadata", "created_at", "updated_at"}, 5000)
	routeBatch := newCopyBatcher(ctx, tx, "bot_channel_routes", []string{"id", "bot_id", "channel_type", "external_conversation_id", "external_thread_id", "conversation_type", "default_reply_target", "metadata", "created_at", "updated_at"}, 5000)
	sessionBatch := newCopyBatcher(ctx, tx, "bot_sessions", []string{"id", "bot_id", "route_id", "channel_type", "type", "title", "metadata", "created_by_user_id", "created_at", "updated_at"}, 5000)
	messageBatch := newCopyBatcher(ctx, tx, "bot_history_messages", []string{"id", "bot_id", "session_id", "sender_channel_identity_id", "sender_account_user_id", "source_message_id", "source_reply_to_message_id", "role", "content", "metadata", "usage", "display_text", "turn_id", "turn_message_seq", "created_at"}, 10000)
	turnBatch := newCopyBatcher(ctx, tx, "bot_history_turns", []string{"id", "bot_id", "session_id", "position", "request_message_id", "assistant_message_id", "created_at", "updated_at"}, 10000)
	approvalBatch := newCopyBatcher(ctx, tx, "tool_approval_requests", approvalShape.columns, 5000)
	userInputBatch := newCopyBatcher(ctx, tx, "user_input_requests", userInputShape.columns, 5000)
	assetBatch := newCopyBatcher(ctx, tx, "bot_history_message_assets", []string{"id", "message_id", "role", "ordinal", "content_hash", "name", "metadata", "created_at"}, 5000)

	catalog := SeedCatalog{
		Marker:   cfg.Seed.Marker,
		BotIDs:   make([]uuid.UUID, 0, cfg.Seed.Bots),
		Sessions: make([]SessionSeed, 0, cfg.Seed.Bots*cfg.Seed.SessionsPerBot),
		Estimate: estimateSeed(cfg),
	}

	sessionGlobalIdx := 0
	messageGlobalIdx := 0
	turnGlobalIdx := 0
	approvalGlobalIdx := 0
	userInputGlobalIdx := 0
	for botIdx := 0; botIdx < cfg.Seed.Bots; botIdx++ {
		userID := uuid.New()
		identityID := uuid.New()
		botID := uuid.New()
		catalog.BotIDs = append(catalog.BotIDs, botID)

		if err := userBatch.add(userID, uniqueName("bench-user", cfg.Seed.Marker, botIdx), uniqueEmail(cfg.Seed.Marker, botIdx), "member", fmt.Sprintf("Bench User %d", botIdx), "UTC", markerJSON, now, now); err != nil {
			return SeedCatalog{}, err
		}
		if err := identityBatch.add(identityID, "local", uniqueName("bench-subject", cfg.Seed.Marker, botIdx), fmt.Sprintf("Bench Identity %d", botIdx), markerJSON, now, now); err != nil {
			return SeedCatalog{}, err
		}
		if err := botBatch.add(botID, userID, uniqueBotName(cfg.Seed.Marker, botIdx), fmt.Sprintf("Bench Bot %d", botIdx), "UTC", markerJSON, now, now); err != nil {
			return SeedCatalog{}, err
		}
		for _, batch := range []*copyBatcher{userBatch, identityBatch, botBatch} {
			if err := batch.flush(); err != nil {
				return SeedCatalog{}, err
			}
		}

		for sessIdx := 0; sessIdx < cfg.Seed.SessionsPerBot; sessIdx++ {
			sessionIdx := sessionGlobalIdx
			sessionGlobalIdx++
			routeID := uuid.New()
			sessionID := uuid.New()
			sessionCreated := now.Add(time.Duration(sessionIdx) * time.Millisecond)
			routeMetadata := jsonText(map[string]any{
				"benchmark":         benchmarkName,
				"benchmark_marker":  cfg.Seed.Marker,
				"conversation_name": fmt.Sprintf("Bench Conversation %d", sessionIdx),
			})
			sessionMetadata := jsonText(map[string]any{
				"benchmark":        benchmarkName,
				"benchmark_marker": cfg.Seed.Marker,
				"hot":              isHotSession(cfg.Seed, sessIdx),
			})
			if err := routeBatch.add(routeID, botID, "local", uniqueName("bench-conversation", cfg.Seed.Marker, sessionIdx), nil, "group", "", routeMetadata, sessionCreated, sessionCreated); err != nil {
				return SeedCatalog{}, err
			}
			if err := sessionBatch.add(sessionID, botID, routeID, "local", "chat", fmt.Sprintf("Bench Session %d", sessionIdx), sessionMetadata, userID, sessionCreated, sessionCreated); err != nil {
				return SeedCatalog{}, err
			}
			for _, batch := range []*copyBatcher{routeBatch, sessionBatch} {
				if err := batch.flush(); err != nil {
					return SeedCatalog{}, err
				}
			}

			sessionSeed := SessionSeed{
				BotID:       botID,
				OwnerUserID: userID,
				RouteID:     routeID,
				SessionID:   sessionID,
			}
			var approvalSeq int32
			var userInputSeq int32
			assetRows := make([]assetSeedRow, 0)
			cursorTargets := cursorTurnIndexes(cfg.Seed.TurnsPerSession)
			pageStart := max(cfg.Seed.TurnsPerSession-(cfg.Workload.PageSize/cfg.Seed.MessagesPerTurn)-2, 0)

			for turnIdx := 0; turnIdx < cfg.Seed.TurnsPerSession; turnIdx++ {
				turnGlobalIdx++
				turnID := uuid.New()
				turnCreated := sessionCreated.Add(time.Duration(turnIdx) * time.Millisecond)
				requestID := uuid.New()
				assistantID := uuid.New()
				requestCreated := turnCreated.Add(time.Microsecond)
				assistantCreated := turnCreated.Add(2 * time.Microsecond)
				toolCalls := make([]pendingToolCallSeed, 0, 2)

				if cfg.Seed.ApprovalEveryNTurns > 0 && turnGlobalIdx%cfg.Seed.ApprovalEveryNTurns == 0 {
					toolCalls = append(toolCalls, pendingToolCallSeed{
						id:    uniqueName("tool-call", cfg.Seed.Marker, turnGlobalIdx),
						name:  "write",
						input: jsonRaw(`{"path":"/tmp/bench.txt"}`),
					})
				}
				if cfg.Seed.UserInputEveryNTurns > 0 && turnGlobalIdx%cfg.Seed.UserInputEveryNTurns == 0 {
					toolCalls = append(toolCalls, pendingToolCallSeed{
						id:    uniqueName("ask-user", cfg.Seed.Marker, turnGlobalIdx),
						name:  "ask_user",
						input: jsonRaw(`{"question":"bench?"}`),
					})
				}

				requestSource := uniqueName("bench-msg", cfg.Seed.Marker, messageGlobalIdx+1)
				if err := messageBatch.add(requestID, botID, sessionID, identityID, userID, requestSource, nil, "user", jsonText(map[string]any{
					"type":    "text",
					"content": fmt.Sprintf("bench user session=%d turn=%d", sessionIdx, turnIdx),
				}), jsonRaw("{}"), jsonRaw("{}"), fmt.Sprintf("bench user %d", turnIdx), turnID, int64(1), requestCreated); err != nil {
					return SeedCatalog{}, err
				}
				messageGlobalIdx++
				assistantSource := uniqueName("bench-msg", cfg.Seed.Marker, messageGlobalIdx+1)
				if err := messageBatch.add(assistantID, botID, sessionID, nil, nil, assistantSource, nil, "assistant", assistantContentWithToolCalls(fmt.Sprintf("bench assistant %d", turnIdx), toolCalls), jsonRaw("{}"), jsonRaw("{}"), fmt.Sprintf("bench assistant %d", turnIdx), turnID, int64(2), assistantCreated); err != nil {
					return SeedCatalog{}, err
				}
				messageGlobalIdx++
				if err := turnBatch.add(turnID, botID, sessionID, int64(turnIdx+1), requestID, assistantID, turnCreated, turnCreated); err != nil {
					return SeedCatalog{}, err
				}

				sessionSeed.LatestTurnID = turnID
				sessionSeed.LatestMessageID = assistantID
				if sessionSeed.ExternalMessageID == "" {
					sessionSeed.ExternalMessageID = requestSource
				}
				if cursorTargets[turnIdx] {
					sessionSeed.CursorMessageIDs = append(sessionSeed.CursorMessageIDs, requestID)
					sessionSeed.CursorCreatedAts = append(sessionSeed.CursorCreatedAts, requestCreated)
					sessionSeed.ExternalMessageIDs = append(sessionSeed.ExternalMessageIDs, requestSource)
				}
				if turnIdx >= pageStart {
					sessionSeed.PageMessageIDs = append(sessionSeed.PageMessageIDs, requestID, assistantID)
					for _, call := range toolCalls {
						if strings.TrimSpace(call.id) != "" {
							sessionSeed.PageToolCallIDs = append(sessionSeed.PageToolCallIDs, call.id)
						}
					}
				}
				if cfg.Seed.AssetEveryNMessages > 0 && messageGlobalIdx%cfg.Seed.AssetEveryNMessages == 0 {
					assetRows = append(assetRows, assetSeedRow{
						id:          uuid.New(),
						messageID:   requestID,
						role:        "attachment",
						ordinal:     0,
						contentHash: uniqueName("hash", cfg.Seed.Marker, messageGlobalIdx),
						name:        fmt.Sprintf("asset-%d.txt", messageGlobalIdx),
						metadata:    markerJSON,
						createdAt:   requestCreated,
					})
				}

				if cfg.Seed.ApprovalEveryNTurns > 0 && turnGlobalIdx%cfg.Seed.ApprovalEveryNTurns == 0 {
					approvalGlobalIdx++
					approvalSeq++
					shortID := approvalSeq
					status, decidedAt := requestStatus(cfg.Seed.PendingRatio, approvalGlobalIdx, "approved")
					requestIDForApproval := uuid.New()
					promptExternalID := uniqueName("bench-approval-prompt", cfg.Seed.Marker, approvalGlobalIdx)
					if status == "pending" {
						sessionSeed.ApprovalRequestID = requestIDForApproval
						sessionSeed.ApprovalShortID = shortID
						sessionSeed.ApprovalPromptID = promptExternalID
					}
					values := approvalInsertValues(approvalShape, requestIDForApproval, botID, sessionID, routeID, identityID, uniqueName("tool-call", cfg.Seed.Marker, turnGlobalIdx), "write", "write", jsonRaw(`{"path":"/tmp/bench.txt"}`), shortID, status, "", identityID, requestID, assistantID, promptExternalID, "local", "", "group", turnCreated, timestampOrNil(decidedAt))
					if err := approvalBatch.add(values...); err != nil {
						return SeedCatalog{}, err
					}
				}
				if cfg.Seed.UserInputEveryNTurns > 0 && turnGlobalIdx%cfg.Seed.UserInputEveryNTurns == 0 {
					userInputGlobalIdx++
					userInputSeq++
					shortID := userInputSeq
					status, respondedAt := requestStatus(cfg.Seed.PendingRatio, userInputGlobalIdx, "submitted")
					requestIDForInput := uuid.New()
					promptExternalID := uniqueName("bench-user-input-prompt", cfg.Seed.Marker, userInputGlobalIdx)
					if status == "pending" {
						sessionSeed.UserInputRequestID = requestIDForInput
						sessionSeed.UserInputShortID = shortID
						sessionSeed.UserInputPromptID = promptExternalID
					}
					values := userInputInsertValues(userInputShape, requestIDForInput, botID, sessionID, routeID, identityID, uniqueName("ask-user", cfg.Seed.Marker, turnGlobalIdx), "ask_user", shortID, status, jsonRaw(`{"question":"bench?"}`), jsonRaw(`{"type":"text"}`), jsonRaw(`{}`), jsonRaw(`{}`), identityID, uuid.Nil, assistantID, uuid.Nil, assistantID, promptExternalID, "local", "", "group", nil, turnCreated, timestampOrNil(respondedAt), nil)
					if err := userInputBatch.add(values...); err != nil {
						return SeedCatalog{}, err
					}
				}
			}

			for _, batch := range []*copyBatcher{messageBatch, turnBatch, approvalBatch, userInputBatch} {
				if err := batch.flush(); err != nil {
					return SeedCatalog{}, err
				}
			}
			for _, row := range assetRows {
				if err := assetBatch.add(row.id, row.messageID, row.role, row.ordinal, row.contentHash, row.name, row.metadata, row.createdAt); err != nil {
					return SeedCatalog{}, err
				}
			}
			if err := assetBatch.flush(); err != nil {
				return SeedCatalog{}, err
			}
			if len(sessionSeed.ExternalMessageIDs) == 0 && sessionSeed.ExternalMessageID != "" {
				sessionSeed.ExternalMessageIDs = []string{sessionSeed.ExternalMessageID}
			}
			catalog.Sessions = append(catalog.Sessions, sessionSeed)
			if isHotSession(cfg.Seed, sessIdx) {
				catalog.HotSessions = append(catalog.HotSessions, len(catalog.Sessions)-1)
			} else {
				catalog.ColdSessions = append(catalog.ColdSessions, len(catalog.Sessions)-1)
			}
		}
	}

	for _, batch := range []*copyBatcher{
		userBatch,
		identityBatch,
		botBatch,
		routeBatch,
		sessionBatch,
		messageBatch,
		turnBatch,
		approvalBatch,
		userInputBatch,
		assetBatch,
	} {
		if err := batch.flush(); err != nil {
			return SeedCatalog{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return SeedCatalog{}, err
	}
	if err := analyzeBenchmarkTables(ctx, pool); err != nil {
		return SeedCatalog{}, fmt.Errorf("analyze seeded tables: %w", err)
	}
	actual, err := actualSeedEstimate(ctx, pool, cfg.Seed.Marker)
	if err != nil {
		return SeedCatalog{}, fmt.Errorf("count seeded rows: %w", err)
	}
	catalog.Estimate = actual
	return catalog, nil
}

func assistantContentWithToolCalls(text string, calls []pendingToolCallSeed) json.RawMessage {
	parts := make([]map[string]any, 0, 1+len(calls))
	parts = append(parts, map[string]any{
		"type": "text",
		"text": text,
	})
	for _, call := range calls {
		if strings.TrimSpace(call.id) == "" {
			continue
		}
		input := any(nil)
		if len(call.input) > 0 {
			input = call.input
		}
		parts = append(parts, map[string]any{
			"type":       "tool-call",
			"toolCallId": call.id,
			"toolName":   call.name,
			"input":      input,
		})
	}
	return jsonText(map[string]any{
		"role":    "assistant",
		"content": parts,
	})
}

func loadSeedCatalog(ctx context.Context, pool *pgxpool.Pool, cfg Config) (SeedCatalog, error) {
	rows, err := pool.Query(ctx, `
WITH benchmark_sessions AS (
  SELECT s.*, b.owner_user_id
  FROM bot_sessions s
  JOIN bots b ON b.id = s.bot_id
  WHERE b.metadata->>'benchmark_marker' = $1
    AND s.metadata->>'benchmark_marker' = $1
)
SELECT
  s.bot_id,
  s.owner_user_id,
  s.route_id,
  s.id,
  COALESCE(latest_turn.id, '00000000-0000-0000-0000-000000000000'::uuid) AS latest_turn_id,
  COALESCE(latest_message.id, '00000000-0000-0000-0000-000000000000'::uuid) AS latest_message_id,
  COALESCE(page.page_message_ids, ARRAY[]::uuid[]) AS page_message_ids,
  COALESCE(page_tools.page_tool_call_ids, ARRAY[]::text[]) AS page_tool_call_ids,
  COALESCE(cursors.cursor_message_ids, ARRAY[]::uuid[]) AS cursor_message_ids,
  COALESCE(cursors.cursor_created_ats, ARRAY[]::timestamptz[]) AS cursor_created_ats,
  COALESCE(cursors.external_message_ids, ARRAY[]::text[]) AS external_message_ids,
  COALESCE(approval.id, '00000000-0000-0000-0000-000000000000'::uuid) AS approval_request_id,
  COALESCE(approval.short_id, 0) AS approval_short_id,
  COALESCE(approval.prompt_external_message_id, '') AS approval_prompt_external_id,
  COALESCE(user_input.id, '00000000-0000-0000-0000-000000000000'::uuid) AS user_input_request_id,
  COALESCE(user_input.short_id, 0) AS user_input_short_id,
  COALESCE(user_input.prompt_external_message_id, '') AS user_input_prompt_external_id,
  COALESCE((s.metadata->>'hot')::boolean, false) AS hot
FROM benchmark_sessions s
LEFT JOIN LATERAL (
  SELECT t.id, t.position
  FROM bot_history_turns t
  WHERE t.session_id = s.id AND t.superseded_at IS NULL
  ORDER BY t.position DESC
  LIMIT 1
) latest_turn ON true
LEFT JOIN LATERAL (
  SELECT m.id
  FROM bot_history_messages m
  WHERE m.turn_id = latest_turn.id
  ORDER BY m.turn_message_seq DESC, m.created_at DESC, m.id DESC
  LIMIT 1
) latest_message ON true
LEFT JOIN LATERAL (
  SELECT array_agg(pm.id ORDER BY pm.position ASC, pm.turn_message_seq ASC, pm.created_at ASC, pm.id ASC) AS page_message_ids
  FROM (
    SELECT t.position, m.turn_message_seq, m.created_at, m.id
    FROM (
      SELECT t.id, t.position
      FROM bot_history_turns t
      WHERE t.session_id = s.id AND t.superseded_at IS NULL
      ORDER BY t.position DESC
      LIMIT $2::int
    ) t
    JOIN bot_history_messages m ON m.turn_id = t.id
    ORDER BY t.position DESC, m.turn_message_seq DESC, m.created_at DESC, m.id DESC
    LIMIT $2::int
  ) pm
) page ON true
LEFT JOIN LATERAL (
  SELECT array_agg(page_tool_calls.tool_call_id ORDER BY page_tool_calls.tool_call_id) AS page_tool_call_ids
  FROM (
    SELECT DISTINCT tool_call.value->>'toolCallId' AS tool_call_id
    FROM (
      SELECT t.position, m.turn_message_seq, m.created_at, m.id, m.content
      FROM (
        SELECT t.id, t.position
        FROM bot_history_turns t
        WHERE t.session_id = s.id AND t.superseded_at IS NULL
        ORDER BY t.position DESC
        LIMIT $2::int
      ) t
      JOIN bot_history_messages m ON m.turn_id = t.id
      ORDER BY t.position DESC, m.turn_message_seq DESC, m.created_at DESC, m.id DESC
      LIMIT $2::int
    ) pm
    CROSS JOIN LATERAL jsonb_array_elements(
      CASE
        WHEN jsonb_typeof(pm.content->'content') = 'array' THEN pm.content->'content'
        ELSE '[]'::jsonb
      END
    ) AS tool_call(value)
    WHERE tool_call.value->>'type' = 'tool-call'
      AND COALESCE(tool_call.value->>'toolCallId', '') <> ''
  ) AS page_tool_calls
) page_tools ON true
LEFT JOIN LATERAL (
  SELECT
    array_agg(cm.id ORDER BY cm.ord) AS cursor_message_ids,
    array_agg(cm.created_at ORDER BY cm.ord) AS cursor_created_ats,
    array_agg(cm.source_message_id ORDER BY cm.ord) FILTER (WHERE cm.source_message_id IS NOT NULL) AS external_message_ids
  FROM (
    SELECT target.ord, m.id, m.created_at, m.source_message_id
    FROM (
      VALUES
        (1, 1::bigint),
        (2, GREATEST(COALESCE(latest_turn.position, 1) / 2, 1)),
        (3, GREATEST(COALESCE(latest_turn.position, 1) - 5, 1))
    ) AS target(ord, target_position)
    CROSS JOIN LATERAL (
      SELECT m.id, m.created_at, m.source_message_id
      FROM bot_history_turns t
      JOIN bot_history_messages m ON m.turn_id = t.id
      WHERE t.session_id = s.id
        AND t.superseded_at IS NULL
        AND t.position >= target.target_position
      ORDER BY t.position ASC, m.turn_message_seq ASC, m.created_at ASC, m.id ASC
      LIMIT 1
    ) m
  ) cm
) cursors ON true
LEFT JOIN LATERAL (
  SELECT id, short_id, prompt_external_message_id
  FROM tool_approval_requests tar
  WHERE tar.session_id = s.id AND tar.status = 'pending'
  ORDER BY tar.created_at DESC, tar.short_id DESC
  LIMIT 1
) approval ON true
LEFT JOIN LATERAL (
  SELECT id, short_id, prompt_external_message_id
  FROM user_input_requests uir
  WHERE uir.session_id = s.id AND uir.status = 'pending'
    AND (uir.expires_at IS NULL OR uir.expires_at > now())
  ORDER BY uir.created_at DESC, uir.short_id DESC
  LIMIT 1
) user_input ON true
ORDER BY s.created_at ASC, s.id ASC`, cfg.Seed.Marker, cfg.Workload.PageSize)
	if err != nil {
		return SeedCatalog{}, err
	}
	defer rows.Close()
	catalog := SeedCatalog{Marker: cfg.Seed.Marker}
	botSeen := map[uuid.UUID]bool{}
	for rows.Next() {
		var s SessionSeed
		var hot bool
		if err := rows.Scan(&s.BotID, &s.OwnerUserID, &s.RouteID, &s.SessionID, &s.LatestTurnID, &s.LatestMessageID, &s.PageMessageIDs, &s.PageToolCallIDs, &s.CursorMessageIDs, &s.CursorCreatedAts, &s.ExternalMessageIDs, &s.ApprovalRequestID, &s.ApprovalShortID, &s.ApprovalPromptID, &s.UserInputRequestID, &s.UserInputShortID, &s.UserInputPromptID, &hot); err != nil {
			return SeedCatalog{}, err
		}
		if len(s.ExternalMessageIDs) > 0 {
			s.ExternalMessageID = s.ExternalMessageIDs[0]
		}
		if !botSeen[s.BotID] {
			botSeen[s.BotID] = true
			catalog.BotIDs = append(catalog.BotIDs, s.BotID)
		}
		idx := len(catalog.Sessions)
		catalog.Sessions = append(catalog.Sessions, s)
		if hot {
			catalog.HotSessions = append(catalog.HotSessions, idx)
		} else {
			catalog.ColdSessions = append(catalog.ColdSessions, idx)
		}
	}
	if err := rows.Err(); err != nil {
		return SeedCatalog{}, err
	}
	if len(catalog.Sessions) == 0 {
		return SeedCatalog{}, fmt.Errorf("no benchmark sessions found for marker %q; run -mode seed or -mode seed-run first", cfg.Seed.Marker)
	}
	actual, err := actualSeedEstimate(ctx, pool, cfg.Seed.Marker)
	if err != nil {
		return SeedCatalog{}, fmt.Errorf("count benchmark rows: %w", err)
	}
	catalog.Estimate = actual
	return catalog, nil
}

func cursorTurnIndexes(turnCount int) map[int]bool {
	indexes := map[int]bool{}
	if turnCount <= 0 {
		return indexes
	}
	for _, idx := range []int{0, turnCount / 2, max(turnCount-3, 0)} {
		if idx >= 0 && idx < turnCount {
			indexes[idx] = true
		}
	}
	return indexes
}

func isHotSession(seed SeedConfig, sessionIndexWithinBot int) bool {
	hotCount := int(math.Ceil(float64(seed.SessionsPerBot) * seed.HotSessionRatio))
	if hotCount <= 0 {
		hotCount = 1
	}
	return sessionIndexWithinBot < hotCount
}

func requestStatus(pendingRatio float64, ordinal int, doneStatus string) (string, time.Time) {
	if pendingRatio >= 1 {
		return "pending", time.Time{}
	}
	if pendingRatio <= 0 {
		return doneStatus, time.Now().UTC()
	}
	period := int(math.Round(1 / pendingRatio))
	if period <= 1 || ordinal%period == 0 {
		return "pending", time.Time{}
	}
	return doneStatus, time.Now().UTC()
}

func jsonText(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return json.RawMessage(b)
}

func jsonRaw(raw string) json.RawMessage {
	return json.RawMessage(raw)
}

func nilUUID(id uuid.UUID) any {
	if id == uuid.Nil {
		return nil
	}
	return id
}

func uniqueName(prefix, marker string, n int) string {
	cleanMarker := strings.ToLower(marker)
	cleanMarker = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		return '-'
	}, cleanMarker)
	cleanMarker = strings.Trim(cleanMarker, "-")
	if cleanMarker == "" {
		cleanMarker = "local"
	}
	return fmt.Sprintf("%s-%s-%d", prefix, cleanMarker, n)
}

func uniqueBotName(marker string, n int) string {
	name := uniqueName("bench-bot", marker, n)
	if len(name) > 63 {
		name = name[:63]
	}
	name = strings.TrimRight(name, "-")
	if len(name) < 2 {
		return fmt.Sprintf("bench-bot-%d", n)
	}
	return name
}

func uniqueEmail(marker string, n int) string {
	return fmt.Sprintf("%s@example.invalid", uniqueName("bench-user", marker, n))
}

func loadApprovalInsertShape(ctx context.Context, pool *pgxpool.Pool) (approvalInsertShape, error) {
	columns, err := tableColumns(ctx, pool, "tool_approval_requests")
	if err != nil {
		return approvalInsertShape{}, err
	}
	shape := approvalInsertShape{
		hasOperation:        columns["operation"],
		hasRequestedMessage: columns["requested_message_id"],
		hasPromptMessage:    columns["prompt_message_id"],
		hasPromptExternal:   columns["prompt_external_message_id"],
	}
	shape.columns = []string{"id", "bot_id", "session_id", "route_id", "channel_identity_id", "tool_call_id", "tool_name"}
	if shape.hasOperation {
		shape.columns = append(shape.columns, "operation")
	}
	shape.columns = append(shape.columns, "tool_input", "short_id", "status", "decision_reason", "requested_by_channel_identity_id")
	if shape.hasRequestedMessage {
		shape.columns = append(shape.columns, "requested_message_id")
	}
	if shape.hasPromptMessage {
		shape.columns = append(shape.columns, "prompt_message_id")
	}
	if shape.hasPromptExternal {
		shape.columns = append(shape.columns, "prompt_external_message_id")
	}
	shape.columns = append(shape.columns, "source_platform", "reply_target", "conversation_type", "created_at", "decided_at")
	return shape, nil
}

func loadUserInputInsertShape(ctx context.Context, pool *pgxpool.Pool) (userInputInsertShape, error) {
	columns, err := tableColumns(ctx, pool, "user_input_requests")
	if err != nil {
		return userInputInsertShape{}, err
	}
	shape := userInputInsertShape{
		hasPromptMessage:  columns["prompt_message_id"],
		hasPromptExternal: columns["prompt_external_message_id"],
	}
	shape.columns = []string{
		"id", "bot_id", "session_id", "route_id", "channel_identity_id",
		"tool_call_id", "tool_name", "short_id", "status", "input_json",
		"ui_payload_json", "result_json", "provider_metadata",
		"requested_by_channel_identity_id", "responded_by_channel_identity_id",
		"assistant_message_id", "tool_result_message_id",
	}
	if shape.hasPromptMessage {
		shape.columns = append(shape.columns, "prompt_message_id")
	}
	if shape.hasPromptExternal {
		shape.columns = append(shape.columns, "prompt_external_message_id")
	}
	shape.columns = append(shape.columns, "source_platform", "reply_target", "conversation_type", "expires_at", "created_at", "updated_at", "responded_at", "canceled_at")
	return shape, nil
}

func tableColumns(ctx context.Context, pool *pgxpool.Pool, table string) (map[string]bool, error) {
	rows, err := pool.Query(ctx, `
SELECT column_name
FROM information_schema.columns
WHERE table_schema = current_schema()
  AND table_name = $1`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(columns) == 0 {
		return nil, fmt.Errorf("table %s was not found in current schema", table)
	}
	return columns, nil
}

func approvalInsertValues(shape approvalInsertShape, id, botID, sessionID, routeID, identityID uuid.UUID, toolCallID, toolName, operation string, toolInput json.RawMessage, shortID int32, status, decisionReason string, requestedByID, requestedMessageID, promptMessageID uuid.UUID, promptExternalID, sourcePlatform, replyTarget, conversationType string, createdAt time.Time, decidedAt any) []any {
	values := []any{id, botID, sessionID, routeID, identityID, toolCallID, toolName}
	if shape.hasOperation {
		values = append(values, operation)
	}
	values = append(values, toolInput, shortID, status, decisionReason, requestedByID)
	if shape.hasRequestedMessage {
		values = append(values, nilUUID(requestedMessageID))
	}
	if shape.hasPromptMessage {
		values = append(values, nilUUID(promptMessageID))
	}
	if shape.hasPromptExternal {
		values = append(values, promptExternalID)
	}
	values = append(values, sourcePlatform, replyTarget, conversationType, createdAt, decidedAt)
	return values
}

func userInputInsertValues(shape userInputInsertShape, id, botID, sessionID, routeID, identityID uuid.UUID, toolCallID, toolName string, shortID int32, status string, inputJSON, uiPayloadJSON, resultJSON, providerMetadata json.RawMessage, requestedByID, respondedByID, assistantMessageID, toolResultMessageID, promptMessageID uuid.UUID, promptExternalID, sourcePlatform, replyTarget, conversationType string, expiresAt any, createdAt time.Time, respondedAt any, canceledAt any) []any {
	values := []any{
		id, botID, sessionID, routeID, identityID, toolCallID, toolName,
		shortID, status, inputJSON, uiPayloadJSON, resultJSON, providerMetadata,
		requestedByID, nilUUID(respondedByID), nilUUID(assistantMessageID), nilUUID(toolResultMessageID),
	}
	if shape.hasPromptMessage {
		values = append(values, nilUUID(promptMessageID))
	}
	if shape.hasPromptExternal {
		values = append(values, promptExternalID)
	}
	values = append(values, sourcePlatform, replyTarget, conversationType, expiresAt, createdAt, createdAt, respondedAt, canceledAt)
	return values
}

func actualSeedEstimate(ctx context.Context, pool *pgxpool.Pool, marker string) (SeedEstimate, error) {
	var estimate SeedEstimate
	err := pool.QueryRow(ctx, `
WITH bench_bots AS (
  SELECT id FROM bots WHERE metadata->>'benchmark_marker' = $1
),
bench_sessions AS (
  SELECT id, bot_id
  FROM bot_sessions
  WHERE metadata->>'benchmark_marker' = $1
)
SELECT
  (SELECT COUNT(*) FROM bench_bots),
  (SELECT COUNT(*) FROM bench_sessions),
  (SELECT COUNT(*) FROM bot_history_turns WHERE session_id IN (SELECT id FROM bench_sessions)),
  (SELECT COUNT(*) FROM bot_history_messages WHERE session_id IN (SELECT id FROM bench_sessions)),
  0,
  (SELECT COUNT(*) FROM tool_approval_requests WHERE session_id IN (SELECT id FROM bench_sessions)),
  (SELECT COUNT(*) FROM user_input_requests WHERE session_id IN (SELECT id FROM bench_sessions)),
  (SELECT COUNT(*)
   FROM bot_history_message_assets a
   JOIN bot_history_messages m ON m.id = a.message_id
   WHERE m.session_id IN (SELECT id FROM bench_sessions))`, marker).Scan(
		&estimate.Bots,
		&estimate.Sessions,
		&estimate.Turns,
		&estimate.Messages,
		&estimate.Heads,
		&estimate.Approvals,
		&estimate.UserInputs,
		&estimate.Assets,
	)
	if err != nil {
		return SeedEstimate{}, err
	}
	return estimate, nil
}

func benchmarkResidualRows(ctx context.Context, pool *pgxpool.Pool, marker string) (SeedEstimate, error) {
	return actualSeedEstimate(ctx, pool, marker)
}
