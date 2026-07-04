package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	queryChatPageUI            = "chat_page_ui"
	queryLocateWindow          = "locate_window"
	queryApprovalResolve       = "approval_resolve"
	queryUserInputResolve      = "user_input_resolve"
	queryLatestPage            = "latest_page"
	queryBeforePage            = "before_page"
	queryAfterPage             = "after_page"
	queryExternalLookup        = "external_lookup"
	queryMessageAssets         = "message_assets"
	queryApprovalList          = "approval_list"
	queryApprovalToolCalls     = "approval_tool_calls"
	queryApprovalPendingList   = "approval_pending_list"
	queryApprovalLatest        = "approval_latest"
	queryApprovalShortID       = "approval_short_id"
	queryApprovalReplyMessage  = "approval_reply_message"
	queryUserInputList         = "user_input_list"
	queryUserInputToolCalls    = "user_input_tool_calls"
	queryUserInputPendingList  = "user_input_pending_list"
	queryUserInputLatest       = "user_input_latest"
	queryUserInputShortID      = "user_input_short_id"
	queryUserInputReplyMessage = "user_input_reply_message"
	queryWriteUserMessage      = "write_user_message"
	queryWriteAssistantMessage = "write_assistant_message"
	queryWriteTurnPair         = "write_turn_pair"
	queryWriteToolTail         = "write_tool_tail"
)

type QueryDefinition struct {
	Name       string
	SourceFile string
	SourceName string
	Args       []string
}

var queryDefinitions = []QueryDefinition{
	{Name: queryLatestPage, SourceFile: "db/postgres/queries/messages.sql", SourceName: "ListMessagesLatestBySession", Args: []string{"session_id", "max_count"}},
	{Name: queryBeforePage, SourceFile: "db/postgres/queries/messages.sql", SourceName: "ListMessagesBeforeMessageBySession", Args: []string{"session_id", "max_count", "before_message_id"}},
	{Name: queryAfterPage, SourceFile: "db/postgres/queries/messages.sql", SourceName: "ListMessagesAfterMessageBySession", Args: []string{"session_id", "max_count", "after_message_id"}},
	{Name: queryExternalLookup, SourceFile: "db/postgres/queries/messages.sql", SourceName: "GetMessageByExternalIDBySession", Args: []string{"session_id", "external_message_id"}},
	{Name: queryMessageAssets, SourceFile: "db/postgres/queries/media.sql", SourceName: "ListMessageAssetsBatch", Args: []string{"message_ids"}},
	{Name: queryApprovalList, SourceFile: "db/postgres/queries/tool_approval.sql", SourceName: "ListToolApprovalsBySession", Args: []string{"bot_id", "session_id"}},
	{Name: queryApprovalToolCalls, SourceFile: "db/postgres/queries/tool_approval.sql", SourceName: "ListToolApprovalsBySessionToolCalls", Args: []string{"bot_id", "session_id", "tool_call_ids"}},
	{Name: queryApprovalPendingList, SourceFile: "db/postgres/queries/tool_approval.sql", SourceName: "ListPendingToolApprovalsBySession", Args: []string{"bot_id", "session_id"}},
	{Name: queryApprovalLatest, SourceFile: "db/postgres/queries/tool_approval.sql", SourceName: "GetLatestPendingToolApprovalBySession", Args: []string{"bot_id", "session_id"}},
	{Name: queryApprovalShortID, SourceFile: "db/postgres/queries/tool_approval.sql", SourceName: "GetPendingToolApprovalBySessionShortID", Args: []string{"bot_id", "session_id", "short_id"}},
	{Name: queryApprovalReplyMessage, SourceFile: "db/postgres/queries/tool_approval.sql", SourceName: "GetPendingToolApprovalByReplyMessage", Args: []string{"bot_id", "session_id", "prompt_external_message_id"}},
	{Name: queryUserInputList, SourceFile: "db/postgres/queries/user_input.sql", SourceName: "ListUserInputsBySession", Args: []string{"bot_id", "session_id"}},
	{Name: queryUserInputToolCalls, SourceFile: "db/postgres/queries/user_input.sql", SourceName: "ListUserInputsBySessionToolCalls", Args: []string{"bot_id", "session_id", "tool_call_ids"}},
	{Name: queryUserInputPendingList, SourceFile: "db/postgres/queries/user_input.sql", SourceName: "ListPendingUserInputsBySession", Args: []string{"bot_id", "session_id"}},
	{Name: queryUserInputLatest, SourceFile: "db/postgres/queries/user_input.sql", SourceName: "GetLatestPendingUserInputBySession", Args: []string{"bot_id", "session_id"}},
	{Name: queryUserInputShortID, SourceFile: "db/postgres/queries/user_input.sql", SourceName: "GetPendingUserInputBySessionShortID", Args: []string{"bot_id", "session_id", "short_id"}},
	{Name: queryUserInputReplyMessage, SourceFile: "db/postgres/queries/user_input.sql", SourceName: "GetPendingUserInputByReplyMessage", Args: []string{"bot_id", "session_id", "prompt_external_message_id"}},
}

var (
	knownQueries   = queryNames(queryDefinitions)
	knownScenarios = append([]string{
		queryChatPageUI,
		queryLocateWindow,
		queryApprovalResolve,
		queryUserInputResolve,
		queryWriteUserMessage,
		queryWriteAssistantMessage,
		queryWriteTurnPair,
		queryWriteToolTail,
	}, knownQueries...)
)

type QuerySet map[string]string

type WeightedQuery struct {
	Name       string
	Cumulative int
}

func queryNames(defs []QueryDefinition) []string {
	names := make([]string, 0, len(defs))
	for _, def := range defs {
		names = append(names, def.Name)
	}
	return names
}

func queryDefinition(name string) (QueryDefinition, bool) {
	for _, def := range queryDefinitions {
		if def.Name == name {
			return def, true
		}
	}
	return QueryDefinition{}, false
}

func loadQueries(dir string) (QuerySet, error) {
	queries := make(QuerySet, len(knownQueries))
	for _, name := range knownQueries {
		path := filepath.Join(dir, name+".sql")
		// #nosec G304 -- benchmark SQL templates are intentionally loaded from a user-provided directory.
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read query %s: %w", name, err)
		}
		sql := strings.TrimSpace(string(raw))
		if sql == "" {
			return nil, fmt.Errorf("query %s is empty", name)
		}
		queries[name] = sql
	}
	return queries, nil
}

func isKnownQuery(name string) bool {
	for _, known := range knownScenarios {
		if name == known {
			return true
		}
	}
	return false
}

func isWriteScenario(name string) bool {
	switch name {
	case queryWriteUserMessage, queryWriteAssistantMessage, queryWriteTurnPair, queryWriteToolTail:
		return true
	default:
		return false
	}
}

func normalizeWeights(weights map[string]int) ([]WeightedQuery, error) {
	if len(weights) == 0 {
		return nil, errors.New("workload.query_weights must not be empty")
	}
	total := 0
	normalized := make([]WeightedQuery, 0, len(weights))
	for _, name := range knownScenarios {
		weight := weights[name]
		if weight <= 0 {
			continue
		}
		total += weight
		normalized = append(normalized, WeightedQuery{Name: name, Cumulative: total})
	}
	if total <= 0 {
		return nil, errors.New("workload.query_weights must include at least one positive known query")
	}
	for name, weight := range weights {
		if weight > 0 && !isKnownQuery(name) {
			return nil, fmt.Errorf("unknown weighted query %q", name)
		}
	}
	return normalized, nil
}

func pickWeightedQuery(weighted []WeightedQuery, n int) string {
	if len(weighted) == 0 {
		return queryLatestPage
	}
	total := weighted[len(weighted)-1].Cumulative
	if total <= 0 {
		return weighted[0].Name
	}
	slot := n % total
	for _, item := range weighted {
		if slot < item.Cumulative {
			return item.Name
		}
	}
	return weighted[len(weighted)-1].Name
}
