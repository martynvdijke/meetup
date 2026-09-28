package live

import (
	"sync"
	"sync/atomic"
)

// Broker is an in-process fan-out broker for SSE.
type Broker struct {
	mu      sync.Mutex
	subs    map[string]map[chan []byte]struct{}
	closed  bool
	dropped atomic.Uint64
}

// New creates a new Broker.
func New() *Broker {
	return &Broker{
		subs: make(map[string]map[chan []byte]struct{}),
	}
}

// Subscribe registers a subscriber for event. Returns receive-only channel and unsubscribe func.
func (b *Broker) Subscribe(event string) (<-chan []byte, func()) {
	ch := make(chan []byte, 16)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		close(ch)
		return ch, func() {}
	}
	m, ok := b.subs[event]
	if !ok {
		m = make(map[chan []byte]struct{})
		b.subs[event] = m
	}
	m[ch] = struct{}{}

	var once sync.Once
	unsub := func() {
		once.Do(func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			if m2, ok := b.subs[event]; ok {
				if _, exists := m2[ch]; exists {
					delete(m2, ch)
					if len(m2) == 0 {
						delete(b.subs, event)
					}
					close(ch)
				}
			}
		})
	}
	return ch, unsub
}

// Broadcast delivers payload to every subscriber of event non-blocking.
func (b *Broker) Broadcast(event string, payload []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	m, ok := b.subs[event]
	if !ok {
		return
	}
	for ch := range m {
		cp := make([]byte, len(payload))
		copy(cp, payload)
		select {
		case ch <- cp:
		default:
			b.dropped.Add(1)
		}
	}
}

// Subscribers reports count for event.
func (b *Broker) Subscribers(event string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs[event])
}

// DroppedTotal returns total dropped payloads.
func (b *Broker) DroppedTotal() uint64 {
	return b.dropped.Load()
}

// Close drops every subscription and closes all channels. Safe to call multiple times.
func (b *Broker) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	for _, m := range b.subs {
		for ch := range m {
			close(ch)
		}
	}
	b.subs = make(map[string]map[chan []byte]struct{})
}
