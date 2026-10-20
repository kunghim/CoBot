package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/engine"
	"github.com/kunghim/CoBot/internal/engine/codex"
	"github.com/kunghim/CoBot/internal/provider"
	"github.com/kunghim/CoBot/internal/providers/presets"
	"github.com/kunghim/CoBot/internal/secrets"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// ProviderView is the projection of a model provider the API returns. The
// secret is never part of it; the user only learns whether one is stored.
type ProviderView struct {
	domain.Provider
	// HasCredential reports whether a key is stored for this provider.
	HasCredential bool `json:"hasCredential"`
}

// providerCredentialRef is where a provider's key lives in the credential
// store. It is derived from the provider id so a provider and its key cannot
// drift apart.
func providerCredentialRef(providerID string) string { return "provider_" + providerID }

// ListProviders returns every configured model provider.
func (a *App) ListProviders(ctx context.Context) ([]ProviderView, error) {
	providers, err := sqlite.ListProviders(ctx, a.DB)
	if err != nil {
		return nil, sqlite.NormalizeError(err)
	}
	out := make([]ProviderView, 0, len(providers))
	for _, p := range providers {
		out = append(out, a.providerView(ctx, p))
	}
	return out, nil
}

// GetProvider returns one model provider.
func (a *App) GetProvider(ctx context.Context, id string) (ProviderView, error) {
	p, err := sqlite.GetProvider(ctx, a.DB, id)
	if err != nil {
		return ProviderView{}, sqlite.NormalizeError(err)
	}
	return a.providerView(ctx, p), nil
}

func (a *App) providerView(ctx context.Context, p domain.Provider) ProviderView {
	has := false
	if a.Credentials != nil && p.CredentialRef != "" {
		if _, err := a.Credentials.Get(ctx, p.CredentialRef); err == nil {
			has = true
		}
	}
	return ProviderView{Provider: p, HasCredential: has}
}

// SaveProvider validates a provider, makes it reachable for the engine and
// stores it. The engine is told first: an unsupported dialect is rejected
// there with an explanation, and a provider CoBot cannot hand to the engine
// never reaches the database.
func (a *App) SaveProvider(ctx context.Context, p domain.Provider, expectedRevision int64) (ProviderView, error) {
	// The credential reference is derived from the id so a provider and its
	// key cannot drift apart; it is filled in before validation, which
	// requires one for every key-carrying provider.
	if p.CredentialRef == "" && p.AuthMode == domain.ProviderAuthAPIKey {
		p.CredentialRef = providerCredentialRef(p.ID)
	}
	// A provider built from the catalogue keeps its catalogue reference even
	// when the caller named the vendor by its id alone. Without it the
	// interface loses the vendor's mark and its localised name.
	if p.PresetID == "" {
		if _, ok := presets.ByID(p.ID); ok {
			p.PresetID = p.ID
		}
	}
	if err := p.Validate(); err != nil {
		return ProviderView{}, err
	}
	if err := a.bindProvider(ctx, p); err != nil {
		return ProviderView{}, err
	}
	saved, err := sqlite.UpsertProvider(ctx, a.DB, p, expectedRevision)
	if err != nil {
		// The engine already knows about a provider the database refused to
		// store; take it back out so the two do not drift.
		_ = a.unbindProvider(ctx, p.ID)
		return ProviderView{}, sqlite.NormalizeError(err)
	}
	return a.providerView(ctx, saved), nil
}

// DeleteProvider removes a provider and tells the engine to forget it.
func (a *App) DeleteProvider(ctx context.Context, id string) error {
	if err := sqlite.DeleteProvider(ctx, a.DB, id); err != nil {
		return sqlite.NormalizeError(err)
	}
	_ = a.unbindProvider(ctx, id)
	return nil
}

// SetProviderCredential stores (or clears) a provider's API key.
func (a *App) SetProviderCredential(ctx context.Context, id string, secret []byte) error {
	if a.Credentials == nil {
		return domain.Internal(errors.New("credential store is not configured"))
	}
	ref := providerCredentialRef(id)
	if len(secret) == 0 {
		if err := a.Credentials.Delete(ctx, ref); err != nil && !errors.Is(err, secrets.ErrNotFound) {
			return domain.Internal(err)
		}
		return nil
	}
	if err := a.Credentials.Put(ctx, ref, secret); err != nil {
		return domain.Internal(err)
	}
	return nil
}

// ProviderEnv returns the engine environment entries that carry provider
// secrets. The variable name is what the engine's configuration refers to;
// the key itself only ever lives in the credential store and in this
// process's environment.
func (a *App) ProviderEnv(ctx context.Context) (map[string]string, error) {
	if a.Credentials == nil {
		return nil, nil
	}
	providers, err := sqlite.ListProviders(ctx, a.DB)
	if err != nil {
		return nil, sqlite.NormalizeError(err)
	}
	env := map[string]string{}
	for _, p := range providers {
		if p.AuthMode != domain.ProviderAuthAPIKey || p.CredentialRef == "" {
			continue
		}
		secret, err := a.Credentials.Get(ctx, p.CredentialRef)
		if err != nil {
			if errors.Is(err, secrets.ErrNotFound) {
				continue
			}
			return nil, domain.Internal(err)
		}
		env[codex.ProviderEnvKey(p.ID)] = string(secret)
	}
	return env, nil
}

// ProbeProvider checks a stored provider's models again and records the
// result. The key is read from the credential store: the user never sees it
// again, so a provider can be re-checked long after it was set up.
func (a *App) ProbeProvider(ctx context.Context, id string) (map[string]provider.Result, error) {
	p, err := sqlite.GetProvider(ctx, a.DB, id)
	if err != nil {
		return nil, sqlite.NormalizeError(err)
	}
	// The check speaks the OpenAI dialect, which is the one the current
	// engine uses; a provider that offers no such endpoint cannot be
	// checked with it.
	ep, ok := p.EndpointFor(domain.DialectOpenAIResponses)
	if !ok {
		return nil, domain.InvalidRequest("error.provider.no_probe_endpoint", map[string]any{
			"dialect": domain.DialectOpenAIResponses,
		})
	}
	ids := make([]string, 0, len(p.Models))
	for _, m := range p.Models {
		ids = append(ids, m.ID)
	}
	if len(ids) == 0 {
		return map[string]provider.Result{}, nil
	}
	key := ""
	if p.CredentialRef != "" && a.Credentials != nil {
		secret, err := a.Credentials.Get(ctx, p.CredentialRef)
		if err != nil && !errors.Is(err, secrets.ErrNotFound) {
			return nil, domain.Internal(err)
		}
		if err == nil {
			key = string(secret)
		}
	}
	results := provider.New().Probe(ctx, ep.BaseURL, key, ids)

	// The outcome belongs to the stored models: a model that cannot run is
	// kept and marked, not dropped.
	updated := append([]domain.ProviderModel(nil), p.Models...)
	for i := range updated {
		if res, ok := results[updated[i].ID]; ok {
			updated[i].Status = domain.ModelStatusOK
			if !res.OK {
				updated[i].Status = domain.ModelStatusFailed
			}
			updated[i].Failure = res.Code
			updated[i].Message = res.Message
		}
	}
	p.Models = updated
	if _, err := sqlite.UpsertProvider(ctx, a.DB, p, p.Revision); err != nil {
		return nil, sqlite.NormalizeError(err)
	}
	return results, nil
}

// bindProvider hands a provider to the engine.
func (a *App) bindProvider(ctx context.Context, p domain.Provider) error {
	if p.AuthMode == domain.ProviderAuthChatGPT {
		// The official provider uses the engine's own login; there is
		// nothing to bind.
		return nil
	}
	binder, err := a.providerBinder(ctx)
	if err != nil {
		return err
	}
	if err := binder.BindProvider(ctx, p); err != nil {
		return providerEngineError(err)
	}
	return nil
}

func (a *App) unbindProvider(ctx context.Context, id string) error {
	binder, err := a.providerBinder(ctx)
	if err != nil {
		return err
	}
	return binder.UnbindProvider(ctx, id)
}

// ResolveModel returns the model a turn should use.
//
// The provider the profile runs with has the first say: its own default
// model is the one the user picked for that vendor. The engine's own
// default model belongs to a vendor the profile may no longer be using, and
// asking for it against a third-party gateway is how a turn ends in a 400
// about a model that vendor never had.
//
// Without a provider the profile's own setting decides, and an empty answer
// lets the engine choose.
func (a *App) ResolveModel(ctx context.Context) (string, error) {
	settings, err := a.GetSettings(ctx)
	if err != nil {
		return "", err
	}
	if settings.DefaultProvider != "" {
		p, err := sqlite.GetProvider(ctx, a.DB, settings.DefaultProvider)
		if err == nil && p.DefaultModel != "" {
			return p.DefaultModel, nil
		}
	}
	return settings.DefaultModel, nil
}

// selectProviderInEngine makes the engine run with the named provider.
//
// The profile's setting is the truth, but the engine keeps the choice in its
// own configuration, and that is where it has to land: a provider that is
// only in the engine's table is never used, and every turn still goes to the
// engine's own endpoint.
func (a *App) selectProviderInEngine(ctx context.Context, id string) error {
	binder, err := a.providerBinder(ctx)
	if err != nil {
		return err
	}
	if err := binder.SelectProvider(ctx, id); err != nil {
		return providerEngineError(err)
	}
	return nil
}

// restoreProviderInEngine puts the engine back on the provider it had before
// a change that did not go through. A failure here cannot be reported to the
// user as the cause of the original failure, so it is only logged.
func (a *App) restoreProviderInEngine(ctx context.Context, id string) {
	if err := a.selectProviderInEngine(ctx, id); err != nil && a.Logger != nil {
		a.Logger.Warn("could not restore the engine's model provider", "providerId", id, "error", err)
	}
}

// providerBinder returns the profile's engine as something that can accept
// model providers. The control server is the one that outlives a single
// team, which matches the shared private engine home.
func (a *App) providerBinder(ctx context.Context) (engine.ProviderBinder, error) {
	if a.ProviderBinderFactory != nil {
		return a.ProviderBinderFactory(ctx)
	}
	server, err := a.control(ctx)
	if err != nil {
		return nil, domain.EngineUnavailable("error.provider.engine_unavailable", nil, err)
	}
	binder, ok := any(server).(engine.ProviderBinder)
	if !ok {
		return nil, domain.Internal(fmt.Errorf("engine does not accept model providers"))
	}
	return binder, nil
}

// providerEngineError turns a rejected engine configuration into a request
// error: the user supplied something the engine cannot use, which is not our
// fault and must not be reported as a server failure.
func providerEngineError(err error) error {
	if codex.IsConfigValidationError(err) {
		return domain.InvalidRequest("error.provider.engine_rejected", map[string]any{
			"reason": codex.ConfigErrorMessage(err),
		})
	}
	return domain.EngineUnavailable("error.provider.bind_failed", nil, err)
}

// presetEndpoints renders a preset's dialects for the UI.
func presetEndpoints(p presets.Preset) []PresetEndpointView {
	out := make([]PresetEndpointView, 0, len(p.Endpoints))
	for _, e := range p.Endpoints {
		out = append(out, PresetEndpointView{Dialect: e.Dialect, BaseURL: e.BaseURL})
	}
	return out
}

// ProviderPresets returns the built-in vendor catalogue for the settings UI.
func ProviderPresets() []PresetView {
	all := presets.All()
	out := make([]PresetView, 0, len(all))
	for _, p := range all {
		out = append(out, PresetView{
			ID:          p.ID,
			Name:        p.Name,
			Short:       p.Short,
			Color:       p.Color,
			KeyURL:      p.KeyURL,
			Official:    p.Official,
			AuthMode:    p.AuthMode,
			Endpoints:   presetEndpoints(p),
			Models:      p.Models,
			KeyPatterns: p.KeyPatterns,
		})
	}
	return out
}

// PresetView is one built-in vendor, as the UI needs it.
type PresetView struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Short       string                 `json:"short"`
	Color       string                 `json:"color"`
	KeyURL      string                 `json:"keyUrl,omitempty"`
	Official    bool                   `json:"official"`
	AuthMode    string                 `json:"authMode"`
	Endpoints   []PresetEndpointView   `json:"endpoints"`
	Models      []domain.ProviderModel `json:"models"`
	KeyPatterns []string               `json:"keyPatterns,omitempty"`
}

// PresetEndpointView is one dialect a built-in vendor offers.
type PresetEndpointView struct {
	Dialect string `json:"dialect"`
	BaseURL string `json:"baseUrl"`
}

// DetectProviderFromKey guesses the vendor a pasted key belongs to. It only
// matches the local patterns; an unknown key returns an empty id rather than
// a guess.
func DetectProviderFromKey(key string) string {
	return presets.DetectVendor(strings.TrimSpace(key))
}
