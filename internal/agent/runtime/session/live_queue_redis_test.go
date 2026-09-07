package sessionruntime

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

// Redis is optional in the normal unit-test environment. When configured, this
// contract deliberately uses two backend instances with one prefix: a queue
// operation that only works inside one process is not a valid Redis backend.
func TestRedisLiveQueueContractOptional(t *testing.T) {
	redisURL := os.Getenv("MEMOH_TEST_REDIS_URL")
	if redisURL == "" {
		redisURL = os.Getenv("MEMOH_TEST_VALKEY_URL")
	}
	if redisURL == "" {
		if os.Getenv("MEMOH_TEST_DISTRIBUTED_REQUIRED") == "1" {
			t.Fatal("distributed queue contract requires MEMOH_TEST_REDIS_URL or MEMOH_TEST_VALKEY_URL")
		}
		t.Skip("set MEMOH_TEST_REDIS_URL or MEMOH_TEST_VALKEY_URL to run the Redis live queue contract")
	}

	prefix := uniqueRuntimeBackendPrefix("live-queue")
	newBackend := func() *RedisBackend {
		backend, err := NewRedisBackend(context.Background(), RedisOptions{
			URL: redisURL, KeyPrefix: prefix, StateTTL: time.Minute,
		})
		if err != nil {
			t.Fatalf("redis backend: %v", err)
		}
		t.Cleanup(func() { _ = backend.Close() })
		return backend
	}
	first, second := newBackend(), newBackend()
	ctx := context.Background()
	key := Key{BotID: "bot-live-queue", SessionID: "session-live-queue"}
	ref := RunRef{
		BotID: key.BotID, SessionID: key.SessionID, RunID: "run-live-queue",
		OwnerID: "owner-live-queue", Generation: "generation-live-queue", FencingToken: 41,
	}
	_, changed, err := first.StartRun(ctx, key, ref, func(snapshot Snapshot, _ bool) (Snapshot, bool, error) {
		snapshot.BotID = key.BotID
		snapshot.SessionID = key.SessionID
		snapshot.CurrentRunView = &CurrentRunView{
			RunID: ref.RunID, OwnerID: ref.OwnerID, Generation: ref.Generation,
			Status: RunStatusRunning,
		}
		return snapshot, true, nil
	})
	if err != nil || !changed {
		t.Fatalf("seed active run: changed=%v err=%v", changed, err)
	}
	handle := RunHandle{
		BotID: key.BotID, SessionID: key.SessionID, RunID: ref.RunID,
		OwnerID: ref.OwnerID, Generation: ref.Generation, FencingToken: ref.FencingToken,
	}

	steerOne, err := first.EnqueueSteer(ctx, key, "steer-1", "invoke-steer-1", []byte("one"))
	if err != nil {
		t.Fatalf("enqueue steer: %v", err)
	}
	steerTwo, err := second.EnqueueSteer(ctx, key, "steer-2", "invoke-steer-2", []byte("two"))
	if err != nil {
		t.Fatalf("enqueue second steer: %v", err)
	}
	follow, err := second.EnqueueFollowUp(ctx, key, "follow-1", "invoke-follow-1", []byte("follow"))
	if err != nil {
		t.Fatalf("enqueue follow-up: %v", err)
	}
	if steerOne.Position >= steerTwo.Position || follow.Position != 1 {
		t.Fatalf("independent positions = steer(%d,%d) follow(%d)", steerOne.Position, steerTwo.Position, follow.Position)
	}

	steers, follows, err := first.PendingQueues(ctx, key, 0)
	if err != nil {
		t.Fatalf("list queues from second instance: %v", err)
	}
	if len(steers) != 2 || string(steers[0].Payload) != "one" || string(steers[1].Payload) != "two" {
		t.Fatalf("steer FIFO = %#v", steers)
	}
	if len(follows) != 1 || string(follows[0].Payload) != "follow" {
		t.Fatalf("follow-up queue = %#v", follows)
	}

	if _, err := first.ReorderSteer(ctx, key, SteerPendingRef{ItemID: steerTwo.ID}, SteerPendingRef{ItemID: steerOne.ID}); err != nil {
		t.Fatalf("accepted-only reorder: %v", err)
	}
	steers, _, err = second.PendingQueues(ctx, key, 0)
	if err != nil || len(steers) != 2 || steers[0].ID != steerTwo.ID || steers[1].ID != steerOne.ID {
		t.Fatalf("reordered steer queue = %#v, err=%v", steers, err)
	}

	claimed, claim, ok, err := second.ClaimNextSteer(ctx, handle, false)
	if err != nil || !ok || claimed.ID != steerTwo.ID {
		t.Fatalf("claim steer = %#v, %#v, %v, %v", claimed, claim, ok, err)
	}
	replayed, replayClaim, ok, err := first.ClaimNextSteer(ctx, handle, false)
	if err != nil || !ok || replayed.ID != claimed.ID || replayClaim != claim {
		t.Fatalf("cross-instance claim replay = %#v, %#v, %v, %v", replayed, replayClaim, ok, err)
	}
	stale := claim
	stale.OwnerID = "stale-owner"
	if err := first.ApplySteer(ctx, key, stale); !errors.Is(err, ErrRunOwnershipLost) {
		t.Fatalf("stale steer apply = %v", err)
	}
	if err := first.ApplySteer(ctx, key, claim); err != nil {
		t.Fatalf("apply steer: %v", err)
	}
	if _, err := second.ReorderSteer(ctx, key, SteerPendingRef{ItemID: steerTwo.ID}, SteerPendingRef{ItemID: steerOne.ID}); !errors.Is(err, ErrQueueNotPending) {
		t.Fatalf("claimed/applied reorder = %v", err)
	}
	promotable, err := first.EnqueueFollowUp(ctx, key, "follow-promote", "invoke-follow-promote", []byte("promote"))
	if err != nil {
		t.Fatalf("enqueue promotable follow-up: %v", err)
	}
	promoted, err := second.PromoteFollowUpToSteer(ctx, key, FollowUpPendingRef{ItemID: promotable.ID})
	if err != nil {
		t.Fatalf("promote follow-up: %v", err)
	}
	if promoted.Steer.ID == "" || string(promoted.Steer.ID) == string(promotable.ID) {
		t.Fatalf("promotion reused follow-up identity: follow=%q steer=%q", promotable.ID, promoted.Steer.ID)
	}
	replayedPromotion, err := first.PromoteFollowUpToSteer(ctx, key, FollowUpPendingRef{ItemID: promotable.ID})
	if err != nil || replayedPromotion.Steer.ID != promoted.Steer.ID {
		t.Fatalf("promotion replay = %#v, err=%v", replayedPromotion, err)
	}

	followed, followClaim, ok, err := first.ClaimNextFollowUp(ctx, key, ref.RunID)
	if err != nil || !ok || followed.ID != follow.ID {
		t.Fatalf("claim follow-up = %#v, %#v, %v, %v", followed, followClaim, ok, err)
	}
	replayedFollowed, replayFollowClaim, ok, err := second.ClaimNextFollowUp(ctx, key, ref.RunID)
	if err != nil || !ok || replayedFollowed.ID != follow.ID || replayFollowClaim != followClaim {
		t.Fatalf("cross-instance follow-up replay = %#v, %#v, %v, %v", replayedFollowed, replayFollowClaim, ok, err)
	}
	if err := second.ApplyFollowUp(ctx, key, followClaim); err != nil {
		t.Fatalf("apply follow-up: %v", err)
	}
	if _, _, ok, err := first.ClaimNextFollowUp(ctx, key, "different-terminal-run"); err != nil || ok {
		t.Fatalf("applied follow-up was claimable again: ok=%v err=%v", ok, err)
	}

	// Terminal close from another instance rejects the remaining steers and
	// seals the run while its live snapshot is still active.
	if err := second.CloseSteerRun(ctx, key, ref.RunID); err != nil {
		t.Fatalf("close steer run: %v", err)
	}
	if steers, _, err := first.PendingQueues(ctx, key, 0); err != nil || len(steers) != 0 {
		t.Fatalf("pending steers after close = %#v, err=%v", steers, err)
	}
	if _, err := first.EnqueueSteer(ctx, key, "steer-late", "invoke-steer-late", []byte("late")); !errors.Is(err, ErrQueueNoActiveRun) {
		t.Fatalf("late steer after close = %v, want %v", err, ErrQueueNoActiveRun)
	}
}
