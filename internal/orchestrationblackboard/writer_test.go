package orchestrationblackboard

import (
	"context"
	"errors"
	"testing"
	"time"
)

func newTestStore(t *testing.T) Store {
	t.Helper()
	store := NewInMemoryStore()
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func fixedClock() func() time.Time {
	return func() time.Time { return time.Unix(1700000000, 0).UTC() }
}

func newOrchestratorWriter(t *testing.T, store Store) *Writer {
	t.Helper()
	w, err := NewWriter(WriterIdentity{Type: WriterOrchestrator, WriterID: "orch-1"}, store, fixedClock())
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	return w
}

func newWorkerWriter(t *testing.T, store Store, taskID string, claimEpoch uint64) *Writer {
	t.Helper()
	w, err := NewWriter(WriterIdentity{
		Type:       WriterWorker,
		WriterID:   "worker-1",
		RunID:      "run-1",
		TaskID:     taskID,
		AttemptID:  "attempt-1",
		ClaimEpoch: claimEpoch,
	}, store, fixedClock())
	if err != nil {
		t.Fatalf("NewWriter worker: %v", err)
	}
	return w
}

func newVerifierWriter(t *testing.T, store Store) *Writer {
	t.Helper()
	w, err := NewWriter(WriterIdentity{
		Type:      WriterVerifier,
		WriterID:  "verifier-1",
		AttemptID: "verification-1",
	}, store, fixedClock())
	if err != nil {
		t.Fatalf("NewWriter verifier: %v", err)
	}
	return w
}

func TestWriterIdentityValidation(t *testing.T) {
	cases := []struct {
		name string
		id   WriterIdentity
		ok   bool
	}{
		{name: "orchestrator ok", id: WriterIdentity{Type: WriterOrchestrator, WriterID: "orch-1"}, ok: true},
		{name: "orchestrator missing id", id: WriterIdentity{Type: WriterOrchestrator}},
		{name: "worker ok", id: WriterIdentity{Type: WriterWorker, WriterID: "w", TaskID: "t", AttemptID: "a"}, ok: true},
		{name: "worker missing task", id: WriterIdentity{Type: WriterWorker, WriterID: "w", AttemptID: "a"}},
		{name: "verifier ok", id: WriterIdentity{Type: WriterVerifier, WriterID: "v", AttemptID: "vid"}, ok: true},
		{name: "verifier missing attempt", id: WriterIdentity{Type: WriterVerifier, WriterID: "v"}},
		{name: "unknown type", id: WriterIdentity{Type: "ghost", WriterID: "x"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.id.Validate()
			if tc.ok && err != nil {
				t.Fatalf("expected ok, got %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatalf("expected error")
			}
		})
	}
}

func TestWriterRejectsForeignTask(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	w := newWorkerWriter(t, store, "task-A", 1)
	key := TaskKey("task-B", NamespaceProgress, "step")
	_, err := w.Put(ctx, key, PersistenceTransient, map[string]any{"n": 1})
	if !errors.Is(err, ErrUnauthorisedWriter) {
		t.Fatalf("expected ErrUnauthorisedWriter, got %v", err)
	}
}

func TestWriterRejectsRunScopeFromWorker(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	w := newWorkerWriter(t, store, "task-A", 1)
	key := RunKey("run-1", NamespaceContext, "goal")
	_, err := w.Put(ctx, key, PersistenceFromPostgres, "no")
	if !errors.Is(err, ErrUnauthorisedWriter) {
		t.Fatalf("expected ErrUnauthorisedWriter, got %v", err)
	}
}

func TestWriterRejectsVerifierNamespaceFromWorker(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	w := newWorkerWriter(t, store, "task-A", 1)
	key := TaskKey("task-A", NamespaceVerifier, "notes")
	_, err := w.Put(ctx, key, PersistenceTransient, "no")
	if !errors.Is(err, ErrUnauthorisedWriter) {
		t.Fatalf("expected ErrUnauthorisedWriter, got %v", err)
	}
}

func TestWriterVerifierScope(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	w := newVerifierWriter(t, store)

	verifierKey := TaskKey("task-A", NamespaceVerifier, "notes")
	if _, err := w.Put(ctx, verifierKey, PersistenceFromPostgres, "ok"); err != nil {
		t.Fatalf("verifier put on verifier namespace: %v", err)
	}

	resultKey := TaskKey("task-A", NamespaceResult, "summary")
	_, err := w.CompareAndSwap(ctx, resultKey, 0, PersistenceFromPostgres, "no")
	if !errors.Is(err, ErrUnauthorisedWriter) {
		t.Fatalf("verifier writing result.* should be unauthorised, got %v", err)
	}
}

func TestWriterResultRequiresCAS(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	w := newWorkerWriter(t, store, "task-A", 1)
	key := TaskKey("task-A", NamespaceResult, "summary")
	_, err := w.Put(ctx, key, PersistenceFromPostgres, "should fail")
	if !errors.Is(err, ErrCASRequired) {
		t.Fatalf("expected ErrCASRequired, got %v", err)
	}
}

func TestWriterResultCASFencesStaleClaim(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	key := TaskKey("task-A", NamespaceResult, "summary")

	newer := newWorkerWriter(t, store, "task-A", 5)
	if _, err := newer.CompareAndSwap(ctx, key, 0, PersistenceFromPostgres, "from epoch 5"); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	older := newWorkerWriter(t, store, "task-A", 2)
	_, err := older.CompareAndSwap(ctx, key, 1, PersistenceFromPostgres, "from epoch 2")
	if !errors.Is(err, ErrStaleWriter) {
		t.Fatalf("expected ErrStaleWriter, got %v", err)
	}
}

func TestWriterCASRevisionConflictPropagates(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	key := TaskKey("task-A", NamespaceResult, "summary")
	w := newWorkerWriter(t, store, "task-A", 3)
	if _, err := w.CompareAndSwap(ctx, key, 0, PersistenceFromPostgres, "first"); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if _, err := w.CompareAndSwap(ctx, key, 0, PersistenceFromPostgres, "second"); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("expected ErrRevisionConflict, got %v", err)
	}
}

func TestWriterOrchestratorCanWriteAnyScope(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	w := newOrchestratorWriter(t, store)

	if _, err := w.Put(ctx, RunKey("run-1", NamespaceContext, "goal"), PersistenceFromPostgres, "g"); err != nil {
		t.Fatalf("run scope: %v", err)
	}
	if _, err := w.Put(ctx, TaskKey("task-A", NamespacePlan, "v1"), PersistenceFromPostgres, "p"); err != nil {
		t.Fatalf("task plan: %v", err)
	}
	if _, err := w.CompareAndSwap(ctx, TaskKey("task-A", NamespaceResult, "summary"), 0, PersistenceFromPostgres, "ok"); err != nil {
		t.Fatalf("orchestrator result CAS: %v", err)
	}
}

func TestWriterDeleteAuthorisation(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	w := newWorkerWriter(t, store, "task-A", 1)
	key := TaskKey("task-B", NamespaceProgress, "step")
	if err := w.Delete(ctx, key); !errors.Is(err, ErrUnauthorisedWriter) {
		t.Fatalf("expected ErrUnauthorisedWriter on cross-task delete, got %v", err)
	}
}

func TestReaderWrapsStore(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	w := newOrchestratorWriter(t, store)
	key := TaskKey("task-A", NamespacePlan, "v1")
	if _, err := w.Put(ctx, key, PersistenceFromPostgres, "p"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	r := NewReader(store)
	got, err := r.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Value.WriterType != WriterOrchestrator {
		t.Fatalf("writer type round trip: %s", got.Value.WriterType)
	}
	listed, err := r.List(ctx, TaskKey("task-A", NamespacePlan))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("List size: %d", len(listed))
	}
}
