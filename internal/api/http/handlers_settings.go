package http

import (
	"net/http"
	"strconv"

	"github.com/kunghim/CoBot/internal/domain"
)

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUser(w, r); !ok {
		return
	}
	settings, err := s.App.GetSettings(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, settings)
}

type patchSettingsRequest struct {
	RequestID        string               `json:"requestId"`
	ExpectedRevision *int64               `json:"expectedRevision,omitempty"`
	Patch            domain.SettingsPatch `json:"patch"`
}

func (s *Server) handlePatchSettings(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req patchSettingsRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	var expected int64
	if req.ExpectedRevision != nil {
		expected = *req.ExpectedRevision
	}
	settings, err := s.App.UpdateSettings(r.Context(), principal, req.Patch, expected)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, settings)
}

// parseAfterCursor reads the ?after= cursor of the events stream.
func parseAfterCursor(r *http.Request) (int64, error) {
	raw := r.URL.Query().Get("after")
	if raw == "" {
		return 0, nil
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || v < 0 {
		return 0, domain.InvalidRequest("error.invalid_cursor", map[string]any{"after": raw})
	}
	return v, nil
}
