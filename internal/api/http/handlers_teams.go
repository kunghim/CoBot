package http

import (
	"net/http"

	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/domain"
)

type createTeamRequest struct {
	RequestID      string   `json:"requestId"`
	Name           string   `json:"name"`
	Directory      string   `json:"directory"`
	InitialRoleIDs []string `json:"initialRoleIds"`
}

func (s *Server) handleListTeams(w http.ResponseWriter, r *http.Request) { s.handlePage(w, r, "teams") }

func (s *Server) handleCreateTeam(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req createTeamRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	team, bots, err := s.App.CreateTeam(r.Context(), principal, app.CreateTeamInput{
		Name: req.Name, Directory: req.Directory, InitialRoleIDs: req.InitialRoleIDs, RequestID: req.RequestID,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusCreated, map[string]any{"team": team, "bots": bots})
}

func (s *Server) handleGetTeam(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireUser(w, r)
	if !ok {
		return
	}
	detail, err := s.App.GetTeamDetail(r.Context(), principal, r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, detail)
}

type addBotRequest struct {
	RequestID string `json:"requestId"`
	RoleID    string `json:"roleId"`
}

func (s *Server) handleAddBot(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req addBotRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	bot, err := s.App.AddBot(r.Context(), principal, r.PathValue("id"), req.RoleID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusCreated, bot)
}

func (s *Server) handleListRoles(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUser(w, r); !ok {
		return
	}
	items, err := s.App.RoleLibrary(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, map[string]any{"items": items, "scenarios": s.App.RoleScenarios(), "nextCursor": ""})
}

// pathTeamID is a tiny helper for handlers that only need the id.
func pathTeamID(r *http.Request) string { return r.PathValue("id") }

var _ = domain.Team{}

// handlePatchTeam renames a team; ownership changes are never part of a
// generic patch.
func (s *Server) handlePatchTeam(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req struct {
		RequestID        string  `json:"requestId"`
		Name             *string `json:"name,omitempty"`
		PermissionMode   *string `json:"permissionMode,omitempty"`
		ExpectedRevision *int64  `json:"expectedRevision,omitempty"`
	}
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	var expected int64
	if req.ExpectedRevision != nil {
		expected = *req.ExpectedRevision
	}
	var team domain.Team
	var err error
	switch {
	case req.PermissionMode != nil && req.Name == nil:
		team, err = s.App.PatchTeamPermissionMode(r.Context(), principal, r.PathValue("id"), *req.PermissionMode, expected)
	case req.Name != nil && req.PermissionMode == nil:
		team, err = s.App.PatchTeamName(r.Context(), principal, r.PathValue("id"), *req.Name, expected)
	default:
		err = domain.InvalidRequest("error.team.patch_one_field", nil)
	}
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, team)
}

// handleArchiveTeam pauses dispatch for a team without touching its work.
func (s *Server) handleArchiveTeam(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req struct {
		RequestID        string `json:"requestId"`
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
	team, err := s.App.ArchiveTeam(r.Context(), principal, r.PathValue("id"), expected)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, team)
}

// handleRestoreTeam resumes an archived team.
func (s *Server) handleRestoreTeam(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req struct {
		RequestID        string `json:"requestId"`
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
	team, err := s.App.RestoreTeam(r.Context(), principal, r.PathValue("id"), expected)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, team)
}

// handleDeleteTeam soft-deletes an archived, work-free team; user files stay.
func (s *Server) handleDeleteTeam(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireUser(w, r)
	if !ok {
		return
	}
	expected := principal.Write.ExpectedRevision
	team, err := s.App.DeleteTeam(r.Context(), principal, r.PathValue("id"), expected)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, team)
}

// handleDeleteBot tombstones a member that carries no unfinished work.
func (s *Server) handleDeleteBot(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireUser(w, r)
	if !ok {
		return
	}
	expected := principal.Write.ExpectedRevision
	bot, err := s.App.DeleteBot(r.Context(), principal, r.PathValue("id"), expected)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, bot)
}
