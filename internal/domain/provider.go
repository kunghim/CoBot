package domain

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Model provider: a vendor or gateway CoBot can send requests to, independent
// of which engine runs the request. The word "provider" in this file never
// means an engine-side session — that is the existing provider_sessions table.
const (
	// ProviderKindOfficial is a vendor CoBot ships a definition for. An
	// official provider that needs no endpoint inherits the engine's own.
	ProviderKindOfficial = "official"
	// ProviderKindCustom is a gateway the user describes themselves.
	ProviderKindCustom = "custom"
)

// Authentication modes a provider can use.
const (
	// ProviderAuthChatGPT is the engine's own browser login. No secret is
	// stored by CoBot; the engine keeps the tokens.
	ProviderAuthChatGPT = "chatgpt"
	// ProviderAuthAPIKey is a secret CoBot stores encrypted and hands to the
	// engine through an environment variable.
	ProviderAuthAPIKey = "api_key"
)

// Wire dialects. One provider may offer several; each engine picks the one it
// can speak. The engine never gets a protocol it does not implement.
const (
	DialectOpenAIResponses   = "openai-responses"
	DialectAnthropicMessages = "anthropic-messages"
)

// Result of the last connectivity check for one model.
const (
	ModelStatusOK      = "ok"
	ModelStatusFailed  = "failed"
	ModelStatusUnknown = "unknown"
)

// providerIDPattern keeps an id usable as a TOML table name in the engine's
// own configuration and as a column-safe key everywhere else.
var providerIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// Provider is one model vendor or gateway. Endpoints are the dialects it
// offers; Models are the ones the user chose to keep, not the vendor's full
// catalogue.
type Provider struct {
	ID            string             `json:"id"`
	Name          string             `json:"name"`
	Kind          string             `json:"kind"`
	AuthMode      string             `json:"authMode"`
	CredentialRef string             `json:"credentialRef,omitempty"`
	Endpoints     []ProviderEndpoint `json:"endpoints"`
	Models        []ProviderModel    `json:"models"`
	DefaultModel  string             `json:"defaultModel,omitempty"`
	Retries       ProviderRetries    `json:"retries"`
	PresetID      string             `json:"presetId,omitempty"`
	// Revision guards concurrent writes of the same provider.
	Revision  int64     `json:"revision"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ProviderEndpoint is one dialect of a provider: the base URL a client adds
// its own path to.
type ProviderEndpoint struct {
	Dialect string `json:"dialect"`
	BaseURL string `json:"baseUrl"`
}

// ProviderModel is one model the user kept, with what is known about it and
// the result of its last connectivity check.
type ProviderModel struct {
	ID               string   `json:"id"`
	DisplayName      string   `json:"displayName,omitempty"`
	ContextWindow    int      `json:"contextWindow,omitempty"`
	DefaultEffort    string   `json:"defaultEffort,omitempty"`
	SupportedEfforts []string `json:"supportedEfforts,omitempty"`
	InputModalities  []string `json:"inputModalities,omitempty"`
	// Status is ModelStatusOK, ModelStatusFailed or ModelStatusUnknown.
	Status string `json:"status,omitempty"`
	// Failure is a stable machine code (see package failure); Message is the
	// human-readable detail, safe to show.
	Failure string `json:"failure,omitempty"`
	Message string `json:"message,omitempty"`
}

// ProviderRetries caps how often the engine retries a request. A provider
// that answers 402 (out of credit) should not be retried five times.
type ProviderRetries struct {
	Request int `json:"request,omitempty"`
	Stream  int `json:"stream,omitempty"`
}

// Validate checks what the API and the engine must be able to rely on.
func (p Provider) Validate() error {
	if !providerIDPattern.MatchString(p.ID) {
		return InvalidRequest("error.provider.invalid_id", map[string]any{"id": p.ID})
	}
	if strings.TrimSpace(p.Name) == "" {
		return InvalidRequest("error.provider.name_required", nil)
	}
	switch p.Kind {
	case ProviderKindOfficial, ProviderKindCustom:
	default:
		return InvalidRequest("error.provider.invalid_kind", map[string]any{"kind": p.Kind})
	}
	switch p.AuthMode {
	case ProviderAuthChatGPT:
		if p.CredentialRef != "" {
			return InvalidRequest("error.provider.chatgpt_without_credential", nil)
		}
	case ProviderAuthAPIKey:
		if strings.TrimSpace(p.CredentialRef) == "" {
			return InvalidRequest("error.provider.credential_required", nil)
		}
	default:
		return InvalidRequest("error.provider.invalid_auth_mode", map[string]any{"authMode": p.AuthMode})
	}
	// The official provider inherits the engine's own provider and login
	// state, so it needs neither a secret nor an endpoint of its own.
	if p.Kind == ProviderKindOfficial && len(p.Endpoints) == 0 {
		return nil
	}
	if len(p.Endpoints) == 0 {
		return InvalidRequest("error.provider.endpoint_required", nil)
	}
	for _, ep := range p.Endpoints {
		switch ep.Dialect {
		case DialectOpenAIResponses, DialectAnthropicMessages:
		default:
			return InvalidRequest("error.provider.invalid_dialect", map[string]any{"dialect": ep.Dialect})
		}
		if err := validateBaseURL(ep.BaseURL); err != nil {
			return err
		}
	}
	seen := make(map[string]bool, len(p.Models))
	for _, m := range p.Models {
		if strings.TrimSpace(m.ID) == "" {
			return InvalidRequest("error.provider.model_id_required", nil)
		}
		if seen[m.ID] {
			return InvalidRequest("error.provider.duplicate_model", map[string]any{"model": m.ID})
		}
		seen[m.ID] = true
	}
	if p.DefaultModel != "" && !seen[p.DefaultModel] {
		return InvalidRequest("error.provider.default_model_not_kept", map[string]any{"model": p.DefaultModel})
	}
	return nil
}

// validateBaseURL keeps a base URL absolute and http(s): the engine appends
// its own path to it, and a relative or opaque value would silently send
// requests somewhere else.
func validateBaseURL(raw string) error {
	value := strings.TrimSpace(raw)
	if value == "" {
		return InvalidRequest("error.provider.endpoint_required", nil)
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return InvalidRequest("error.provider.invalid_endpoint", map[string]any{"baseUrl": raw})
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return InvalidRequest("error.provider.invalid_endpoint", map[string]any{"baseUrl": raw})
	}
	if parsed.Host == "" {
		return InvalidRequest("error.provider.invalid_endpoint", map[string]any{"baseUrl": raw})
	}
	return nil
}

// SupportsDialect reports whether the provider offers a dialect.
func (p Provider) SupportsDialect(dialect string) bool {
	for _, ep := range p.Endpoints {
		if ep.Dialect == dialect {
			return true
		}
	}
	return false
}

// EndpointFor returns the provider's endpoint for a dialect.
func (p Provider) EndpointFor(dialect string) (ProviderEndpoint, bool) {
	for _, ep := range p.Endpoints {
		if ep.Dialect == dialect {
			return ep, true
		}
	}
	return ProviderEndpoint{}, false
}

// KeepsModel reports whether the model survived the user's selection.
func (p Provider) KeepsModel(id string) bool {
	for _, m := range p.Models {
		if m.ID == id {
			return true
		}
	}
	return false
}

// ProviderPatch carries optional fields of PATCH /providers/{id}. Nil
// pointers mean "leave unchanged"; an empty slice clears the list.
type ProviderPatch struct {
	Name          *string             `json:"name"`
	CredentialRef *string             `json:"credentialRef"`
	Endpoints     *[]ProviderEndpoint `json:"endpoints"`
	Models        *[]ProviderModel    `json:"models"`
	DefaultModel  *string             `json:"defaultModel"`
	Retries       *ProviderRetries    `json:"retries"`
}

// Apply returns the provider value after the patch, without persisting.
func (p Provider) Apply(patch ProviderPatch) Provider {
	out := p
	if patch.Name != nil {
		out.Name = strings.TrimSpace(*patch.Name)
	}
	if patch.CredentialRef != nil {
		out.CredentialRef = strings.TrimSpace(*patch.CredentialRef)
	}
	if patch.Endpoints != nil {
		out.Endpoints = append([]ProviderEndpoint(nil), (*patch.Endpoints)...)
	}
	if patch.Models != nil {
		out.Models = append([]ProviderModel(nil), (*patch.Models)...)
	}
	if patch.DefaultModel != nil {
		out.DefaultModel = strings.TrimSpace(*patch.DefaultModel)
	}
	if patch.Retries != nil {
		out.Retries = *patch.Retries
	}
	return out
}

// String renders compactly for logs (never contains secrets).
func (p Provider) String() string {
	return fmt.Sprintf("Provider{id=%s kind=%s endpoints=%d models=%d}", p.ID, p.Kind, len(p.Endpoints), len(p.Models))
}
