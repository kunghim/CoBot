package http

import (
	"net/http"
	"strings"
	"time"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
)

type bootstrapRequest struct {
	RequestID string `json:"requestId"`
	Secret    string `json:"secret"`
}

type bootstrapResponse struct {
	SessionCookieName string    `json:"sessionCookieName"`
	CSRFCookieName    string    `json:"csrfCookieName"`
	SessionExpiresAt  time.Time `json:"sessionExpiresAt"`
	UserID            string    `json:"userId"`
	// BearerToken is issued for the desktop shell, whose page runs on the
	// Wails asset origin and cannot rely on SameSite=Strict cookies. The
	// browser flow ignores it; credentials stay purpose-separated.
	BearerToken     string    `json:"bearerToken,omitempty"`
	BearerExpiresAt time.Time `json:"bearerExpiresAt,omitempty"`
}

// handleSessionBootstrap exchanges a one-time startup secret for a page
// session. Secrets are single-use and short-lived; the raw value never
// reaches logs or the URL query.
func (s *Server) handleSessionBootstrap(w http.ResponseWriter, r *http.Request) {
	var req bootstrapRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	if req.Secret == "" {
		writeError(w, r, domain.InvalidRequest("error.bootstrap.secret_required", nil))
		return
	}
	if err := s.Issuer.Redeem(req.Secret); err != nil {
		writeError(w, r, domain.Unauthorized("error.unauthorized.bad_bootstrap"))
		return
	}
	sess, err := s.Sessions.Create(auth.LocalUserID)
	if err != nil {
		writeError(w, r, domain.Internal(err))
		return
	}
	bearer, err := s.Bearer.Issue(auth.LocalUserID)
	if err != nil {
		writeError(w, r, domain.Internal(err))
		return
	}
	// Loopback HTTP only, so Secure is off in practice; SameSite=Strict plus
	// the CSRF token still bind state-changing calls to this origin.
	s.setSessionCookies(w, sess, false)
	writeData(w, r, http.StatusOK, bootstrapResponse{
		SessionCookieName: s.sessionCookieName(), CSRFCookieName: s.csrfCookieName(),
		SessionExpiresAt: sess.ExpiresAt,
		UserID:           sess.UserID,
		BearerToken:      bearer.Token,
		BearerExpiresAt:  bearer.ExpiresAt,
	})
}

type logoutResponse struct {
	LoggedOut bool `json:"loggedOut"`
}

func (s *Server) handleSessionLogout(w http.ResponseWriter, r *http.Request) {
	p, ok := requireUser(w, r)
	if !ok {
		return
	}
	if p.SessionID != "" {
		s.Sessions.Delete(p.SessionID)
	}
	if h := r.Header.Get("Authorization"); len(h) > 7 && h[:7] == "Bearer " {
		s.Bearer.Delete(strings.TrimSpace(h[7:]))
	}
	s.clearSessionCookies(w)
	writeData(w, r, http.StatusOK, logoutResponse{LoggedOut: true})
}

type linkRequest struct {
	RequestID string `json:"requestId"`
}

func (s *Server) handleInternalStop(w http.ResponseWriter, r *http.Request) {
	if PrincipalOf(r).Kind != auth.KindLocal {
		writeError(w, r, domain.Unauthorized("error.unauthorized.local_required"))
		return
	}
	var req linkRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	if s.RequestShutdown == nil {
		writeError(w, r, domain.Conflict("error.run.not_active", nil))
		return
	}
	writeData(w, r, http.StatusAccepted, map[string]any{"stopping": true})
	s.RequestShutdown()
}

type linkResponse struct {
	BootstrapSecret string    `json:"bootstrapSecret"` // raw, delivered to same-user helper only
	Endpoint        string    `json:"endpoint"`
	ExpiresAt       time.Time `json:"expiresAt"`
}

// handleInternalLink mints a fresh startup secret for same-user helper
// processes (desktop shell, duplicate serve) that hold the runtime
// credential. It is an internal route: browser identities cannot call it.
func (s *Server) handleInternalLink(w http.ResponseWriter, r *http.Request) {
	p := PrincipalOf(r)
	if p.Kind != auth.KindLocal {
		writeError(w, r, domain.Unauthorized("error.unauthorized.local_required"))
		return
	}
	var req linkRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	secret, err := s.Issuer.Issue()
	if err != nil {
		writeError(w, r, domain.Internal(err))
		return
	}
	writeData(w, r, http.StatusOK, linkResponse{
		BootstrapSecret: secret,
		Endpoint:        s.Endpoint,
		ExpiresAt:       time.Now().UTC().Add(auth.DefaultBootstrapTTL),
	})
}
