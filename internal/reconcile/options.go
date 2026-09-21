package reconcile

import (
	"math/rand/v2"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Options tune a Loop and its Backoff. Zero values take the defaults.
type Options struct {
	// Owner is this instance's lease owner id (host/short-uuid by default).
	Owner string
	// Interval is the tick between passes when nothing kicks the loop.
	Interval time.Duration
	// Lease is how long a claimed row belongs to this instance; long
	// operations renew it every Lease/3.
	Lease time.Duration
	// BackoffBase and BackoffCap bound the fast retry schedule (exponential
	// from base, capped, ±20% jitter).
	BackoffBase time.Duration
	BackoffCap  time.Duration
	// MaxAttempts is the fast retry budget; beyond it a row retries every
	// SlowRetryInterval and callers treat the failure as final.
	MaxAttempts       int32
	SlowRetryInterval time.Duration
	// Batch caps the rows one pass claims; Concurrency caps in-flight handlers.
	Batch       int32
	Concurrency int
	// WriteTimeout bounds every store write the loop performs on its own
	// (claims, renewals, releases).
	WriteTimeout time.Duration
	// Now and Rand feed scheduling and jitter; tests pin them. Because they
	// are read on every use, a caller may swap them after construction.
	Now  func() time.Time
	Rand func() float64
}

// WithDefaults fills zero fields. The retry schedule defaults are those the
// workspace reconciler was tuned with: 10s, 20s, 40s, 80s, 90s, 90s of fast
// retries (about 5.5 minutes), then every 15 minutes.
func (o Options) WithDefaults() Options {
	if o.Owner == "" {
		host, _ := os.Hostname()
		o.Owner = strings.TrimSpace(host) + "/" + uuid.NewString()[:8]
	}
	if o.Interval <= 0 {
		o.Interval = 15 * time.Second
	}
	if o.Lease <= 0 {
		o.Lease = 2 * time.Minute
	}
	if o.BackoffBase <= 0 {
		o.BackoffBase = 10 * time.Second
	}
	if o.BackoffCap <= 0 {
		o.BackoffCap = 90 * time.Second
	}
	if o.MaxAttempts <= 0 {
		o.MaxAttempts = 6
	}
	if o.SlowRetryInterval <= 0 {
		o.SlowRetryInterval = 15 * time.Minute
	}
	if o.Batch <= 0 {
		o.Batch = 20
	}
	if o.Concurrency <= 0 {
		o.Concurrency = 4
	}
	if o.WriteTimeout <= 0 {
		o.WriteTimeout = 15 * time.Second
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Rand == nil {
		o.Rand = rand.Float64
	}
	return o
}

// Backoff returns the retry policy these options describe.
func (o Options) Backoff() Backoff {
	return Backoff{
		Base: o.BackoffBase, Cap: o.BackoffCap, MaxAttempts: o.MaxAttempts,
		SlowRetryInterval: o.SlowRetryInterval, Now: o.Now, Rand: o.Rand,
	}
}
