package http

import (
	"net/http"
	"path/filepath"
	"strings"

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
	w.Header().Set("Content-Type", workspaceContentType(f.Info.Kind, f.Info.Name))
	http.ServeContent(w, r, f.Info.Name, f.Info.ModifiedAt, f.Handle)
}

func workspaceContentType(kind, name string) string {
	switch kind {
	case "text", "markdown", "html", "latex", "notebook":
		return "text/plain; charset=utf-8"
	case "pdf":
		return "application/pdf"
	case "svg":
		return "image/svg+xml"
	case "image":
		ext := strings.ToLower(filepath.Ext(name))
		if ext == ".jpg" {
			ext = ".jpeg"
		}
		if ext == ".ico" {
			return "image/x-icon"
		}
		return "image/" + strings.TrimPrefix(ext, ".")
	case "audio":
		switch strings.ToLower(filepath.Ext(name)) {
		case ".mp3":
			return "audio/mpeg"
		case ".m4a", ".mp4a":
			return "audio/mp4"
		case ".oga", ".opus", ".ogg":
			return "audio/ogg"
		case ".wav":
			return "audio/wav"
		case ".flac":
			return "audio/flac"
		case ".aac":
			return "audio/aac"
		}
	case "video":
		switch strings.ToLower(filepath.Ext(name)) {
		case ".mp4":
			return "video/mp4"
		case ".ogv":
			return "video/ogg"
		case ".webm":
			return "video/webm"
		}
	}
	return "application/octet-stream"
}
