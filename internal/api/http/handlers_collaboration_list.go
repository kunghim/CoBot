package http

import (
	"net/http"
)

// handleListCollaboration returns the read-only exchange between the named
// member and one peer. It is a record, not a chat: sending happens through the
// bot's own tools or the user's conversation.
func (s *Server) handleListCollaboration(w http.ResponseWriter, r *http.Request) {
	s.handlePage(w, r, "collaboration")
}
