package sessionruntime

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func liveQueueFixture(t *testing.T) (*MemoryBackend, Key, RunHandle) {
	t.Helper()
	b := NewMemoryBackend()
	key := Key{BotID: "bot", SessionID: "session"}
	_, _, err := b.Update(context.Background(), key, func(snapshot Snapshot, _ bool) (Snapshot, bool, error) {
		snapshot.BotID, snapshot.SessionID = key.BotID, key.SessionID
		snapshot.CurrentRunView = &CurrentRunView{RunID: "run-1", TurnID: "turn-1", Generation: "gen-1", OwnerID: "owner-1", Status: RunStatusRunning}
		return snapshot, true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	handle := RunHandle{BotID: key.BotID, SessionID: key.SessionID, RunID: "run-1", OwnerID: "owner-1", Generation: "gen-1", FencingToken: 1}
	return b, key, handle
}

func TestMemoryLiveQueuesAreIndependentAndFIFO(t *testing.T) {
	b, key, _ := liveQueueFixture(t)
	ctx := context.Background()
	s1, err := b.EnqueueSteer(ctx, key, "s1", "i1", []byte("one"))
	if err != nil {
		t.Fatal(err)
	}
	s2, err := b.EnqueueSteer(ctx, key, "s2", "i2", []byte("two"))
	if err != nil {
		t.Fatal(err)
	}
	f1, err := b.EnqueueFollowUp(ctx, key, "f1", "i1", []byte("follow"))
	if err != nil {
		t.Fatal(err)
	}
	if s1.Position >= s2.Position || s1.ID == SteerItemID(f1.ID) {
		t.Fatalf("unexpected independent positions or ids: %#v %#v %#v", s1, s2, f1)
	}
	steers, follows, err := b.PendingQueues(ctx, key, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(steers) != 2 || string(steers[0].Payload) != "one" || string(steers[1].Payload) != "two" {
		t.Fatalf("steer FIFO = %#v", steers)
	}
	if len(follows) != 1 || string(follows[0].Payload) != "follow" {
		t.Fatalf("follow-up queue = %#v", follows)
	}
}

func TestMemoryLiveQueueAcceptedOnlyMutationAndReplay(t *testing.T) {
	b, key, handle := liveQueueFixture(t)
	ctx := context.Background()
	item, err := b.EnqueueSteer(ctx, key, "s1", "invoke-1", []byte("one"))
	if err != nil {
		t.Fatal(err)
	}
	replay, err := b.EnqueueSteer(ctx, key, "different-id", "invoke-1", []byte("one"))
	if err != nil || replay.ID != item.ID {
		t.Fatalf("replay = %#v, err = %v", replay, err)
	}
	if _, err := b.EnqueueSteer(ctx, key, "s2", "invoke-1", []byte("changed")); !errors.Is(err, ErrQueueInvocationConflict) {
		t.Fatalf("conflicting replay error = %v", err)
	}
	if _, err := b.UpdateSteer(ctx, key, item.ID, nil); !errors.Is(err, ErrQueueInvalidReference) {
		t.Fatalf("empty update error = %v", err)
	}
	_, claim, ok, err := b.ClaimNextSteer(ctx, handle, false)
	if err != nil || !ok {
		t.Fatalf("claim = %#v, %v, %v", item, err, ok)
	}
	if _, err := b.UpdateSteer(ctx, key, item.ID, []byte("changed")); !errors.Is(err, ErrQueueNotPending) {
		t.Fatalf("claimed update error = %v", err)
	}
	if err := b.ApplySteer(ctx, key, claim); err != nil {
		t.Fatal(err)
	}
	if err := b.CancelSteer(ctx, key, item.ID); !errors.Is(err, ErrQueueNotPending) {
		t.Fatalf("applied cancel error = %v", err)
	}
}

func TestMemoryLiveQueueReorderOnlyAccepted(t *testing.T) {
	b, key, handle := liveQueueFixture(t)
	ctx := context.Background()
	for _, id := range []string{"s1", "s2", "s3"} {
		if _, err := b.EnqueueSteer(ctx, key, id, "invoke-"+id, []byte(id)); err != nil {
			t.Fatal(err)
		}
	}
	_, claim, ok, err := b.ClaimNextSteer(ctx, handle, false)
	if err != nil || !ok {
		t.Fatal(err)
	}
	items, err := b.ReorderSteer(ctx, key, SteerPendingRef{ItemID: "s3"}, SteerPendingRef{ItemID: "s2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].ID != "s3" || items[1].ID != "s2" {
		t.Fatalf("reordered pending items = %#v", items)
	}
	if _, err := b.ReorderSteer(ctx, key, SteerPendingRef{ItemID: claim.ItemID}, SteerPendingRef{ItemID: "s2"}); !errors.Is(err, ErrQueueNotPending) {
		t.Fatalf("claimed reorder error = %v", err)
	}
}

func TestMemoryLiveQueueClaimFencingAndSingleWinner(t *testing.T) {
	b, key, handle := liveQueueFixture(t)
	ctx := context.Background()
	item, err := b.EnqueueSteer(ctx, key, "s1", "invoke", []byte("one"))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	claims := make(chan SteerClaimRef, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, claim, ok, err := b.ClaimNextSteer(ctx, handle, false)
			if err == nil && ok {
				claims <- claim
			}
		}()
	}
	wg.Wait()
	close(claims)
	var claim SteerClaimRef
	winners := 0
	for got := range claims {
		if claim.ClaimToken != "" && got.ClaimToken != claim.ClaimToken {
			t.Fatal("two distinct claim winners")
		}
		claim = got
		winners++
	}
	if claim.ClaimToken == "" || winners != 2 {
		t.Fatal("no claim winner")
	}
	stale := claim
	stale.OwnerID = "old-owner"
	if err := b.ApplySteer(ctx, key, stale); !errors.Is(err, ErrRunOwnershipLost) {
		t.Fatalf("stale apply error = %v", err)
	}
	if err := b.ReleaseSteer(ctx, key, claim); err != nil {
		t.Fatal(err)
	}
	if pending, _, err := b.PendingQueues(ctx, key, 0); err != nil || len(pending) != 1 || pending[0].ID != item.ID {
		t.Fatalf("released claim not pending: %#v, %v", pending, err)
	}
}

func TestMemoryFollowUpClaimReplayAndRelease(t *testing.T) {
	b, key, _ := liveQueueFixture(t)
	ctx := context.Background()
	item, err := b.EnqueueFollowUp(ctx, key, "f1", "invoke-f", []byte("follow"))
	if err != nil {
		t.Fatal(err)
	}
	claimed, claim, ok, err := b.ClaimNextFollowUp(ctx, key, "run-1")
	if err != nil || !ok || claimed.ID != item.ID {
		t.Fatalf("claim = %#v, %#v, %v, %v", claimed, claim, ok, err)
	}
	replayed, replayClaim, ok, err := b.ClaimNextFollowUp(ctx, key, "run-1")
	if err != nil || !ok || replayed.ID != item.ID || replayClaim != claim {
		t.Fatalf("claim replay = %#v, %#v, %v, %v", replayed, replayClaim, ok, err)
	}
	if err := b.ReleaseFollowUp(ctx, key, claim); err != nil {
		t.Fatal(err)
	}
	if _, _, ok, err := b.ClaimNextFollowUp(ctx, key, "run-2"); err != nil || !ok {
		t.Fatalf("released follow-up was not claimable: %v, %v", ok, err)
	}
}

func TestMemoryFollowUpPromotionKeepsQueueIdentitiesSeparate(t *testing.T) {
	b, key, _ := liveQueueFixture(t)
	ctx := context.Background()
	follow, err := b.EnqueueFollowUp(ctx, key, "f1", "invoke-f", []byte("follow"))
	if err != nil {
		t.Fatal(err)
	}

	promoted, err := b.PromoteFollowUpToSteer(ctx, key, FollowUpPendingRef{ItemID: follow.ID})
	if err != nil {
		t.Fatal(err)
	}
	if promoted.Steer.ID == "" || string(promoted.Steer.ID) == string(follow.ID) {
		t.Fatalf("promotion reused follow-up identity: follow=%q steer=%q", follow.ID, promoted.Steer.ID)
	}
	replay, err := b.PromoteFollowUpToSteer(ctx, key, FollowUpPendingRef{ItemID: follow.ID})
	if err != nil {
		t.Fatal(err)
	}
	if replay.Steer.ID != promoted.Steer.ID {
		t.Fatalf("promotion replay created a second steer: first=%q replay=%q", promoted.Steer.ID, replay.Steer.ID)
	}
	steers, follows, err := b.PendingQueues(ctx, key, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(steers) != 1 || steers[0].ID != promoted.Steer.ID || len(follows) != 0 {
		t.Fatalf("promoted queues = steers:%#v follows:%#v", steers, follows)
	}
}
