package decisionruntime

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/memohai/memoh/internal/agent/application"
	"github.com/memohai/memoh/internal/agent/application/runprojection"
	sessionruntime "github.com/memohai/memoh/internal/agent/runtime/session"
)

func (a *NativeContinuationAdmission) consumeEvents(ctx context.Context, handle sessionruntime.RunHandle, eventCh <-chan application.StreamEventPayload, output chan<- application.StreamEventPayload, cancel context.CancelFunc, transport runprojection.Hooks) error {
	hooks := transport
	if hooks.OnDecodeError == nil {
		hooks.OnDecodeError = func(err error) error {
			return fmt.Errorf("decode decision stream event: %w", err)
		}
	}
	if output != nil {
		transportOnRaw := hooks.OnRaw
		outputEnabled := true
		hooks.OnRaw = func(rawCtx context.Context, raw json.RawMessage) {
			if transportOnRaw != nil {
				transportOnRaw(rawCtx, raw)
			}
			if !outputEnabled {
				return
			}
			select {
			case output <- raw:
			case <-rawCtx.Done():
				outputEnabled = false
			}
		}
	}
	return runprojection.Pump(ctx, a.manager, handle, eventCh, cancel, hooks)
}
