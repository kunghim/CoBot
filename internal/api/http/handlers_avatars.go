package http

import (
	"encoding/base64"
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

// handleSaveAvatarUpload stores a picture the interface already cropped to
// 256×256 PNG; it is kept in the data root and never leaves the computer.
func (s *Server) handleSaveAvatarUpload(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireUser(w, r)
	if !ok {
		return
	}
	var body struct {
		RequestID string `json:"requestId"`
		Data      string `json:"data"`
	}
	if err := decodeBody(w, r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	data, err := base64.StdEncoding.DecodeString(body.Data)
	if err != nil {
		writeError(w, r, domain.InvalidRequest("error.avatar.upload_format", nil))
		return
	}
	name, err := s.App.SaveAvatarUpload(r.Context(), principal, data)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusCreated, map[string]any{"file": name})
}

func (s *Server) handleGetAvatarUpload(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireUser(w, r)
	if !ok {
		return
	}
	path, err := s.App.AvatarUploadPath(principal, r.PathValue("file"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// The name is the content's hash: the file never changes.
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	http.ServeFile(w, r, path)
}
