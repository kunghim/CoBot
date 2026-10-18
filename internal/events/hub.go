// Package events provides the bounded "something was committed" notification
// used by the SSE stream. Durable facts stay in SQLite; this hub never
// becomes a second source of truth and drops signals instead of blocking
// writers when a consumer is slow.
package events

import "sync"

// Hub fans out commit signals to subscribers.
type Hub struct {
	mu   sync.Mutex
	subs map[int]chan struct{}
	next int
}

// NewHub creates an empty hub.
func NewHub() *Hub {
	return &Hub{subs: map[int]chan struct{}{}}
}

// Publish wakes every subscriber without blocking the writer.
func (h *Hub) Publish() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, ch := range h.subs {
		select {
		case ch <- struct{}{}:
		default: // already pending; consumer will re-read the DB anyway
		}
	}
}

// Subscribe returns a signal channel and an unsubscribe function.
func (h *Hub) Subscribe() (<-chan struct{}, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	id := h.next
	h.next++
	ch := make(chan struct{}, 1)
	h.subs[id] = ch
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subs, id)
			h.mu.Unlock()
		})
	}
}
