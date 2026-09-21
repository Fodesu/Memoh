package application

import (
	"context"
	"log/slog"
	"testing"

	sessionruntime "github.com/felinics/memoh/internal/agent/runtime/session"
	messagepkg "github.com/felinics/memoh/internal/chat/message"
)

type persistedTurnRecorder struct {
	handles []sessionruntime.RunHandle
	turns   []sessionruntime.PersistedTurnView
}

func (r *persistedTurnRecorder) record(_ context.Context, handle sessionruntime.RunHandle, turn sessionruntime.PersistedTurnView) error {
	r.handles = append(r.handles, handle)
	r.turns = append(r.turns, turn)
	return nil
}

func persistedTurnRound() []ModelMessage {
	return []ModelMessage{
		{Role: "user", Content: newTextContent("hello")},
		{Role: "assistant", Content: newTextContent("answer")},
	}
}

// The run learns which history turn it wrote at the moment the round lands,
// keyed by the turn the admission allocated: persisted rows do not carry it.
func TestStoreRoundRecordsPersistedTurnOnAdmittedRun(t *testing.T) {
	t.Parallel()
	recorder := &persistedTurnRecorder{}
	service := &Service{
		messageService:      &recordingMessageService{},
		logger:              slog.New(slog.DiscardHandler),
		recordPersistedTurn: recorder.record,
	}
	position := int64(7)
	handle := sessionruntime.RunHandle{BotID: "bot-1", SessionID: "session-1", RunID: "run-1", FencingToken: 4}
	_, err := service.storeRoundWithOptionsResult(t.Context(), ChatRequest{
		BotID:        "bot-1",
		ThreadID:     "session-1",
		Query:        "hello",
		RunHandle:    handle,
		TurnID:       "turn-1",
		TurnPosition: &position,
	}, persistedTurnRound(), "model-1", storeRoundOptions{})
	if err != nil {
		t.Fatalf("storeRoundWithOptionsResult() error = %v", err)
	}
	if len(recorder.turns) != 1 {
		t.Fatalf("recorded turns = %d, want 1", len(recorder.turns))
	}
	if recorder.handles[0] != handle {
		t.Fatalf("recorded handle = %+v, want %+v", recorder.handles[0], handle)
	}
	got := recorder.turns[0]
	if got.TurnID != "turn-1" || got.Position == nil || *got.Position != 7 {
		t.Fatalf("persisted turn identity = %+v, want turn-1 at position 7", got)
	}
	if got.RequestMessageID != "message-id" || got.AssistantMessageID != "message-id" {
		t.Fatalf("persisted turn anchors = %+v, want both persisted row ids", got)
	}
}

func TestStoreRoundSkipsPersistedTurnWithoutRunHandle(t *testing.T) {
	t.Parallel()
	recorder := &persistedTurnRecorder{}
	service := &Service{
		messageService:      &recordingMessageService{},
		logger:              slog.New(slog.DiscardHandler),
		recordPersistedTurn: recorder.record,
	}
	_, err := service.storeRoundWithOptionsResult(t.Context(), ChatRequest{
		BotID:    "bot-1",
		ThreadID: "session-1",
		Query:    "hello",
		TurnID:   "turn-1",
	}, persistedTurnRound(), "model-1", storeRoundOptions{})
	if err != nil {
		t.Fatalf("storeRoundWithOptionsResult() error = %v", err)
	}
	if len(recorder.turns) != 0 {
		t.Fatalf("recorded turns = %+v, want none without a run handle", recorder.turns)
	}
}

// Replacement rows are not history until ReplaceTurn publishes them, so the
// round store must not advertise them; replacePersistedTurn records instead.
func TestStoreRoundDefersPersistedTurnForReplacementRun(t *testing.T) {
	t.Parallel()
	recorder := &persistedTurnRecorder{}
	service := &Service{
		messageService:      &recordingMessageService{},
		logger:              slog.New(slog.DiscardHandler),
		recordPersistedTurn: recorder.record,
	}
	position := int64(7)
	_, err := service.storeRoundWithOptionsResult(t.Context(), ChatRequest{
		BotID:           "bot-1",
		ThreadID:        "session-1",
		Query:           "hello",
		RunHandle:       sessionruntime.RunHandle{RunID: "run-1", FencingToken: 4},
		TurnID:          "turn-1",
		TurnPosition:    &position,
		TurnReplacement: &messagepkg.TurnReplacement{OldTurnID: "turn-0", ReplacementTurnID: "turn-1"},
	}, persistedTurnRound(), "model-1", storeRoundOptions{})
	if err != nil {
		t.Fatalf("storeRoundWithOptionsResult() error = %v", err)
	}
	if len(recorder.turns) != 0 {
		t.Fatalf("recorded turns = %+v, want none before the replacement is published", recorder.turns)
	}
}

// A recorder failure never turns durable history into a failed store.
func TestStoreRoundToleratesPersistedTurnRecordFailure(t *testing.T) {
	t.Parallel()
	service := &Service{
		messageService: &recordingMessageService{},
		logger:         slog.New(slog.DiscardHandler),
		recordPersistedTurn: func(context.Context, sessionruntime.RunHandle, sessionruntime.PersistedTurnView) error {
			return sessionruntime.ErrRunOwnershipLost
		},
	}
	persisted, err := service.storeRoundWithOptionsResult(t.Context(), ChatRequest{
		BotID:     "bot-1",
		ThreadID:  "session-1",
		Query:     "hello",
		RunHandle: sessionruntime.RunHandle{RunID: "run-1", FencingToken: 4},
		TurnID:    "turn-1",
	}, persistedTurnRound(), "model-1", storeRoundOptions{})
	if err != nil {
		t.Fatalf("storeRoundWithOptionsResult() error = %v", err)
	}
	if len(persisted) != 2 {
		t.Fatalf("persisted = %d rows, want 2", len(persisted))
	}
}
