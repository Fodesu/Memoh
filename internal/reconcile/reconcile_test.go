package reconcile

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type row struct {
	key string
}

// memStore hands out the rows it holds once per Claim call and records
// leases; Renew fails once lost is set for a key.
type memStore struct {
	mu       sync.Mutex
	due      []row
	claims   int
	released []string
	renewed  int
	lost     map[string]bool
}

func (m *memStore) Claim(_ context.Context, _ string, _ time.Duration, limit int32) ([]row, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.claims++
	n := len(m.due)
	if int32(n) > limit { //nolint:gosec // test sizes
		n = int(limit)
	}
	out := append([]row(nil), m.due[:n]...)
	m.due = m.due[n:]
	return out, nil
}

func (m *memStore) Renew(_ context.Context, key, _ string, _ time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.renewed++
	if m.lost[key] {
		return errors.New("lease lost")
	}
	return nil
}

func (m *memStore) Release(_ context.Context, key, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.released = append(m.released, key)
	return nil
}

func TestLoopHandlesClaimedRowsWithinConcurrencyAndDedups(t *testing.T) {
	store := &memStore{due: []row{{"a"}, {"b"}, {"c"}, {"a"}}}
	var handled atomic.Int32
	var inFlight, peak atomic.Int32
	release := make(chan struct{})
	loop := NewLoop(store, func(r row) string { return r.key }, func(_ context.Context, _ row) {
		cur := inFlight.Add(1)
		for {
			p := peak.Load()
			if cur <= p || peak.CompareAndSwap(p, cur) {
				break
			}
		}
		<-release
		inFlight.Add(-1)
		handled.Add(1)
	}, nil, Options{Owner: "test", Concurrency: 2, Batch: 10})

	var wg sync.WaitGroup
	// First pass: two slots, so two rows (a, b).
	n, err := loop.pass(context.Background(), &wg)
	if err != nil || n != 2 {
		t.Fatalf("pass() = %d, %v; want 2 rows", n, err)
	}
	// Both handlers are in flight before the next pass is judged.
	for deadline := time.Now().Add(2 * time.Second); inFlight.Load() < 2; {
		if time.Now().After(deadline) {
			t.Fatalf("in flight = %d, want 2", inFlight.Load())
		}
		time.Sleep(time.Millisecond)
	}
	// No free slot: nothing is even claimed.
	if n, _ := loop.pass(context.Background(), &wg); n != 0 || store.claims != 1 {
		t.Fatalf("second pass started %d rows with %d claims, want 0 rows and no new claim", n, store.claims)
	}
	close(release)
	wg.Wait()
	if handled.Load() != 2 || peak.Load() != 2 {
		t.Fatalf("handled=%d peak=%d, want 2/2", handled.Load(), peak.Load())
	}
	// Remaining rows c and a: a is no longer running so both are handled.
	release = make(chan struct{})
	close(release)
	if n, _ := loop.ReconcileOnce(context.Background()); n != 2 {
		t.Fatalf("ReconcileOnce() = %d, want 2", n)
	}
}

func TestLoopSkipsRowStillRunningInThisProcess(t *testing.T) {
	store := &memStore{due: []row{{"a"}}}
	started := make(chan struct{})
	block := make(chan struct{})
	loop := NewLoop(store, func(r row) string { return r.key }, func(_ context.Context, _ row) {
		close(started)
		<-block
	}, nil, Options{Owner: "test", Concurrency: 4})
	var wg sync.WaitGroup
	if n, _ := loop.pass(context.Background(), &wg); n != 1 {
		t.Fatalf("first pass = %d, want 1", n)
	}
	<-started
	// The same key comes back (lease expired elsewhere): not started twice,
	// and not released either, since the running goroutine still owns it.
	store.mu.Lock()
	store.due = []row{{"a"}}
	store.mu.Unlock()
	if n, _ := loop.pass(context.Background(), &wg); n != 0 {
		t.Fatalf("re-claimed running row started %d handlers, want 0", n)
	}
	if len(store.released) != 0 {
		t.Fatalf("released %v while the handler was running", store.released)
	}
	close(block)
	wg.Wait()
}

func TestStopBeforeStartReturnsImmediately(t *testing.T) {
	loop := NewLoop(&memStore{}, func(r row) string { return r.key }, func(context.Context, row) {}, nil, Options{Owner: "test"})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := loop.Stop(ctx); err != nil {
		t.Fatalf("Stop() before Start = %v, want nil", err)
	}
	// A late Start after Stop must not launch a loop that nobody will stop.
	if err := loop.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestLeasedContextCancelsWhenRenewalFails(t *testing.T) {
	store := &memStore{lost: map[string]bool{"a": true}}
	loop := NewLoop(store, func(r row) string { return r.key }, func(context.Context, row) {}, nil, Options{Owner: "test", Lease: 3 * time.Second})
	ctx, cancel := loop.LeasedContext(context.Background(), "a", time.Minute)
	defer cancel()
	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("context not cancelled after the lease was lost")
	}
}

func TestBackoffSpendFollowsFastBudgetThenSlowCadence(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	b := Backoff{
		Base: 10 * time.Second, Cap: 90 * time.Second, MaxAttempts: 6, SlowRetryInterval: 15 * time.Minute,
		Now: func() time.Time { return now }, Rand: func() float64 { return 0.5 },
	}
	want := []time.Duration{10 * time.Second, 20 * time.Second, 40 * time.Second, 80 * time.Second, 90 * time.Second}
	var attempts int32
	for i, d := range want {
		var next time.Time
		attempts, next = b.Spend(attempts, true)
		if attempts != int32(i+1) || next.Sub(now) != d { //nolint:gosec // small loop index
			t.Fatalf("attempt %d: attempts=%d next=+%s, want %d/+%s", i+1, attempts, next.Sub(now), i+1, d)
		}
		if !b.Pending(attempts) {
			t.Fatalf("attempt %d should still be pending", attempts)
		}
	}
	attempts, next := b.Spend(attempts, true)
	if attempts != 6 || next.Sub(now) != 15*time.Minute || b.Pending(attempts) {
		t.Fatalf("budget spent: attempts=%d next=+%s pending=%v", attempts, next.Sub(now), b.Pending(attempts))
	}
	// A non-retryable failure spends the whole budget at once.
	attempts, next = b.Spend(0, false)
	if attempts != 6 || next.Sub(now) != 15*time.Minute {
		t.Fatalf("non-retryable: attempts=%d next=+%s", attempts, next.Sub(now))
	}
}

func TestBrokerDeliversToSubscribersWithoutBlocking(t *testing.T) {
	b := NewBroker[int](1)
	ch, unsubscribe := b.Subscribe("k")
	b.Publish("k", 1)
	b.Publish("k", 2) // dropped: buffer full
	b.Publish("other", 3)
	if got := <-ch; got != 1 {
		t.Fatalf("got %d, want 1", got)
	}
	select {
	case v := <-ch:
		t.Fatalf("unexpected extra event %d", v)
	default:
	}
	unsubscribe()
	unsubscribe()
	b.Publish("k", 4)
	select {
	case v := <-ch:
		t.Fatalf("event %d delivered after unsubscribe", v)
	default:
	}
}

func TestAwaitReturnsFinalValueOrLastSeenOnTimeout(t *testing.T) {
	calls := 0
	v, err := Await(context.Background(), time.Millisecond, func(context.Context) (int, error) { calls++; return calls, nil }, func(v int) bool { return v >= 3 })
	if err != nil || v != 3 {
		t.Fatalf("Await() = %d, %v; want 3", v, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	v, err = Await(ctx, time.Millisecond, func(context.Context) (int, error) { return 7, nil }, func(int) bool { return false })
	if !errors.Is(err, context.DeadlineExceeded) || v != 7 {
		t.Fatalf("Await() on timeout = %d, %v; want 7 with deadline error", v, err)
	}
}
