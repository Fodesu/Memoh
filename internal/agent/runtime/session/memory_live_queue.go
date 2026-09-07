package sessionruntime

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
)

func (b *MemoryBackend) purgeLiveQueuesLocked(now time.Time) {
	for key, state := range b.steerQueues {
		if !state.UpdatedAt.IsZero() && now.Sub(state.UpdatedAt) >= b.stateTTL {
			delete(b.steerQueues, key)
		}
	}
	for key, state := range b.followUpQueues {
		if !state.UpdatedAt.IsZero() && now.Sub(state.UpdatedAt) >= b.stateTTL {
			delete(b.followUpQueues, key)
		}
	}
}

func (b *MemoryBackend) liveSnapshotLocked(key Key, now time.Time) (Snapshot, bool) {
	b.purgeExpiredLocked(now)
	snapshot, ok := b.snapshots[key.String()]
	return snapshot, ok
}

func (b *MemoryBackend) EnqueueSteer(ctx context.Context, key Key, itemID, invocationID string, payload []byte) (SteerItem, error) {
	if err := contextError(ctx); err != nil {
		return SteerItem{}, err
	}
	if err := validateQueueKey(key); err != nil || strings.TrimSpace(itemID) == "" || strings.TrimSpace(invocationID) == "" || len(payload) == 0 {
		return SteerItem{}, ErrQueueInvalidReference
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return SteerItem{}, ErrLiveQueueUnavailable
	}
	now := time.Now().UTC()
	b.purgeLiveQueuesLocked(now)
	state := b.steerQueues[key.String()]
	if item, ok, err := replaySteer(state, invocationID, payload); ok {
		return item, err
	}
	snapshot, ok := b.liveSnapshotLocked(key, now)
	run, active := activeRun(snapshot, ok)
	if !active || state.ClosedRunID == run.RunID {
		return SteerItem{}, ErrQueueNoActiveRun
	}
	item := SteerItem{
		ID: SteerItemID(itemID), BotID: key.BotID, SessionID: key.SessionID,
		TargetRunID: run.RunID, InvocationID: invocationID, Payload: append([]byte(nil), payload...),
		Status: QueueAccepted, Position: nextSteerPosition(state), CreatedAt: now,
	}
	state.Items = append(state.Items, item)
	state.UpdatedAt = now
	if state.ClosedRunID != "" && state.ClosedRunID != run.RunID {
		state.ClosedRunID = ""
	}
	b.steerQueues[key.String()] = state
	return cloneSteerItem(item), nil
}

func (b *MemoryBackend) EnqueueFollowUp(ctx context.Context, key Key, itemID, invocationID string, payload []byte) (FollowUpItem, error) {
	if err := contextError(ctx); err != nil {
		return FollowUpItem{}, err
	}
	if err := validateQueueKey(key); err != nil || strings.TrimSpace(itemID) == "" || strings.TrimSpace(invocationID) == "" || validatePayload(payload) != nil {
		return FollowUpItem{}, ErrQueueInvalidReference
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return FollowUpItem{}, ErrLiveQueueUnavailable
	}
	now := time.Now().UTC()
	b.purgeLiveQueuesLocked(now)
	state := b.followUpQueues[key.String()]
	if item, ok, err := replayFollowUp(state, invocationID, payload); ok {
		return item, err
	}
	snapshot, ok := b.liveSnapshotLocked(key, now)
	run, active := activeRun(snapshot, ok)
	if !active {
		return FollowUpItem{}, ErrQueueNoActiveRun
	}
	item := FollowUpItem{
		ID: FollowUpItemID(itemID), BotID: key.BotID, SessionID: key.SessionID,
		EnqueuedDuringRunID: run.RunID, InvocationID: invocationID, Payload: append([]byte(nil), payload...),
		Status: QueueAccepted, Position: nextFollowUpPosition(state), CreatedAt: now,
	}
	state.Items = append(state.Items, item)
	state.UpdatedAt = now
	b.followUpQueues[key.String()] = state
	return cloneFollowUpItem(item), nil
}

func (b *MemoryBackend) PendingQueues(ctx context.Context, key Key, limit int) ([]SteerItem, []FollowUpItem, error) {
	if err := contextError(ctx); err != nil {
		return nil, nil, err
	}
	if err := validateQueueKey(key); err != nil {
		return nil, nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, nil, ErrLiveQueueUnavailable
	}
	b.purgeLiveQueuesLocked(time.Now().UTC())
	return pendingSteers(b.steerQueues[key.String()], limit), pendingFollowUps(b.followUpQueues[key.String()], limit), nil
}

func (b *MemoryBackend) ReorderSteer(ctx context.Context, key Key, item, before SteerPendingRef) ([]SteerItem, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, ErrLiveQueueUnavailable
	}
	now := time.Now().UTC()
	b.purgeLiveQueuesLocked(now)
	if err := validateQueueKey(key); err != nil {
		return nil, err
	}
	state := b.steerQueues[key.String()]
	items, err := reorderSteerState(&state, item, before)
	if err != nil {
		return nil, err
	}
	state.UpdatedAt = now
	b.steerQueues[key.String()] = state
	return items, nil
}

func (b *MemoryBackend) ReorderFollowUp(ctx context.Context, key Key, item, before FollowUpPendingRef) ([]FollowUpItem, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, ErrLiveQueueUnavailable
	}
	now := time.Now().UTC()
	b.purgeLiveQueuesLocked(now)
	if err := validateQueueKey(key); err != nil {
		return nil, err
	}
	state := b.followUpQueues[key.String()]
	items, err := reorderFollowUpState(&state, item, before)
	if err != nil {
		return nil, err
	}
	state.UpdatedAt = now
	b.followUpQueues[key.String()] = state
	return items, nil
}

func (b *MemoryBackend) UpdateSteer(ctx context.Context, key Key, itemID SteerItemID, payload []byte) (SteerItem, error) {
	if err := contextError(ctx); err != nil {
		return SteerItem{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return SteerItem{}, ErrLiveQueueUnavailable
	}
	if err := validateQueueKey(key); err != nil || itemID == "" || validatePayload(payload) != nil {
		return SteerItem{}, ErrQueueInvalidReference
	}
	state := b.steerQueues[key.String()]
	for i := range state.Items {
		if state.Items[i].ID == itemID && state.Items[i].Status == QueueAccepted {
			state.Items[i].Payload = append([]byte(nil), payload...)
			state.UpdatedAt = time.Now().UTC()
			b.steerQueues[key.String()] = state
			return cloneSteerItem(state.Items[i]), nil
		}
	}
	return SteerItem{}, ErrQueueNotPending
}

func (b *MemoryBackend) UpdateFollowUp(ctx context.Context, key Key, itemID FollowUpItemID, payload []byte) (FollowUpItem, error) {
	if err := contextError(ctx); err != nil {
		return FollowUpItem{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return FollowUpItem{}, ErrLiveQueueUnavailable
	}
	if err := validateQueueKey(key); err != nil || itemID == "" || validatePayload(payload) != nil {
		return FollowUpItem{}, ErrQueueInvalidReference
	}
	state := b.followUpQueues[key.String()]
	for i := range state.Items {
		if state.Items[i].ID == itemID && state.Items[i].Status == QueueAccepted {
			state.Items[i].Payload = append([]byte(nil), payload...)
			state.UpdatedAt = time.Now().UTC()
			b.followUpQueues[key.String()] = state
			return cloneFollowUpItem(state.Items[i]), nil
		}
	}
	return FollowUpItem{}, ErrQueueNotPending
}

func (b *MemoryBackend) CancelSteer(ctx context.Context, key Key, itemID SteerItemID) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return ErrLiveQueueUnavailable
	}
	if err := validateQueueKey(key); err != nil || itemID == "" {
		return ErrQueueInvalidReference
	}
	state := b.steerQueues[key.String()]
	for i := range state.Items {
		if state.Items[i].ID == itemID && state.Items[i].Status == QueueAccepted {
			state.Items[i].Status = QueueCanceled
			state.UpdatedAt = time.Now().UTC()
			b.steerQueues[key.String()] = state
			return nil
		}
	}
	return ErrQueueNotPending
}

func (b *MemoryBackend) CancelFollowUp(ctx context.Context, key Key, itemID FollowUpItemID) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return ErrLiveQueueUnavailable
	}
	if err := validateQueueKey(key); err != nil || itemID == "" {
		return ErrQueueInvalidReference
	}
	state := b.followUpQueues[key.String()]
	for i := range state.Items {
		if state.Items[i].ID == itemID && state.Items[i].Status == QueueAccepted {
			state.Items[i].Status = QueueCanceled
			state.UpdatedAt = time.Now().UTC()
			b.followUpQueues[key.String()] = state
			return nil
		}
	}
	return ErrQueueNotPending
}

func (b *MemoryBackend) PromoteFollowUpToSteer(ctx context.Context, key Key, ref FollowUpPendingRef) (PromoteFollowUpResult, error) {
	if err := contextError(ctx); err != nil {
		return PromoteFollowUpResult{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return PromoteFollowUpResult{}, ErrLiveQueueUnavailable
	}
	if err := validateQueueKey(key); err != nil || ref.ItemID == "" {
		return PromoteFollowUpResult{}, ErrQueueInvalidReference
	}
	now := time.Now().UTC()
	snapshot, ok := b.liveSnapshotLocked(key, now)
	run, active := activeRun(snapshot, ok)
	steers := b.steerQueues[key.String()]
	if !active || steers.ClosedRunID == run.RunID {
		return PromoteFollowUpResult{}, ErrQueueNoActiveRun
	}
	follows := b.followUpQueues[key.String()]
	if steerID := steers.PromotedFollowUpItems[string(ref.ItemID)]; steerID != "" {
		for _, existing := range steers.Items {
			if existing.ID == SteerItemID(steerID) {
				return PromoteFollowUpResult{FollowUp: ref, Steer: cloneSteerItem(existing)}, nil
			}
		}
		return PromoteFollowUpResult{}, ErrQueueInvalidReference
	}
	for i := range follows.Items {
		if follows.Items[i].ID != ref.ItemID || follows.Items[i].Status != QueueAccepted {
			continue
		}
		steer := SteerItem{
			ID: SteerItemID(uuid.NewString()), BotID: key.BotID, SessionID: key.SessionID,
			TargetRunID: run.RunID, InvocationID: "promote:" + string(ref.ItemID),
			Payload: append([]byte(nil), follows.Items[i].Payload...), Status: QueueAccepted,
			Position: nextSteerPosition(steers), CreatedAt: now,
		}
		steers.Items = append(steers.Items, steer)
		if steers.PromotedFollowUpItems == nil {
			steers.PromotedFollowUpItems = make(map[string]string)
		}
		steers.PromotedFollowUpItems[string(ref.ItemID)] = string(steer.ID)
		steers.UpdatedAt = now
		follows.Items[i].Status = QueueCanceled
		follows.UpdatedAt = now
		b.steerQueues[key.String()] = steers
		b.followUpQueues[key.String()] = follows
		return PromoteFollowUpResult{FollowUp: ref, Steer: cloneSteerItem(steer)}, nil
	}
	return PromoteFollowUpResult{}, ErrQueueNotPending
}

func (b *MemoryBackend) ClaimNextSteer(ctx context.Context, handle RunHandle, sealIfEmpty bool) (SteerItem, SteerClaimRef, bool, error) {
	if err := contextError(ctx); err != nil {
		return SteerItem{}, SteerClaimRef{}, false, err
	}
	handle = handle.normalized()
	if !handle.valid() || handle.OwnerID == "" || handle.FencingToken <= 0 {
		return SteerItem{}, SteerClaimRef{}, false, ErrQueueInvalidReference
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return SteerItem{}, SteerClaimRef{}, false, ErrLiveQueueUnavailable
	}
	now := time.Now().UTC()
	snapshot, ok := b.liveSnapshotLocked(handle.key(), now)
	if !ok || !runMatchesHandle(snapshot.CurrentRunView, handle) || snapshot.CurrentRunView.OwnerID != handle.OwnerID || !isActiveRunStatus(snapshot.CurrentRunView.Status) {
		return SteerItem{}, SteerClaimRef{}, false, ErrRunOwnershipLost
	}
	state := b.steerQueues[handle.key().String()]
	for _, item := range state.Items {
		if item.Status == QueueClaimed && item.Claim != nil && item.Claim.RunID == handle.RunID {
			return cloneSteerItem(item), *item.Claim, true, nil
		}
	}
	best := -1
	for i := range state.Items {
		if state.Items[i].Status == QueueAccepted && state.Items[i].TargetRunID == handle.RunID &&
			(best < 0 || state.Items[i].Position < state.Items[best].Position) {
			best = i
		}
	}
	if best < 0 {
		if sealIfEmpty {
			state.ClosedRunID = handle.RunID
			state.UpdatedAt = now
			b.steerQueues[handle.key().String()] = state
		}
		return SteerItem{}, SteerClaimRef{}, false, nil
	}
	claim := SteerClaimRef{ItemID: state.Items[best].ID, RunID: handle.RunID, OwnerID: handle.OwnerID, Generation: handle.Generation, FencingToken: handle.FencingToken, ClaimToken: uuid.NewString()}
	state.Items[best].Status = QueueClaimed
	state.Items[best].Claim = &claim
	state.UpdatedAt = now
	b.steerQueues[handle.key().String()] = state
	return cloneSteerItem(state.Items[best]), claim, true, nil
}

func (b *MemoryBackend) ApplySteer(ctx context.Context, key Key, ref SteerClaimRef) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return ErrLiveQueueUnavailable
	}
	if err := validateSteerClaim(key, ref); err != nil {
		return err
	}
	snapshot, ok := b.liveSnapshotLocked(key, time.Now().UTC())
	if !ok || !runMatchesSteerClaim(snapshot.CurrentRunView, ref) {
		return ErrRunOwnershipLost
	}
	state := b.steerQueues[key.String()]
	for i := range state.Items {
		claim := state.Items[i].Claim
		if state.Items[i].ID == ref.ItemID && state.Items[i].Status == QueueClaimed && claim != nil && *claim == ref {
			state.Items[i].Status = QueueApplied
			state.UpdatedAt = time.Now().UTC()
			b.steerQueues[key.String()] = state
			return nil
		}
	}
	return ErrQueueInvalidReference
}

func (b *MemoryBackend) ReleaseSteer(ctx context.Context, key Key, ref SteerClaimRef) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return ErrLiveQueueUnavailable
	}
	if err := validateSteerClaim(key, ref); err != nil {
		return err
	}
	snapshot, ok := b.liveSnapshotLocked(key, time.Now().UTC())
	if !ok || !runMatchesSteerClaim(snapshot.CurrentRunView, ref) {
		return ErrRunOwnershipLost
	}
	state := b.steerQueues[key.String()]
	for i := range state.Items {
		claim := state.Items[i].Claim
		if state.Items[i].ID == ref.ItemID && state.Items[i].Status == QueueClaimed && claim != nil && *claim == ref {
			state.Items[i].Status = QueueAccepted
			state.Items[i].Claim = nil
			state.UpdatedAt = time.Now().UTC()
			b.steerQueues[key.String()] = state
			return nil
		}
	}
	return ErrQueueInvalidReference
}

func (b *MemoryBackend) ClaimNextFollowUp(ctx context.Context, key Key, triggerRunID string) (FollowUpItem, FollowUpClaimRef, bool, error) {
	if err := contextError(ctx); err != nil {
		return FollowUpItem{}, FollowUpClaimRef{}, false, err
	}
	triggerRunID = strings.TrimSpace(triggerRunID)
	if triggerRunID == "" {
		return FollowUpItem{}, FollowUpClaimRef{}, false, ErrQueueInvalidReference
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return FollowUpItem{}, FollowUpClaimRef{}, false, ErrLiveQueueUnavailable
	}
	if err := validateQueueKey(key); err != nil {
		return FollowUpItem{}, FollowUpClaimRef{}, false, err
	}
	state := b.followUpQueues[key.String()]
	if state.TerminalClaims == nil {
		state.TerminalClaims = make(map[string]string)
	}
	if itemID := state.TerminalClaims[triggerRunID]; itemID != "" {
		for _, item := range state.Items {
			if string(item.ID) == itemID && item.Status == QueueClaimed && item.Claim != nil {
				return cloneFollowUpItem(item), *item.Claim, true, nil
			}
		}
		return FollowUpItem{}, FollowUpClaimRef{}, false, nil
	}
	best := -1
	for i := range state.Items {
		if state.Items[i].Status == QueueAccepted && (best < 0 || state.Items[i].Position < state.Items[best].Position) {
			best = i
		}
	}
	if best < 0 {
		return FollowUpItem{}, FollowUpClaimRef{}, false, nil
	}
	claim := FollowUpClaimRef{ItemID: state.Items[best].ID, TriggerRunID: triggerRunID, ClaimToken: uuid.NewString()}
	state.Items[best].Status = QueueClaimed
	state.Items[best].Claim = &claim
	state.TerminalClaims[triggerRunID] = string(state.Items[best].ID)
	state.UpdatedAt = time.Now().UTC()
	b.followUpQueues[key.String()] = state
	return cloneFollowUpItem(state.Items[best]), claim, true, nil
}

func (b *MemoryBackend) ApplyFollowUp(ctx context.Context, key Key, ref FollowUpClaimRef) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return ErrLiveQueueUnavailable
	}
	if err := validateFollowUpClaim(key, ref); err != nil {
		return err
	}
	state := b.followUpQueues[key.String()]
	for i := range state.Items {
		claim := state.Items[i].Claim
		if state.Items[i].ID == ref.ItemID && state.Items[i].Status == QueueClaimed && claim != nil && *claim == ref {
			state.Items[i].Status = QueueApplied
			state.UpdatedAt = time.Now().UTC()
			b.followUpQueues[key.String()] = state
			return nil
		}
	}
	return ErrQueueInvalidReference
}

func (b *MemoryBackend) ReleaseFollowUp(ctx context.Context, key Key, ref FollowUpClaimRef) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return ErrLiveQueueUnavailable
	}
	if err := validateFollowUpClaim(key, ref); err != nil {
		return err
	}
	state := b.followUpQueues[key.String()]
	for i := range state.Items {
		claim := state.Items[i].Claim
		if state.Items[i].ID == ref.ItemID && state.Items[i].Status == QueueClaimed && claim != nil && *claim == ref {
			state.Items[i].Status = QueueAccepted
			state.Items[i].Claim = nil
			delete(state.TerminalClaims, ref.TriggerRunID)
			state.UpdatedAt = time.Now().UTC()
			b.followUpQueues[key.String()] = state
			return nil
		}
	}
	return ErrQueueInvalidReference
}

var _ LiveQueueBackend = (*MemoryBackend)(nil)
