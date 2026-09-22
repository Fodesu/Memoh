package messageconv

import (
	"bytes"
	"encoding/json"

	sdk "github.com/felinics/twilight/sdk"

	"github.com/felinics/memoh/internal/agent/partmeta"
)

// The SDK types a tool call's arguments as sdk.ToolArguments{json|text}, a
// tool's output as sdk.ToolOutput{text|json} and provider metadata as
// namespace → name → string. bot_history_messages predates those types: it
// holds the arguments object itself, the output value itself and Memoh's
// annotations as nested objects. The two rewrites below keep the stored shape
// exactly as it was, so existing rows replay and rows written now read back
// on either side of the change.

// storedPartsFromSDK rewrites the content array json.Marshal(sdk.Message)
// produced into the stored shape.
func storedPartsFromSDK(content json.RawMessage) json.RawMessage {
	return rewriteParts(content, func(part map[string]json.RawMessage, partType string) {
		switch partType {
		case "tool-call":
			if raw, ok := part["input"]; ok {
				var args sdk.ToolArguments
				if json.Unmarshal(raw, &args) == nil {
					part["input"] = storedArguments(args)
				}
			}
		case "tool-result":
			if raw, ok := part["result"]; ok {
				var output sdk.ToolOutput
				if json.Unmarshal(raw, &output) == nil {
					part["result"] = storedOutput(output)
				}
			}
		}
		if raw, ok := part["providerMetadata"]; ok {
			var meta sdk.ProviderMetadata
			if json.Unmarshal(raw, &meta) != nil {
				return
			}
			stored := partmeta.Unfold(meta)
			if len(stored) == 0 {
				delete(part, "providerMetadata")
				return
			}
			if encoded, err := json.Marshal(stored); err == nil {
				part["providerMetadata"] = encoded
			}
		}
	})
}

// sdkPartsFromStored rewrites a stored content array into the shape
// sdk.Message.UnmarshalJSON reads.
func sdkPartsFromStored(content json.RawMessage) json.RawMessage {
	return rewriteParts(content, func(part map[string]json.RawMessage, partType string) {
		switch partType {
		case "tool-call":
			if raw, ok := part["input"]; ok {
				if typed, ok := typedArguments(raw); ok {
					part["input"] = typed
				} else {
					delete(part, "input")
				}
			}
		case "tool-result":
			if raw, ok := part["result"]; ok {
				if typed, ok := typedOutput(raw); ok {
					part["result"] = typed
				} else {
					delete(part, "result")
				}
			}
		}
		if raw, ok := part["providerMetadata"]; ok {
			var stored map[string]any
			if json.Unmarshal(raw, &stored) != nil {
				delete(part, "providerMetadata")
				return
			}
			meta := partmeta.Fold(stored)
			if meta == nil {
				delete(part, "providerMetadata")
				return
			}
			if encoded, err := json.Marshal(meta); err == nil {
				part["providerMetadata"] = encoded
			}
		}
	})
}

// rewriteParts applies rewrite to every object in a content array. String
// content and anything that does not parse are returned unchanged.
func rewriteParts(content json.RawMessage, rewrite func(part map[string]json.RawMessage, partType string)) json.RawMessage {
	trimmed := bytes.TrimSpace(content)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return content
	}
	var parts []json.RawMessage
	if err := json.Unmarshal(trimmed, &parts); err != nil {
		return content
	}
	out := make([]json.RawMessage, 0, len(parts))
	for _, raw := range parts {
		var part map[string]json.RawMessage
		if err := json.Unmarshal(raw, &part); err != nil || part == nil {
			out = append(out, raw)
			continue
		}
		var partType string
		_ = json.Unmarshal(part["type"], &partType)
		rewrite(part, partType)
		encoded, err := json.Marshal(part)
		if err != nil {
			out = append(out, raw)
			continue
		}
		out = append(out, encoded)
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return content
	}
	return encoded
}

// storedArguments is the arguments as the row has always held them: the
// document itself, or the invalid text as a string.
func storedArguments(args sdk.ToolArguments) json.RawMessage {
	if !args.Valid() {
		encoded, _ := json.Marshal(args.Text)
		return encoded
	}
	return args.Object()
}

// typedArguments reads stored arguments: a string is the invalid text a
// provider kept verbatim, any other document is the arguments object.
func typedArguments(raw json.RawMessage) (json.RawMessage, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, false
	}
	var args sdk.ToolArguments
	if trimmed[0] == '"' {
		var text string
		if err := json.Unmarshal(trimmed, &text); err != nil {
			return nil, false
		}
		args = sdk.ParseToolArguments(text)
	} else {
		args = sdk.ToolArguments{JSON: trimmed}
	}
	encoded, err := json.Marshal(args)
	if err != nil {
		return nil, false
	}
	return encoded, true
}

// storedOutput is the output as the row has always held it: text as a
// string, a document as itself, and no output at all as null. The zero
// ToolOutput maps to null rather than "" so a row read back and written again
// keeps its bytes.
func storedOutput(output sdk.ToolOutput) json.RawMessage {
	if output.IsJSON() {
		return output.JSON
	}
	if output.Text == "" {
		return json.RawMessage("null")
	}
	encoded, _ := json.Marshal(output.Text)
	return encoded
}

// typedOutput reads a stored output: a string is text, anything else is a
// document.
func typedOutput(raw json.RawMessage) (json.RawMessage, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, false
	}
	var output sdk.ToolOutput
	if trimmed[0] == '"' {
		var text string
		if err := json.Unmarshal(trimmed, &text); err != nil {
			return nil, false
		}
		output = sdk.TextOutput(text)
	} else {
		output = sdk.RawJSONOutput(trimmed)
	}
	encoded, err := json.Marshal(output)
	if err != nil {
		return nil, false
	}
	return encoded, true
}
