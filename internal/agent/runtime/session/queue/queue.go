// Package queue keeps the public queue vocabulary separate from the session
// runtime implementation. Storage is owned by the configured live backend.
package queue

import sessionruntime "github.com/felinics/memoh/internal/agent/runtime/session"

type (
	Status                = sessionruntime.QueueStatus
	SteerItemID           = sessionruntime.SteerItemID
	FollowUpItemID        = sessionruntime.FollowUpItemID
	SteerPendingRef       = sessionruntime.SteerPendingRef
	FollowUpPendingRef    = sessionruntime.FollowUpPendingRef
	SteerClaimRef         = sessionruntime.SteerClaimRef
	FollowUpClaimRef      = sessionruntime.FollowUpClaimRef
	SteerItem             = sessionruntime.SteerItem
	FollowUpItem          = sessionruntime.FollowUpItem
	PromoteFollowUpResult = sessionruntime.PromoteFollowUpResult
)

const (
	Accepted = sessionruntime.QueueAccepted
	Claimed  = sessionruntime.QueueClaimed
	Applied  = sessionruntime.QueueApplied
	Rejected = sessionruntime.QueueRejected
	Expired  = sessionruntime.QueueExpired
	Canceled = sessionruntime.QueueCanceled

	DefaultPendingListLimit = 256
)

var (
	ErrNoActiveRun         = sessionruntime.ErrQueueNoActiveRun
	ErrInvalidReference    = sessionruntime.ErrQueueInvalidReference
	ErrNotPending          = sessionruntime.ErrQueueNotPending
	ErrInvocationConflict  = sessionruntime.ErrQueueInvocationConflict
	ErrAdmissionOverloaded = sessionruntime.ErrQueueAdmissionOverloaded
)
