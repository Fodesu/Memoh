// Package channelqueue adapts channel queue controls to the application
// service without exposing queue storage types to channels or RPC.
package channelqueue

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/felinics/memoh/internal/agent/application"
	"github.com/felinics/memoh/internal/agent/runtime/session/queue"
	"github.com/felinics/memoh/internal/channel/inbound"
)

type Adapter struct{ service *application.Service }

func New(service *application.Service) *Adapter { return &Adapter{service: service} }

func (a *Adapter) EnqueueSteer(ctx context.Context, input inbound.QueueCommandInput) error {
	return a.enqueue(ctx, input, func(payload []byte) error {
		_, err := a.service.EnqueueSteer(ctx, input.BotID, input.SessionID, input.InvocationID, payload)
		return err
	})
}

func (a *Adapter) EnqueueFollowUp(ctx context.Context, input inbound.QueueCommandInput) error {
	return a.enqueue(ctx, input, func(payload []byte) error {
		_, err := a.service.EnqueueFollowUp(ctx, input.BotID, input.SessionID, input.InvocationID, payload)
		return err
	})
}

func (a *Adapter) enqueue(_ context.Context, input inbound.QueueCommandInput, admit func([]byte) error) error {
	if a == nil || a.service == nil {
		return inbound.NewQueueCommandError(inbound.QueueCommandCodeUnavailable)
	}
	if strings.TrimSpace(input.BotID) == "" || strings.TrimSpace(input.SessionID) == "" ||
		strings.TrimSpace(input.InvocationID) == "" || strings.TrimSpace(input.Text) == "" {
		return inbound.NewQueueCommandError(inbound.QueueCommandCodeInvalid)
	}
	payload, err := json.Marshal(map[string]string{"text": strings.TrimSpace(input.Text)})
	if err != nil {
		return inbound.NewQueueCommandError(inbound.QueueCommandCodeInvalid)
	}
	return mapAdmissionError(admit(payload))
}

func mapAdmissionError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, queue.ErrNoActiveRun):
		return inbound.NewQueueCommandError(inbound.QueueCommandCodeNoActiveRun)
	case errors.Is(err, queue.ErrInvocationConflict):
		return inbound.NewQueueCommandError(inbound.QueueCommandCodeConflict)
	case errors.Is(err, queue.ErrAdmissionOverloaded):
		return inbound.NewQueueCommandError(inbound.QueueCommandCodeOverloaded)
	case errors.Is(err, queue.ErrInvalidReference):
		return inbound.NewQueueCommandError(inbound.QueueCommandCodeInvalid)
	default:
		return err
	}
}

var _ inbound.QueueCommandHandler = (*Adapter)(nil)
