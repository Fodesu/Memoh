package orchestrationblackboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// WriterIdentity captures everything the CAS layer needs to authorise and
// fence a write. It is the runtime projection of the kernel's view of who
// is talking to the blackboard, plus the live claim epoch the worker /
// verifier presented when it picked up its lease.
//
// For WriterWorker the TaskID and AttemptID fields must be populated; the
// authorisation layer rejects any write that targets a different task.
// For WriterVerifier the ClaimEpoch is required so verifier writes carry
// the same fence the verifier daemon was issued.
// For WriterOrchestrator the kernel itself is calling, so only WriterID
// is required.
type WriterIdentity struct {
	Type       WriterType
	WriterID   string
	RunID      string
	TaskID     string
	AttemptID  string
	ClaimEpoch uint64
}

// Validate sanity checks the identity. It is called automatically by
// NewWriter; tests can call it directly for fixtures.
func (id WriterIdentity) Validate() error {
	switch id.Type {
	case WriterOrchestrator:
		if id.WriterID == "" {
			return errors.New("orchestrationblackboard: orchestrator writer_id is required")
		}
	case WriterWorker:
		if id.WriterID == "" || id.TaskID == "" || id.AttemptID == "" {
			return errors.New("orchestrationblackboard: worker writer requires writer_id, task_id, attempt_id")
		}
	case WriterVerifier:
		if id.WriterID == "" || id.AttemptID == "" {
			return errors.New("orchestrationblackboard: verifier writer requires writer_id and attempt_id")
		}
	default:
		return fmt.Errorf("orchestrationblackboard: invalid writer type %q", id.Type)
	}
	return nil
}

// Writer wraps a Store with the writer ownership and CAS rules described
// in PLAN.md Stage 2.2.
//
// Ownership table:
//
//   - Workers may write under their own task scope (bb.task.{task_id}.*)
//     and may not touch the verifier namespace.
//   - Verifiers may write the verifier namespace at run or task scope and
//     may not touch results, plans, or progress directly.
//   - The orchestrator may write any scope.
//
// CAS rule: result.* writes always require CompareAndSwap with a matching
// expected revision and a claim_epoch >= the value already in the store.
// The Writer rejects bare Put on result.* with ErrCASRequired and rejects
// CAS attempts whose ClaimEpoch is older than the stored value with
// ErrStaleWriter, even if the revision matches.
type Writer struct {
	identity WriterIdentity
	store    Store
	clock    func() time.Time
}

// NewWriter builds a Writer bound to identity. The clock parameter lets
// tests pin UpdatedAt to a deterministic value; pass nil to use
// time.Now().UTC.
func NewWriter(identity WriterIdentity, store Store, clock func() time.Time) (*Writer, error) {
	if err := identity.Validate(); err != nil {
		return nil, err
	}
	if store == nil {
		return nil, errors.New("orchestrationblackboard: nil store")
	}
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	return &Writer{identity: identity, store: store, clock: clock}, nil
}

// Identity returns the WriterIdentity bound to this writer.
func (w *Writer) Identity() WriterIdentity { return w.identity }

// Put writes payload at key without comparing revisions. It is the right
// call for namespaces that do not require CAS (anything outside result.*).
// The payload is JSON-encoded and wrapped in a Value envelope using the
// writer's identity and the configured persistence class.
func (w *Writer) Put(ctx context.Context, key Key, persistence PersistenceClass, payload any) (Revision, error) {
	if err := w.authorise(key); err != nil {
		return 0, err
	}
	if requiresCAS(key) {
		return 0, fmt.Errorf("%w: key=%s", ErrCASRequired, key.String())
	}
	value, err := w.buildValue(persistence, payload)
	if err != nil {
		return 0, err
	}
	return w.store.Put(ctx, key, value)
}

// CompareAndSwap writes payload at key only when the current revision
// matches expected. Pass expected == 0 when the caller believes the key
// is unset.
//
// CompareAndSwap also fences stale writers: if the stored value already
// carries a ClaimEpoch greater than the writer's ClaimEpoch, the call
// returns ErrStaleWriter without touching the backend.
func (w *Writer) CompareAndSwap(ctx context.Context, key Key, expected Revision, persistence PersistenceClass, payload any) (Revision, error) {
	if err := w.authorise(key); err != nil {
		return 0, err
	}
	value, err := w.buildValue(persistence, payload)
	if err != nil {
		return 0, err
	}
	current, getErr := w.store.Get(ctx, key)
	switch {
	case errors.Is(getErr, ErrNotFound):
		// fall through; expected==0 path handled by the backend.
	case getErr != nil:
		return 0, getErr
	default:
		if current.Value.ClaimEpoch > w.identity.ClaimEpoch {
			return 0, fmt.Errorf("%w: stored claim_epoch=%d writer claim_epoch=%d", ErrStaleWriter, current.Value.ClaimEpoch, w.identity.ClaimEpoch)
		}
	}
	return w.store.CompareAndSwap(ctx, key, expected, value)
}

// Delete removes key. Authorisation rules match Put.
func (w *Writer) Delete(ctx context.Context, key Key) error {
	if err := w.authorise(key); err != nil {
		return err
	}
	return w.store.Delete(ctx, key)
}

func (w *Writer) authorise(key Key) error {
	if err := key.Validate(); err != nil {
		return err
	}
	switch w.identity.Type {
	case WriterOrchestrator:
		return nil
	case WriterWorker:
		if key.Scope != ScopeTask {
			return fmt.Errorf("%w: workers may only write task scope", ErrUnauthorisedWriter)
		}
		if key.OwnerID != w.identity.TaskID {
			return fmt.Errorf("%w: worker task_id=%s key task_id=%s", ErrUnauthorisedWriter, w.identity.TaskID, key.OwnerID)
		}
		if key.Namespace == NamespaceVerifier {
			return fmt.Errorf("%w: workers may not write verifier namespace", ErrUnauthorisedWriter)
		}
		return nil
	case WriterVerifier:
		if key.Namespace != NamespaceVerifier {
			return fmt.Errorf("%w: verifiers may only write verifier namespace", ErrUnauthorisedWriter)
		}
		return nil
	default:
		return fmt.Errorf("%w: unknown writer type %q", ErrUnauthorisedWriter, w.identity.Type)
	}
}

func (w *Writer) buildValue(persistence PersistenceClass, payload any) (Value, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return Value{}, fmt.Errorf("orchestrationblackboard: encode payload: %w", err)
	}
	value := Value{
		SchemaVersion:    SchemaVersion,
		WriterType:       w.identity.Type,
		WriterID:         w.identity.WriterID,
		AttemptID:        w.identity.AttemptID,
		ClaimEpoch:       w.identity.ClaimEpoch,
		UpdatedAt:        w.clock(),
		PersistenceClass: persistence,
		Payload:          raw,
	}
	if err := value.Validate(); err != nil {
		return Value{}, err
	}
	return value, nil
}

// requiresCAS encodes the namespaces whose contract demands CompareAndSwap.
// Today only result.* qualifies; future stages may extend this list.
func requiresCAS(key Key) bool {
	return key.Namespace == NamespaceResult
}

// Reader is a thin convenience around Store.Get / Store.List for callers
// that only need to read. It exists mostly to mirror Writer for symmetry
// in dependency injection; backends are free to expose Store directly.
type Reader struct {
	store Store
}

// NewReader returns a Reader backed by store.
func NewReader(store Store) *Reader { return &Reader{store: store} }

// Get returns the entry at key.
func (r *Reader) Get(ctx context.Context, key Key) (Entry, error) {
	return r.store.Get(ctx, key)
}

// List returns every entry under prefix.
func (r *Reader) List(ctx context.Context, prefix Key) ([]Entry, error) {
	return r.store.List(ctx, prefix)
}
