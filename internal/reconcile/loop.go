package reconcile

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Store is the lease-bearing claim queue the Loop drains. Claim returns up to
// limit rows that are due and unleased, leased to owner for lease; the store
// decides what "due" means. Renew fails when the row is no longer leased to
// owner (another instance took it over). Release drops owner's lease.
type Store[T any] interface {
	Claim(ctx context.Context, owner string, lease time.Duration, limit int32) ([]T, error)
	Renew(ctx context.Context, key, owner string, lease time.Duration) error
	Release(ctx context.Context, key, owner string) error
}

// Loop claims due rows and hands each to a handler on its own goroutine, with
// at most Options.Concurrency in flight and never the same key twice at once
// in this process. The handler owns the row's lease for the duration of the
// call: it must release it (Release) or write a final observation that does.
type Loop[T any] struct {
	store  Store[T]
	key    func(T) string
	handle func(context.Context, T)
	log    *slog.Logger
	opts   Options

	afterPass func(context.Context)

	kick chan struct{}

	runMu   sync.Mutex
	running map[string]struct{}

	stop   chan struct{}
	done   chan struct{}
	startd sync.Once
	stopd  sync.Once
}

// NewLoop builds a Loop over store; key names a row, handle processes one
// claimed row. The loop starts with Start.
func NewLoop[T any](store Store[T], key func(T) string, handle func(context.Context, T), log *slog.Logger, opts Options) *Loop[T] {
	if log == nil {
		log = slog.Default()
	}
	return &Loop[T]{
		store:   store,
		key:     key,
		handle:  handle,
		log:     log,
		opts:    opts.WithDefaults(),
		kick:    make(chan struct{}, 1),
		running: make(map[string]struct{}),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
}

// Options returns the effective (defaulted) options.
func (l *Loop[T]) Options() Options { return l.opts }

// Owner is this instance's lease owner id.
func (l *Loop[T]) Owner() string { return l.opts.Owner }

// SetAfterPass registers a hook that runs after every pass of the loop (not
// after ReconcileOnce), for periodic work that rides on the same ticker.
func (l *Loop[T]) SetAfterPass(fn func(context.Context)) { l.afterPass = fn }

// Kick wakes the loop for an immediate pass.
func (l *Loop[T]) Kick() {
	select {
	case l.kick <- struct{}{}:
	default:
	}
}

// Start launches the loop; it returns immediately.
func (l *Loop[T]) Start(ctx context.Context) error {
	l.startd.Do(func() {
		go l.run(context.WithoutCancel(ctx))
	})
	return nil
}

// Stop asks the loop to exit and waits for in-flight handlers to finish
// (bounded by ctx).
func (l *Loop[T]) Stop(ctx context.Context) error {
	l.stopd.Do(func() { close(l.stop) })
	select {
	case <-l.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (l *Loop[T]) run(ctx context.Context) {
	defer close(l.done)
	ticker := time.NewTicker(l.opts.Interval)
	defer ticker.Stop()
	var wg sync.WaitGroup
	for {
		select {
		case <-l.stop:
			wg.Wait()
			return
		case <-ticker.C:
		case <-l.kick:
		}
		if _, err := l.pass(ctx, &wg); err != nil {
			l.log.ErrorContext(ctx, "reconcile pass failed", slog.Any("error", err))
		}
		if l.afterPass != nil {
			l.afterPass(ctx)
		}
	}
}

// ReconcileOnce runs a single pass synchronously and waits for the handlers
// it started (tests, admin tooling). It returns how many rows were handled.
func (l *Loop[T]) ReconcileOnce(ctx context.Context) (int, error) {
	var wg sync.WaitGroup
	n, err := l.pass(ctx, &wg)
	wg.Wait()
	return n, err
}

func (l *Loop[T]) pass(ctx context.Context, wg *sync.WaitGroup) (int, error) {
	limit := l.freeSlots()
	if limit == 0 {
		return 0, nil
	}
	claimCtx, cancel := context.WithTimeout(ctx, l.opts.WriteTimeout)
	rows, err := l.store.Claim(claimCtx, l.opts.Owner, l.opts.Lease, limit)
	cancel()
	if err != nil {
		return 0, err
	}
	started := 0
	for _, row := range rows {
		key := l.key(row)
		if !l.markRunning(key) {
			// This instance is still processing the row (its lease expired but
			// the goroutine is alive). Do not release: Release matches by
			// owner and would strip the lease from the running goroutine. The
			// claim bumped the version, so that goroutine's next write fails
			// the version check and it backs off on its own.
			continue
		}
		started++
		wg.Add(1)
		go func(row T, key string) {
			defer wg.Done()
			defer l.unmarkRunning(key)
			l.handle(ctx, row)
		}(row, key)
	}
	return started, nil
}

// freeSlots is how many rows a pass may claim: a claimed row holds a lease,
// so claiming more than can start immediately would park leased rows in a
// queue until the lease expires and another instance takes them over.
func (l *Loop[T]) freeSlots() int32 {
	l.runMu.Lock()
	defer l.runMu.Unlock()
	free := l.opts.Concurrency - len(l.running)
	if free <= 0 {
		return 0
	}
	if free > int(l.opts.Batch) {
		return l.opts.Batch
	}
	return int32(free) //nolint:gosec // free <= Batch, which is an int32
}

func (l *Loop[T]) markRunning(key string) bool {
	l.runMu.Lock()
	defer l.runMu.Unlock()
	if _, ok := l.running[key]; ok {
		return false
	}
	l.running[key] = struct{}{}
	return true
}

func (l *Loop[T]) unmarkRunning(key string) {
	l.runMu.Lock()
	defer l.runMu.Unlock()
	delete(l.running, key)
}

// Release drops this instance's lease on key on a fresh short-lived context,
// logging (not returning) a failure: the lease expires on its own anyway.
func (l *Loop[T]) Release(ctx context.Context, key string) {
	rctx, cancel := WriteContext(ctx, l.opts.WriteTimeout)
	defer cancel()
	if err := l.store.Release(rctx, key, l.opts.Owner); err != nil {
		l.log.WarnContext(ctx, "release lease failed", slog.String("key", key), slog.Any("error", err))
	}
}

// LeasedContext bounds a long operation by timeout and by lease: it keeps
// renewing the row's lease while the operation runs, and cancels the context
// as soon as a renewal fails (another instance took the row over), so two
// instances never drive the same row at once.
func (l *Loop[T]) LeasedContext(parent context.Context, key string, timeout time.Duration) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	interval := l.opts.Lease / 3
	if interval < time.Second {
		interval = time.Second
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				rctx, rcancel := WriteContext(ctx, l.opts.WriteTimeout)
				err := l.store.Renew(rctx, key, l.opts.Owner, l.opts.Lease)
				rcancel()
				if err != nil {
					l.log.WarnContext(parent, "lease lost; abandoning operation", slog.String("key", key), slog.Any("error", err))
					cancel()
					return
				}
			}
		}
	}()
	return ctx, cancel
}
