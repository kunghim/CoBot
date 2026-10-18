package http

import (
	"net/http"
	"time"

	"github.com/kunghim/CoBot/internal/auth"
)

// EngineStatus is the non-secret engine projection of GET /status and
// GET /engine. M2 fills it from the real Codex adapter; until then the
// product reports "not_configured" instead of pretending an engine exists.
type EngineStatus struct {
	Status       string   `json:"status"` // not_configured | ready | error | starting
	BinaryPath   string   `json:"binaryPath,omitempty"`
	Version      string   `json:"version,omitempty"`
	Protocol     string   `json:"protocol,omitempty"`
	LoggedIn     bool     `json:"loggedIn"`
	Capabilities []string `json:"capabilities,omitempty"`
	ErrorKey     string   `json:"errorKey,omitempty"`
}

// EngineStatusProvider lets bootstrap plug the real engine later without
// changing this package.
type EngineStatusProvider func() EngineStatus

type statusResponse struct {
	SessionCookieName string       `json:"sessionCookieName"`
	CSRFCookieName    string       `json:"csrfCookieName"`
	Product           string       `json:"product"`
	Version           string       `json:"version"`
	Profile           string       `json:"profile"`
	Environment       string       `json:"environment"`
	DataRoot          string       `json:"dataRoot"`
	Endpoint          string       `json:"endpoint"`
	InstanceID        string       `json:"instanceId"`
	ServiceEpoch      string       `json:"serviceEpoch"`
	StartedAt         time.Time    `json:"startedAt"`
	Authorized        bool         `json:"authorized"`
	Engine            EngineStatus `json:"engine"`
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	p := PrincipalOf(r)
	engine := EngineStatus{Status: "not_configured"}
	if s.EngineStatus != nil {
		engine = s.EngineStatus()
	}
	writeData(w, r, http.StatusOK, statusResponse{
		SessionCookieName: s.sessionCookieName(), CSRFCookieName: s.csrfCookieName(),
		Product:      "CoBot",
		Version:      s.Version,
		Profile:      string(s.Paths.Environment),
		Environment:  string(s.Paths.Environment),
		DataRoot:     s.Paths.Root,
		Endpoint:     s.Endpoint,
		InstanceID:   s.App.InstanceID,
		ServiceEpoch: s.App.ServiceEpoch,
		StartedAt:    s.StartedAt,
		Authorized:   p.Kind == auth.KindUser,
		Engine:       engine,
	})
}
