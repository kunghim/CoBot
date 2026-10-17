package http

import (
	"net/http"

	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/domain"
)

func (s *Server) handleEngine(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUser(w, r); !ok {
		return
	}
	info := s.App.EngineStatus(r.Context())
	writeData(w, r, http.StatusOK, engineProjection(info))
}

// handleEngineCheck re-runs the engine self-check, including native hook
// trust, for the settings page's retry button.
func (s *Server) handleEngineCheck(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUser(w, r); !ok {
		return
	}
	var req struct {
		RequestID string `json:"requestId"`
	}
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	info := s.App.RetryEngineCheck(r.Context())
	writeData(w, r, http.StatusOK, engineProjection(info))
}

func engineProjection(info app.EngineStatusInfo) EngineStatus {
	return EngineStatus{
		Status:       info.Status,
		BinaryPath:   info.BinaryPath,
		Version:      info.Version,
		Protocol:     info.Protocol,
		LoggedIn:     info.LoggedIn,
		Capabilities: append(info.Capabilities, "authMode:"+info.AuthMode),
		ErrorKey:     info.ErrorKey,
	}
}

func (s *Server) handleEngineLogin(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUser(w, r); !ok {
		return
	}
	var req struct {
		RequestID string `json:"requestId"`
	}
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	login, err := s.App.StartEngineLogin(r.Context())
	if err != nil {
		writeError(w, r, domain.EngineUnavailable("error.engine.login_start_failed", nil, err))
		return
	}
	writeData(w, r, http.StatusAccepted, login)
}

func (s *Server) handleEngineLoginCancel(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUser(w, r); !ok {
		return
	}
	var req struct {
		RequestID string `json:"requestId"`
	}
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	if err := s.App.CancelEngineLogin(r.Context()); err != nil {
		writeError(w, r, domain.EngineUnavailable("error.engine.login_cancel_failed", nil, err))
		return
	}
	writeData(w, r, http.StatusOK, map[string]any{"loginCancelled": true})
}

func (s *Server) handleEngineLogout(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUser(w, r); !ok {
		return
	}
	var req struct {
		RequestID string `json:"requestId"`
	}
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	if err := s.App.EngineLogout(r.Context()); err != nil {
		writeError(w, r, domain.EngineUnavailable("error.engine.logout_failed", nil, err))
		return
	}
	writeData(w, r, http.StatusOK, map[string]any{"loggedOut": true})
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUser(w, r); !ok {
		return
	}
	models, err := s.App.ModelList(r.Context())
	if err != nil {
		writeError(w, r, domain.EngineUnavailable("error.engine.models_unavailable", nil, err))
		return
	}
	writeData(w, r, http.StatusOK, models)
}

// handleBotStats returns one member's session statistics.
func (s *Server) handleBotStats(w http.ResponseWriter, r *http.Request) {
	p, ok := requireUser(w, r)
	if !ok {
		return
	}
	stats, err := s.App.BotStats(r.Context(), p, r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, stats)
}
