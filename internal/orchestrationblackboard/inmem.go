package orchestrationblackboard

import (
	"context"
	"strings"
	"sync"
)

// InMemoryStore is a process-local Store used by tests and by single-process
// deployments where the JetStream backend is not configured. It mirrors the
// semantics of the JetStream KV implementation: monotonically increasing
// revisions, CAS rejection on stale expected revisions, and prefix listing
// over canonical key strings.
type InMemoryStore struct {
	mu      sync.RWMutex
	entries map[string]inMemEntry
	closed  bool
	nextRev Revision
}

type inMemEntry struct {
	key      Key
	value    Value
	revision Revision
}

// NewInMemoryStore returns a fresh in-memory blackboard. The store starts
// empty and the first successful write receives Revision(1).
func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{entries: make(map[string]inMemEntry)}
}

// Get implements Store.
func (s *InMemoryStore) Get(_ context.Context, key Key) (Entry, error) {
	if err := key.Validate(); err != nil {
		return Entry{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return Entry{}, ErrClosed
	}
	entry, ok := s.entries[key.String()]
	if !ok {
		return Entry{}, ErrNotFound
	}
	return Entry{Key: entry.key, Value: entry.value, Revision: entry.revision}, nil
}

// Put implements Store.
func (s *InMemoryStore) Put(_ context.Context, key Key, value Value) (Revision, error) {
	if err := key.Validate(); err != nil {
		return 0, err
	}
	if err := value.Validate(); err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, ErrClosed
	}
	s.nextRev++
	s.entries[key.String()] = inMemEntry{key: key, value: value, revision: s.nextRev}
	return s.nextRev, nil
}

// CompareAndSwap implements Store. Pass expected == 0 to insert when the
// caller believes the key is unset; the backend rejects with
// ErrRevisionConflict if a value already exists.
func (s *InMemoryStore) CompareAndSwap(_ context.Context, key Key, expected Revision, value Value) (Revision, error) {
	if err := key.Validate(); err != nil {
		return 0, err
	}
	if err := value.Validate(); err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, ErrClosed
	}
	current, ok := s.entries[key.String()]
	switch {
	case !ok:
		if expected != 0 {
			return 0, ErrRevisionConflict
		}
	default:
		if current.revision != expected {
			return 0, ErrRevisionConflict
		}
	}
	s.nextRev++
	s.entries[key.String()] = inMemEntry{key: key, value: value, revision: s.nextRev}
	return s.nextRev, nil
}

// Delete implements Store.
func (s *InMemoryStore) Delete(_ context.Context, key Key) error {
	if err := key.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	delete(s.entries, key.String())
	return nil
}

// List implements Store. The prefix is interpreted as a canonical Key
// string; List returns entries whose key sits under that prefix in the
// scope/namespace tree.
func (s *InMemoryStore) List(_ context.Context, prefix Key) ([]Entry, error) {
	if err := prefix.Validate(); err != nil {
		return nil, err
	}
	prefixStr := prefix.String()
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return nil, ErrClosed
	}
	var out []Entry
	for k, entry := range s.entries {
		if !strings.HasPrefix(k, prefixStr) {
			continue
		}
		if k != prefixStr && len(k) > len(prefixStr) && k[len(prefixStr)] != '.' {
			continue
		}
		out = append(out, Entry{Key: entry.key, Value: entry.value, Revision: entry.revision})
	}
	return out, nil
}

// Close implements Store. Subsequent operations return ErrClosed.
func (s *InMemoryStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}
