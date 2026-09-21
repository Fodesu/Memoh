package reconcile

import "time"

// Jitter spreads retries of rows that failed together (a registry hiccup
// across many bots, several Server instances) over ±20% so they do not come
// back as one spike.
const Jitter = 0.2

// NextBackoff returns when the next attempt may run after attempt number
// `attempts` (1-based) failed. Exponential from base, capped, no jitter.
func NextBackoff(now time.Time, attempts int32, base, capDuration time.Duration) time.Time {
	if attempts < 1 {
		attempts = 1
	}
	d := base
	for i := int32(1); i < attempts && d < capDuration; i++ {
		d *= 2
	}
	if d > capDuration {
		d = capDuration
	}
	return now.Add(d)
}

// Backoff is the retry policy: a fast exponential budget of MaxAttempts, then
// a slow fixed cadence that never gives up. Build it with Options.Backoff() so
// every field is defaulted; a zero MaxAttempts or SlowRetryInterval would
// spend the budget on the first failure and retry immediately.
type Backoff struct {
	Base              time.Duration
	Cap               time.Duration
	MaxAttempts       int32
	SlowRetryInterval time.Duration
	Now               func() time.Time
	Rand              func() float64
}

func (b Backoff) now() time.Time {
	if b.Now == nil {
		return time.Now()
	}
	return b.Now()
}

func (b Backoff) jittered(d time.Duration) time.Duration {
	r := 0.5
	if b.Rand != nil {
		r = b.Rand()
	}
	factor := 1 - Jitter + 2*Jitter*r
	return time.Duration(float64(d) * factor)
}

// NextAttempt schedules the retry after attempt number `attempts` failed.
func (b Backoff) NextAttempt(attempts int32) time.Time {
	now := b.now()
	d := NextBackoff(now, attempts, b.Base, b.Cap).Sub(now)
	return now.Add(b.jittered(d))
}

// SlowRetryAt schedules the next background attempt once the fast budget is
// spent.
func (b Backoff) SlowRetryAt() time.Time {
	return b.now().Add(b.jittered(b.SlowRetryInterval))
}

// Spend records one more failed attempt on top of `attempts` and returns the
// new attempt count with the time of the next try. A retryable failure inside
// the budget schedules a fast retry; anything else spends the whole budget at
// once (so callers see a final outcome) and falls back to the slow cadence.
func (b Backoff) Spend(attempts int32, retryable bool) (int32, time.Time) {
	attempts++
	if retryable && attempts < b.MaxAttempts {
		return attempts, b.NextAttempt(attempts)
	}
	if attempts < b.MaxAttempts {
		attempts = b.MaxAttempts
	}
	return attempts, b.SlowRetryAt()
}

// Pending reports whether a failure with `attempts` consumed is still inside
// the fast budget, i.e. the loop is about to retry it soon.
func (b Backoff) Pending(attempts int32) bool {
	return attempts < b.MaxAttempts
}
