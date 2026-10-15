package http

import (
	"net/http"
	"strconv"
)

// handleListCollaboration returns the read-only exchange between the named
// member and one peer. It is a record, not a chat: sending happens through the
// bot's own tools or the user's conversation.
func (s *Server) handleListCollaboration(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireUser(w, r)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	messages, err := s.App.ListCollaboration(r.Context(), principal, r.PathValue("id"), limit)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, map[string]any{"items": messages, "nextCursor": ""})
}
