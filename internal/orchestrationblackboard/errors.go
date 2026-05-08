package orchestrationblackboard

import "errors"

var (
	// ErrNotFound is returned when the key has never been written or was
	// explicitly deleted.
	ErrNotFound = errors.New("orchestrationblackboard: key not found")

	// ErrRevisionConflict is returned by CompareAndSwap when the supplied
	// expected revision does not match the current store revision. Callers
	// should refetch and retry.
	ErrRevisionConflict = errors.New("orchestrationblackboard: revision conflict")

	// ErrStaleWriter is returned by the writer authorisation layer when a
	// writer with an older claim_epoch tries to overwrite a value produced
	// by a newer claim. The kernel uses this to fence lost-lease writers
	// at the blackboard, not just at Postgres.
	ErrStaleWriter = errors.New("orchestrationblackboard: stale writer")

	// ErrUnauthorisedWriter is returned when a writer attempts a key
	// outside its allowed namespace (worker writing run scope, verifier
	// writing a result key, ...).
	ErrUnauthorisedWriter = errors.New("orchestrationblackboard: unauthorised writer")

	// ErrCASRequired is returned when a writer calls Put on a namespace
	// that the contract requires CAS for (currently result.*).
	ErrCASRequired = errors.New("orchestrationblackboard: namespace requires compare-and-swap")

	// ErrClosed is returned by a backend after Close has been called.
	ErrClosed = errors.New("orchestrationblackboard: store closed")
)
