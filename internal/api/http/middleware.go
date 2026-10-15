package http

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
)

type ctxKey int

const (
	ctxRequestID ctxKey = iota
	ctxPrincipal
)

func requestIDOf(r *http.Request) string {
	if v, ok := r.Context().Value(ctxRequestID).(string); ok {
		return v
	}
	return ""
}

// PrincipalOf returns the authenticated identity of a request.
func PrincipalOf(r *http.Request) auth.Principal {
	if v, ok := r.Context().Value(ctxPrincipal).(auth.Principal); ok {
		return v
	}
	return auth.Principal{}
}

func newRequestID() string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

// withBaseMiddleware installs request id, panic recovery, host validation,
// CORS/origin enforcement and credential resolution in that order.
func (s *Server) withBaseMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := r.Header.Get("X-Request-Id")
		if reqID == "" || len(reqID) > 128 {
			reqID = newRequestID()
		}
		ctx := context.WithValue(r.Context(), ctxRequestID, reqID)
		r = r.WithContext(ctx)

		defer func() {
			if rec := recover(); rec != nil {
				s.Logger.Error("panic in handler", "requestId", reqID, "panic", rec)
				writeError(w, r, domain.Internal(nil))
			}
		}()

		if !s.hostAllowed(r.Host) {
			writeError(w, r, domain.Forbidden("error.forbidden.host", nil))
			return
		}

		origin := r.Header.Get("Origin")
		if origin != "" {
			if !s.originAllowed(origin) {
				writeError(w, r, domain.Forbidden("error.forbidden.origin", map[string]any{"origin": origin}))
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Add("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-CSRF-Token, X-Request-Id, X-Instance-Secret, Last-Event-ID, Idempotency-Key, If-Match")
			w.Header().Set("Access-Control-Max-Age", "600")
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// A fresh one-time secret establishes a new page identity. A stale
		// cookie/bearer must not prevent its redemption after restart.
		bootstrap := r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/bootstrap"
		// The public shell must load before JavaScript can exchange a new
		// secret. Otherwise a restart's stale cookie returns JSON instead of
		// the app, even on a valid link whose fragment never reaches HTTP.
		asset := !strings.HasPrefix(r.URL.Path, "/api/") && !strings.HasPrefix(r.URL.Path, "/internal/")
		var principal auth.Principal
		var err error
		if !bootstrap && !asset {
			principal, err = s.resolvePrincipal(r)
		}
		if err != nil && r.Method == http.MethodGet && r.URL.Path == "/api/v1/status" {
			principal, err = auth.Principal{}, nil
		}
		if err != nil {
			writeError(w, r, err)
			return
		}
		if principal.Kind != "" {
			ctx = context.WithValue(ctx, ctxPrincipal, principal)
			r = r.WithContext(ctx)
		}

		// Cookie sessions carry ambient authority, so state-changing requests
		// must prove same-origin intent with the CSRF token. Bearer clients
		// (desktop) hold no ambient cookie and skip this check.
		if isStateChanging(r.Method) && principal.Kind == auth.KindUser && principal.SessionID != "" {
			if !s.csrfValid(r, principal.SessionID) {
				writeError(w, r, domain.Forbidden("error.forbidden.csrf", nil))
				return
			}
		}
		if isStateChanging(r.Method) && principal.Kind == auth.KindUser && strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/api/v1/directories/check" {
			raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
			if err != nil {
				writeError(w, r, domain.InvalidRequest("error.invalid_json", nil))
				return
			}
			var body map[string]any
			if len(raw) == 0 && r.Method == http.MethodDelete {
				raw = []byte(`{}`)
			}
			if err := json.Unmarshal(raw, &body); err != nil || body == nil {
				writeError(w, r, domain.InvalidRequest("error.invalid_json", nil))
				return
			}
			key, isString := body["requestId"].(string)
			if _, present := body["requestId"]; present && !isString {
				writeError(w, r, domain.InvalidRequest("error.idempotency.required", nil))
				return
			}
			if header := r.Header.Get("Idempotency-Key"); header != "" {
				if key != "" && key != header {
					writeError(w, r, domain.InvalidRequest("error.idempotency.header_conflict", nil))
					return
				}
				if r.Method == http.MethodDelete {
					key = header
					body["requestId"] = header
				}
			}
			if header := r.Header.Get("If-Match"); header != "" {
				version, err := strconv.ParseInt(strings.Trim(header, "\""), 10, 64)
				if err != nil || version < 1 {
					writeError(w, r, domain.InvalidRequest("error.revision.required", nil))
					return
				}
				if value, ok := body["expectedRevision"]; ok && value != float64(version) {
					writeError(w, r, domain.InvalidRequest("error.revision.header_conflict", nil))
					return
				}
				body["expectedRevision"] = version
			}
			needsRevision := r.Method == http.MethodPatch ||
				(r.Method == http.MethodDelete && !strings.Contains(r.URL.Path, "/memories/")) ||
				strings.HasSuffix(r.URL.Path, "/decisions") || strings.HasSuffix(r.URL.Path, "/confirm") ||
				strings.HasSuffix(r.URL.Path, "/archive") || strings.HasSuffix(r.URL.Path, "/restore") ||
				strings.HasSuffix(r.URL.Path, "/directory/rebind") || strings.HasSuffix(r.URL.Path, "/retry") ||
				strings.HasSuffix(r.URL.Path, "/directory/rebuild-marker") ||
				strings.HasSuffix(r.URL.Path, "/answers") || strings.Contains(r.URL.Path, "/questions/") ||
				(strings.Contains(r.URL.Path, "/transfers/") && strings.HasSuffix(r.URL.Path, "/accept"))
			revision, hasRevision := body["expectedRevision"].(float64)
			if value, ok := body["expectedRevision"].(int64); ok {
				revision, hasRevision = float64(value), true
			}
			if (needsRevision && !hasRevision) || (hasRevision && (revision < 1 || revision != float64(int64(revision)))) {
				writeError(w, r, domain.InvalidRequest("error.revision.required", nil))
				return
			}
			if r.Method == http.MethodDelete {
				if r.Header.Get("Idempotency-Key") == "" || (needsRevision && r.Header.Get("If-Match") == "") {
					writeError(w, r, domain.InvalidRequest("error.revision.required", nil))
					return
				}
				if legacy := r.URL.Query().Get("expectedRevision"); legacy != "" && legacy != strconv.FormatInt(int64(revision), 10) {
					writeError(w, r, domain.InvalidRequest("error.revision.header_conflict", nil))
					return
				}
			}
			if strings.TrimSpace(key) == "" || len(key) > 128 {
				writeError(w, r, domain.InvalidRequest("error.idempotency.required", nil))
				return
			}
			raw, err = json.Marshal(body)
			if err != nil {
				writeError(w, r, domain.InvalidRequest("error.invalid_json", nil))
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(raw))
			scope := r.Method + " " + r.URL.Path
			if mux, ok := next.(*http.ServeMux); ok {
				_, scope = mux.Handler(r)
			}
			principal, err = app.WithWriteRequest(principal, "http."+scope, key, map[string]any{"path": r.URL.Path, "query": r.URL.Query(), "body": body})
			if err != nil {
				writeError(w, r, err)
				return
			}
			if hasRevision {
				principal.Write.ExpectedRevision = int64(revision)
			}
			r = r.WithContext(context.WithValue(r.Context(), ctxPrincipal, principal))
		}

		next.ServeHTTP(w, r)
	})
}

func isStateChanging(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func (s *Server) csrfValid(r *http.Request, sessionID string) bool {
	sess, ok := s.Sessions.Get(sessionID)
	if !ok {
		return false
	}
	header := r.Header.Get("X-CSRF-Token")
	return header != "" && auth.CompareSecret(header, sess.CSRF)
}

// hostAllowed rejects non-loopback hosts (DNS-rebinding protection). The
// product is loopback-only; remote shares are out of scope for v1.
func (s *Server) hostAllowed(host string) bool {
	hostname := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		hostname = h
	}
	hostname = strings.Trim(hostname, "[]")
	switch strings.ToLower(hostname) {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}

func (s *Server) originAllowed(origin string) bool {
	for _, allowed := range s.AllowedOrigins {
		if origin == allowed {
			return true
		}
	}
	return false
}

// resolvePrincipal maps credentials onto a principal. Exactly one credential
// family is honored per request; cookie and bearer tokens are not
// interchangeable.
func (s *Server) resolvePrincipal(r *http.Request) (auth.Principal, error) {
	if secret := r.Header.Get("X-Instance-Secret"); secret != "" {
		if auth.CompareSecret(secret, s.AdminSecret) {
			return auth.NewPrincipalLocal(), nil
		}
		return auth.Principal{}, domain.Unauthorized("error.unauthorized.bad_secret")
	}
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		token := strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
		if _, ok := s.Bearer.Get(token); ok {
			return auth.NewPrincipalUser(""), nil
		}
		return auth.Principal{}, domain.Unauthorized("error.unauthorized.bad_bearer")
	}
	if c, err := r.Cookie(s.sessionCookieName()); err == nil && c.Value != "" {
		if sess, ok := s.Sessions.Get(c.Value); ok {
			_ = sess
			return auth.NewPrincipalUser(c.Value), nil
		}
		// Stale cookie: force a clean 401 so the UI shows the re-auth entry
		// instead of silently proceeding anonymous.
		return auth.Principal{}, domain.Unauthorized("error.unauthorized.session_expired")
	}
	return auth.Principal{}, nil
}

// SessionCookieName is the HttpOnly page session cookie.
const SessionCookieName = "cobot_session"

// CSRFCookieName is the JS-readable counterpart of the CSRF header token.
const CSRFCookieName = "cobot_csrf"

func (s *Server) cookieSuffix() string {
	digest := sha256.Sum256([]byte(s.App.InstanceID))
	return hex.EncodeToString(digest[:8])
}
func (s *Server) sessionCookieName() string { return SessionCookieName + "_" + s.cookieSuffix() }
func (s *Server) csrfCookieName() string    { return CSRFCookieName + "_" + s.cookieSuffix() }

func (s *Server) setSessionCookies(w http.ResponseWriter, sess *auth.Session, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.sessionCookieName(),
		Value:    sess.ID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   secure,
		Expires:  sess.ExpiresAt,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     s.csrfCookieName(),
		Value:    sess.CSRF,
		Path:     "/",
		HttpOnly: false,
		SameSite: http.SameSiteStrictMode,
		Secure:   secure,
		Expires:  sess.ExpiresAt,
	})
}

func (s *Server) clearSessionCookies(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: s.sessionCookieName(), Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	http.SetCookie(w, &http.Cookie{Name: s.csrfCookieName(), Value: "", Path: "/", MaxAge: -1, SameSite: http.SameSiteStrictMode})
}

func requireUser(w http.ResponseWriter, r *http.Request) (auth.Principal, bool) {
	p := PrincipalOf(r)
	if p.Kind != auth.KindUser {
		writeError(w, r, domain.Unauthorized("error.unauthorized.user_required"))
		return auth.Principal{}, false
	}
	return p, true
}

func nowUTC() time.Time { return time.Now().UTC() }
