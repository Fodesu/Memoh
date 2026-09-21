package sessionruntime

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRecordPersistedTurnSurvivesFinish(t *testing.T) {
	t.Parallel()
	fixture := newAdmitFixture(t)
	admission, err := fixture.manager.Admit(context.Background(), fixture.input("inv-persisted-turn", `{"text":"hi"}`))
	if err != nil {
		t.Fatal(err)
	}
	turn := PersistedTurnView{TurnID: admission.TurnID}
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
	if got := *snapshot.CurrentRunView.PersistedTurn; got != turn {
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
	if run.PersistedTurn == nil || run.PersistedTurn.TurnID != turn.TurnID {
		t.Fatalf("terminal view lost the persisted turn: %+v", run.PersistedTurn)
	}
}

// The auditor is the safety net for a persistence path that forgot to record:
// it sees every failed finish without a persisted turn, and nothing else.
func TestFinishRunAuditsFailedRunsWithoutPersistedTurn(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		record  bool
		status  string
		audited bool
	}{
		{name: "errored without record", record: false, status: RunStatusErrored, audited: true},
		{name: "errored with record", record: true, status: RunStatusErrored, audited: false},
		{name: "completed without record", record: false, status: RunStatusCompleted, audited: false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fixture := newAdmitFixture(t)
			admission, err := fixture.manager.Admit(context.Background(), fixture.input("inv-audit-"+tc.name, `{"text":"hi"}`))
			if err != nil {
				t.Fatal(err)
			}
			audited := make(chan CurrentRunView, 1)
			fixture.manager.SetPersistedTurnAuditor(func(_ context.Context, handle RunHandle, run CurrentRunView) {
				if handle.RunID != admission.RunID {
					t.Errorf("audited handle run = %q, want %q", handle.RunID, admission.RunID)
				}
				audited <- run
			})
			if tc.record {
				if err := fixture.manager.RecordPersistedTurn(context.Background(), admission.Handle, PersistedTurnView{TurnID: admission.TurnID}); err != nil {
					t.Fatal(err)
				}
			}
			message := ""
			if tc.status == RunStatusErrored {
				message = "agent.response_timeout"
			}
			if err := fixture.manager.FinishRun(context.Background(), admission.Handle, tc.status, message); err != nil {
				t.Fatal(err)
			}
			select {
			case run := <-audited:
				if !tc.audited {
					t.Fatalf("auditor ran for %s: %+v", tc.name, run)
				}
				if run.TurnID != admission.TurnID || run.Status != RunStatusErrored || run.PersistedTurn != nil {
					t.Fatalf("audited view = %+v", run)
				}
			case <-time.After(2 * time.Second):
				if tc.audited {
					t.Fatalf("auditor did not run for %s", tc.name)
				}
			}
		})
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
