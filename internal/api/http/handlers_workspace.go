package http

import (
	"net/http"

	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/workspace"
)

func (s *Server) handleWorkspaceEntries(w http.ResponseWriter, r *http.Request) {
	p, ok := requireUser(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	result, err := s.App.ListWorkspaceEntries(r.Context(), p, r.PathValue("scope"), r.PathValue("id"), q.Get("path"), q.Get("cursor"), q.Get("includeHidden") == "true")
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, result)
}

func (s *Server) handleWorkspaceFile(w http.ResponseWriter, r *http.Request) {
	p, ok := requireUser(w, r)
	if !ok {
		return
	}
	f, err := s.App.OpenWorkspaceFile(r.Context(), p, r.PathValue("scope"), r.PathValue("id"), r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	defer f.Close()
	writeData(w, r, http.StatusOK, f.Info)
}

func (s *Server) handleWorkspaceContent(w http.ResponseWriter, r *http.Request) {
	p, ok := requireUser(w, r)
	if !ok {
		return
	}
	f, err := s.App.OpenWorkspaceFile(r.Context(), p, r.PathValue("scope"), r.PathValue("id"), r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	defer f.Close()
	if f.Info.Kind == "directory" {
		writeError(w, r, domain.InvalidRequest("error.workspace.invalid_path", nil))
		return
	}
	if f.Info.Size > workspace.MaxPreviewBytes {
		writeError(w, r, domain.InvalidRequest("error.workspace.too_large", nil))
		return
	}
	w.Header().Set("ETag", f.Info.Version)
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'; style-src 'unsafe-inline'; img-src data: blob:; media-src blob:; form-action 'none'; base-uri 'none'")
	// Executable formats stay inert even if this URL is opened directly.
	w.Header().Set("Content-Type", workspace.ContentType(f.Info.Kind, f.Info.Name))
	http.ServeContent(w, r, f.Info.Name, f.Info.ModifiedAt, f.Handle)
}
