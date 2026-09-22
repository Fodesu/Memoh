package native

import (
	"context"
	"sync/atomic"
	"testing"

	sdk "github.com/felinics/twilight/sdk"
	"github.com/google/jsonschema-go/jsonschema"

	"github.com/felinics/memoh/internal/agent/step"
	agenttools "github.com/felinics/memoh/internal/agent/tool"
	"github.com/felinics/memoh/internal/agent/toolexec"
)

// deferredApprovalBatch drives one model step that emits an unguarded tool call
// followed by a call the approval handler defers. toolexec.ExecuteTools runs the
// pending batch before it returns at the deferral index, so the deferred step
// record must carry the completed result instead of dropping it.
func deferredApprovalBatch(t *testing.T) (*Agent, *atomicMockProvider, *atomic.Int32, *atomic.Int32) {
	t.Helper()

	provider := &atomicMockProvider{
		handler: func(int, sdk.Request) (sdk.ModelResult, error) {
			return sdk.ModelResult{
				FinishReason: sdk.FinishReasonToolCalls,
				ToolCalls: []sdk.ToolCall{
					{ToolCallID: "call-search", ToolName: "web_search", Input: toolexec.ArgumentsFromValue(map[string]any{"q": "one"})},
					{ToolCallID: "call-exec", ToolName: "exec", Input: toolexec.ArgumentsFromValue(map[string]any{"cmd": "ls"})},
				},
			}, nil
		},
	}

	var searchRuns, execRuns atomic.Int32
	a := New(Deps{})
	a.SetToolProviders([]agenttools.ToolProvider{
		staticToolProvider{
			tools: []toolexec.Tool{
				{
					Name:       "web_search",
					Parameters: &jsonschema.Schema{Type: "object"},
					Execute: toolexec.AdaptLegacyExecute(func(*toolexec.ToolExecContext, any) (any, error) {
						searchRuns.Add(1)
						return map[string]any{"hits": 3}, nil
					}),
				},
				{
					Name:       "exec",
					Parameters: &jsonschema.Schema{Type: "object"},
					Execute: toolexec.AdaptLegacyExecute(func(*toolexec.ToolExecContext, any) (any, error) {
						execRuns.Add(1)
						return map[string]any{"stdout": "ok"}, nil
					}),
				},
			},
		},
	})
	return a, provider, &searchRuns, &execRuns
}

func deferOnExec(_ context.Context, call sdk.ToolCall) (toolexec.ToolApprovalResult, error) {
	if call.ToolName != "exec" {
		return toolexec.ToolApprovalResult{Decision: toolexec.ToolApprovalDecisionApproved}, nil
	}
	return toolexec.ToolApprovalResult{
		Decision:   toolexec.ToolApprovalDecisionDeferred,
		ApprovalID: "approval-1",
		Metadata:   map[string]any{"tool_call_id": call.ToolCallID},
	}, nil
}

func assertDeferredStepKeepsFinishedResults(t *testing.T, record *step.Record, searchRuns, execRuns *atomic.Int32) {
	t.Helper()

	if record == nil {
		t.Fatal("no step record was committed")
	}
	if record.Deferred == nil {
		t.Fatal("record.Deferred = nil, want the parked approval")
	}
	if got := searchRuns.Load(); got != 1 {
		t.Fatalf("web_search executions = %d, want 1", got)
	}
	if got := execRuns.Load(); got != 0 {
		t.Fatalf("exec executions = %d, want 0 while the approval is parked", got)
	}

	if len(record.ToolResults) != 1 || record.ToolResults[0].ToolCallID != "call-search" {
		t.Fatalf("record.ToolResults = %#v, want the finished web_search result", record.ToolResults)
	}

	var results []sdk.ToolResultPart
	var calls []sdk.ToolCallPart
	for _, msg := range record.Messages {
		for _, part := range msg.Content {
			switch p := part.(type) {
			case sdk.ToolResultPart:
				results = append(results, p)
			case sdk.ToolCallPart:
				calls = append(calls, p)
			}
		}
	}
	if len(calls) != 2 {
		t.Fatalf("persisted tool calls = %d, want 2", len(calls))
	}
	// The finished call is answered; the deferred one stays dangling until the
	// approval resolves and the resume path appends its result.
	if len(results) != 1 || results[0].ToolCallID != "call-search" {
		t.Fatalf("persisted tool results = %#v, want only call-search", results)
	}
}

func TestAgentGenerateDeferredStepKeepsFinishedToolResults(t *testing.T) {
	t.Parallel()

	a, provider, searchRuns, execRuns := deferredApprovalBatch(t)
	var record *step.Record
	if _, err := a.Generate(context.Background(), RunConfig{
		Model:               &sdk.Model{ID: "mock-model", Provider: provider},
		Messages:            []sdk.Message{sdk.UserMessage("start")},
		SupportsToolCall:    true,
		Identity:            SessionContext{BotID: "bot-1"},
		ToolApprovalHandler: deferOnExec,
		OnStepCommitted: func(_ context.Context, _ int, sr *step.Record) (StepDirective, error) {
			record = sr
			return StepDirective{}, nil
		},
	}); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	assertDeferredStepKeepsFinishedResults(t, record, searchRuns, execRuns)
}

func TestAgentStreamDeferredStepKeepsFinishedToolResults(t *testing.T) {
	t.Parallel()

	a, provider, searchRuns, execRuns := deferredApprovalBatch(t)
	var record *step.Record
	for range a.Stream(context.Background(), RunConfig{
		Model:               &sdk.Model{ID: "mock-model", Provider: provider},
		Messages:            []sdk.Message{sdk.UserMessage("start")},
		SupportsToolCall:    true,
		Identity:            SessionContext{BotID: "bot-1"},
		ToolApprovalHandler: deferOnExec,
		OnStepCommitted: func(_ context.Context, _ int, sr *step.Record) (StepDirective, error) {
			record = sr
			return StepDirective{}, nil
		},
	}) {
	}
	assertDeferredStepKeepsFinishedResults(t, record, searchRuns, execRuns)
}
