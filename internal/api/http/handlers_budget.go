package http

import (
	"encoding/json"
	"net/http"

	"github.com/kunghim/CoBot/internal/domain"
)

type budgetBody struct {
	RequestID string `json:"requestId"`
	// Limit is a number of tokens a day, 0 for no limit, or null to follow
	// the default. It must be present: leaving it out is not "follow the
	// default", it is a mistake.
	Limit json.RawMessage `json:"limit"`
}

func (b budgetBody) limit() (*int64, error) {
	if len(b.Limit) == 0 {
		return nil, domain.InvalidRequest("error.budget.limit_required", nil)
	}
	if string(b.Limit) == "null" {
		return nil, nil
	}
	var value int64
	if err := json.Unmarshal(b.Limit, &value); err != nil {
		return nil, domain.InvalidRequest("error.budget.limit_range", nil)
	}
	return &value, nil
}

func (s *Server) handleGetTeamBudget(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireUser(w, r)
	if !ok {
		return
	}
	budget, err := s.App.TeamBudget(r.Context(), principal, r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, budget)
}

func (s *Server) handlePutTeamBudget(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireUser(w, r)
	if !ok {
		return
	}
	var body budgetBody
	if err := decodeBody(w, r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	limit, err := body.limit()
	if err != nil {
		writeError(w, r, err)
		return
	}
	result, err := s.App.SetTeamBudget(r.Context(), principal, r.PathValue("id"), limit)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, map[string]any{"limit": limit, "clamped": result.Clamped})
}

func (s *Server) handlePutBotBudget(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireUser(w, r)
	if !ok {
		return
	}
	var body budgetBody
	if err := decodeBody(w, r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	limit, err := body.limit()
	if err != nil {
		writeError(w, r, err)
		return
	}
	result, err := s.App.SetBotBudget(r.Context(), principal, r.PathValue("id"), limit)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, map[string]any{"limit": limit, "clamped": result.Clamped})
}

type resetBody struct {
	RequestID string `json:"requestId"`
}

func (s *Server) handleResetTeamBudget(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireUser(w, r)
	if !ok {
		return
	}
	var body resetBody
	if err := decodeBody(w, r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	if err := s.App.ResetTeamBudget(r.Context(), principal, r.PathValue("id")); err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, map[string]any{"reset": true})
}

func (s *Server) handleResetBotBudget(w http.ResponseWriter, r *http.Request) {
	principal, ok := requireUser(w, r)
	if !ok {
		return
	}
	var body resetBody
	if err := decodeBody(w, r, &body); err != nil {
		writeError(w, r, err)
		return
	}
	if err := s.App.ResetBotBudget(r.Context(), principal, r.PathValue("id")); err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, map[string]any{"reset": true})
}
