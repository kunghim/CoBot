package http

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/kunghim/CoBot/internal/browser"
	"github.com/kunghim/CoBot/internal/domain"
)

// handleBrowserPanel lists the tabs of a team's browser.
func (s *Server) handleBrowserPanel(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireUser(w, r)
	if !ok {
		return
	}
	result, err := s.App.BrowserPanel(r.Context(), principal, r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, result)
}

// handleBrowserStream pushes a tab's pictures as server-sent events. Only the
// newest picture matters, so a slow client skips frames instead of lagging.
func (s *Server) handleBrowserStream(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireUser(w, r)
	if !ok {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, r, domain.CapabilityMissing("error.capability.no_flusher", nil, nil))
		return
	}
	frames := make(chan browser.Frame, 1)
	send := func(f browser.Frame) {
		for {
			select {
			case frames <- f:
				return
			default:
				// Replace the waiting picture with the newer one.
				select {
				case <-frames:
				default:
				}
			}
		}
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	failed := make(chan error, 1)
	go func() {
		failed <- s.App.BrowserWatch(ctx, principal, r.PathValue("id"), r.URL.Query().Get("tab"), send)
	}()

	opened := false
	open := func() {
		if opened {
			return
		}
		opened = true
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
		fmt.Fprint(w, ": connected\n\n")
		flusher.Flush()
	}
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-s.StreamStop:
			return
		case <-ctx.Done():
			return
		case err := <-failed:
			if err == nil || ctx.Err() != nil {
				return
			}
			if !opened {
				writeError(w, r, err)
				return
			}
			fmt.Fprintf(w, "event: ended\ndata: %q\n\n", err.Error())
			flusher.Flush()
			return
		case f := <-frames:
			open()
			payload, _ := json.Marshal(map[string]any{
				"tab": f.Tab, "width": f.Width, "height": f.Height,
				"data": base64.StdEncoding.EncodeToString(f.JPEG),
			})
			if _, err := fmt.Fprintf(w, "event: frame\ndata: %s\n\n", payload); err != nil {
				return
			}
			flusher.Flush()
		case <-heartbeat.C:
			open()
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// handleBrowserInput applies one user input to a tab.
func (s *Server) handleBrowserInput(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req struct {
		RequestID string `json:"requestId"` // accepted, not needed: an input is not replayed
		Tab       string `json:"tab"`
		browser.Input
	}
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	if err := s.App.BrowserInput(r.Context(), principal, r.PathValue("id"), req.Tab, req.Input); err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, map[string]any{"ok": true})
}

// handleOpenBrowserTab opens a page in a tab the user owns. The user asked
// for it, so the team's browser comes up when it is down.
func (s *Server) handleOpenBrowserTab(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req struct {
		RequestID string `json:"requestId"` // accepted, not needed: an open is not replayed
		URL       string `json:"url"`
	}
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	tab, err := s.App.OpenUserTab(r.Context(), principal, r.PathValue("id"), req.URL)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusCreated, map[string]any{"tab": tab})
}

// handleCloseBrowserTab closes one tab: the user's own page or a member's.
func (s *Server) handleCloseBrowserTab(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireUser(w, r)
	if !ok {
		return
	}
	if err := s.App.CloseBrowserTab(r.Context(), principal, r.PathValue("id"), r.PathValue("tab")); err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, map[string]any{"ok": true})
}

// handleBrowserViewport sets a tab's real window size (a preset or an
// explicit size); the page renders at it for the user and every member.
func (s *Server) handleBrowserViewport(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req struct {
		RequestID string `json:"requestId"` // accepted, not needed: a viewport is not replayed
		browser.ViewportRequest
	}
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	viewport, err := s.App.SetBrowserViewport(r.Context(), principal, r.PathValue("id"), r.PathValue("tab"), req.ViewportRequest)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, map[string]any{"viewport": viewport})
}

func (s *Server) handleStartPreview(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req struct {
		RequestID string `json:"requestId"`
		Command   string `json:"command"`
	}
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	result, err := s.App.StartPreview(r.Context(), principal, r.PathValue("id"), req.Command)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, result)
}

func (s *Server) handleStopPreview(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req struct {
		RequestID string `json:"requestId"`
		Command   string `json:"command"`
	}
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	if err := s.App.StopPreview(r.Context(), principal, r.PathValue("id"), req.Command); err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, map[string]any{"stopped": true})
}
