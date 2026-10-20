package http

import (
	"net/http"

	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/provider"
)

// providerRequest is the body of a provider write. The API key travels here
// once and goes straight into the encrypted credential store; it is never
// part of the stored provider record and never comes back out.
type providerRequest struct {
	// RequestID is the idempotency key every write carries. The client adds
	// it to every POST body, so a handler that does not declare it would
	// refuse its own product's request.
	RequestID    string                    `json:"requestId"`
	ID           string                    `json:"id"`
	Name         string                    `json:"name"`
	Kind         string                    `json:"kind"`
	AuthMode     string                    `json:"authMode"`
	Endpoints    []domain.ProviderEndpoint `json:"endpoints"`
	Models       []domain.ProviderModel    `json:"models"`
	DefaultModel string                    `json:"defaultModel"`
	Retries      *domain.ProviderRetries   `json:"retries"`
	PresetID     string                    `json:"presetId"`
	APIKey       string                    `json:"apiKey,omitempty"`
	// ExpectedRevision guards concurrent writes of the same provider.
	ExpectedRevision int64 `json:"expectedRevision,omitempty"`
}

func (r providerRequest) toDomain() domain.Provider {
	p := domain.Provider{
		ID:           r.ID,
		Name:         r.Name,
		Kind:         r.Kind,
		AuthMode:     r.AuthMode,
		Endpoints:    r.Endpoints,
		Models:       r.Models,
		DefaultModel: r.DefaultModel,
		PresetID:     r.PresetID,
	}
	if r.Retries != nil {
		p.Retries = *r.Retries
	}
	return p
}

func (s *Server) handleListProviders(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUser(w, r); !ok {
		return
	}
	providers, err := s.App.ListProviders(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, map[string]any{"providers": providers})
}

func (s *Server) handleGetProvider(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUser(w, r); !ok {
		return
	}
	provider, err := s.App.GetProvider(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, provider)
}

func (s *Server) handleCreateProvider(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUser(w, r); !ok {
		return
	}
	var req providerRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	if req.ID == "" {
		writeError(w, r, domain.InvalidRequest("error.provider.invalid_id", map[string]any{"id": req.ID}))
		return
	}
	saved, err := s.App.SaveProvider(r.Context(), req.toDomain(), 0)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if req.APIKey != "" {
		if err := s.App.SetProviderCredential(r.Context(), saved.ID, []byte(req.APIKey)); err != nil {
			writeError(w, r, err)
			return
		}
		saved, err = s.App.GetProvider(r.Context(), saved.ID)
		if err != nil {
			writeError(w, r, err)
			return
		}
	}
	writeData(w, r, http.StatusCreated, saved)
}

func (s *Server) handleUpdateProvider(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUser(w, r); !ok {
		return
	}
	var req providerRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	id := r.PathValue("id")
	req.ID = id
	saved, err := s.App.SaveProvider(r.Context(), req.toDomain(), req.ExpectedRevision)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if req.APIKey != "" {
		if err := s.App.SetProviderCredential(r.Context(), saved.ID, []byte(req.APIKey)); err != nil {
			writeError(w, r, err)
			return
		}
		saved, err = s.App.GetProvider(r.Context(), saved.ID)
		if err != nil {
			writeError(w, r, err)
			return
		}
	}
	writeData(w, r, http.StatusOK, saved)
}

func (s *Server) handleDeleteProvider(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUser(w, r); !ok {
		return
	}
	if err := s.App.DeleteProvider(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, map[string]any{"deleted": true})
}

// handleProviderPresets returns the built-in vendor catalogue. It is public
// within the profile: no secret is involved.
func (s *Server) handleProviderPresets(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUser(w, r); !ok {
		return
	}
	writeData(w, r, http.StatusOK, map[string]any{"presets": app.ProviderPresets()})
}

// handleDetectProvider guesses the vendor a pasted key belongs to, using the
// local patterns only. It never calls the network.
func (s *Server) handleDetectProvider(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUser(w, r); !ok {
		return
	}
	var req struct {
		RequestID string `json:"requestId"`
		APIKey    string `json:"apiKey"`
	}
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, map[string]any{"presetId": app.DetectProviderFromKey(req.APIKey)})
}

// handleDiscoverProviderModels lists the models an endpoint offers, using the
// endpoint and key the caller just typed. It is stateless on purpose: the
// user must be able to see the models before the provider is stored.
func (s *Server) handleDiscoverProviderModels(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUser(w, r); !ok {
		return
	}
	var req struct {
		RequestID string `json:"requestId"`
		BaseURL   string `json:"baseUrl"`
		APIKey    string `json:"apiKey"`
	}
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	models, err := provider.New().Discover(r.Context(), req.BaseURL, req.APIKey)
	if err != nil {
		writeError(w, r, domain.InvalidRequest("error.provider.discover_failed", map[string]any{"reason": err.Error()}))
		return
	}
	writeData(w, r, http.StatusOK, map[string]any{"models": models})
}

// handleProbeProviderModels checks the given models against an endpoint
// before they are stored, so a provider is never saved with a model that
// cannot run.
func (s *Server) handleProbeProviderModels(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUser(w, r); !ok {
		return
	}
	var req struct {
		RequestID string   `json:"requestId"`
		BaseURL   string   `json:"baseUrl"`
		APIKey    string   `json:"apiKey"`
		ModelIDs  []string `json:"modelIds"`
	}
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}
	results := provider.New().Probe(r.Context(), req.BaseURL, req.APIKey, req.ModelIDs)
	writeData(w, r, http.StatusOK, map[string]any{"results": results})
}

// handleReprobeProvider checks a stored provider's models again. The key
// comes from the credential store, so the user never has to paste it twice.
func (s *Server) handleReprobeProvider(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUser(w, r); !ok {
		return
	}
	results, err := s.App.ProbeProvider(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, map[string]any{"results": results})
}
