package http

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/kunghim/CoBot/internal/domain"
)

func (s *Server) handleWorkspaceWatch(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireUser(w, r)
	if !ok {
		return
	}
	var input struct {
		RequestID string   `json:"requestId"`
		Paths     []string `json:"paths"`
	}
	if err := decodeBody(w, r, &input); err != nil {
		writeError(w, r, err)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, r, domain.CapabilityMissing("error.workspace.watch_unavailable", nil, nil))
		return
	}
	base, release := s.workspaceTaskContext(r.Context())
	defer release()
	ctx, cancel := context.WithTimeout(base, 90*time.Second)
	defer cancel()
	changed, stop, err := s.App.WatchWorkspace(ctx, principal, r.PathValue("scope"), r.PathValue("id"), input.Paths)
	if err != nil {
		if ctx.Err() == nil {
			writeError(w, r, err)
		}
		return
	}
	defer stop()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	if _, err := fmt.Fprint(w, "event: ready\ndata: {}\n\n"); err != nil {
		return
	}
	flusher.Flush()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	var debounce <-chan time.Time
	var timer *time.Timer
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-changed:
			if !ok {
				fmt.Fprint(w, "event: unavailable\ndata: {}\n\n")
				flusher.Flush()
				return
			}
			if debounce == nil {
				timer = time.NewTimer(150 * time.Millisecond)
				debounce = timer.C
			}
		case <-debounce:
			debounce = nil
			if _, err := fmt.Fprint(w, "event: changed\ndata: {}\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
