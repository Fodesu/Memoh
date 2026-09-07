package sessionruntime

import (
	"bytes"
	"context"
	"errors"
	"sort"
	"strings"
	"time"
)

type QueueStatus string

const (
	QueueAccepted QueueStatus = "accepted"
	QueueClaimed  QueueStatus = "claimed"
	QueueApplied  QueueStatus = "applied"
	QueueRejected QueueStatus = "rejected"
	QueueExpired  QueueStatus = "expired"
	QueueCanceled QueueStatus = "canceled"
)

var (
	ErrQueueNoActiveRun         = errors.New("queue: no active run")
	ErrQueueInvalidReference    = errors.New("queue: invalid claim reference")
	ErrQueueNotPending          = errors.New("queue: item is not an accepted pending item")
	ErrQueueInvocationConflict  = errors.New("queue: invocation payload conflicts with an existing item")
	ErrLiveQueueUnavailable     = errors.New("session runtime queue is unavailable")
	ErrQueueAdmissionOverloaded = errors.New("queue: admission overloaded")
)

type (
	SteerItemID    string
	FollowUpItemID string
)

type SteerPendingRef struct {
	ItemID SteerItemID `json:"item_id"`
}

type FollowUpPendingRef struct {
	ItemID FollowUpItemID `json:"item_id"`
}

type SteerClaimRef struct {
	ItemID       SteerItemID
	RunID        string
	OwnerID      string
	Generation   string
	FencingToken int64
	ClaimToken   string
}

type FollowUpClaimRef struct {
	ItemID       FollowUpItemID
	TriggerRunID string
	ClaimToken   string
}

type SteerItem struct {
	ID                            SteerItemID
	BotID, SessionID, TargetRunID string
	InvocationID                  string
	Payload                       []byte
	Status                        QueueStatus
	Position                      int64
	Claim                         *SteerClaimRef
	CreatedAt                     time.Time
}

type FollowUpItem struct {
	ID                  FollowUpItemID
	BotID, SessionID    string
	EnqueuedDuringRunID string
	InvocationID        string
	Payload             []byte
	Status              QueueStatus
	Position            int64
	Claim               *FollowUpClaimRef
	CreatedAt           time.Time
}

type PromoteFollowUpResult struct {
	FollowUp FollowUpPendingRef
	Steer    SteerItem
}

// LiveQueueBackend is transient session coordination. Implementations must
// serialize each operation with the live run state for the same session.
type LiveQueueBackend interface {
	EnqueueSteer(context.Context, Key, string, string, []byte) (SteerItem, error)
	EnqueueFollowUp(context.Context, Key, string, string, []byte) (FollowUpItem, error)
	PendingQueues(context.Context, Key, int) ([]SteerItem, []FollowUpItem, error)
	ReorderSteer(context.Context, Key, SteerPendingRef, SteerPendingRef) ([]SteerItem, error)
	ReorderFollowUp(context.Context, Key, FollowUpPendingRef, FollowUpPendingRef) ([]FollowUpItem, error)
	UpdateSteer(context.Context, Key, SteerItemID, []byte) (SteerItem, error)
	UpdateFollowUp(context.Context, Key, FollowUpItemID, []byte) (FollowUpItem, error)
	CancelSteer(context.Context, Key, SteerItemID) error
	CancelFollowUp(context.Context, Key, FollowUpItemID) error
	PromoteFollowUpToSteer(context.Context, Key, FollowUpPendingRef) (PromoteFollowUpResult, error)
	ClaimNextSteer(context.Context, RunHandle, bool) (SteerItem, SteerClaimRef, bool, error)
	ApplySteer(context.Context, Key, SteerClaimRef) error
	ReleaseSteer(context.Context, Key, SteerClaimRef) error
	ClaimNextFollowUp(context.Context, Key, string) (FollowUpItem, FollowUpClaimRef, bool, error)
	ApplyFollowUp(context.Context, Key, FollowUpClaimRef) error
	ReleaseFollowUp(context.Context, Key, FollowUpClaimRef) error
}

type steerQueueState struct {
	Items                 []SteerItem       `json:"items"`
	PromotedFollowUpItems map[string]string `json:"promoted_follow_up_items,omitempty"`
	ClosedRunID           string            `json:"closed_run_id,omitempty"`
	UpdatedAt             time.Time         `json:"updated_at"`
}

type followUpQueueState struct {
	Items          []FollowUpItem    `json:"items"`
	TerminalClaims map[string]string `json:"terminal_claims,omitempty"`
	UpdatedAt      time.Time         `json:"updated_at"`
}

func activeRun(snapshot Snapshot, ok bool) (*CurrentRunView, bool) {
	if !ok || snapshot.CurrentRunView == nil || !isActiveRunStatus(snapshot.CurrentRunView.Status) {
		return nil, false
	}
	return snapshot.CurrentRunView, true
}

func validateQueueKey(key Key) error {
	if strings.TrimSpace(key.BotID) == "" || strings.TrimSpace(key.SessionID) == "" {
		return ErrQueueInvalidReference
	}
	return nil
}

func validatePayload(payload []byte) error {
	if len(payload) == 0 {
		return ErrQueueInvalidReference
	}
	return nil
}

func validateSteerClaim(key Key, ref SteerClaimRef) error {
	if err := validateQueueKey(key); err != nil {
		return err
	}
	if ref.ItemID == "" || strings.TrimSpace(ref.RunID) == "" || strings.TrimSpace(ref.OwnerID) == "" ||
		strings.TrimSpace(ref.Generation) == "" || ref.FencingToken <= 0 || strings.TrimSpace(ref.ClaimToken) == "" {
		return ErrQueueInvalidReference
	}
	return nil
}

func validateFollowUpClaim(key Key, ref FollowUpClaimRef) error {
	if err := validateQueueKey(key); err != nil {
		return err
	}
	if ref.ItemID == "" || strings.TrimSpace(ref.TriggerRunID) == "" || strings.TrimSpace(ref.ClaimToken) == "" {
		return ErrQueueInvalidReference
	}
	return nil
}

func runMatchesSteerClaim(run *CurrentRunView, ref SteerClaimRef) bool {
	return run != nil && run.RunID == ref.RunID && run.OwnerID == ref.OwnerID && run.Generation == ref.Generation && isActiveRunStatus(run.Status)
}

func cloneSteerItem(item SteerItem) SteerItem {
	item.Payload = append([]byte(nil), item.Payload...)
	if item.Claim != nil {
		claim := *item.Claim
		item.Claim = &claim
	}
	return item
}

func cloneFollowUpItem(item FollowUpItem) FollowUpItem {
	item.Payload = append([]byte(nil), item.Payload...)
	if item.Claim != nil {
		claim := *item.Claim
		item.Claim = &claim
	}
	return item
}

func pendingSteers(state steerQueueState, limit int) []SteerItem {
	items := make([]SteerItem, 0, len(state.Items))
	for _, item := range state.Items {
		if item.Status == QueueAccepted {
			items = append(items, cloneSteerItem(item))
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Position < items[j].Position })
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items
}

func pendingFollowUps(state followUpQueueState, limit int) []FollowUpItem {
	items := make([]FollowUpItem, 0, len(state.Items))
	for _, item := range state.Items {
		if item.Status == QueueAccepted {
			items = append(items, cloneFollowUpItem(item))
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Position < items[j].Position })
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items
}

func nextSteerPosition(state steerQueueState) int64 {
	var position int64
	for _, item := range state.Items {
		if item.Position > position {
			position = item.Position
		}
	}
	return position + 1
}

func nextFollowUpPosition(state followUpQueueState) int64 {
	var position int64
	for _, item := range state.Items {
		if item.Position > position {
			position = item.Position
		}
	}
	return position + 1
}

func reorderSteerState(state *steerQueueState, itemRef, beforeRef SteerPendingRef) ([]SteerItem, error) {
	if state == nil || itemRef.ItemID == "" || itemRef.ItemID == beforeRef.ItemID {
		return nil, ErrQueueInvalidReference
	}
	indices := make([]int, 0, len(state.Items))
	itemPos, beforePos := -1, -1
	for i := range state.Items {
		if state.Items[i].Status != QueueAccepted {
			continue
		}
		indices = append(indices, i)
		if state.Items[i].ID == itemRef.ItemID {
			itemPos = len(indices) - 1
		}
		if state.Items[i].ID == beforeRef.ItemID {
			beforePos = len(indices) - 1
		}
	}
	if itemPos < 0 || (beforeRef.ItemID != "" && beforePos < 0) {
		return nil, ErrQueueNotPending
	}
	order := append([]int(nil), indices...)
	moving := order[itemPos]
	order = append(order[:itemPos], order[itemPos+1:]...)
	insertAt := len(order)
	if beforeRef.ItemID != "" {
		insertAt = 0
		for insertAt < len(order) && state.Items[order[insertAt]].ID != beforeRef.ItemID {
			insertAt++
		}
	}
	order = append(order, 0)
	copy(order[insertAt+1:], order[insertAt:])
	order[insertAt] = moving
	for position, index := range order {
		state.Items[index].Position = int64(position + 1)
	}
	return pendingSteers(*state, 0), nil
}

func reorderFollowUpState(state *followUpQueueState, itemRef, beforeRef FollowUpPendingRef) ([]FollowUpItem, error) {
	if state == nil || itemRef.ItemID == "" || itemRef.ItemID == beforeRef.ItemID {
		return nil, ErrQueueInvalidReference
	}
	indices := make([]int, 0, len(state.Items))
	itemPos, beforePos := -1, -1
	for i := range state.Items {
		if state.Items[i].Status != QueueAccepted {
			continue
		}
		indices = append(indices, i)
		if state.Items[i].ID == itemRef.ItemID {
			itemPos = len(indices) - 1
		}
		if state.Items[i].ID == beforeRef.ItemID {
			beforePos = len(indices) - 1
		}
	}
	if itemPos < 0 || (beforeRef.ItemID != "" && beforePos < 0) {
		return nil, ErrQueueNotPending
	}
	order := append([]int(nil), indices...)
	moving := order[itemPos]
	order = append(order[:itemPos], order[itemPos+1:]...)
	insertAt := len(order)
	if beforeRef.ItemID != "" {
		insertAt = 0
		for insertAt < len(order) && state.Items[order[insertAt]].ID != beforeRef.ItemID {
			insertAt++
		}
	}
	order = append(order, 0)
	copy(order[insertAt+1:], order[insertAt:])
	order[insertAt] = moving
	for position, index := range order {
		state.Items[index].Position = int64(position + 1)
	}
	return pendingFollowUps(*state, 0), nil
}

func replaySteer(state steerQueueState, invocationID string, payload []byte) (SteerItem, bool, error) {
	for _, item := range state.Items {
		if item.InvocationID != invocationID {
			continue
		}
		if !bytes.Equal(item.Payload, payload) {
			return SteerItem{}, true, ErrQueueInvocationConflict
		}
		return cloneSteerItem(item), true, nil
	}
	return SteerItem{}, false, nil
}

func replayFollowUp(state followUpQueueState, invocationID string, payload []byte) (FollowUpItem, bool, error) {
	for _, item := range state.Items {
		if item.InvocationID != invocationID {
			continue
		}
		if !bytes.Equal(item.Payload, payload) {
			return FollowUpItem{}, true, ErrQueueInvocationConflict
		}
		return cloneFollowUpItem(item), true, nil
	}
	return FollowUpItem{}, false, nil
}
