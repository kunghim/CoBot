package http

import (
	"net/http"

	"github.com/kunghim/CoBot/internal/domain"
)

// handleCheckForUpdate looks up the newest published release now.
func (s *Server) handleCheckForUpdate(w http.ResponseWriter, r *http.Request) {
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
	result, err := s.App.CheckForUpdate(r.Context(), principal)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, result)
}

// handleModelCatalog says which model catalog is in use.
func (s *Server) handleModelCatalog(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUser(w, r); !ok {
		return
	}
	writeData(w, r, http.StatusOK, s.App.ModelCatalogStatus(r.Context()))
}

// handleUpdateModelCatalog downloads the newest model catalog now.
func (s *Server) handleUpdateModelCatalog(w http.ResponseWriter, r *http.Request) {
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
	result, err := s.App.UpdateModelCatalog(r.Context(), principal)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, result)
}

// handleResolveModels says what is known about models before they are stored,
// so the settings dialog can show what the catalog gives and what is left to
// the user.
func (s *Server) handleResolveModels(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUser(w, r); !ok {
		return
	}
	var req struct {
		RequestID string                 `json:"requestId"`
		Models    []domain.ProviderModel `json:"models"`
	}
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, map[string]any{"specs": s.App.ResolveModels(req.Models)})
}

// handleApplyEngineCatalog restarts the idle engines that run an older model catalog.
func (s *Server) handleApplyEngineCatalog(w http.ResponseWriter, r *http.Request) {
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
	result, err := s.App.ApplyEngineCatalog(r.Context(), principal)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, result)
}
