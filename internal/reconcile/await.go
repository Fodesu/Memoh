package reconcile

import (
	"context"
	"time"
)

// Await polls get every interval until done reports the value final, and
// returns it. On ctx expiry it returns the last value seen with ctx.Err(), so
// a caller can still describe where things stood. Because it reads the store
// it works across Server instances, unlike Broker events.
func Await[T any](ctx context.Context, interval time.Duration, get func(context.Context) (T, error), done func(T) bool) (T, error) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var last T
	for {
		v, err := get(ctx)
		if err != nil {
			return last, err
		}
		last = v
		if done(v) {
			return v, nil
		}
		select {
		case <-ctx.Done():
			return v, ctx.Err()
		case <-ticker.C:
		}
	}
}

// WriteContext derives a fresh short-lived context from ctx that survives its
// cancellation, for persisting an outcome after the operation context is gone.
func WriteContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), timeout)
}
