package route

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	session "github.com/felinics/memoh/internal/chat/thread"
)

// sharedRouteStore is one route's pointer as the database holds it, shared by
// every caller.
type sharedRouteStore struct {
	mu       sync.Mutex
	activeID string
	// afterRead, when set, runs once after GetByID returns, to let another
	// process move the pointer between a caller's read and its write.
	afterRead func(*sharedRouteStore)
}

func (s *sharedRouteStore) GetByID(context.Context, string) (Route, error) {
	s.mu.Lock()
	route := Route{ID: "route-1", ActiveThreadID: s.activeID}
	hook := s.afterRead
	s.afterRead = nil
	s.mu.Unlock()
	if hook != nil {
		hook(s)
	}
	return route, nil
}

func (s *sharedRouteStore) SetActiveThread(_ context.Context, _ string, threadID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activeID = threadID
	return nil
}

func (s *sharedRouteStore) CompareAndSetActiveThread(_ context.Context, _, expected, threadID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeID != expected {
		return false, nil
	}
	s.activeID = threadID
	return true, nil
}

type countingThreadStore struct {
	mu      sync.Mutex
	threads map[string]session.Thread
	created int
	deleted []string
	// slowCreate keeps a creation in flight long enough for every caller to
	// arrive while it is.
	slowCreate time.Duration
}

func (s *countingThreadStore) Get(_ context.Context, id string) (session.Thread, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	thread, ok := s.threads[id]
	if !ok {
		return session.Thread{}, pgx.ErrNoRows
	}
	return thread, nil
}

func (s *countingThreadStore) Create(_ context.Context, input session.CreateInput) (session.Thread, error) {
	time.Sleep(s.slowCreate)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.created++
	thread := session.Thread{ID: fmt.Sprintf("thread-%d", s.created), Type: input.Type}
	if s.threads == nil {
		s.threads = map[string]session.Thread{}
	}
	s.threads[thread.ID] = thread
	return thread, nil
}

func (s *countingThreadStore) SoftDelete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleted = append(s.deleted, id)
	delete(s.threads, id)
	return nil
}

func TestThreadCoordinatorCreateIfAbsentReturnsUsableActiveThread(t *testing.T) {
	routes := &sharedRouteStore{activeID: "thread-existing"}
	threads := &countingThreadStore{threads: map[string]session.Thread{"thread-existing": {ID: "thread-existing"}}}
	got, err := NewThreadCoordinator(nil, routes, threads).CreateIfAbsent(context.Background(), session.CreateInput{RouteID: "route-1"})
	if err != nil {
		t.Fatalf("CreateIfAbsent: %v", err)
	}
	if got.ID != "thread-existing" || threads.created != 0 {
		t.Fatalf("got %q with %d creations, want the existing thread and none", got.ID, threads.created)
	}
}

func TestThreadCoordinatorCreateIfAbsentReplacesUnusableActiveThread(t *testing.T) {
	routes := &sharedRouteStore{activeID: "thread-deleted"}
	threads := &countingThreadStore{}
	got, err := NewThreadCoordinator(nil, routes, threads).CreateIfAbsent(context.Background(), session.CreateInput{RouteID: "route-1"})
	if err != nil {
		t.Fatalf("CreateIfAbsent: %v", err)
	}
	if got.ID != "thread-1" || routes.activeID != "thread-1" || got.Type != session.TypeChat {
		t.Fatalf("got %+v, route points at %q; want a new chat thread selected", got, routes.activeID)
	}
}

func TestThreadCoordinatorCreateIfAbsentGivesConcurrentCallersOneThread(t *testing.T) {
	routes := &sharedRouteStore{}
	threads := &countingThreadStore{slowCreate: 50 * time.Millisecond}
	coordinator := NewThreadCoordinator(nil, routes, threads)

	const callers = 50
	ids := make(chan string, callers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			thread, err := coordinator.CreateIfAbsent(context.Background(), session.CreateInput{RouteID: "route-1"})
			if err != nil {
				t.Errorf("CreateIfAbsent: %v", err)
				return
			}
			ids <- thread.ID
		}()
	}
	close(start)
	wg.Wait()
	close(ids)
	for id := range ids {
		if id != "thread-1" {
			t.Fatalf("a caller got %q, want every caller on thread-1", id)
		}
	}
	if threads.created != 1 || routes.activeID != "thread-1" {
		t.Fatalf("created %d threads, route points at %q; want one thread selected", threads.created, routes.activeID)
	}
}

func TestThreadCoordinatorCreateIfAbsentYieldsToAnotherProcess(t *testing.T) {
	threads := &countingThreadStore{threads: map[string]session.Thread{"thread-other-process": {ID: "thread-other-process"}}}
	routes := &sharedRouteStore{afterRead: func(s *sharedRouteStore) {
		// Another process selects its own thread after this caller read an
		// empty pointer.
		s.activeID = "thread-other-process"
	}}

	got, err := NewThreadCoordinator(nil, routes, threads).CreateIfAbsent(context.Background(), session.CreateInput{RouteID: "route-1"})
	if err != nil {
		t.Fatalf("CreateIfAbsent: %v", err)
	}
	if got.ID != "thread-other-process" || routes.activeID != "thread-other-process" {
		t.Fatalf("got %q, route points at %q; want the other process's thread kept", got.ID, routes.activeID)
	}
	if len(threads.deleted) != 1 || threads.deleted[0] != "thread-1" {
		t.Fatalf("deleted %v, want the thread this caller created", threads.deleted)
	}
}
