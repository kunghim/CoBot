package codex

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

// WatchDirectory provides invalidation hints only. Callers still authorize and
// read files through their workspace boundary, including after every event.
// Canceling ctx releases this connection's native subscription.
func (s *Server) WatchDirectory(ctx context.Context, path string) (<-chan struct{}, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	id := "cobot-" + rand.Text()
	changed := make(chan struct{}, 1)
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, errors.New("app-server is closed")
	}
	if s.watches == nil {
		s.watches = make(map[string]chan struct{})
	}
	s.watches[id] = changed
	s.mu.Unlock()
	stop := sync.OnceFunc(func() {
		defer close(changed)
		s.mu.Lock()
		delete(s.watches, id)
		s.mu.Unlock()
		select {
		case <-s.client.Closed():
			return
		default:
		}
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.client.Call(cleanup, "fs/unwatch", map[string]any{"watchId": id}, nil); err != nil && s.logger != nil {
			s.logger.Warn("filesystem unwatch failed", "error", err)
		}
	})
	registration, registered := context.WithTimeout(ctx, 5*time.Second)
	err := s.client.Call(registration, "fs/watch", map[string]any{"watchId": id, "path": path}, nil)
	registered()
	if err != nil {
		// A canceled RPC can have reached the engine before its response was
		// discarded. Unwatch the same ID even when registration is uncertain.
		go stop()
		return nil, err
	}
	go func() {
		select {
		case <-ctx.Done():
		case <-s.client.Closed():
		}
		stop()
	}()
	return changed, nil
}

func (s *Server) filesystemChanged(raw json.RawMessage) {
	var event struct {
		WatchID string `json:"watchId"`
	}
	if json.Unmarshal(raw, &event) != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if changed := s.watches[event.WatchID]; changed != nil {
		select {
		case changed <- struct{}{}:
		default:
		}
	}
}
