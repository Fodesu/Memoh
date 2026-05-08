package orchestrationenv

import "errors"

// Sentinel errors are wrapped by every Manager and Backend call so
// callers can branch on semantic outcomes without parsing strings.
//
// ErrInvalidArgument is reserved for caller-visible validation
// failures (missing IDs, unknown kinds, empty names). It is never used
// to signal runtime concurrency outcomes.
//
// ErrResourceNotFound and ErrSessionNotFound mark the requested row
// as missing in Postgres rather than as a stale-lease rejection.
//
// ErrCapacityExceeded is returned by Acquire when the resource has
// reached its configured capacity. Stage 4 admission queueing will
// upgrade this to a deferred reservation; today the caller decides
// whether to back off or surface the failure.
//
// ErrStaleLease fires when a writer presents a lease_token /
// lease_epoch that does not match the current session row. It is the
// env-runtime analogue of the kernel's claim-epoch fencing.
//
// ErrBackendUnavailable signals that no backend has been registered
// for the resource's kind. The manager refuses to allocate rather
// than fall back to an in-memory shim, so a misconfigured deployment
// fails loud at the dispatch boundary.
var (
	ErrInvalidArgument    = errors.New("env: invalid argument")
	ErrResourceNotFound   = errors.New("env: resource not found")
	ErrSessionNotFound    = errors.New("env: session not found")
	ErrBindingNotFound    = errors.New("env: binding not found")
	ErrCapacityExceeded   = errors.New("env: resource capacity exceeded")
	ErrStaleLease         = errors.New("env: stale lease token or epoch")
	ErrSessionTerminal    = errors.New("env: session is in a terminal state")
	ErrBindingTerminal    = errors.New("env: binding is in a terminal state")
	ErrBackendUnavailable = errors.New("env: backend not registered for kind")
)
