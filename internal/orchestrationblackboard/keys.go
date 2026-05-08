package orchestrationblackboard

import (
	"errors"
	"fmt"
	"strings"
)

// Scope says whether a key is bound to an entire run or a single task.
// Run-scope keys collect cross-task context like the goal, plan, and
// verifier summary; task-scope keys hold per-task progress, results, and
// artifact intents.
type Scope string

const (
	ScopeRun  Scope = "run"
	ScopeTask Scope = "task"
)

// Namespace partitions the keyspace by intent. Each namespace pins the
// allowed WriterType set; see writer.go for the ownership table.
type Namespace string

const (
	NamespaceContext   Namespace = "context"
	NamespacePlan      Namespace = "plan"
	NamespaceDeps      Namespace = "deps"
	NamespaceProgress  Namespace = "progress"
	NamespaceResult    Namespace = "result"
	NamespaceArtifacts Namespace = "artifacts"
	NamespaceVerifier  Namespace = "verifier"
	NamespaceHuman     Namespace = "human"
)

// Key uniquely names a blackboard entry. Backends serialise it via
// String(); the canonical form is "bb.{scope}.{owner}.{namespace}[.path…]".
type Key struct {
	Scope     Scope
	OwnerID   string
	Namespace Namespace
	Path      []string
}

// RunKey builds a run-scope key under the given namespace.
func RunKey(runID string, ns Namespace, path ...string) Key {
	return Key{Scope: ScopeRun, OwnerID: runID, Namespace: ns, Path: append([]string(nil), path...)}
}

// TaskKey builds a task-scope key under the given namespace.
func TaskKey(taskID string, ns Namespace, path ...string) Key {
	return Key{Scope: ScopeTask, OwnerID: taskID, Namespace: ns, Path: append([]string(nil), path...)}
}

// String renders the key in the canonical NATS-friendly form.
func (k Key) String() string {
	var b strings.Builder
	b.WriteString("bb.")
	b.WriteString(string(k.Scope))
	b.WriteByte('.')
	b.WriteString(k.OwnerID)
	b.WriteByte('.')
	b.WriteString(string(k.Namespace))
	for _, segment := range k.Path {
		b.WriteByte('.')
		b.WriteString(segment)
	}
	return b.String()
}

// Validate enforces the supported scope/namespace/path shape. CAS layers
// call this before talking to the backend so callers never see partial
// writes from malformed keys.
func (k Key) Validate() error {
	switch k.Scope {
	case ScopeRun, ScopeTask:
	default:
		return fmt.Errorf("orchestrationblackboard: invalid scope %q", k.Scope)
	}
	if strings.TrimSpace(k.OwnerID) == "" {
		return errors.New("orchestrationblackboard: owner id required")
	}
	switch k.Namespace {
	case NamespaceContext, NamespacePlan, NamespaceDeps, NamespaceProgress,
		NamespaceResult, NamespaceArtifacts, NamespaceVerifier, NamespaceHuman:
	default:
		return fmt.Errorf("orchestrationblackboard: invalid namespace %q", k.Namespace)
	}
	for _, segment := range k.Path {
		if strings.TrimSpace(segment) == "" {
			return fmt.Errorf("orchestrationblackboard: empty path segment in key %q", k.String())
		}
		if strings.ContainsAny(segment, ". ") {
			return fmt.Errorf("orchestrationblackboard: path segment %q must not contain spaces or dots", segment)
		}
	}
	return nil
}

// HasPrefix reports whether key sits under prefix in the keyspace tree.
// It is the basis for List backends and replay filtering.
func (k Key) HasPrefix(prefix Key) bool {
	if k.Scope != prefix.Scope || k.OwnerID != prefix.OwnerID || k.Namespace != prefix.Namespace {
		return false
	}
	if len(prefix.Path) > len(k.Path) {
		return false
	}
	for i, segment := range prefix.Path {
		if k.Path[i] != segment {
			return false
		}
	}
	return true
}
