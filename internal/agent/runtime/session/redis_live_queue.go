package sessionruntime

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const redisQueueMaxRetries = 8

func (b *RedisBackend) ensureQueueOpen() error {
	if b == nil || b.client == nil {
		return ErrLiveQueueUnavailable
	}
	b.subscriptionsMu.Lock()
	closed := b.closed
	b.subscriptionsMu.Unlock()
	if closed {
		return ErrLiveQueueUnavailable
	}
	return nil
}

func waitRedisQueueRetry(ctx context.Context, attempt int) error {
	if attempt >= redisQueueMaxRetries {
		return ErrQueueAdmissionOverloaded
	}
	delay := time.Duration(1<<min(attempt, 6)) * time.Millisecond
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func redisWatch(ctx context.Context, b *RedisBackend, keys []string, fn func(*redis.Tx) error) error {
	if err := b.ensureQueueOpen(); err != nil {
		return err
	}
	for attempt := 0; ; attempt++ {
		err := b.client.Watch(ctx, fn, keys...)
		if !errors.Is(err, redis.TxFailedErr) {
			return err
		}
		if err := waitRedisQueueRetry(ctx, attempt); err != nil {
			return err
		}
		if err := b.ensureQueueOpen(); err != nil {
			return err
		}
	}
}

func loadRedisJSON[T any](ctx context.Context, cmd redis.Cmdable, key string) (T, error) {
	var value T
	data, err := cmd.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return value, nil
	}
	if err != nil {
		return value, err
	}
	return value, json.Unmarshal(data, &value)
}

func storeRedisJSON(ctx context.Context, tx *redis.Tx, key string, value any, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.Set(ctx, key, data, ttl)
		return nil
	})
	return err
}

func redisMutate[T any, R any](ctx context.Context, b *RedisBackend, key string, mutate func(*T, time.Time) (R, error)) (R, error) {
	var zero R
	if err := b.ensureQueueOpen(); err != nil {
		return zero, err
	}
	var result R
	err := redisWatch(ctx, b, []string{key}, func(tx *redis.Tx) error {
		state, err := loadRedisJSON[T](ctx, tx, key)
		if err != nil {
			return err
		}
		now, err := tx.Time(ctx).Result()
		if err != nil {
			return err
		}
		result, err = mutate(&state, now.UTC())
		if err != nil {
			return err
		}
		return storeRedisJSON(ctx, tx, key, state, b.stateTTL)
	})
	if err != nil {
		return zero, err
	}
	return result, nil
}

func redisMutateWithKeys[T any, R any](ctx context.Context, b *RedisBackend, keys []string, key string, mutate func(*redis.Tx, *T, time.Time) (R, error)) (R, error) {
	var zero R
	if err := b.ensureQueueOpen(); err != nil {
		return zero, err
	}
	var result R
	err := redisWatch(ctx, b, keys, func(tx *redis.Tx) error {
		state, err := loadRedisJSON[T](ctx, tx, key)
		if err != nil {
			return err
		}
		now, err := tx.Time(ctx).Result()
		if err != nil {
			return err
		}
		result, err = mutate(tx, &state, now.UTC())
		if err != nil {
			return err
		}
		return storeRedisJSON(ctx, tx, key, state, b.stateTTL)
	})
	if err != nil {
		return zero, err
	}
	return result, nil
}

func (b *RedisBackend) EnqueueSteer(ctx context.Context, key Key, itemID, invocationID string, payload []byte) (SteerItem, error) {
	if err := b.ensureQueueOpen(); err != nil {
		return SteerItem{}, err
	}
	if err := validateQueueKey(key); err != nil || strings.TrimSpace(itemID) == "" || strings.TrimSpace(invocationID) == "" || validatePayload(payload) != nil {
		return SteerItem{}, ErrQueueInvalidReference
	}
	stateKey, queueKey := b.stateKey(key), b.steerQueueKey(key)
	var item SteerItem
	err := redisWatch(ctx, b, []string{stateKey, queueKey}, func(tx *redis.Tx) error {
		queue, err := loadRedisJSON[steerQueueState](ctx, tx, queueKey)
		if err != nil {
			return err
		}
		if replay, ok, replayErr := replaySteer(queue, invocationID, payload); ok {
			item = replay
			return replayErr
		}
		snapshot, ok, err := loadRedisSnapshot(ctx, tx, stateKey)
		if err != nil {
			return err
		}
		run, active := activeRun(snapshot, ok)
		if !active || queue.ClosedRunID == run.RunID {
			return ErrQueueNoActiveRun
		}
		now, err := tx.Time(ctx).Result()
		if err != nil {
			return err
		}
		item = SteerItem{ID: SteerItemID(itemID), BotID: key.BotID, SessionID: key.SessionID, TargetRunID: run.RunID, InvocationID: invocationID, Payload: append([]byte(nil), payload...), Status: QueueAccepted, Position: nextSteerPosition(queue), CreatedAt: now.UTC()}
		queue.Items = append(queue.Items, item)
		queue.UpdatedAt = now.UTC()
		if queue.ClosedRunID != "" && queue.ClosedRunID != run.RunID {
			queue.ClosedRunID = ""
		}
		return storeRedisJSON(ctx, tx, queueKey, queue, b.stateTTL)
	})
	return item, err
}

func (b *RedisBackend) EnqueueFollowUp(ctx context.Context, key Key, itemID, invocationID string, payload []byte) (FollowUpItem, error) {
	if err := b.ensureQueueOpen(); err != nil {
		return FollowUpItem{}, err
	}
	if err := validateQueueKey(key); err != nil || strings.TrimSpace(itemID) == "" || strings.TrimSpace(invocationID) == "" || validatePayload(payload) != nil {
		return FollowUpItem{}, ErrQueueInvalidReference
	}
	stateKey, queueKey := b.stateKey(key), b.followUpQueueKey(key)
	var item FollowUpItem
	err := redisWatch(ctx, b, []string{stateKey, queueKey}, func(tx *redis.Tx) error {
		queue, err := loadRedisJSON[followUpQueueState](ctx, tx, queueKey)
		if err != nil {
			return err
		}
		if replay, ok, replayErr := replayFollowUp(queue, invocationID, payload); ok {
			item = replay
			return replayErr
		}
		snapshot, ok, err := loadRedisSnapshot(ctx, tx, stateKey)
		if err != nil {
			return err
		}
		run, active := activeRun(snapshot, ok)
		if !active {
			return ErrQueueNoActiveRun
		}
		now, err := tx.Time(ctx).Result()
		if err != nil {
			return err
		}
		item = FollowUpItem{ID: FollowUpItemID(itemID), BotID: key.BotID, SessionID: key.SessionID, EnqueuedDuringRunID: run.RunID, InvocationID: invocationID, Payload: append([]byte(nil), payload...), Status: QueueAccepted, Position: nextFollowUpPosition(queue), CreatedAt: now.UTC()}
		queue.Items = append(queue.Items, item)
		queue.UpdatedAt = now.UTC()
		return storeRedisJSON(ctx, tx, queueKey, queue, b.stateTTL)
	})
	return item, err
}

func (b *RedisBackend) PendingQueues(ctx context.Context, key Key, limit int) ([]SteerItem, []FollowUpItem, error) {
	if err := b.ensureQueueOpen(); err != nil {
		return nil, nil, err
	}
	if err := validateQueueKey(key); err != nil {
		return nil, nil, err
	}
	pipe := b.client.Pipeline()
	steerCmd := pipe.Get(ctx, b.steerQueueKey(key))
	followCmd := pipe.Get(ctx, b.followUpQueueKey(key))
	_, err := pipe.Exec(ctx)
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, nil, err
	}
	var steers steerQueueState
	if data, getErr := steerCmd.Bytes(); getErr == nil {
		if err := json.Unmarshal(data, &steers); err != nil {
			return nil, nil, err
		}
	} else if !errors.Is(getErr, redis.Nil) {
		return nil, nil, getErr
	}
	var follows followUpQueueState
	if data, getErr := followCmd.Bytes(); getErr == nil {
		if err := json.Unmarshal(data, &follows); err != nil {
			return nil, nil, err
		}
	} else if !errors.Is(getErr, redis.Nil) {
		return nil, nil, getErr
	}
	return pendingSteers(steers, limit), pendingFollowUps(follows, limit), nil
}

func (b *RedisBackend) ReorderSteer(ctx context.Context, key Key, item, before SteerPendingRef) ([]SteerItem, error) {
	if err := validateQueueKey(key); err != nil {
		return nil, err
	}
	return redisMutate(ctx, b, b.steerQueueKey(key), func(state *steerQueueState, now time.Time) ([]SteerItem, error) {
		items, err := reorderSteerState(state, item, before)
		state.UpdatedAt = now
		return items, err
	})
}

func (b *RedisBackend) ReorderFollowUp(ctx context.Context, key Key, item, before FollowUpPendingRef) ([]FollowUpItem, error) {
	if err := validateQueueKey(key); err != nil {
		return nil, err
	}
	return redisMutate(ctx, b, b.followUpQueueKey(key), func(state *followUpQueueState, now time.Time) ([]FollowUpItem, error) {
		items, err := reorderFollowUpState(state, item, before)
		state.UpdatedAt = now
		return items, err
	})
}

func (b *RedisBackend) UpdateSteer(ctx context.Context, key Key, itemID SteerItemID, payload []byte) (SteerItem, error) {
	if err := validateQueueKey(key); err != nil || itemID == "" || validatePayload(payload) != nil {
		return SteerItem{}, ErrQueueInvalidReference
	}
	return redisMutate(ctx, b, b.steerQueueKey(key), func(state *steerQueueState, now time.Time) (SteerItem, error) {
		for i := range state.Items {
			if state.Items[i].ID == itemID && state.Items[i].Status == QueueAccepted {
				state.Items[i].Payload = append([]byte(nil), payload...)
				state.UpdatedAt = now
				return cloneSteerItem(state.Items[i]), nil
			}
		}
		return SteerItem{}, ErrQueueNotPending
	})
}

func (b *RedisBackend) UpdateFollowUp(ctx context.Context, key Key, itemID FollowUpItemID, payload []byte) (FollowUpItem, error) {
	if err := validateQueueKey(key); err != nil || itemID == "" || validatePayload(payload) != nil {
		return FollowUpItem{}, ErrQueueInvalidReference
	}
	return redisMutate(ctx, b, b.followUpQueueKey(key), func(state *followUpQueueState, now time.Time) (FollowUpItem, error) {
		for i := range state.Items {
			if state.Items[i].ID == itemID && state.Items[i].Status == QueueAccepted {
				state.Items[i].Payload = append([]byte(nil), payload...)
				state.UpdatedAt = now
				return cloneFollowUpItem(state.Items[i]), nil
			}
		}
		return FollowUpItem{}, ErrQueueNotPending
	})
}

func (b *RedisBackend) CancelSteer(ctx context.Context, key Key, itemID SteerItemID) error {
	if err := validateQueueKey(key); err != nil || itemID == "" {
		return ErrQueueInvalidReference
	}
	_, err := redisMutate(ctx, b, b.steerQueueKey(key), func(state *steerQueueState, now time.Time) (struct{}, error) {
		for i := range state.Items {
			if state.Items[i].ID == itemID && state.Items[i].Status == QueueAccepted {
				state.Items[i].Status = QueueCanceled
				state.UpdatedAt = now
				return struct{}{}, nil
			}
		}
		return struct{}{}, ErrQueueNotPending
	})
	return err
}

func (b *RedisBackend) CancelFollowUp(ctx context.Context, key Key, itemID FollowUpItemID) error {
	if err := validateQueueKey(key); err != nil || itemID == "" {
		return ErrQueueInvalidReference
	}
	_, err := redisMutate(ctx, b, b.followUpQueueKey(key), func(state *followUpQueueState, now time.Time) (struct{}, error) {
		for i := range state.Items {
			if state.Items[i].ID == itemID && state.Items[i].Status == QueueAccepted {
				state.Items[i].Status = QueueCanceled
				state.UpdatedAt = now
				return struct{}{}, nil
			}
		}
		return struct{}{}, ErrQueueNotPending
	})
	return err
}

func (b *RedisBackend) PromoteFollowUpToSteer(ctx context.Context, key Key, ref FollowUpPendingRef) (PromoteFollowUpResult, error) {
	if err := b.ensureQueueOpen(); err != nil {
		return PromoteFollowUpResult{}, err
	}
	stateKey, steerKey, followKey := b.stateKey(key), b.steerQueueKey(key), b.followUpQueueKey(key)
	if err := validateQueueKey(key); err != nil || ref.ItemID == "" {
		return PromoteFollowUpResult{}, ErrQueueInvalidReference
	}
	var result PromoteFollowUpResult
	err := redisWatch(ctx, b, []string{stateKey, steerKey, followKey}, func(tx *redis.Tx) error {
		snapshot, ok, err := loadRedisSnapshot(ctx, tx, stateKey)
		if err != nil {
			return err
		}
		run, active := activeRun(snapshot, ok)
		steers, err := loadRedisJSON[steerQueueState](ctx, tx, steerKey)
		if err != nil {
			return err
		}
		if !active || steers.ClosedRunID == run.RunID {
			return ErrQueueNoActiveRun
		}
		if steerID := steers.PromotedFollowUpItems[string(ref.ItemID)]; steerID != "" {
			for _, existing := range steers.Items {
				if existing.ID == SteerItemID(steerID) {
					result = PromoteFollowUpResult{FollowUp: ref, Steer: cloneSteerItem(existing)}
					return nil
				}
			}
			return ErrQueueInvalidReference
		}
		follows, err := loadRedisJSON[followUpQueueState](ctx, tx, followKey)
		if err != nil {
			return err
		}
		now, err := tx.Time(ctx).Result()
		if err != nil {
			return err
		}
		for i := range follows.Items {
			if follows.Items[i].ID != ref.ItemID || follows.Items[i].Status != QueueAccepted {
				continue
			}
			steer := SteerItem{ID: SteerItemID(uuid.NewString()), BotID: key.BotID, SessionID: key.SessionID, TargetRunID: run.RunID, InvocationID: "promote:" + string(ref.ItemID), Payload: append([]byte(nil), follows.Items[i].Payload...), Status: QueueAccepted, Position: nextSteerPosition(steers), CreatedAt: now.UTC()}
			steers.Items = append(steers.Items, steer)
			if steers.PromotedFollowUpItems == nil {
				steers.PromotedFollowUpItems = make(map[string]string)
			}
			steers.PromotedFollowUpItems[string(ref.ItemID)] = string(steer.ID)
			steers.UpdatedAt = now.UTC()
			follows.Items[i].Status = QueueCanceled
			follows.UpdatedAt = now.UTC()
			steerData, err := json.Marshal(steers)
			if err != nil {
				return err
			}
			followData, err := json.Marshal(follows)
			if err != nil {
				return err
			}
			_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
				pipe.Set(ctx, steerKey, steerData, b.stateTTL)
				pipe.Set(ctx, followKey, followData, b.stateTTL)
				return nil
			})
			result = PromoteFollowUpResult{FollowUp: ref, Steer: cloneSteerItem(steer)}
			return err
		}
		return ErrQueueNotPending
	})
	return result, err
}

func (b *RedisBackend) ClaimNextSteer(ctx context.Context, handle RunHandle, sealIfEmpty bool) (SteerItem, SteerClaimRef, bool, error) {
	if err := b.ensureQueueOpen(); err != nil {
		return SteerItem{}, SteerClaimRef{}, false, err
	}
	handle = handle.normalized()
	if !handle.valid() || handle.OwnerID == "" || handle.FencingToken <= 0 {
		return SteerItem{}, SteerClaimRef{}, false, ErrQueueInvalidReference
	}
	key := handle.key()
	stateKey, runKey, queueKey := b.stateKey(key), b.runKey(key, handle.RunID), b.steerQueueKey(key)
	var item SteerItem
	var claim SteerClaimRef
	var claimed bool
	err := redisWatch(ctx, b, []string{stateKey, runKey, queueKey}, func(tx *redis.Tx) error {
		snapshot, ok, err := loadRedisSnapshot(ctx, tx, stateKey)
		if err != nil {
			return err
		}
		ref, refOK, err := loadRedisRunRef(ctx, tx, runKey)
		if err != nil {
			return err
		}
		if !ok || !refOK || !runMatchesHandle(snapshot.CurrentRunView, handle) || ref.FencingToken != handle.FencingToken || ref.OwnerID != handle.OwnerID || ref.Generation != handle.Generation || !isActiveRunStatus(snapshot.CurrentRunView.Status) {
			return ErrRunOwnershipLost
		}
		state, err := loadRedisJSON[steerQueueState](ctx, tx, queueKey)
		if err != nil {
			return err
		}
		for _, existing := range state.Items {
			if existing.Status == QueueClaimed && existing.Claim != nil && existing.Claim.RunID == handle.RunID {
				item, claim, claimed = cloneSteerItem(existing), *existing.Claim, true
				return nil
			}
		}
		best := -1
		for i := range state.Items {
			if state.Items[i].Status == QueueAccepted && state.Items[i].TargetRunID == handle.RunID && (best < 0 || state.Items[i].Position < state.Items[best].Position) {
				best = i
			}
		}
		if best < 0 && !sealIfEmpty {
			return nil
		}
		now, err := tx.Time(ctx).Result()
		if err != nil {
			return err
		}
		if best < 0 {
			state.ClosedRunID = handle.RunID
			state.UpdatedAt = now.UTC()
			return storeRedisJSON(ctx, tx, queueKey, state, b.stateTTL)
		}
		claim = SteerClaimRef{ItemID: state.Items[best].ID, RunID: handle.RunID, OwnerID: handle.OwnerID, Generation: handle.Generation, FencingToken: handle.FencingToken, ClaimToken: uuid.NewString()}
		state.Items[best].Status = QueueClaimed
		state.Items[best].Claim = &claim
		state.UpdatedAt = now.UTC()
		item, claimed = cloneSteerItem(state.Items[best]), true
		return storeRedisJSON(ctx, tx, queueKey, state, b.stateTTL)
	})
	return item, claim, claimed, err
}

func (b *RedisBackend) ApplySteer(ctx context.Context, key Key, ref SteerClaimRef) error {
	if err := validateSteerClaim(key, ref); err != nil {
		return err
	}
	stateKey, runKey, queueKey := b.stateKey(key), b.runKey(key, ref.RunID), b.steerQueueKey(key)
	_, err := redisMutateWithKeys(ctx, b, []string{stateKey, runKey, queueKey}, queueKey, func(tx *redis.Tx, state *steerQueueState, now time.Time) (struct{}, error) {
		snapshot, ok, err := loadRedisSnapshot(ctx, tx, stateKey)
		if err != nil {
			return struct{}{}, err
		}
		run, runOK, err := loadRedisRunRef(ctx, tx, runKey)
		if err != nil {
			return struct{}{}, err
		}
		if !ok || !runOK || !runMatchesSteerClaim(snapshot.CurrentRunView, ref) || run.FencingToken != ref.FencingToken || run.OwnerID != ref.OwnerID || run.Generation != ref.Generation {
			return struct{}{}, ErrRunOwnershipLost
		}
		for i := range state.Items {
			claim := state.Items[i].Claim
			if state.Items[i].ID == ref.ItemID && state.Items[i].Status == QueueClaimed && claim != nil && *claim == ref {
				state.Items[i].Status = QueueApplied
				state.UpdatedAt = now
				return struct{}{}, nil
			}
		}
		return struct{}{}, ErrQueueInvalidReference
	})
	return err
}

func (b *RedisBackend) ReleaseSteer(ctx context.Context, key Key, ref SteerClaimRef) error {
	if err := validateSteerClaim(key, ref); err != nil {
		return err
	}
	stateKey, runKey, queueKey := b.stateKey(key), b.runKey(key, ref.RunID), b.steerQueueKey(key)
	_, err := redisMutateWithKeys(ctx, b, []string{stateKey, runKey, queueKey}, queueKey, func(tx *redis.Tx, state *steerQueueState, now time.Time) (struct{}, error) {
		snapshot, ok, err := loadRedisSnapshot(ctx, tx, stateKey)
		if err != nil {
			return struct{}{}, err
		}
		run, runOK, err := loadRedisRunRef(ctx, tx, runKey)
		if err != nil {
			return struct{}{}, err
		}
		if !ok || !runOK || !runMatchesSteerClaim(snapshot.CurrentRunView, ref) || run.FencingToken != ref.FencingToken || run.OwnerID != ref.OwnerID || run.Generation != ref.Generation {
			return struct{}{}, ErrRunOwnershipLost
		}
		for i := range state.Items {
			claim := state.Items[i].Claim
			if state.Items[i].ID == ref.ItemID && state.Items[i].Status == QueueClaimed && claim != nil && *claim == ref {
				state.Items[i].Status = QueueAccepted
				state.Items[i].Claim = nil
				state.UpdatedAt = now
				return struct{}{}, nil
			}
		}
		return struct{}{}, ErrQueueInvalidReference
	})
	return err
}

func (b *RedisBackend) ClaimNextFollowUp(ctx context.Context, key Key, triggerRunID string) (FollowUpItem, FollowUpClaimRef, bool, error) {
	if err := b.ensureQueueOpen(); err != nil {
		return FollowUpItem{}, FollowUpClaimRef{}, false, err
	}
	if err := validateQueueKey(key); err != nil {
		return FollowUpItem{}, FollowUpClaimRef{}, false, err
	}
	triggerRunID = strings.TrimSpace(triggerRunID)
	if triggerRunID == "" {
		return FollowUpItem{}, FollowUpClaimRef{}, false, ErrQueueInvalidReference
	}
	type result struct {
		item    FollowUpItem
		claim   FollowUpClaimRef
		claimed bool
	}
	claimed, err := redisMutate(ctx, b, b.followUpQueueKey(key), func(state *followUpQueueState, now time.Time) (result, error) {
		if state.TerminalClaims == nil {
			state.TerminalClaims = make(map[string]string)
		}
		if itemID := state.TerminalClaims[triggerRunID]; itemID != "" {
			for _, item := range state.Items {
				if string(item.ID) == itemID && item.Status == QueueClaimed && item.Claim != nil {
					return result{item: cloneFollowUpItem(item), claim: *item.Claim, claimed: true}, nil
				}
			}
			return result{}, nil
		}
		best := -1
		for i := range state.Items {
			if state.Items[i].Status == QueueAccepted && (best < 0 || state.Items[i].Position < state.Items[best].Position) {
				best = i
			}
		}
		if best < 0 {
			return result{}, nil
		}
		claim := FollowUpClaimRef{ItemID: state.Items[best].ID, TriggerRunID: triggerRunID, ClaimToken: uuid.NewString()}
		state.Items[best].Status = QueueClaimed
		state.Items[best].Claim = &claim
		state.TerminalClaims[triggerRunID] = string(state.Items[best].ID)
		state.UpdatedAt = now
		return result{item: cloneFollowUpItem(state.Items[best]), claim: claim, claimed: true}, nil
	})
	return claimed.item, claimed.claim, claimed.claimed, err
}

func (b *RedisBackend) ApplyFollowUp(ctx context.Context, key Key, ref FollowUpClaimRef) error {
	if err := validateFollowUpClaim(key, ref); err != nil {
		return err
	}
	_, err := redisMutate(ctx, b, b.followUpQueueKey(key), func(state *followUpQueueState, now time.Time) (struct{}, error) {
		for i := range state.Items {
			claim := state.Items[i].Claim
			if state.Items[i].ID == ref.ItemID && state.Items[i].Status == QueueClaimed && claim != nil && *claim == ref {
				state.Items[i].Status = QueueApplied
				state.UpdatedAt = now
				return struct{}{}, nil
			}
		}
		return struct{}{}, ErrQueueInvalidReference
	})
	return err
}

func (b *RedisBackend) ReleaseFollowUp(ctx context.Context, key Key, ref FollowUpClaimRef) error {
	if err := validateFollowUpClaim(key, ref); err != nil {
		return err
	}
	_, err := redisMutate(ctx, b, b.followUpQueueKey(key), func(state *followUpQueueState, now time.Time) (struct{}, error) {
		for i := range state.Items {
			claim := state.Items[i].Claim
			if state.Items[i].ID == ref.ItemID && state.Items[i].Status == QueueClaimed && claim != nil && *claim == ref {
				state.Items[i].Status = QueueAccepted
				state.Items[i].Claim = nil
				delete(state.TerminalClaims, ref.TriggerRunID)
				state.UpdatedAt = now
				return struct{}{}, nil
			}
		}
		return struct{}{}, ErrQueueInvalidReference
	})
	return err
}

func (b *RedisBackend) steerQueueKey(key Key) string {
	return b.keyPrefix + "steer_queue:" + key.String()
}

func (b *RedisBackend) followUpQueueKey(key Key) string {
	return b.keyPrefix + "follow_up_queue:" + key.String()
}

var _ LiveQueueBackend = (*RedisBackend)(nil)
