// Package orchestrationblackboard is the Stage 2 reconstructable runtime view
// for the orchestration kernel. Postgres remains authoritative; this package
// is the shared view that workers, verifiers, and the orchestrator coordinate
// through during a run.
//
// A blackboard backend is a key-value store with revision-based CAS. Every
// stored entry carries a Value envelope that records who wrote it (writer
// type / writer id / attempt id / claim epoch), when it was written, what
// schema it follows, and how it can be recovered after a wipe
// (persistence_class). Wipe-and-rebuild from Postgres is the recovery
// strategy of last resort.
package orchestrationblackboard

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// SchemaVersion is the wire-format tag stamped onto every Value. Bump it
// when required envelope fields change so readers can either upcast in
// place or refuse the value and trigger a rebuild.
const SchemaVersion = "v1"

// PersistenceClass tells the rebuild path how to recover a value when the
// backend is wiped.
type PersistenceClass string

const (
	// PersistenceFromPostgres marks values that can be reconstructed from
	// Postgres tables (runs/tasks/attempts/results/artifacts/...). The
	// rebuild routine knows how to materialise these.
	PersistenceFromPostgres PersistenceClass = "from_postgres"

	// PersistenceTransient marks short-lived progress signals that the
	// kernel does not promise to recover. Workers must treat them as
	// best-effort hints; verifier outcomes never depend on them.
	PersistenceTransient PersistenceClass = "transient"
)

// WriterType identifies the actor that produced a Value. The CAS layer in
// writer.go pairs this with a per-namespace ownership rule.
type WriterType string

const (
	WriterOrchestrator WriterType = "orchestrator"
	WriterWorker       WriterType = "worker"
	WriterVerifier     WriterType = "verifier"
)

// Value is the on-the-wire envelope stored under every blackboard key.
// Payload is opaque JSON so the package does not couple to specific
// runtime types.
type Value struct {
	SchemaVersion    string           `json:"schema_version"`
	WriterType       WriterType       `json:"writer_type"`
	WriterID         string           `json:"writer_id"`
	AttemptID        string           `json:"attempt_id,omitempty"`
	ClaimEpoch       uint64           `json:"claim_epoch,omitempty"`
	UpdatedAt        time.Time        `json:"updated_at"`
	PersistenceClass PersistenceClass `json:"persistence_class"`
	Payload          json.RawMessage  `json:"payload"`
}

// Encode serialises the value for storage on the backend.
func (v Value) Encode() ([]byte, error) {
	return json.Marshal(v)
}

// DecodeValue parses bytes from the backend back into a Value.
func DecodeValue(b []byte) (Value, error) {
	var v Value
	if err := json.Unmarshal(b, &v); err != nil {
		return Value{}, fmt.Errorf("orchestrationblackboard: decode value: %w", err)
	}
	return v, nil
}

// Validate enforces the minimum invariants every Value must satisfy. The
// CAS layer calls Validate before forwarding to the backend so malformed
// payloads never reach the bus.
func (v Value) Validate() error {
	if v.SchemaVersion == "" {
		return errors.New("orchestrationblackboard: schema_version is required")
	}
	switch v.WriterType {
	case WriterOrchestrator, WriterWorker, WriterVerifier:
	default:
		return fmt.Errorf("orchestrationblackboard: invalid writer_type %q", v.WriterType)
	}
	if v.WriterID == "" {
		return errors.New("orchestrationblackboard: writer_id is required")
	}
	switch v.PersistenceClass {
	case PersistenceFromPostgres, PersistenceTransient:
	default:
		return fmt.Errorf("orchestrationblackboard: invalid persistence_class %q", v.PersistenceClass)
	}
	if v.UpdatedAt.IsZero() {
		return errors.New("orchestrationblackboard: updated_at is required")
	}
	if len(v.Payload) == 0 {
		return errors.New("orchestrationblackboard: payload is required")
	}
	return nil
}
