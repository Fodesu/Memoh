package orchestrationblackboard

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func mustValue(t *testing.T, payload any) Value {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return Value{
		SchemaVersion:    SchemaVersion,
		WriterType:       WriterOrchestrator,
		WriterID:         "orch-1",
		UpdatedAt:        time.Unix(1700000000, 0).UTC(),
		PersistenceClass: PersistenceFromPostgres,
		Payload:          raw,
	}
}

func TestInMemoryStorePutAndGet(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryStore()
	t.Cleanup(func() { _ = store.Close() })

	key := RunKey("run-1", NamespaceContext, "goal")
	rev, err := store.Put(ctx, key, mustValue(t, map[string]string{"text": "hello"}))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if rev != 1 {
		t.Fatalf("first revision = %d, want 1", rev)
	}

	got, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Revision != rev {
		t.Fatalf("revision mismatch: got %d want %d", got.Revision, rev)
	}
	if got.Value.WriterID != "orch-1" {
		t.Fatalf("writer id round-trip failed: %q", got.Value.WriterID)
	}
}

func TestInMemoryStoreCASInsert(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryStore()
	t.Cleanup(func() { _ = store.Close() })

	key := TaskKey("task-1", NamespaceResult, "summary")
	if _, err := store.CompareAndSwap(ctx, key, 1, mustValue(t, "data")); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("expected revision conflict on missing key with non-zero expected; got %v", err)
	}

	rev, err := store.CompareAndSwap(ctx, key, 0, mustValue(t, "data"))
	if err != nil {
		t.Fatalf("insert via CAS: %v", err)
	}

	if _, err := store.CompareAndSwap(ctx, key, 0, mustValue(t, "data")); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("CAS with stale expected should conflict; got %v", err)
	}

	if _, err := store.CompareAndSwap(ctx, key, rev, mustValue(t, "newer")); err != nil {
		t.Fatalf("CAS with current revision should succeed: %v", err)
	}
}

func TestInMemoryStoreList(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryStore()
	t.Cleanup(func() { _ = store.Close() })

	keys := []Key{
		TaskKey("task-1", NamespaceResult, "summary"),
		TaskKey("task-1", NamespaceResult, "details"),
		TaskKey("task-2", NamespaceResult, "summary"),
		TaskKey("task-1", NamespaceArtifacts, "doc"),
	}
	for _, k := range keys {
		if _, err := store.Put(ctx, k, mustValue(t, "x")); err != nil {
			t.Fatalf("Put %s: %v", k.String(), err)
		}
	}

	prefix := TaskKey("task-1", NamespaceResult)
	listed, err := store.List(ctx, prefix)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got, want := len(listed), 2; got != want {
		t.Fatalf("List returned %d entries, want %d (entries=%v)", got, want, listed)
	}
}

func TestInMemoryStoreDelete(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryStore()
	t.Cleanup(func() { _ = store.Close() })

	key := TaskKey("task-1", NamespaceProgress, "step")
	if _, err := store.Put(ctx, key, mustValue(t, "x")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := store.Delete(ctx, key); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Get(ctx, key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after Delete should be ErrNotFound, got %v", err)
	}
}

func TestInMemoryStoreClosed(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryStore()
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	key := RunKey("run-1", NamespaceContext)
	if _, err := store.Put(ctx, key, mustValue(t, "x")); !errors.Is(err, ErrClosed) {
		t.Fatalf("Put after Close should return ErrClosed, got %v", err)
	}
}

func TestValueValidate(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*Value)
		wantErr bool
	}{
		{name: "valid", mutate: func(_ *Value) {}},
		{name: "missing schema", mutate: func(v *Value) { v.SchemaVersion = "" }, wantErr: true},
		{name: "invalid writer type", mutate: func(v *Value) { v.WriterType = "ghost" }, wantErr: true},
		{name: "missing writer id", mutate: func(v *Value) { v.WriterID = "" }, wantErr: true},
		{name: "invalid persistence", mutate: func(v *Value) { v.PersistenceClass = "" }, wantErr: true},
		{name: "missing updated_at", mutate: func(v *Value) { v.UpdatedAt = time.Time{} }, wantErr: true},
		{name: "missing payload", mutate: func(v *Value) { v.Payload = nil }, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := mustValue(t, "ok")
			tc.mutate(&v)
			err := v.Validate()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
