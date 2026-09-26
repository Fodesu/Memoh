package native

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	sdk "github.com/felinics/twilight/sdk"

	"github.com/felinics/memoh/internal/agent/step"
	"github.com/felinics/memoh/internal/agent/toolexec"
)

// A call to a tool the model was not offered is a tool step like any other:
// the executor answers it with an error result, the step commits with that
// result, and the loop continues so the model can correct itself. A step
// carrying an open call must never be committed as final, because a queued
// steer would resume the thread on a request every provider rejects.
func TestUnknownToolCallIsAnsweredAndTheLoopContinues(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		t.Run(map[bool]string{false: "generate", true: "stream"}[streaming], func(t *testing.T) {
			var calls atomic.Int32
			var sawError atomic.Bool
			next := func(params sdk.Request) (sdk.ModelResult, error) {
				switch calls.Add(1) {
				case 1:
					return sdk.ModelResult{FinishReason: sdk.FinishReasonToolCalls, ToolCalls: []sdk.ToolCall{{ToolCallID: "c1", ToolName: "no_such_tool", Input: toolexec.ArgumentsFromValue(map[string]any{})}}}, nil
				default:
					if result, ok := findToolResult(params.Messages, "no_such_tool"); ok && result.IsError && strings.Contains(result.Result.String(), "not found") {
						sawError.Store(true)
					}
					return sdk.ModelResult{FinishReason: sdk.FinishReasonStop, Text: "recovered"}, nil
				}
			}
			a := New(Deps{})
			a.SetToolProviders(mockToolLoopTools())
			var records []*step.Record
			cfg := RunConfig{
				Messages:         []sdk.Message{sdk.UserMessage("go")},
				SupportsToolCall: true,
				Identity:         SessionContext{BotID: "bot-1"},
				OnStepCommitted: func(_ context.Context, _ int, sr *step.Record) (StepDirective, error) {
					records = append(records, sr)
					return StepDirective{}, nil
				},
			}
			mock := &atomicMockProvider{handler: func(_ int, params sdk.Request) (sdk.ModelResult, error) { return next(params) }}
			mock.stream = func(_ context.Context, params sdk.Request) (<-chan sdk.StreamPart, error) {
				result, err := next(params)
				if err != nil {
					return nil, err
				}
				parts := []sdk.StreamPart{}
				for _, call := range result.ToolCalls {
					parts = append(parts, &sdk.StreamToolCallPart{ToolCallID: call.ToolCallID, ToolName: call.ToolName, Input: call.Input})
				}
				if result.Text != "" {
					parts = append(parts, &sdk.TextDeltaPart{Text: result.Text})
				}
				parts = append(parts, &sdk.FinishStepPart{FinishReason: result.FinishReason})
				return closedAgentTestStream(parts...), nil
			}
			cfg.Model = &sdk.Model{ID: "mock", Provider: mock}
			var toolCallEnds int
			if streaming {
				for event := range a.Stream(context.Background(), cfg) {
					if event.Type == EventToolCallEnd {
						toolCallEnds++
					}
					if event.Type == EventError {
						t.Fatalf("unexpected error event: %s", event.Error)
					}
				}
				if toolCallEnds != 1 {
					t.Fatalf("tool_call_end events = %d, want the refused call closed", toolCallEnds)
				}
			} else if _, err := a.Generate(context.Background(), cfg); err != nil {
				t.Fatalf("Generate: %v", err)
			}
			if calls.Load() != 2 || !sawError.Load() {
				t.Fatalf("calls=%d sawError=%v, want the error result fed back and a second call", calls.Load(), sawError.Load())
			}
			if len(records) != 2 || len(records[0].ToolResults) != 1 || !strings.Contains(records[0].ToolResults[0].Output.String(), "not found") {
				t.Fatalf("records = %#v, want a tool step with the error result then a final step", records)
			}
		})
	}
}
