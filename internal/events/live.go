package events

import "sync"

// Live is a transient progress signal of a running turn: a step that started
// or changed, streamed text, a reconnect. It is never a durable fact; a
// client that misses one converges on the stored steps at its next read.
type Live struct {
	Type   string `json:"type"` // step | delta
	ChatID string `json:"chatId,omitempty"`
	TeamID string `json:"teamId"`
	BotID  string `json:"botId"`
	RunID  string `json:"runId"`
	// Step is the stored step for Type "step".
	Step any `json:"step,omitempty"`
	// ItemID/Kind/Text describe a "delta": Kind is message | commentary |
	// reasoning | output.
	ItemID string `json:"itemId,omitempty"`
	Kind   string `json:"kind,omitempty"`
	Text   string `json:"text,omitempty"`
}

// LiveHub fans out live signals. A slow subscriber loses signals instead of
// slowing the engine reader.
type LiveHub struct {
	mu   sync.Mutex
	subs map[int]chan Live
	next int
}

// NewLiveHub creates an empty live hub.
func NewLiveHub() *LiveHub {
	return &LiveHub{subs: map[int]chan Live{}}
}

// Publish delivers a signal to every subscriber that has room for it.
func (h *LiveHub) Publish(signal Live) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, ch := range h.subs {
		select {
		case ch <- signal:
		default:
		}
	}
}

// Subscribe returns a signal channel and an unsubscribe function.
func (h *LiveHub) Subscribe() (<-chan Live, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	id := h.next
	h.next++
	ch := make(chan Live, 512)
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
