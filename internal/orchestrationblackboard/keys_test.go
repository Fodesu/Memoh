package orchestrationblackboard

import (
	"errors"
	"testing"
)

func TestKeyString(t *testing.T) {
	cases := []struct {
		name string
		key  Key
		want string
	}{
		{
			name: "run scope without path",
			key:  RunKey("run-1", NamespaceContext),
			want: "bb.run.run-1.context",
		},
		{
			name: "task scope with nested path",
			key:  TaskKey("task-1", NamespaceResult, "summary", "v1"),
			want: "bb.task.task-1.result.summary.v1",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.key.String()
			if got != tc.want {
				t.Fatalf("String() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestKeyValidate(t *testing.T) {
	cases := []struct {
		name    string
		key     Key
		wantErr bool
	}{
		{name: "valid run key", key: RunKey("run-1", NamespaceContext)},
		{name: "valid task key with path", key: TaskKey("task-1", NamespaceResult, "summary")},
		{name: "missing owner", key: Key{Scope: ScopeRun, Namespace: NamespaceContext}, wantErr: true},
		{name: "invalid scope", key: Key{Scope: "world", OwnerID: "id", Namespace: NamespaceContext}, wantErr: true},
		{name: "invalid namespace", key: Key{Scope: ScopeRun, OwnerID: "run-1", Namespace: "secrets"}, wantErr: true},
		{name: "empty path segment", key: Key{Scope: ScopeRun, OwnerID: "run-1", Namespace: NamespaceContext, Path: []string{""}}, wantErr: true},
		{name: "path segment with dot", key: Key{Scope: ScopeRun, OwnerID: "run-1", Namespace: NamespaceContext, Path: []string{"with.dot"}}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.key.Validate()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestKeyHasPrefix(t *testing.T) {
	parent := TaskKey("task-1", NamespaceResult)
	child := TaskKey("task-1", NamespaceResult, "summary", "v1")
	sibling := TaskKey("task-2", NamespaceResult, "summary")
	other := RunKey("task-1", NamespaceResult, "summary")

	if !child.HasPrefix(parent) {
		t.Fatalf("child should sit under parent")
	}
	if sibling.HasPrefix(parent) {
		t.Fatalf("sibling task should not sit under parent")
	}
	if other.HasPrefix(parent) {
		t.Fatalf("run-scope key should not match task-scope prefix")
	}
}

func TestErrorsAreSentinels(t *testing.T) {
	wrapped := errors.Join(ErrRevisionConflict, errors.New("retry"))
	if !errors.Is(wrapped, ErrRevisionConflict) {
		t.Fatalf("ErrRevisionConflict must be detectable via errors.Is")
	}
}
