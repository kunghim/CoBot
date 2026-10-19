package http

import "net/http"

// handleRetryRun puts the work of a member's failed turn back in its queue.
func (s *Server) handleRetryRun(w http.ResponseWriter, r *http.Request) {
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
	result, err := s.App.RetryRun(r.Context(), principal, r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusAccepted, result)
}
