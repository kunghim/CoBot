package http

import "net/http"

func (s *Server) handleWorkspaceChangeRuns(w http.ResponseWriter, r *http.Request) {
	p, ok := requireUser(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	result, err := s.App.ListWorkspaceChangeRuns(r.Context(), p, r.PathValue("scope"), r.PathValue("id"), query.Get("botId"), query.Get("cursor"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, result)
}

func (s *Server) handleWorkspaceChangeFiles(w http.ResponseWriter, r *http.Request) {
	p, ok := requireUser(w, r)
	if !ok {
		return
	}
	result, err := s.App.ListWorkspaceChangeFiles(r.Context(), p, r.PathValue("scope"), r.PathValue("id"), r.PathValue("runId"), r.URL.Query().Get("cursor"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, result)
}

func (s *Server) handleWorkspaceChangeDiff(w http.ResponseWriter, r *http.Request) {
	p, ok := requireUser(w, r)
	if !ok {
		return
	}
	result, err := s.App.GetWorkspaceChangeDiff(r.Context(), p, r.PathValue("scope"), r.PathValue("id"), r.PathValue("runId"), r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, result)
}
