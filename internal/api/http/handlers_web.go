package http

import (
	"net/http"
)

// The Web completeness surface (M5): activity feed, member memories,
// directory picker/check, live authorizations, and directory rebind. Every
// handler is user-only and delegates to the same application methods the
// rest of the product uses.

func (s *Server) handleListActivity(w http.ResponseWriter, r *http.Request) {
	s.handlePage(w, r, "activity")
}

func (s *Server) handleListMemories(w http.ResponseWriter, r *http.Request) {
	s.handlePage(w, r, "memories")
}

func (s *Server) handleDeleteMemory(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireUser(w, r)
	if !ok {
		return
	}
	if err := s.App.DeleteMemory(r.Context(), principal, r.PathValue("memoryId")); err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, map[string]any{"deleted": true})
}

func (s *Server) handleListDirectories(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireUser(w, r)
	if !ok {
		return
	}
	result, err := s.App.ListDirectoryChildren(r.Context(), principal, r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, result)
}

func (s *Server) handlePickDirectory(w http.ResponseWriter, r *http.Request) {
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
	result, err := s.App.PickDirectory(r.Context(), principal)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, result)
}

func (s *Server) handleCheckDirectory(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req struct {
		RequestID string `json:"requestId"`
		Path      string `json:"path"`
	}
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	result, err := s.App.CheckDirectory(r.Context(), principal, req.Path)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, result)
}

func (s *Server) handleListAuthorizations(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUser(w, r); !ok {
		return
	}
	writeData(w, r, http.StatusOK, map[string]any{"items": s.App.ListAuthorizations()})
}

func (s *Server) handleRevokeAuthorization(w http.ResponseWriter, r *http.Request) {
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
	if err := s.App.RevokeAuthorization(r.Context(), principal, r.PathValue("id")); err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, map[string]any{"revoked": true})
}

func (s *Server) handleRebindDirectory(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req struct {
		RequestID        string `json:"requestId"`
		Directory        string `json:"directory"`
		AdoptMarker      bool   `json:"adoptMarker,omitempty"`
		ExpectedRevision *int64 `json:"expectedRevision,omitempty"`
	}
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	var expected int64
	if req.ExpectedRevision != nil {
		expected = *req.ExpectedRevision
	}
	team, err := s.App.RebindTeamDirectory(r.Context(), principal, r.PathValue("id"), req.Directory, req.AdoptMarker, expected)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, team)
}
