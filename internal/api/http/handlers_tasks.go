package http

import (
	"net/http"
)

// handleListTasks projects a team's cards. A member's scope is narrowed
// server-side; the browser never decides who may see what.
func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) { s.handlePage(w, r, "tasks") }

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireUser(w, r)
	if !ok {
		return
	}
	detail, err := s.App.GetTaskDetail(r.Context(), principal, r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, detail)
}

// handleListRequirements lists a team's requirements with their routes.
func (s *Server) handleListRequirements(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireUser(w, r)
	if !ok {
		return
	}
	reqs, err := s.App.ListRequirements(r.Context(), principal, r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, map[string]any{"items": reqs})
}

// handleCancelRequirement stops a requirement for the user.
func (s *Server) handleCancelRequirement(w http.ResponseWriter, r *http.Request) {
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
	requirement, err := s.App.CancelRequirement(r.Context(), principal, r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, requirement)
}
