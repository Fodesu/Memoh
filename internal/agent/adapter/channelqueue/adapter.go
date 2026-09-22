// Package channelqueue adapts channel queue controls to the application
// service without exposing queue storage types to channels or RPC.
package channelqueue

import (
	"context"
	"errors"
	"strings"

	"github.com/felinics/memoh/internal/agent/application"
	sessionruntime "github.com/felinics/memoh/internal/agent/runtime/session"
	"github.com/felinics/memoh/internal/channel/inbound"
)

type Adapter struct{ service *application.Service }

func New(service *application.Service) *Adapter { return &Adapter{service: service} }

func (a *Adapter) EnqueueSteer(ctx context.Context, input inbound.QueueCommandInput) error {
	return a.enqueue(input, func(queued application.QueueInput) error {
		_, err := a.service.EnqueueSteer(ctx, queued)
		return err
	})
}

func (a *Adapter) EnqueueFollowUp(ctx context.Context, input inbound.QueueCommandInput) error {
	return a.enqueue(input, func(queued application.QueueInput) error {
		_, err := a.service.EnqueueFollowUp(ctx, queued)
		return err
	})
}

func (a *Adapter) enqueue(input inbound.QueueCommandInput, admit func(application.QueueInput) error) error {
	if a == nil || a.service == nil {
		return inbound.NewQueueCommandError(inbound.QueueCommandCodeUnavailable)
	}
	if strings.TrimSpace(input.BotID) == "" || strings.TrimSpace(input.SessionID) == "" ||
		strings.TrimSpace(input.InvocationID) == "" || strings.TrimSpace(input.Text) == "" {
		return inbound.NewQueueCommandError(inbound.QueueCommandCodeInvalid)
	}
	return mapAdmissionError(admit(application.QueueInput{
		TeamID:                  input.TeamID,
		BotID:                   input.BotID,
		SessionID:               input.SessionID,
		InvocationID:            input.InvocationID,
		UserID:                  input.UserID,
		SourceChannelIdentityID: input.ChannelIdentityID,
		Text:                    input.Text,
	}))
}

func mapAdmissionError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, sessionruntime.ErrQueueSteerUnsupported):
		return inbound.NewQueueCommandError(inbound.QueueCommandCodeUnsupported)
	case errors.Is(err, sessionruntime.ErrQueueNoActiveRun):
		return inbound.NewQueueCommandError(inbound.QueueCommandCodeNoActiveRun)
	case errors.Is(err, sessionruntime.ErrQueueInvocationConflict):
		return inbound.NewQueueCommandError(inbound.QueueCommandCodeConflict)
	case errors.Is(err, sessionruntime.ErrQueueAdmissionOverloaded):
		return inbound.NewQueueCommandError(inbound.QueueCommandCodeOverloaded)
	case errors.Is(err, sessionruntime.ErrQueueCapacityExceeded):
		return inbound.NewQueueCommandError(inbound.QueueCommandCodeCapacity)
	case errors.Is(err, sessionruntime.ErrQueueInvalidReference):
		return inbound.NewQueueCommandError(inbound.QueueCommandCodeInvalid)
	case errors.Is(err, application.ErrQueueInputIncomplete):
		// The channel boundary did not record a team for this item. That is a
		// server wiring fault, so the sender sees the generic unavailable code.
		return inbound.NewQueueCommandError(inbound.QueueCommandCodeUnavailable)
	default:
		return err
	}
}

var _ inbound.QueueCommandHandler = (*Adapter)(nil)
