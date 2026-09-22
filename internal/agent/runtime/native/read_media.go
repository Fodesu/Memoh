package native

import (
	"fmt"
	"strings"
	"sync"

	sdk "github.com/felinics/twilight/sdk"

	contextfrag "github.com/felinics/memoh/internal/agent/context/fragment"
	agenttools "github.com/felinics/memoh/internal/agent/tool"
	"github.com/felinics/memoh/internal/models"
)

func decorateReadMediaTools(model *sdk.Model, tools []sdk.Tool) ([]sdk.Tool, *readMediaDecorationState) {
	if len(tools) == 0 {
		return tools, nil
	}
	state := &readMediaDecorationState{
		pendingMedia: make(map[string]sdk.MessagePart),
	}
	wrapped := decorateReadMediaToolsWithState(model, tools, state)
	if len(wrapped) == len(tools) && !readMediaToolPresent(tools) {
		return tools, nil
	}
	return wrapped, state
}

// readMediaToolPresent reports whether the set carries an executable read tool.
func readMediaToolPresent(tools []sdk.Tool) bool {
	for _, tool := range tools {
		if tool.Name == agenttools.ReadMediaToolName().String() && tool.Execute != nil {
			return true
		}
	}
	return false
}

// decorateReadMediaToolsWithState wraps the read tool over an existing state,
// which is how a capability refresh keeps the media captured so far while the
// tool set is rebuilt. A nil state or a set without the read tool returns the
// tools unchanged.
func decorateReadMediaToolsWithState(model *sdk.Model, tools []sdk.Tool, state *readMediaDecorationState) []sdk.Tool {
	if len(tools) == 0 || state == nil {
		return tools
	}
	clientType := models.ResolveClientType(model)
	wrapped := make([]sdk.Tool, 0, len(tools))
	found := false

	for _, tool := range tools {
		if tool.Name != agenttools.ReadMediaToolName().String() || tool.Execute == nil {
			wrapped = append(wrapped, tool)
			continue
		}

		found = true
		originalExecute := tool.Execute
		toolCopy := tool
		toolCopy.Execute = func(ctx *sdk.ToolExecContext, input any) (any, error) {
			output, err := originalExecute(ctx, input)
			if err != nil {
				return output, err
			}

			publicResult, media, ok := normalizeReadMediaOutput(output, clientType)
			if !ok {
				return output, nil
			}
			if ctx != nil && strings.TrimSpace(ctx.ToolCallID) != "" && mediaPartHasContent(media) {
				state.mu.Lock()
				if _, exists := state.pendingMedia[ctx.ToolCallID]; !exists {
					state.pendingOrder = append(state.pendingOrder, ctx.ToolCallID)
				}
				state.pendingMedia[ctx.ToolCallID] = media
				state.mu.Unlock()
			}
			return publicResult, nil
		}
		wrapped = append(wrapped, toolCopy)
	}

	if !found {
		return tools
	}
	return wrapped
}

type readMediaDecorationState struct {
	mu           sync.Mutex
	pendingOrder []string
	pendingMedia map[string]sdk.MessagePart
}

// takePendingParts drains the media captured by completed read_media calls, in
// call order, clearing the pending set.
func (s *readMediaDecorationState) takePendingParts() []sdk.MessagePart {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.pendingOrder) == 0 {
		return nil
	}
	parts := make([]sdk.MessagePart, 0, len(s.pendingOrder))
	for _, toolCallID := range s.pendingOrder {
		media, ok := s.pendingMedia[toolCallID]
		delete(s.pendingMedia, toolCallID)
		if !ok || !mediaPartHasContent(media) {
			continue
		}
		parts = append(parts, media)
	}
	s.pendingOrder = s.pendingOrder[:0]
	return parts
}

// drainReadMediaMessage converts the pending read-media parts into one user
// message appended to the thread at the current step boundary, tracked as a
// loop-owned dynamic input.
func drainReadMediaMessage(
	state *readMediaDecorationState,
	dynamic *loopDynamicInputs,
	ledger *contextfrag.MutationLedger,
	_ int,
	messages []sdk.Message,
) []sdk.Message {
	parts := state.takePendingParts()
	if len(parts) == 0 {
		return messages
	}
	ledger.Record(contextfrag.MutationReadMedia, fmt.Sprintf("images=%d", len(parts)))
	message := sdk.Message{
		Role:    sdk.MessageRoleUser,
		Content: parts,
	}
	dynamic.append(message, true, "", len(messages))
	return append(messages, message)
}

func normalizeReadMediaOutput(output any, clientType string) (any, sdk.MessagePart, bool) {
	switch value := output.(type) {
	case agenttools.ReadMediaToolOutput:
		return value.Public, buildReadMediaPart(clientType, value), true
	case *agenttools.ReadMediaToolOutput:
		if value == nil {
			return nil, nil, false
		}
		return value.Public, buildReadMediaPart(clientType, *value), true
	default:
		return nil, nil, false
	}
}

// buildReadMediaPart converts a read-media tool output into the message part
// injected on the next step: documents become FilePart (bare base64 by the
// twilight convention — provider framing is the adapters' job), images keep
// the legacy per-client data URL shaping.
func buildReadMediaPart(clientType string, value agenttools.ReadMediaToolOutput) sdk.MessagePart {
	if strings.TrimSpace(value.FileBase64) != "" {
		return sdk.FilePart{
			Data:      strings.TrimSpace(value.FileBase64),
			MediaType: strings.TrimSpace(value.FileMediaType),
			Filename:  strings.TrimSpace(value.Filename),
		}
	}
	return buildReadMediaImagePart(clientType, value.ImageBase64, value.ImageMediaType)
}

// mediaPartHasContent reports whether an injected media part carries a payload.
func mediaPartHasContent(part sdk.MessagePart) bool {
	switch p := part.(type) {
	case sdk.ImagePart:
		return strings.TrimSpace(p.Image) != ""
	case sdk.FilePart:
		return strings.TrimSpace(p.Data) != ""
	default:
		return false
	}
}

func publicReadMediaToolResult(output any) any {
	publicResult, _, ok := normalizeReadMediaOutput(output, "")
	if !ok {
		return output
	}
	return publicResult
}

func buildReadMediaImagePart(clientType, imageBase64, mediaType string) sdk.ImagePart {
	imageBase64 = strings.TrimSpace(imageBase64)
	mediaType = strings.TrimSpace(mediaType)
	if imageBase64 == "" {
		return sdk.ImagePart{}
	}
	if mediaType == "" {
		mediaType = "image/png"
	}

	image := imageBase64
	if clientType != string(models.ClientTypeAnthropicMessages) {
		image = fmt.Sprintf("data:%s;base64,%s", mediaType, imageBase64)
	}
	return sdk.ImagePart{
		Image:     image,
		MediaType: mediaType,
	}
}
