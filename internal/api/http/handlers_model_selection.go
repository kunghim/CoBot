package http

import (
	"net/http"
)

type botModelBody struct {
	RequestID        string `json:"requestId"`
	ExpectedRevision *int64 `json:"expectedRevision,omitempty"`
	ProviderID       string `json:"providerId"`
	Model            string `json:"model"`
}

// handlePutBotModel sets the model a member runs with; an empty provider
// puts the member back on the default model.
func (s *Server) handlePutBotModel(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireUser(w, r)
	if !ok {
		return
	}
	var body botModelBody
	if err := decodeBody(w, r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	bot, err := s.App.SetBotModel(r.Context(), principal, r.PathValue("id"), body.ProviderID, body.Model, principal.Write.ExpectedRevision)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, bot)
}

type providerEnabledBody struct {
	RequestID        string `json:"requestId"`
	ExpectedRevision *int64 `json:"expectedRevision,omitempty"`
	Enabled          bool   `json:"enabled"`
}

// handlePutProviderEnabled switches whether members may pick a provider's
// models.
func (s *Server) handlePutProviderEnabled(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireUser(w, r)
	if !ok {
		return
	}
	var body providerEnabledBody
	if err := decodeBody(w, r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	view, err := s.App.SetProviderEnabled(r.Context(), r.PathValue("id"), body.Enabled, principal.Write.ExpectedRevision)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, view)
}
