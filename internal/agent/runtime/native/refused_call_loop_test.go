package native

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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

// Refused calls never reach a wrapped Execute, so the tool-loop guard does
// not see them; the loop bounds them itself, with or without loop detection:
// after maxRefusedBatches consecutive steps whose whole batch was refused, the
// run ends as a tool loop with those steps committed. A batch with one
// executable call in it resets the count.
func TestRefusedBatchesAreBounded(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		t.Run(map[bool]string{false: "generate", true: "stream"}[streaming], func(t *testing.T) {
			var calls atomic.Int32
			next := func(sdk.Request) (sdk.ModelResult, error) {
				n := calls.Add(1)
				toolCalls := []sdk.ToolCall{{ToolCallID: "c", ToolName: "no_such_tool", Input: toolexec.ArgumentsFromValue(map[string]any{"n": n})}}
				if n == 2 {
					// One executable call in the batch resets the count.
					toolCalls = append(toolCalls, sdk.ToolCall{ToolCallID: "ok", ToolName: "lookup", Input: toolexec.ArgumentsFromValue(map[string]any{})})
				}
				return sdk.ModelResult{FinishReason: sdk.FinishReasonToolCalls, ToolCalls: toolCalls}, nil
			}
			a := New(Deps{})
			a.SetToolProviders(mockToolLoopTools())
			var committed int
			cfg := RunConfig{
				Messages:         []sdk.Message{sdk.UserMessage("go")},
				SupportsToolCall: true,
				Identity:         SessionContext{BotID: "bot-1"},
				OnStepCommitted: func(context.Context, int, *step.Record) (StepDirective, error) {
					committed++
					return StepDirective{}, nil
				},
			}
			mock := &atomicMockProvider{handler: func(_ int, params sdk.Request) (sdk.ModelResult, error) { return next(params) }}
			mock.stream = func(_ context.Context, params sdk.Request) (<-chan sdk.StreamPart, error) {
				result, _ := next(params)
				parts := []sdk.StreamPart{}
				for _, call := range result.ToolCalls {
					parts = append(parts, &sdk.StreamToolCallPart{ToolCallID: call.ToolCallID, ToolName: call.ToolName, Input: call.Input})
				}
				parts = append(parts, &sdk.FinishStepPart{FinishReason: result.FinishReason})
				return closedAgentTestStream(parts...), nil
			}
			cfg.Model = &sdk.Model{ID: "mock", Provider: mock}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if streaming {
				var terminal StreamEvent
				for event := range a.Stream(ctx, cfg) {
					if event.IsTerminal() {
						terminal = event
					}
				}
				if terminal.Type != EventAgentAbort {
					t.Fatalf("terminal = %q (%s), want the loop abort", terminal.Type, terminal.Error)
				}
			} else if _, err := a.Generate(ctx, cfg); !errors.Is(err, ErrToolLoopDetected) {
				t.Fatalf("Generate error = %v, want ErrToolLoopDetected", err)
			}
			// Step 1 refused, step 2 mixed (reset), then maxRefusedBatches refused.
			if want := int32(2 + maxRefusedBatches); calls.Load() != want || committed != int(want) {
				t.Fatalf("calls=%d committed=%d, want %d of each", calls.Load(), committed, want)
			}
		})
	}
}
