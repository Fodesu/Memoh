package toolexec

import sdk "github.com/felinics/twilight/sdk"

// BuildStepMessages assembles the messages produced by one agent step: an
// assistant message carrying reasoning parts, text, and tool calls (with usage
// attached for output tracking), followed by a tool message when results are
// present.
//
// Reasoning blocks lead the assistant message, one part each, in provider
// emission order and are never filtered on empty text: a redacted thinking
// block carries its payload entirely in metadata.
func BuildStepMessages(text string, textMeta sdk.ProviderMetadata, reasoningParts []sdk.ReasoningPart, toolCalls []sdk.ToolCall, toolResults []sdk.ToolResultPart, usage *sdk.Usage) []sdk.Message {
	var assistantParts []sdk.MessagePart
	for i := range reasoningParts {
		assistantParts = append(assistantParts, reasoningParts[i])
	}
	if text != "" {
		assistantParts = append(assistantParts, sdk.TextPart{Text: text, ProviderMetadata: textMeta})
	}
	for _, tc := range toolCalls {
		assistantParts = append(assistantParts, sdk.ToolCallPart{
			ToolCallID:       tc.ToolCallID,
			ToolName:         tc.ToolName,
			Input:            tc.Input,
			ProviderMetadata: tc.ProviderMetadata,
		})
	}

	msgs := []sdk.Message{{Role: sdk.MessageRoleAssistant, Content: assistantParts, Usage: usage}}
	if len(toolResults) > 0 {
		msgs = append(msgs, sdk.ToolMessage(toolResults...))
	}
	return msgs
}
