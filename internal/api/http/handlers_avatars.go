package http

import (
	"encoding/json"
	"net/http"

	"github.com/kunghim/CoBot/internal/domain"
)

// avatarBody is the body of an avatar change. The avatar field must be
// present: an object sets the avatar, null returns to the default.
type avatarBody struct {
	RequestID        string          `json:"requestId"`
	ExpectedRevision *int64          `json:"expectedRevision,omitempty"`
	Avatar           json.RawMessage `json:"avatar"`
}

func (b avatarBody) ref() (*domain.AvatarRef, error) {
	if len(b.Avatar) == 0 {
		return nil, domain.InvalidRequest("error.avatar.invalid", map[string]any{"field": "avatar"})
	}
	if string(b.Avatar) == "null" {
		return nil, nil
	}
	var ref domain.AvatarRef
	if err := json.Unmarshal(b.Avatar, &ref); err != nil {
		return nil, domain.InvalidRequest("error.avatar.invalid", map[string]any{"field": "avatar"})
	}
	return &ref, nil
}

func (s *Server) handleGetAvatarCatalog(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireUser(w, r)
	if !ok {
		return
	}
	catalog, err := s.App.AvatarCatalog(r.Context(), principal)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, catalog)
}

func (s *Server) handleSyncAvatarCatalog(w http.ResponseWriter, r *http.Request) {
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
	catalog, err := s.App.SyncAvatarCatalog(r.Context(), principal)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, catalog)
}

// handlePatchBot changes a member itself; today only its avatar.
func (s *Server) handlePatchBot(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireUser(w, r)
	if !ok {
		return
	}
	var body avatarBody
	if err := decodeBody(w, r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	ref, err := body.ref()
	if err != nil {
		writeError(w, r, err)
		return
	}
	bot, err := s.App.SetBotAvatar(r.Context(), principal, r.PathValue("id"), ref, principal.Write.ExpectedRevision)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, bot)
}

// handlePutRoleAvatar sets a role's avatar; null returns it to the default.
func (s *Server) handlePutRoleAvatar(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireUser(w, r)
	if !ok {
		return
	}
	var body avatarBody
	if err := decodeBody(w, r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	ref, err := body.ref()
	if err != nil {
		writeError(w, r, err)
		return
	}
	if err := s.App.SetRoleAvatar(r.Context(), principal, r.PathValue("id"), ref); err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, map[string]any{"roleId": r.PathValue("id"), "avatar": ref})
}
