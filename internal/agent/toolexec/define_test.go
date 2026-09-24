package toolexec

import (
	"context"
	"encoding/json"
	"testing"

	sdk "github.com/felinics/twilight/sdk"
)

type defineArgs struct {
	TaskID  string   `json:"task_id" jsonschema:"Background task ID"`
	Timeout *float64 `json:"timeout,omitempty" jsonschema:"Max seconds"`
	Tags    []string `json:"tags,omitempty"`
	Nested  []struct {
		Label string `json:"label"`
	} `json:"nested,omitempty"`
}

func TestDefineInfersTheHandWrittenSchemaShape(t *testing.T) {
	tool := Define("probe", "desc", func(_ *ToolExecContext, args defineArgs) (sdk.ToolOutput, error) {
		if args.Timeout == nil {
			return sdk.TextOutput(args.TaskID + ":absent"), nil
		}
		return sdk.TextOutput(args.TaskID), nil
	}, Range("timeout", 1, 600), Enum("task_id", "a", "b"))

	got, _ := json.Marshal(SchemaValue(tool.Parameters))
	want := `{"properties":{"nested":{"items":{"properties":{"label":{"type":"string"}},"required":["label"],"type":"object"},"type":"array"},"tags":{"items":{"type":"string"},"type":"array"},"task_id":{"description":"Background task ID","enum":["a","b"],"type":"string"},"timeout":{"description":"Max seconds","maximum":600,"minimum":1,"type":"number"}},"required":["task_id"],"type":"object"}`
	if string(got) != want {
		t.Fatalf("schema\n got  %s\n want %s", got, want)
	}

	out, err := tool.Execute(&ToolExecContext{Context: context.Background()}, sdk.ParseToolArguments(`{"task_id":"t1"}`))
	if err != nil || out.Text != "t1:absent" {
		t.Fatalf("execute = %#v, %v", out, err)
	}
	// A number for a string property is stringified, as the map helpers did.
	out, err = tool.Execute(&ToolExecContext{Context: context.Background()}, sdk.ParseToolArguments(`{"task_id":5}`))
	if err != nil || out.Text != "5:absent" {
		t.Fatalf("execute(number for string) = %#v, %v", out, err)
	}
	if _, err := tool.Execute(&ToolExecContext{Context: context.Background()}, sdk.ParseToolArguments(`{"task_id":{"x":1}}`)); err == nil {
		t.Fatal("a document that does not decode into the struct must fail")
	}
}

func TestDefineEmptyArgumentsKeepProperties(t *testing.T) {
	tool := Define("noargs", "desc", func(*ToolExecContext, struct{}) (sdk.ToolOutput, error) {
		return sdk.TextOutput("ok"), nil
	})
	got, _ := json.Marshal(SchemaValue(tool.Parameters))
	if string(got) != `{"properties":{},"type":"object"}` {
		t.Fatalf("schema = %s", got)
	}
}

type coercionArgs struct {
	Limit   int     `json:"limit"`
	Offset  *int    `json:"offset,omitempty"`
	Ratio   float64 `json:"ratio,omitempty"`
	Name    string  `json:"name"`
	Verbose bool    `json:"verbose,omitempty"`
	Nested  struct {
		Count int `json:"count"`
	} `json:"nested"`
	IDs []int `json:"ids,omitempty"`
}

// The map-based helpers accepted whole-number floats and numeric strings for
// integers and stringified numbers for strings; the typed decode keeps that.
func TestTypedCoercesLenientScalars(t *testing.T) {
	var got coercionArgs
	execute := Typed(func(_ *ToolExecContext, args coercionArgs) (sdk.ToolOutput, error) {
		got = args
		return sdk.ToolOutput{}, nil
	})
	input := sdk.ParseToolArguments(`{"limit": 50.0, "offset": " 2 ", "ratio": "0.5", "name": 123, "verbose": "true", "nested": {"count": 3.0}, "ids": [1.0, "2"]}`)
	if _, err := execute(&ToolExecContext{ToolName: "probe"}, input); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got.Limit != 50 || got.Offset == nil || *got.Offset != 2 || got.Ratio != 0.5 || got.Name != "123" || !got.Verbose || got.Nested.Count != 3 {
		t.Fatalf("decoded = %+v", got)
	}
	if len(got.IDs) != 2 || got.IDs[0] != 1 || got.IDs[1] != 2 {
		t.Fatalf("ids = %v", got.IDs)
	}
}

func TestTypedReportsDecodeErrorsByProperty(t *testing.T) {
	execute := Typed(func(*ToolExecContext, coercionArgs) (sdk.ToolOutput, error) { return sdk.ToolOutput{}, nil })
	_, err := execute(&ToolExecContext{ToolName: "probe"}, sdk.ParseToolArguments(`{"limit": 2.5}`))
	if err == nil || err.Error() != "invalid arguments for probe: limit must be an integer, got number 2.5" {
		t.Fatalf("error = %v", err)
	}
	_, err = execute(&ToolExecContext{ToolName: "probe"}, sdk.ParseToolArguments(`{"name": {"x": 1}}`))
	if err == nil || err.Error() != "invalid arguments for probe: name must be a string, got object" {
		t.Fatalf("error = %v", err)
	}
	// A nil context must not panic; the tool name falls back.
	_, err = execute(nil, sdk.ParseToolArguments(`[1]`))
	if err == nil || err.Error() != "invalid arguments for tool: arguments must be an object, got array" {
		t.Fatalf("nil ctx error = %v", err)
	}
	_, err = execute(&ToolExecContext{ToolName: "probe"}, sdk.ParseToolArguments(`not json`))
	if err == nil || err.Error() != "invalid arguments for probe: not a JSON object" {
		t.Fatalf("invalid text error = %v", err)
	}
}

type ownDecoder struct{ raw string }

func (d *ownDecoder) UnmarshalJSON(data []byte) error { d.raw = string(data); return nil }

// A field with its own UnmarshalJSON defines its own tolerance; coercion must
// not rewrite what it receives.
func TestTypedLeavesCustomDecodersAlone(t *testing.T) {
	type args struct {
		Value ownDecoder `json:"value"`
		Limit int        `json:"limit"`
	}
	var got args
	execute := Typed(func(_ *ToolExecContext, a args) (sdk.ToolOutput, error) { got = a; return sdk.ToolOutput{}, nil })
	if _, err := execute(&ToolExecContext{ToolName: "probe"}, sdk.ParseToolArguments(`{"value": 2.0, "limit": 2.0}`)); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got.Value.raw != "2.0" || got.Limit != 2 {
		t.Fatalf("decoded = %+v", got)
	}
}
