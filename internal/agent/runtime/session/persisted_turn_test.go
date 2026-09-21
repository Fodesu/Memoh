package sessionruntime

import (
	"context"
	"errors"
	"testing"
)

func TestRecordPersistedTurnSurvivesFinish(t *testing.T) {
	t.Parallel()
	fixture := newAdmitFixture(t)
	admission, err := fixture.manager.Admit(context.Background(), fixture.input("inv-persisted-turn", `{"text":"hi"}`))
	if err != nil {
		t.Fatal(err)
	}
	position := int64(3)
	turn := PersistedTurnView{
		TurnID:             admission.TurnID,
		Position:           &position,
		RequestMessageID:   "user-1",
		AssistantMessageID: "assistant-1",
	}
	if err := fixture.manager.RecordPersistedTurn(context.Background(), admission.Handle, turn); err != nil {
		t.Fatalf("RecordPersistedTurn() error = %v", err)
	}
	snapshot, err := fixture.manager.Snapshot(context.Background(), testBotID, testSessionID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.CurrentRunView == nil || snapshot.CurrentRunView.PersistedTurn == nil {
		t.Fatalf("live view = %+v, want persisted turn recorded", snapshot.CurrentRunView)
	}
	if got := *snapshot.CurrentRunView.PersistedTurn; got.TurnID != turn.TurnID || got.RequestMessageID != "user-1" ||
		got.AssistantMessageID != "assistant-1" || got.Position == nil || *got.Position != 3 {
		t.Fatalf("persisted turn = %+v, want %+v", got, turn)
	}

	if err := fixture.manager.FinishRun(context.Background(), admission.Handle, RunStatusErrored, "agent.response_timeout"); err != nil {
		t.Fatal(err)
	}
	snapshot, err = fixture.manager.Snapshot(context.Background(), testBotID, testSessionID)
	if err != nil {
		t.Fatal(err)
	}
	run := snapshot.CurrentRunView
	if run == nil || run.Status != RunStatusErrored {
		t.Fatalf("terminal view = %+v, want errored", run)
	}
	if run.PersistedTurn == nil || run.PersistedTurn.TurnID != turn.TurnID || run.PersistedTurn.AssistantMessageID != "assistant-1" {
		t.Fatalf("terminal view lost the persisted turn: %+v", run.PersistedTurn)
	}
}

func TestRecordPersistedTurnRejectsStaleHandle(t *testing.T) {
	t.Parallel()
	fixture := newAdmitFixture(t)
	admission, err := fixture.manager.Admit(context.Background(), fixture.input("inv-persisted-turn-stale", `{"text":"hi"}`))
	if err != nil {
		t.Fatal(err)
	}
	// A handle from a superseded run names a run id the live view no longer
	// shows; the local backend has no fencing token to reject, so run identity
	// is what protects the view.
	stale := admission.Handle
	stale.RunID = "run-superseded"
	err = fixture.manager.RecordPersistedTurn(context.Background(), stale, PersistedTurnView{TurnID: admission.TurnID})
	if !errors.Is(err, ErrRunOwnershipLost) {
		t.Fatalf("RecordPersistedTurn(stale) error = %v, want ErrRunOwnershipLost", err)
	}
	snapshot, err := fixture.manager.Snapshot(context.Background(), testBotID, testSessionID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.CurrentRunView == nil || snapshot.CurrentRunView.PersistedTurn != nil {
		t.Fatalf("live view = %+v, want no persisted turn", snapshot.CurrentRunView)
	}
}

func TestRecordPersistedTurnRequiresTurnID(t *testing.T) {
	t.Parallel()
	fixture := newAdmitFixture(t)
	admission, err := fixture.manager.Admit(context.Background(), fixture.input("inv-persisted-turn-empty", `{"text":"hi"}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.manager.RecordPersistedTurn(context.Background(), admission.Handle, PersistedTurnView{TurnID: "  "}); err == nil {
		t.Fatal("RecordPersistedTurn(empty turn id): want error")
	}
}
