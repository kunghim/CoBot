package http

import (
	"net/http"

	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/domain"
)

type sendMessageRequest struct {
	RequestID string `json:"requestId"`
	Text      string `json:"text"`
}

func (s *Server) handleListMessages(w http.ResponseWriter, r *http.Request) {
	s.handlePage(w, r, "messages")
}

func (s *Server) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req sendMessageRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	result, err := s.App.SendMessage(r.Context(), principal, app.SendMessageInput{
		BotID: r.PathValue("id"), Text: req.Text, RequestID: req.RequestID,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	// 202: persisted and queued, not delivered/completed.
	writeData(w, r, http.StatusAccepted, result)
}

func (s *Server) handleCancelMessage(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req struct {
		RequestID string `json:"requestId"`
	}
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	message, err := s.App.CancelQueuedMessage(r.Context(), principal, r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, message)
}

func (s *Server) handleInterruptRun(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireUser(w, r)
	if !ok {
		return
	}
	if principal.Kind == "" {
		writeError(w, r, domain.Unauthorized("error.unauthorized.user_required"))
		return
	}
	var req struct {
		RequestID string `json:"requestId"`
	}
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	result, err := s.App.RequestInterrupt(r.Context(), principal, r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusAccepted, result)
}
