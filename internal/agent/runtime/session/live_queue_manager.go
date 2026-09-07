package sessionruntime

import "context"

func (m *Manager) liveQueueBackend() (LiveQueueBackend, error) {
	if m == nil || m.backend == nil {
		return nil, ErrManagerClosed
	}
	queue, ok := m.backend.(LiveQueueBackend)
	if !ok {
		return nil, ErrLiveQueueUnavailable
	}
	return queue, nil
}

func (m *Manager) EnqueueSteer(ctx context.Context, key Key, itemID, invocationID string, payload []byte) (SteerItem, error) {
	queue, err := m.liveQueueBackend()
	if err != nil {
		return SteerItem{}, err
	}
	return queue.EnqueueSteer(ctx, key, itemID, invocationID, payload)
}

func (m *Manager) EnqueueFollowUp(ctx context.Context, key Key, itemID, invocationID string, payload []byte) (FollowUpItem, error) {
	queue, err := m.liveQueueBackend()
	if err != nil {
		return FollowUpItem{}, err
	}
	return queue.EnqueueFollowUp(ctx, key, itemID, invocationID, payload)
}

func (m *Manager) PendingQueues(ctx context.Context, key Key, limit int) ([]SteerItem, []FollowUpItem, error) {
	queue, err := m.liveQueueBackend()
	if err != nil {
		return nil, nil, err
	}
	return queue.PendingQueues(ctx, key, limit)
}

func (m *Manager) ReorderSteer(ctx context.Context, key Key, item, before SteerPendingRef) ([]SteerItem, error) {
	queue, err := m.liveQueueBackend()
	if err != nil {
		return nil, err
	}
	return queue.ReorderSteer(ctx, key, item, before)
}

func (m *Manager) ReorderFollowUp(ctx context.Context, key Key, item, before FollowUpPendingRef) ([]FollowUpItem, error) {
	queue, err := m.liveQueueBackend()
	if err != nil {
		return nil, err
	}
	return queue.ReorderFollowUp(ctx, key, item, before)
}

func (m *Manager) UpdateSteer(ctx context.Context, key Key, itemID SteerItemID, payload []byte) (SteerItem, error) {
	queue, err := m.liveQueueBackend()
	if err != nil {
		return SteerItem{}, err
	}
	return queue.UpdateSteer(ctx, key, itemID, payload)
}

func (m *Manager) UpdateFollowUp(ctx context.Context, key Key, itemID FollowUpItemID, payload []byte) (FollowUpItem, error) {
	queue, err := m.liveQueueBackend()
	if err != nil {
		return FollowUpItem{}, err
	}
	return queue.UpdateFollowUp(ctx, key, itemID, payload)
}

func (m *Manager) CancelSteer(ctx context.Context, key Key, itemID SteerItemID) error {
	queue, err := m.liveQueueBackend()
	if err != nil {
		return err
	}
	return queue.CancelSteer(ctx, key, itemID)
}

func (m *Manager) CancelFollowUp(ctx context.Context, key Key, itemID FollowUpItemID) error {
	queue, err := m.liveQueueBackend()
	if err != nil {
		return err
	}
	return queue.CancelFollowUp(ctx, key, itemID)
}

func (m *Manager) PromoteFollowUpToSteer(ctx context.Context, key Key, ref FollowUpPendingRef) (PromoteFollowUpResult, error) {
	queue, err := m.liveQueueBackend()
	if err != nil {
		return PromoteFollowUpResult{}, err
	}
	return queue.PromoteFollowUpToSteer(ctx, key, ref)
}

func (m *Manager) ClaimNextSteer(ctx context.Context, handle RunHandle, sealIfEmpty bool) (SteerItem, SteerClaimRef, bool, error) {
	queue, err := m.liveQueueBackend()
	if err != nil {
		return SteerItem{}, SteerClaimRef{}, false, err
	}
	return queue.ClaimNextSteer(ctx, handle, sealIfEmpty)
}

func (m *Manager) ApplySteer(ctx context.Context, key Key, ref SteerClaimRef) error {
	queue, err := m.liveQueueBackend()
	if err != nil {
		return err
	}
	return queue.ApplySteer(ctx, key, ref)
}

func (m *Manager) ReleaseSteer(ctx context.Context, key Key, ref SteerClaimRef) error {
	queue, err := m.liveQueueBackend()
	if err != nil {
		return err
	}
	return queue.ReleaseSteer(ctx, key, ref)
}

func (m *Manager) ClaimNextFollowUp(ctx context.Context, key Key, triggerRunID string) (FollowUpItem, FollowUpClaimRef, bool, error) {
	queue, err := m.liveQueueBackend()
	if err != nil {
		return FollowUpItem{}, FollowUpClaimRef{}, false, err
	}
	return queue.ClaimNextFollowUp(ctx, key, triggerRunID)
}

func (m *Manager) ApplyFollowUp(ctx context.Context, key Key, ref FollowUpClaimRef) error {
	queue, err := m.liveQueueBackend()
	if err != nil {
		return err
	}
	return queue.ApplyFollowUp(ctx, key, ref)
}

func (m *Manager) ReleaseFollowUp(ctx context.Context, key Key, ref FollowUpClaimRef) error {
	queue, err := m.liveQueueBackend()
	if err != nil {
		return err
	}
	return queue.ReleaseFollowUp(ctx, key, ref)
}
