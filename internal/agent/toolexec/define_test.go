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
	if _, err := tool.Execute(&ToolExecContext{Context: context.Background()}, sdk.ParseToolArguments(`{"task_id":5}`)); err == nil {
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
