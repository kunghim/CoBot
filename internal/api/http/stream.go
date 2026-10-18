package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/events"
)

// handleSnapshot returns the consistent read clients take before following
// the event stream.
func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUser(w, r); !ok {
		return
	}
	snapshot, err := s.App.Snapshot(r.Context(), PrincipalOf(r))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, snapshot)
}

// handleEvents streams durable events after a cursor with fetch SSE. The
// handler subscribes to the commit hub first, then reads the database, so a
// commit landing between subscribe and first read is never missed. A slow
// client only loses wakeup hints: state always converges through the DB
// query, and clients re-sync from a snapshot when the cursor no longer
// matches this instance.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUser(w, r); !ok {
		return
	}
	after, err := parseAfterCursor(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, r, domain.CapabilityMissing("error.capability.no_flusher", nil, nil))
		return
	}
	scope := domain.Scope{TeamID: r.URL.Query().Get("teamId"), BotID: r.URL.Query().Get("botId")}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	fmt.Fprintf(w, ": connected cursor=%d\n\n", after)
	flusher.Flush()

	notify, unsubscribe := s.HubSubscribe()
	defer unsubscribe()
	// Live progress rides the same stream as its own event type. It carries
	// no cursor: a missed signal is recovered from the stored steps.
	var live <-chan events.Live
	if s.App.Live != nil {
		ch, unsubscribeLive := s.App.Live.Subscribe()
		defer unsubscribeLive()
		live = ch
	}

	poll := time.NewTicker(2 * time.Second)
	defer poll.Stop()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	send := func() (int64, error) {
		events, err := s.App.ListEvents(r.Context(), PrincipalOf(r), scope, after, 100)
		if err != nil {
			return after, err
		}
		for _, ev := range events {
			payload, err := json.Marshal(ev)
			if err != nil {
				return after, err
			}
			if _, err := fmt.Fprintf(w, "id: %d\nevent: cobot.event\ndata: %s\n\n", ev.Seq, payload); err != nil {
				return after, err
			}
			after = ev.Seq
		}
		flusher.Flush()
		return after, nil
	}

	for {
		var err error
		select {
		case <-s.StreamStop:
			return
		case <-r.Context().Done():
			return
		case <-notify:
			after, err = send()
		case <-poll.C:
			after, err = send()
		case signal := <-live:
			if scope.TeamID != "" && signal.TeamID != scope.TeamID || scope.BotID != "" && signal.BotID != scope.BotID {
				continue
			}
			payload, merr := json.Marshal(signal)
			if merr != nil {
				continue
			}
			if _, werr := fmt.Fprintf(w, "event: cobot.live\ndata: %s\n\n", payload); werr != nil {
				return
			}
			flusher.Flush()
			continue
		case <-heartbeat.C:
			if _, err := fmt.Fprintf(w, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
			continue
		}
		if err != nil {
			s.Logger.Warn("sse stream ended", "error", err)
			return
		}
	}
}

// HubSubscribe exposes the commit hub to the stream handler.
func (s *Server) HubSubscribe() (<-chan struct{}, func()) {
	return s.Hub.Subscribe()
}
