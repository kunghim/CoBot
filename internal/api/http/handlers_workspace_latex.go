package http

import (
	"context"
	"net/http"
)

// Ephemeral workspace work must stop before HTTP shutdown waits for handlers.
func (s *Server) workspaceTaskContext(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	select {
	case <-s.StreamStop:
		cancel()
		return ctx, cancel
	default:
	}
	if s.StreamStop != nil {
		go func() {
			select {
			case <-s.StreamStop:
				cancel()
			case <-ctx.Done():
			}
		}()
	}
	return ctx, cancel
}

func (s *Server) handleWorkspaceLatexProject(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireUser(w, r)
	if !ok {
		return
	}
	result, err := s.App.DiscoverWorkspaceLatex(r.Context(), principal, r.PathValue("scope"), r.PathValue("id"), r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeData(w, r, http.StatusOK, result)
}

func (s *Server) handleWorkspaceLatexCompile(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireUser(w, r)
	if !ok {
		return
	}
	var input struct {
		RequestID string `json:"requestId"`
		Main      string `json:"main"`
	}
	if err := decodeBody(w, r, &input); err != nil {
		writeError(w, r, err)
		return
	}
	ctx, cancel := s.workspaceTaskContext(r.Context())
	defer cancel()
	result, err := s.App.CompileWorkspaceLatex(ctx, principal, r.PathValue("scope"), r.PathValue("id"), input.Main)
	if ctx.Err() != nil {
		return
	}
	if err != nil {
		writeError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeData(w, r, http.StatusOK, result)
}
