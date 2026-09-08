//go:build integration

package acceptance

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func enqueueTestInput(t *testing.T, fixture acceptanceFixture, sessionID, kind, text string) string {
	t.Helper()
	var item struct {
		ID string `json:"item_id"`
	}
	if err := fixture.api.request(http.MethodPost, "/bots/"+fixture.botID+"/sessions/"+sessionID+"/"+kind+"-queue", map[string]string{"invocation_id": uniqueMarker("queue"), "text": text}, &item, http.StatusAccepted); err != nil {
		t.Fatal(err)
	}
	if item.ID == "" {
		t.Fatal("queue response has no item identity")
	}
	return item.ID
}

func TestQueueFollowUpsPreserveRepeatedReorderAndDrain(t *testing.T) {
	fixture := requireFixture(t, false)
	prepareFakeModel(t)
	sessionID := mustCreateSession(t, fixture, "queue-drain")
	marker := uniqueMarker("queue-origin")
	invocation := "invocation-" + marker
	conn := mustDial(t, loadEnvironment().primaryURL, fixture)
	defer closeWebSocket(conn)
	mustSubscribeAndReadSnapshot(t, conn, sessionID)
	_, admitted := mustSendAndAccept(t, fixture, conn, sessionID, invocation, directiveMode(marker, 1, 0, "block"))
	if !globalFakeModel.WaitRequestCount(marker, 1, 5*time.Second) {
		t.Fatal("origin never reached model")
	}
	defer globalFakeModel.Release(marker)
	ids := make([]string, 3)
	markers := make([]string, 3)
	for i := range ids {
		markers[i] = uniqueMarker(fmt.Sprintf("follow-%d", i))
		ids[i] = enqueueTestInput(t, fixture, sessionID, "follow-up", directive(markers[i], 2, 5))
	}
	endpoint := "/bots/" + fixture.botID + "/sessions/" + sessionID + "/follow-up-queue/reorder"
	for _, move := range [][2]int{{2, 0}, {1, 0}} {
		var response any
		if err := fixture.api.request(http.MethodPut, endpoint, map[string]any{"item": map[string]string{"item_id": ids[move[0]]}, "before": map[string]string{"item_id": ids[move[1]]}}, &response, http.StatusOK); err != nil {
			t.Fatal(err)
		}
	}
	globalFakeModel.Release(marker)
	mustReadRunTerminal(t, conn, admitted.RunID)
	var priorPosition int64 = -1
	for _, i := range []int{2, 1, 0} {
		completed := mustWaitRunState(t, sessionID, "follow-up:"+ids[i], func(run sessionRunRecord) bool { return run.State == "completed" })
		if completed.TurnPosition <= priorPosition {
			t.Fatalf("queue order regressed: position %d after %d", completed.TurnPosition, priorPosition)
		}
		priorPosition = completed.TurnPosition
		assertTerminalHistory(t, completed)
		if count := globalFakeModel.RequestCount(markers[i]); count != 1 {
			t.Fatalf("follow-up %d executed %d times", i, count)
		}
	}
	var queues struct {
		FollowUp []any `json:"follow_up"`
	}
	if err := fixture.api.request(http.MethodGet, "/bots/"+fixture.botID+"/sessions/"+sessionID+"/queue", nil, &queues, http.StatusOK); err != nil {
		t.Fatal(err)
	}
	if len(queues.FollowUp) != 0 {
		t.Fatalf("queue did not drain: %v", queues.FollowUp)
	}
}

func TestQueueSteerDecisionKeepsInputAndHistory(t *testing.T) {
	testQueueSteerDecisionKeepsInputAndHistory(t, false)
}

func TestQueueSteerDecisionSurvivesOwnerRestart(t *testing.T) {
	if !envBool(crashEnv) {
		t.Skipf("set %s=1 only against the isolated acceptance topology", crashEnv)
	}
	testQueueSteerDecisionKeepsInputAndHistory(t, true)
}

func testQueueSteerDecisionKeepsInputAndHistory(t *testing.T, restartOwner bool) {
	t.Helper()
	fixture := requireFixture(t, restartOwner)
	prepareFakeModel(t)
	sessionID := mustCreateSession(t, fixture, "steer-decision")
	marker := uniqueMarker("steer-origin")
	invocation := "invocation-" + marker
	conn := mustDial(t, loadEnvironment().primaryURL, fixture)
	defer closeWebSocket(conn)
	mustSubscribeAndReadSnapshot(t, conn, sessionID)
	_, admitted := mustSendAndAccept(t, fixture, conn, sessionID, invocation, directiveMode(marker, 1, 0, "block"))
	if !globalFakeModel.WaitRequestCount(marker, 1, 5*time.Second) {
		t.Fatal("origin never reached model")
	}
	defer globalFakeModel.Release(marker)
	steerMarker := uniqueMarker("steer-decision")
	steerText := directiveMode(steerMarker, 2, 5, "ask_user") + " ask after steering"
	enqueueTestInput(t, fixture, sessionID, "steer", steerText)
	globalFakeModel.Release(marker)
	waiting := mustWaitRunState(t, sessionID, invocation, func(run sessionRunRecord) bool { return run.State == "waiting_decision" })
	decision := mustPendingUserInput(t, waiting)
	answers, err := firstDecisionAnswer(decision.UIPayload)
	if err != nil {
		t.Fatal(err)
	}
	afterDecision := directive(uniqueMarker("after-decision"), 2, 5) + " continue after the answer"
	enqueueTestInput(t, fixture, sessionID, "steer", afterDecision)
	if restartOwner {
		env := loadEnvironment()
		if err := killAndRestartPrimary(env); err != nil {
			t.Fatalf("restart owner after steered decision: %v", err)
		}
		closeWebSocket(conn)
		recovered := mustWaitRunState(t, sessionID, invocation, func(run sessionRunRecord) bool {
			return run.State == "waiting_decision" && run.FencingToken > waiting.FencingToken
		})
		pending := mustPendingUserInput(t, recovered)
		if pending.ID != decision.ID || recovered.RunID != admitted.RunID || recovered.TurnID != waiting.TurnID {
			t.Fatalf("recovery changed durable control identity: decision=%+v run=%+v", pending, recovered)
		}
		conn = mustDial(t, peerURL(env), fixture)
		defer closeWebSocket(conn)
		mustSubscribeAndReadSnapshot(t, conn, sessionID)
	}
	control := "control-" + uniqueMarker("steer-answer")
	if err := sendUserInputResponse(conn, sessionID, admitted.RunID, decision.ID, control, answers); err != nil {
		t.Fatal(err)
	}
	ack := mustReadControlAck(t, conn, control)
	if !ack.Applied {
		t.Fatalf("steer decision was not accepted: %+v", ack)
	}
	mustReadRunCompleted(t, conn, admitted.RunID)
	completed := mustWaitRunState(t, sessionID, invocation, func(run sessionRunRecord) bool { return run.State == "completed" })
	history, err := fixture.api.history(fixture.botID, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if !historyContainsRoleText(history, "user", steerText) {
		t.Fatalf("applied steer input missing from durable history: %#v", history)
	}
	if !historyContainsRoleText(history, "user", afterDecision) {
		t.Fatalf("post-decision steer missing from history: %#v", history)
	}
	if completed.RunID != admitted.RunID {
		t.Fatal("steer created a second run")
	}
	ctx, cancel := context.WithTimeout(context.Background(), databaseTimeout)
	defer cancel()
	if status, err := requireLedger(t).userInputStatus(ctx, decision.ID); err != nil || status != "submitted" {
		t.Fatalf("decision status=%s err=%v", status, err)
	}
	// Presence alone would miss duplicate input or continuation output written
	// under the root turn after owner recovery. Inspect explicit DB membership.
	rows, err := requireLedger(t).pool.Query(ctx, `
SELECT turn_id::text, min(turn_position),
       count(*) FILTER (WHERE role = 'user'),
       count(*) FILTER (WHERE role = 'assistant'),
       count(*) FILTER (WHERE role = 'tool'),
       count(*) FILTER (WHERE run_id IS DISTINCT FROM $2::uuid)
FROM bot_history_messages
WHERE session_id = $1::uuid
GROUP BY turn_id
ORDER BY min(turn_position)`, sessionID, admitted.RunID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	segments := 0
	for rows.Next() {
		var turnID string
		var position int64
		var users, assistants, tools, wrongRun int
		if err := rows.Scan(&turnID, &position, &users, &assistants, &tools, &wrongRun); err != nil {
			t.Fatal(err)
		}
		segments++
		wantTools := 0
		if segments == 2 {
			wantTools = 1
		}
		if users != 1 || assistants == 0 || tools != wantTools || wrongRun != 0 || position != int64(segments) {
			t.Errorf("segment %d: turn=%s position=%d users=%d assistants=%d tools=%d wrong_run=%d", segments, turnID, position, users, assistants, tools, wrongRun)
		}
		if (segments == 1) != (turnID == completed.TurnID) {
			t.Errorf("segment %d incorrectly uses root control turn %s", segments, completed.TurnID)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if segments != 3 {
		t.Fatalf("got %d durable turn segments, want origin + steer + post-decision steer", segments)
	}
}
