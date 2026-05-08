package orchestrationblackboard

import "context"

// Revision is an opaque, monotonically increasing sequence number assigned
// by the backend on every successful write to a key. CAS callers pass the
// last revision they observed; the backend rejects the write when it has
// since moved on.
type Revision uint64

// Entry pairs a stored Value with the Revision the backend assigned it.
type Entry struct {
	Key      Key
	Value    Value
	Revision Revision
}

// Store is the read/write interface every blackboard backend implements.
// Backends must be safe for concurrent use; the CAS layer fans out from
// many goroutines.
//
// Get returns ErrNotFound when no value is present.
// Put writes without comparing revisions. Use it for namespaces that do
// not require CAS (anything outside result.*).
// CompareAndSwap writes only when the current revision matches expected.
// Pass 0 for expected when the caller believes the key is unset.
// Delete removes a key. List returns every entry under prefix.
type Store interface {
	Get(ctx context.Context, key Key) (Entry, error)
	Put(ctx context.Context, key Key, value Value) (Revision, error)
	CompareAndSwap(ctx context.Context, key Key, expected Revision, value Value) (Revision, error)
	Delete(ctx context.Context, key Key) error
	List(ctx context.Context, prefix Key) ([]Entry, error)
	Close() error
}
