package orchestrationbus

import "testing"

func TestRunEventSubjectSanitizesTokens(t *testing.T) {
	cases := []struct {
		name      string
		runID     string
		eventType string
		want      string
	}{
		{name: "happy", runID: "run-1", eventType: "task.created", want: "memoh.orch.run.event.run-1.task_created"},
		{name: "empty event", runID: "run-1", eventType: "", want: "memoh.orch.run.event.run-1.unknown"},
		{name: "wildcard sanitised", runID: "run.*>", eventType: ">", want: "memoh.orch.run.event.run___.unknown"},
		{name: "trim whitespace", runID: "  run  ", eventType: "  ev  ", want: "memoh.orch.run.event.run.ev"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RunEventSubject(tc.runID, tc.eventType)
			if got != tc.want {
				t.Fatalf("subject mismatch: want %q, got %q", tc.want, got)
			}
		})
	}
}

func TestAttemptFactSubjectSanitizesTokens(t *testing.T) {
	got := AttemptFactSubject("run.1", "att 1", "started")
	want := "memoh.orch.attempt.fact.run_1.att_1.started"
	if got != want {
		t.Fatalf("subject mismatch: want %q, got %q", want, got)
	}
}

func TestRunEventRunSubjectIsWildcard(t *testing.T) {
	if got := RunEventRunSubject("run-1"); got != "memoh.orch.run.event.run-1.>" {
		t.Fatalf("unexpected subject: %s", got)
	}
}
