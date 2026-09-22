package inbound

import (
	"context"
	"errors"
)

const (
	// QueueCommandCodeNoActiveRun means the route has no active run that can
	// accept a queue item. It is deliberately shared by an absent active
	// session and a session whose run ended between route lookup and admission.
	QueueCommandCodeNoActiveRun = "queue_no_active_run"
	QueueCommandCodeOverloaded  = "queue_admission_overloaded"
	QueueCommandCodeUnavailable = "queue_admission_unavailable"
	QueueCommandCodeConflict    = "queue_invocation_conflict"
	QueueCommandCodeInvalid     = "queue_request_invalid"
	QueueCommandCodeUnsupported = "queue_unsupported_session"
	QueueCommandCodeCapacity    = "queue_capacity_exceeded"
	// QueueCommandCodeFollowUpUnsupportedChannel means the channel cannot
	// receive the reply of a run that the server starts from the follow-up
	// queue: platform channels deliver replies from the inbound call's run
	// handle, which a queued run does not have.
	QueueCommandCodeFollowUpUnsupportedChannel = "queue_follow_up_unsupported_channel"
)

// QueueCommandInput contains only facts derived by the channel boundary. The
// session is resolved from the current route; callers cannot select a run or
// supply queue provenance. Team and sender identity are recorded with the
// item because a queued follow-up starts after this request is gone, and the
// admission that starts it needs the same identity an ordinary turn carries.
type QueueCommandInput struct {
	TeamID            string `json:"team_id"`
	BotID             string `json:"bot_id"`
	SessionID         string `json:"session_id"`
	InvocationID      string `json:"invocation_id"`
	UserID            string `json:"user_id,omitempty"`
	ChannelIdentityID string `json:"channel_identity_id,omitempty"`
	Text              string `json:"text"`
}

// QueueCommandHandler is the narrow live-queue port used by channel slash
// controls. The embedded Server uses a local adapter; split Channel uses the
// authenticated server-runtime RPC client.
type QueueCommandHandler interface {
	EnqueueSteer(context.Context, QueueCommandInput) error
	EnqueueFollowUp(context.Context, QueueCommandInput) error
}

// QueueCommandError carries a stable, user-safe error code across the local
// and split-runtime boundaries. It intentionally contains no database or RPC
// diagnostic text.
type QueueCommandError struct{ Code string }

func (e QueueCommandError) Error() string { return e.Code }

func NewQueueCommandError(code string) error { return QueueCommandError{Code: code} }

func QueueCommandErrorCode(err error) string {
	var queueErr QueueCommandError
	if !errors.As(err, &queueErr) {
		return ""
	}
	return NormalizeQueueCommandCode(queueErr.Code)
}

// NormalizeQueueCommandCode accepts only the stable error vocabulary allowed
// to cross a channel boundary. It is used by the split-runtime RPC client,
// where the generic RPC transport reconstructs a public error from its code.
func NormalizeQueueCommandCode(code string) string {
	switch code {
	case QueueCommandCodeNoActiveRun,
		QueueCommandCodeOverloaded,
		QueueCommandCodeUnavailable,
		QueueCommandCodeConflict,
		QueueCommandCodeInvalid,
		QueueCommandCodeUnsupported,
		QueueCommandCodeCapacity,
		QueueCommandCodeFollowUpUnsupportedChannel:
		return code
	default:
		return ""
	}
}
