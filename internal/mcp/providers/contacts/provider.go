package contacts

import (
	"context"
	"log/slog"
	"strings"

	"github.com/memohai/memoh/internal/channel/route"
	mcpgw "github.com/memohai/memoh/internal/mcp"
)

const toolGetContacts = "get_contacts"

// Executor exposes get_contacts as an MCP tool.
type Executor struct {
	routeService route.Service
	logger       *slog.Logger
}

// NewExecutor creates a contacts tool executor.
func NewExecutor(log *slog.Logger, routeService route.Service) *Executor {
	if log == nil {
		log = slog.Default()
	}
	return &Executor{
		routeService: routeService,
		logger:       log.With(slog.String("provider", "contacts_tool")),
	}
}

func (p *Executor) ListTools(_ context.Context, _ mcpgw.ToolSessionContext) ([]mcpgw.ToolDescriptor, error) {
	if p.routeService == nil {
		return []mcpgw.ToolDescriptor{}, nil
	}
	return []mcpgw.ToolDescriptor{
		{
			Name:        toolGetContacts,
			Description: "List all known contacts and conversations for the current bot. Returns platform, conversation type, reply target, and metadata for each route.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"platform": map[string]any{
						"type":        "string",
						"description": "Filter by channel platform (e.g. telegram, feishu). Returns all platforms when omitted.",
					},
				},
				"required": []string{},
			},
		},
	}, nil
}

func (p *Executor) CallTool(ctx context.Context, session mcpgw.ToolSessionContext, toolName string, arguments map[string]any) (map[string]any, error) {
	if toolName != toolGetContacts {
		return nil, mcpgw.ErrToolNotFound
	}
	if p.routeService == nil {
		return mcpgw.BuildToolErrorResult("contacts service not available"), nil
	}

	botID := strings.TrimSpace(session.BotID)
	if botID == "" {
		return mcpgw.BuildToolErrorResult("bot_id is required"), nil
	}

	routes, err := p.routeService.List(ctx, botID)
	if err != nil {
		p.logger.Warn("list routes failed", slog.String("bot_id", botID), slog.Any("error", err))
		return mcpgw.BuildToolErrorResult(err.Error()), nil
	}

	platformFilter := strings.ToLower(strings.TrimSpace(mcpgw.FirstStringArg(arguments, "platform")))

	contacts := make([]map[string]any, 0, len(routes))
	for _, r := range routes {
		if platformFilter != "" && !strings.EqualFold(r.Platform, platformFilter) {
			continue
		}
		entry := map[string]any{
			"route_id":          r.ID,
			"platform":          r.Platform,
			"conversation_type": r.ConversationType,
			"target":            r.ReplyTarget,
			"conversation_id":   r.ConversationID,
			"last_active":       r.UpdatedAt.Format("2006-01-02T15:04:05Z"),
		}
		if len(r.Metadata) > 0 {
			entry["metadata"] = r.Metadata
		}
		contacts = append(contacts, entry)
	}

	payload := map[string]any{
		"ok":       true,
		"bot_id":   botID,
		"count":    len(contacts),
		"contacts": contacts,
	}
	return mcpgw.BuildToolSuccessResult(payload), nil
}
