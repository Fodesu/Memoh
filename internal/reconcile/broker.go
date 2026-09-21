package reconcile

import "sync"

// Broker fans events out to in-process subscribers keyed by row. Delivery is
// best effort: a subscriber that falls behind loses events, so the durable
// answer must always come from the store (see Await).
type Broker[E any] struct {
	buffer int
	mu     sync.Mutex
	subs   map[string]map[chan E]struct{}
}

// NewBroker builds a Broker whose subscriber channels hold `buffer` events.
func NewBroker[E any](buffer int) *Broker[E] {
	if buffer <= 0 {
		buffer = 64
	}
	return &Broker[E]{buffer: buffer, subs: make(map[string]map[chan E]struct{})}
}

// Subscribe returns a channel of events for key and a function that
// unsubscribes it; calling the function more than once is safe.
func (b *Broker[E]) Subscribe(key string) (<-chan E, func()) {
	ch := make(chan E, b.buffer)
	b.mu.Lock()
	if b.subs[key] == nil {
		b.subs[key] = make(map[chan E]struct{})
	}
	b.subs[key][ch] = struct{}{}
	b.mu.Unlock()
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			b.mu.Lock()
			if set, ok := b.subs[key]; ok {
				delete(set, ch)
				if len(set) == 0 {
					delete(b.subs, key)
				}
			}
			b.mu.Unlock()
		})
	}
}

// Publish delivers ev to every subscriber of key without blocking.
func (b *Broker[E]) Publish(key string, ev E) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs[key] {
		select {
		case ch <- ev:
		default:
		}
	}
}
