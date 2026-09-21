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

// The run learns that it wrote its history turn at the moment the round lands,
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
	if got := recorder.turns[0]; got.TurnID != "turn-1" {
		t.Fatalf("persisted turn = %+v, want turn-1", got)
	}
}

// Continuations rebuild the request without the admission's turn fields; the
// run handle still names the turn.
func TestStoreRoundFallsBackToHandleTurnID(t *testing.T) {
	t.Parallel()
	recorder := &persistedTurnRecorder{}
	service := &Service{
		messageService:      &recordingMessageService{},
		logger:              slog.New(slog.DiscardHandler),
		recordPersistedTurn: recorder.record,
	}
	_, err := service.storeRoundWithOptionsResult(t.Context(), ChatRequest{
		BotID:     "bot-1",
		ThreadID:  "session-1",
		Query:     "hello",
		RunHandle: sessionruntime.RunHandle{RunID: "run-1", TurnID: "turn-from-handle", FencingToken: 4},
	}, persistedTurnRound(), "model-1", storeRoundOptions{})
	if err != nil {
		t.Fatalf("storeRoundWithOptionsResult() error = %v", err)
	}
	if len(recorder.turns) != 1 || recorder.turns[0].TurnID != "turn-from-handle" {
		t.Fatalf("recorded turns = %+v, want the handle's turn", recorder.turns)
	}
}

type auditLookupMessageService struct {
	recordingMessageService
	turn messagepkg.HistoryTurn
	err  error
}

func (s *auditLookupMessageService) GetHistoryTurn(context.Context, string, string) (messagepkg.HistoryTurn, error) {
	return s.turn, s.err
}

type capturingHandler struct {
	records []slog.Record
}

func (*capturingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *capturingHandler) Handle(_ context.Context, r slog.Record) error {
	h.records = append(h.records, r)
	return nil
}
func (h *capturingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *capturingHandler) WithGroup(string) slog.Handler      { return h }

// The auditor turns a forgotten notePersistedTurn into an error log instead of
// a silent client-side draft restore.
func TestAuditUnrecordedPersistedTurnReportsHistoryDisagreement(t *testing.T) {
	t.Parallel()
	handle := sessionruntime.RunHandle{RunID: "run-1", SessionID: "session-1", TurnID: "turn-1", FencingToken: 4}
	run := sessionruntime.CurrentRunView{RunID: "run-1", TurnID: "turn-1", Status: "errored", ErrorCode: "agent.response_timeout"}

	t.Run("history holds the turn", func(t *testing.T) {
		t.Parallel()
		logs := &capturingHandler{}
		service := &Service{
			messageService: &auditLookupMessageService{turn: messagepkg.HistoryTurn{ID: "turn-1", RequestMessageID: "user-1", AssistantMessageID: "assistant-1"}},
			logger:         slog.New(logs),
		}
		service.auditUnrecordedPersistedTurn(t.Context(), handle, run)
		if len(logs.records) != 1 || logs.records[0].Level != slog.LevelError {
			t.Fatalf("logs = %+v, want one error record", logs.records)
		}
	})
	t.Run("history has no such turn", func(t *testing.T) {
		t.Parallel()
		logs := &capturingHandler{}
		service := &Service{
			messageService: &auditLookupMessageService{err: messagepkg.ErrHistoryTurnNotFound},
			logger:         slog.New(logs),
		}
		service.auditUnrecordedPersistedTurn(t.Context(), handle, run)
		if len(logs.records) != 0 {
			t.Fatalf("logs = %+v, want none: an unsent run is the expected state", logs.records)
		}
	})
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
