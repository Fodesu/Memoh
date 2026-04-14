package agent

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	sdk "github.com/memohai/twilight-ai/sdk"

	agenttools "github.com/memohai/memoh/internal/agent/tools"
)

type staticToolProvider struct {
	tools []sdk.Tool
}

func (p staticToolProvider) Tools(context.Context, agenttools.SessionContext) ([]sdk.Tool, error) {
	return p.tools, nil
}

type atomicMockProvider struct {
	calls   atomic.Int32
	handler func(call int, params sdk.GenerateParams) (*sdk.GenerateResult, error)
}

func (*atomicMockProvider) Name() string {
	return "mock"
}

func (*atomicMockProvider) ListModels(context.Context) ([]sdk.Model, error) {
	return nil, nil
}

func (*atomicMockProvider) Test(context.Context) *sdk.ProviderTestResult {
	return &sdk.ProviderTestResult{Status: sdk.ProviderStatusOK, Message: "ok"}
}

func (*atomicMockProvider) TestModel(context.Context, string) (*sdk.ModelTestResult, error) {
	return &sdk.ModelTestResult{Supported: true, Message: "supported"}, nil
}

func (m *atomicMockProvider) DoGenerate(_ context.Context, params sdk.GenerateParams) (*sdk.GenerateResult, error) {
	call := int(m.calls.Add(1))
	return m.handler(call, params)
}

func (m *atomicMockProvider) DoStream(ctx context.Context, params sdk.GenerateParams) (*sdk.StreamResult, error) {
	result, err := m.DoGenerate(ctx, params)
	if err != nil {
		return nil, err
	}
	ch := make(chan sdk.StreamPart, 8)
	go func() {
		defer close(ch)
		ch <- &sdk.StartPart{}
		ch <- &sdk.StartStepPart{}
		if result.Text != "" {
			ch <- &sdk.TextStartPart{ID: "mock"}
			ch <- &sdk.TextDeltaPart{ID: "mock", Text: result.Text}
			ch <- &sdk.TextEndPart{ID: "mock"}
		}
		for _, tc := range result.ToolCalls {
			ch <- &sdk.StreamToolCallPart{
				ToolCallID: tc.ToolCallID,
				ToolName:   tc.ToolName,
				Input:      tc.Input,
			}
		}
		ch <- &sdk.FinishStepPart{
			FinishReason: result.FinishReason,
			Usage:        result.Usage,
			Response:     result.Response,
		}
		ch <- &sdk.FinishPart{
			FinishReason: result.FinishReason,
			TotalUsage:   result.Usage,
		}
	}()
	return &sdk.StreamResult{Stream: ch}, nil
}

func TestAgentGenerateStopsOnToolLoopAbort(t *testing.T) {
	t.Parallel()

	modelProvider := &atomicMockProvider{
		handler: func(_ int, _ sdk.GenerateParams) (*sdk.GenerateResult, error) {
			return &sdk.GenerateResult{
				FinishReason: sdk.FinishReasonToolCalls,
				ToolCalls: []sdk.ToolCall{{
					ToolCallID: "call-same",
					ToolName:   "loop_tool",
					Input:      map[string]any{"query": "same"},
				}},
			}, nil
		},
	}

	a := New(Deps{})
	a.SetToolProviders([]agenttools.ToolProvider{
		staticToolProvider{
			tools: []sdk.Tool{{
				Name:       "loop_tool",
				Parameters: &jsonschema.Schema{Type: "object"},
				Execute: func(_ *sdk.ToolExecContext, _ any) (any, error) {
					return map[string]any{"ok": true}, nil
				},
			}},
		},
	})

	_, err := a.Generate(context.Background(), RunConfig{
		Model:            &sdk.Model{ID: "mock-model", Provider: modelProvider},
		Messages:         []sdk.Message{sdk.UserMessage("loop")},
		SupportsToolCall: true,
		Identity:         SessionContext{BotID: "bot-1"},
		LoopDetection:    LoopDetectionConfig{Enabled: true},
	})
	if !errors.Is(err, ErrToolLoopDetected) {
		t.Fatalf("expected ErrToolLoopDetected, got %v", err)
	}
	if modelProvider.calls.Load() >= 20 {
		t.Fatalf("expected tool loop to stop generation, got %d provider calls", modelProvider.calls.Load())
	}
}

func TestAgentGenerateStopsOnTextLoopAbort(t *testing.T) {
	t.Parallel()

	repeatedText := "abcdefghijklmnopqrstuvwxyz0123456789 repeated text chunk for loop detection"
	modelProvider := &atomicMockProvider{
		handler: func(call int, _ sdk.GenerateParams) (*sdk.GenerateResult, error) {
			return &sdk.GenerateResult{
				Text:         repeatedText,
				FinishReason: sdk.FinishReasonToolCalls,
				ToolCalls: []sdk.ToolCall{{
					ToolCallID: "call-text",
					ToolName:   "noop_tool",
					Input:      map[string]any{"step": call},
				}},
			}, nil
		},
	}

	a := New(Deps{})
	a.SetToolProviders([]agenttools.ToolProvider{
		staticToolProvider{
			tools: []sdk.Tool{{
				Name:       "noop_tool",
				Parameters: &jsonschema.Schema{Type: "object"},
				Execute: func(_ *sdk.ToolExecContext, _ any) (any, error) {
					return map[string]any{"ok": true}, nil
				},
			}},
		},
	})

	_, err := a.Generate(context.Background(), RunConfig{
		Model:            &sdk.Model{ID: "mock-model", Provider: modelProvider},
		Messages:         []sdk.Message{sdk.UserMessage("loop text")},
		SupportsToolCall: true,
		Identity:         SessionContext{BotID: "bot-1"},
		LoopDetection:    LoopDetectionConfig{Enabled: true},
	})
	if !errors.Is(err, ErrTextLoopDetected) {
		t.Fatalf("expected ErrTextLoopDetected, got %v", err)
	}
	if modelProvider.calls.Load() >= 10 {
		t.Fatalf("expected text loop to stop generation, got %d provider calls", modelProvider.calls.Load())
	}
}

func TestAgentGenerateStopsOnTerminalTextLoopAbort(t *testing.T) {
	t.Parallel()

	repeatedText := "abcdefghijklmnopqrstuvwxyz0123456789 repeated text chunk for loop detection"
	modelProvider := &atomicMockProvider{
		handler: func(call int, _ sdk.GenerateParams) (*sdk.GenerateResult, error) {
			finishReason := sdk.FinishReasonToolCalls
			var toolCalls []sdk.ToolCall
			if call < 4 {
				toolCalls = []sdk.ToolCall{{
					ToolCallID: "call-terminal",
					ToolName:   "noop_tool",
					Input:      map[string]any{"step": call},
				}}
			} else {
				finishReason = sdk.FinishReasonStop
			}
			return &sdk.GenerateResult{
				Text:         repeatedText,
				FinishReason: finishReason,
				ToolCalls:    toolCalls,
			}, nil
		},
	}

	a := New(Deps{})
	a.SetToolProviders([]agenttools.ToolProvider{
		staticToolProvider{
			tools: []sdk.Tool{{
				Name:       "noop_tool",
				Parameters: &jsonschema.Schema{Type: "object"},
				Execute: func(_ *sdk.ToolExecContext, _ any) (any, error) {
					return map[string]any{"ok": true}, nil
				},
			}},
		},
	})

	_, err := a.Generate(context.Background(), RunConfig{
		Model:            &sdk.Model{ID: "mock-model", Provider: modelProvider},
		Messages:         []sdk.Message{sdk.UserMessage("loop text terminal")},
		SupportsToolCall: true,
		Identity:         SessionContext{BotID: "bot-1"},
		LoopDetection:    LoopDetectionConfig{Enabled: true},
	})
	if !errors.Is(err, ErrTextLoopDetected) {
		t.Fatalf("expected ErrTextLoopDetected, got %v", err)
	}
	if modelProvider.calls.Load() != 4 {
		t.Fatalf("expected terminal text loop to abort on final step, got %d provider calls", modelProvider.calls.Load())
	}
}

func TestAgentStreamStopsOnToolLoopAbort(t *testing.T) {
	t.Parallel()

	modelProvider := &atomicMockProvider{
		handler: func(call int, _ sdk.GenerateParams) (*sdk.GenerateResult, error) {
			if call >= 20 {
				return &sdk.GenerateResult{
					Text:         "unexpected-final-step",
					FinishReason: sdk.FinishReasonStop,
				}, nil
			}
			return &sdk.GenerateResult{
				FinishReason: sdk.FinishReasonToolCalls,
				ToolCalls: []sdk.ToolCall{{
					ToolCallID: "call-stream",
					ToolName:   "loop_tool",
					Input:      map[string]any{"query": "same"},
				}},
			}, nil
		},
	}

	a := New(Deps{})
	a.SetToolProviders([]agenttools.ToolProvider{
		staticToolProvider{
			tools: []sdk.Tool{{
				Name:       "loop_tool",
				Parameters: &jsonschema.Schema{Type: "object"},
				Execute: func(_ *sdk.ToolExecContext, _ any) (any, error) {
					return map[string]any{"ok": true}, nil
				},
			}},
		},
	})

	var terminal StreamEvent
	for event := range a.Stream(context.Background(), RunConfig{
		Model:            &sdk.Model{ID: "mock-model", Provider: modelProvider},
		Messages:         []sdk.Message{sdk.UserMessage("loop stream")},
		SupportsToolCall: true,
		Identity:         SessionContext{BotID: "bot-1"},
		LoopDetection:    LoopDetectionConfig{Enabled: true},
	}) {
		if event.IsTerminal() {
			terminal = event
		}
	}

	if terminal.Type != EventAgentAbort {
		t.Fatalf("expected EventAgentAbort, got %q", terminal.Type)
	}
	if modelProvider.calls.Load() >= 20 {
		t.Fatalf("expected stream tool loop to abort before fallback stop, got %d provider calls", modelProvider.calls.Load())
	}
}
