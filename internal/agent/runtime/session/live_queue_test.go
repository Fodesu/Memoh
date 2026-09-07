package sessionruntime

import (
	"context"
	"errors"
	"fmt"
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

func TestMemoryCloseSteerRunRejectsPendingAndClaimedSteers(t *testing.T) {
	b, key, handle := liveQueueFixture(t)
	ctx := context.Background()
	for _, id := range []string{"s1", "s2"} {
		if _, err := b.EnqueueSteer(ctx, key, id, "invoke-"+id, []byte(id)); err != nil {
			t.Fatal(err)
		}
	}
	_, claim, ok, err := b.ClaimNextSteer(ctx, handle, false)
	if err != nil || !ok {
		t.Fatalf("claim = %v, %v", ok, err)
	}
	if err := b.CloseSteerRun(ctx, key, handle.RunID); err != nil {
		t.Fatalf("close steer run: %v", err)
	}
	pending, _, err := b.PendingQueues(ctx, key, 0)
	if err != nil || len(pending) != 0 {
		t.Fatalf("pending after close = %#v, %v", pending, err)
	}
	b.mu.Lock()
	state := b.steerQueues[key.String()]
	b.mu.Unlock()
	if len(state.Items) != 2 {
		t.Fatalf("items after close = %#v", state.Items)
	}
	for _, item := range state.Items {
		if item.Status != QueueRejected || item.ErrorCode != QueueErrorTargetRunNotActive || item.Claim != nil {
			t.Fatalf("closed item = %#v", item)
		}
	}
	if state.ClosedRunID != handle.RunID {
		t.Fatalf("closed run id = %q, want %q", state.ClosedRunID, handle.RunID)
	}
	if err := b.ApplySteer(ctx, key, claim); err == nil {
		t.Fatal("apply after close succeeded")
	}
	// The fixture's live snapshot still shows the run as active: the seal must
	// refuse a late steer on its own.
	if _, err := b.EnqueueSteer(ctx, key, "s3", "invoke-s3", []byte("three")); !errors.Is(err, ErrQueueNoActiveRun) {
		t.Fatalf("late enqueue error = %v, want %v", err, ErrQueueNoActiveRun)
	}
	// Closing a run nobody steered is a no-op.
	if err := b.CloseSteerRun(ctx, Key{BotID: "bot", SessionID: "other"}, "run-9"); err != nil {
		t.Fatalf("close unknown session: %v", err)
	}
}

func TestMemoryLiveQueueCapacityBound(t *testing.T) {
	b, key, _ := liveQueueFixture(t)
	ctx := context.Background()
	for i := 0; i < MaxPendingQueueItems; i++ {
		id := fmt.Sprintf("s%d", i)
		if _, err := b.EnqueueSteer(ctx, key, id, "invoke-"+id, []byte(id)); err != nil {
			t.Fatalf("enqueue %s: %v", id, err)
		}
	}
	if _, err := b.EnqueueSteer(ctx, key, "overflow", "invoke-overflow", []byte("overflow")); !errors.Is(err, ErrQueueCapacityExceeded) {
		t.Fatalf("overflow error = %v, want %v", err, ErrQueueCapacityExceeded)
	}
	replay, err := b.EnqueueSteer(ctx, key, "ignored", "invoke-s0", []byte("s0"))
	if err != nil || replay.ID != "s0" {
		t.Fatalf("replay at capacity = %#v, %v", replay, err)
	}
	// Queues are bounded independently.
	if _, err := b.EnqueueFollowUp(ctx, key, "f1", "invoke-f1", []byte("follow")); err != nil {
		t.Fatalf("follow-up enqueue while steer queue is full: %v", err)
	}
	if err := b.CancelSteer(ctx, key, "s0"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.EnqueueSteer(ctx, key, "overflow", "invoke-overflow", []byte("overflow")); err != nil {
		t.Fatalf("enqueue after cancel freed capacity: %v", err)
	}
}

func TestMemoryLiveQueueCompactsTerminalItems(t *testing.T) {
	b, key, _ := liveQueueFixture(t)
	ctx := context.Background()
	if _, err := b.EnqueueFollowUp(ctx, key, "keep", "invoke-keep", []byte("keep")); err != nil {
		t.Fatal(err)
	}
	total := queueTerminalRetention + 10
	for i := 0; i < total; i++ {
		id := FollowUpItemID(fmt.Sprintf("f%d", i))
		if _, err := b.EnqueueFollowUp(ctx, key, string(id), "invoke-"+string(id), []byte(id)); err != nil {
			t.Fatal(err)
		}
		if err := b.CancelFollowUp(ctx, key, id); err != nil {
			t.Fatal(err)
		}
	}
	b.mu.Lock()
	state := b.followUpQueues[key.String()]
	b.mu.Unlock()
	terminal, keep := 0, false
	ids := make(map[FollowUpItemID]struct{}, len(state.Items))
	for _, item := range state.Items {
		ids[item.ID] = struct{}{}
		switch {
		case item.ID == "keep" && item.Status == QueueAccepted:
			keep = true
		case item.Status.terminal():
			terminal++
		}
	}
	if !keep || terminal != queueTerminalRetention {
		t.Fatalf("compacted queue keep=%v terminal=%d items=%d", keep, terminal, len(state.Items))
	}
	if _, oldest := ids["f0"]; oldest {
		t.Fatal("oldest terminal item survived compaction")
	}
	if _, newest := ids[FollowUpItemID(fmt.Sprintf("f%d", total-1))]; !newest {
		t.Fatal("newest terminal item was compacted away")
	}
}

func TestMemoryClaimNextSteerAdvancesClaimToNewOwner(t *testing.T) {
	b, key, oldHandle := liveQueueFixture(t)
	ctx := context.Background()
	item, err := b.EnqueueSteer(ctx, key, "s1", "invoke-s1", []byte("one"))
	if err != nil {
		t.Fatal(err)
	}
	_, oldClaim, ok, err := b.ClaimNextSteer(ctx, oldHandle, false)
	if err != nil || !ok {
		t.Fatalf("initial claim = %v, %v", ok, err)
	}

	// The parked run is reclaimed by another owner with a newer fencing token.
	newHandle := oldHandle
	newHandle.OwnerID, newHandle.Generation, newHandle.FencingToken = "owner-2", "gen-2", 2
	if _, _, err := b.Update(ctx, key, func(snapshot Snapshot, _ bool) (Snapshot, bool, error) {
		snapshot.CurrentRunView.OwnerID, snapshot.CurrentRunView.Generation = newHandle.OwnerID, newHandle.Generation
		return snapshot, true, nil
	}); err != nil {
		t.Fatal(err)
	}

	reclaimed, newClaim, ok, err := b.ClaimNextSteer(ctx, newHandle, false)
	if err != nil || !ok || reclaimed.ID != item.ID {
		t.Fatalf("reclaim = %#v, %v, %v", reclaimed, ok, err)
	}
	if newClaim.ClaimToken != oldClaim.ClaimToken || newClaim.OwnerID != "owner-2" || newClaim.Generation != "gen-2" || newClaim.FencingToken != 2 {
		t.Fatalf("advanced claim = %#v, old = %#v", newClaim, oldClaim)
	}
	if err := b.ApplySteer(ctx, key, oldClaim); err == nil {
		t.Fatal("previous owner applied a claim it no longer holds")
	}
	if err := b.ApplySteer(ctx, key, newClaim); err != nil {
		t.Fatalf("new owner apply: %v", err)
	}
	if _, _, ok, err := b.ClaimNextSteer(ctx, newHandle, false); err != nil || ok {
		t.Fatalf("applied steer was claimable again: ok=%v err=%v", ok, err)
	}
}

// A run admitted through the memory Manager carries the manager's owner on its
// handle, but the memory backend never records an owner on the live run view.
// The queue must treat that admission as the run's owner; otherwise every step
// commit in single-process deployments fails with ErrRunOwnershipLost.
func TestMemoryLiveQueueClaimsForRealAdmission(t *testing.T) {
	f := newAdmitFixture(t)
	ctx := context.Background()
	admission, err := f.manager.Admit(ctx, f.input("inv-queue", `{"text":"hi"}`))
	if err != nil || !admission.Started {
		t.Fatalf("admit: %+v, %v", admission, err)
	}
	key := Key{BotID: testBotID, SessionID: testSessionID}
	item, err := f.manager.EnqueueSteer(ctx, key, "s1", "invoke-s1", []byte(`{"text":"steer"}`))
	if err != nil {
		t.Fatalf("enqueue steer: %v", err)
	}
	claimed, claim, ok, err := f.manager.ClaimNextSteer(ctx, admission.Handle, false)
	if err != nil || !ok || claimed.ID != item.ID {
		t.Fatalf("claim with the admission handle = %#v, %v, %v", claimed, ok, err)
	}
	if err := f.manager.ApplySteer(ctx, key, claim); err != nil {
		t.Fatalf("apply with the admission handle: %v", err)
	}
	if _, _, ok, err := f.manager.ClaimNextSteer(ctx, admission.Handle, true); err != nil || ok {
		t.Fatalf("seal after apply = ok:%v err:%v", ok, err)
	}
	f.finish(t, admission)
}

func TestMemoryPromoteFollowUpRespectsSteerCapacity(t *testing.T) {
	b, key, _ := liveQueueFixture(t)
	ctx := context.Background()
	for i := 0; i < MaxPendingQueueItems; i++ {
		id := fmt.Sprintf("s%d", i)
		if _, err := b.EnqueueSteer(ctx, key, id, "invoke-"+id, []byte(id)); err != nil {
			t.Fatal(err)
		}
	}
	follow, err := b.EnqueueFollowUp(ctx, key, "f1", "invoke-f1", []byte("follow"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.PromoteFollowUpToSteer(ctx, key, FollowUpPendingRef{ItemID: follow.ID}); !errors.Is(err, ErrQueueCapacityExceeded) {
		t.Fatalf("promotion past capacity = %v, want %v", err, ErrQueueCapacityExceeded)
	}
	if _, follows, err := b.PendingQueues(ctx, key, 0); err != nil || len(follows) != 1 || follows[0].Status != QueueAccepted {
		t.Fatalf("follow-up must stay pending after refused promotion: %#v, %v", follows, err)
	}
}

func TestMemoryFollowUpTerminalClaimSurvivesCompaction(t *testing.T) {
	b, key, _ := liveQueueFixture(t)
	ctx := context.Background()
	item, err := b.EnqueueFollowUp(ctx, key, "f-applied", "invoke-f-applied", []byte("first"))
	if err != nil {
		t.Fatal(err)
	}
	_, claim, ok, err := b.ClaimNextFollowUp(ctx, key, "run-done")
	if err != nil || !ok || claim.ItemID != item.ID {
		t.Fatalf("claim = %#v, %v, %v", claim, ok, err)
	}
	if err := b.ApplyFollowUp(ctx, key, claim); err != nil {
		t.Fatal(err)
	}
	// Push the applied item out of terminal retention.
	for i := 0; i < queueTerminalRetention+5; i++ {
		id := FollowUpItemID(fmt.Sprintf("f%d", i))
		if _, err := b.EnqueueFollowUp(ctx, key, string(id), "invoke-"+string(id), []byte(id)); err != nil {
			t.Fatal(err)
		}
		if err := b.CancelFollowUp(ctx, key, id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := b.EnqueueFollowUp(ctx, key, "f-next", "invoke-f-next", []byte("next")); err != nil {
		t.Fatal(err)
	}
	// A repeated terminal observation for the same run must not claim again.
	if _, _, ok, err := b.ClaimNextFollowUp(ctx, key, "run-done"); err != nil || ok {
		t.Fatalf("repeated trigger claimed a second follow-up: ok=%v err=%v", ok, err)
	}
	if _, _, ok, err := b.ClaimNextFollowUp(ctx, key, "run-later"); err != nil || !ok {
		t.Fatalf("a new terminal run could not claim the pending follow-up: ok=%v err=%v", ok, err)
	}
}
