package http

import (
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/events"
	"github.com/kunghim/CoBot/internal/platform"
)

// Server assembles every /api/v1 route plus the embedded single-page app.
type Server struct {
	App            *app.App
	Paths          platform.Paths
	Sessions       *auth.SessionStore
	Bearer         *auth.BearerStore
	Issuer         *auth.BootstrapIssuer
	Hub            *events.Hub
	AdminSecret    string
	AllowedOrigins []string
	Endpoint       string
	Version        string
	StartedAt      time.Time
	Logger         *slog.Logger
	Assets         fs.FS
	EngineStatus   EngineStatusProvider
}

// Handler builds the full HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/v1/status", s.handleStatus)
	mux.HandleFunc("POST /api/v1/session/bootstrap", s.handleSessionBootstrap)
	mux.HandleFunc("POST /api/v1/session/logout", s.handleSessionLogout)
	mux.HandleFunc("GET /api/v1/settings", s.handleGetSettings)
	mux.HandleFunc("PATCH /api/v1/settings", s.handlePatchSettings)
	mux.HandleFunc("GET /api/v1/snapshot", s.handleSnapshot)
	mux.HandleFunc("GET /api/v1/events", s.handleEvents)
	mux.HandleFunc("POST /internal/v1/session/link", s.handleInternalLink)

	// Engine (Codex) surface: status, official login, models.
	mux.HandleFunc("GET /api/v1/engine", s.handleEngine)
	mux.HandleFunc("POST /api/v1/auth/login", s.handleEngineLogin)
	mux.HandleFunc("POST /api/v1/auth/login/cancel", s.handleEngineLoginCancel)
	mux.HandleFunc("POST /api/v1/auth/logout", s.handleEngineLogout)
	mux.HandleFunc("GET /api/v1/models", s.handleModels)

	// Teams, roles and members.
	mux.HandleFunc("GET /api/v1/teams", s.handleListTeams)
	mux.HandleFunc("POST /api/v1/teams", s.handleCreateTeam)
	mux.HandleFunc("GET /api/v1/teams/{id}", s.handleGetTeam)
	mux.HandleFunc("POST /api/v1/teams/{id}/bots", s.handleAddBot)
	mux.HandleFunc("POST /api/v1/teams/{id}/retry", s.handleRetryTeam)
	mux.HandleFunc("POST /api/v1/teams/{id}/directory/rebuild-marker", s.handleRebuildMarker)
	mux.HandleFunc("GET /api/v1/roles", s.handleListRoles)

	// Conversations and runs.
	mux.HandleFunc("GET /api/v1/bots/{id}/messages", s.handleListMessages)
	mux.HandleFunc("POST /api/v1/bots/{id}/messages", s.handleSendMessage)
	mux.HandleFunc("POST /api/v1/messages/{id}/cancel", s.handleCancelMessage)
	mux.HandleFunc("POST /api/v1/runs/{id}/interrupt", s.handleInterruptRun)

	// Approvals.
	mux.HandleFunc("GET /api/v1/approvals", s.handleListApprovals)
	mux.HandleFunc("POST /api/v1/approvals/{id}/decisions", s.handleDecideApproval)

	// SPA fallback: any non-/api path renders the frontend entry, which then
	// routes client-side. Business requests never touch the asset handler.
	mux.HandleFunc("/", s.handleSPA)

	return s.withBaseMiddleware(mux)
}

func (s *Server) handleSPA(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/internal/") {
		http.NotFound(w, r)
		return
	}
	if s.Assets == nil {
		http.Error(w, "frontend assets not built; run scripts/build or scripts/dev", http.StatusNotFound)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}
	data, err := fs.ReadFile(s.Assets, path)
	if err != nil {
		// Unknown paths fall back to index.html so client routes deep-link.
		data, err = fs.ReadFile(s.Assets, "index.html")
		if err != nil {
			http.Error(w, "frontend assets incomplete", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(data)
		return
	}
	w.Header().Set("Content-Type", contentTypeFor(path))
	if strings.HasPrefix(path, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	_, _ = w.Write(data)
}

func contentTypeFor(path string) string {
	switch {
	case strings.HasSuffix(path, ".html"):
		return "text/html; charset=utf-8"
	case strings.HasSuffix(path, ".js"):
		return "text/javascript; charset=utf-8"
	case strings.HasSuffix(path, ".css"):
		return "text/css; charset=utf-8"
	case strings.HasSuffix(path, ".svg"):
		return "image/svg+xml"
	case strings.HasSuffix(path, ".json"):
		return "application/json"
	case strings.HasSuffix(path, ".woff2"):
		return "font/woff2"
	default:
		return "application/octet-stream"
	}
}

// AllowedOriginsFor builds the precise origin allowlist for an environment.
// Development also admits the Vite dev server; production only admits the
// loopback endpoints of this instance plus any explicitly configured extra
// origins (the desktop shell's asset origin is the only planned extra).
func AllowedOriginsFor(env platform.Environment, endpoint string, devServer string, extra ...string) []string {
	origins := []string{endpoint}
	if env == platform.EnvDevelopment && devServer != "" {
		origins = append(origins, devServer, "http://localhost:5173", "http://127.0.0.1:5173")
	}
	for _, o := range extra {
		if o != "" {
			origins = append(origins, o)
		}
	}
	return origins
}
