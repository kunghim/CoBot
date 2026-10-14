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

func (s *Server) handleListTeams(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireUser(w, r)
	if !ok {
		return
	}
	teams, err := s.App.ListTeams(r.Context(), principal)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, map[string]any{"items": teams, "nextCursor": ""})
}

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

func (s *Server) handleRetryTeam(w http.ResponseWriter, r *http.Request) {
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
	team, err := s.App.RetryTeamCreation(r.Context(), principal, r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, team)
}

func (s *Server) handleRebuildMarker(w http.ResponseWriter, r *http.Request) {
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
	result, err := s.App.RebuildMarker(r.Context(), principal, r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, result)
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
	writeData(w, r, http.StatusOK, map[string]any{"items": items, "nextCursor": ""})
}

// pathTeamID is a tiny helper for handlers that only need the id.
func pathTeamID(r *http.Request) string { return r.PathValue("id") }

var _ = domain.Team{}
