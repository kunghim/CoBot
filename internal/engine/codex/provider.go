package codex

import (
	"context"
	"fmt"
	"sync"

	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/engine"
)

// builtinProvider is the provider the engine uses when no other one is
// selected: its own account and its own endpoint.
const builtinProvider = "openai"

// Dialects lists the wire dialects this engine speaks. Codex 0.161 accepts
// only the OpenAI Responses dialect: a chat-completions provider is rejected
// by the engine outright rather than silently downgraded to its own default
// endpoint, which would send a user's third-party key to the wrong host.
func (s *Server) Dialects() []string { return []string{domain.DialectOpenAIResponses} }

// ProviderEnvKey is the variable that carries a provider's key into Codex.
func ProviderEnvKey(providerID string) string { return engine.ProviderEnvKey(providerID) }

// BindProvider makes a provider reachable for this engine.
//
// The configuration is written through the engine rather than by editing
// config.toml here: Codex owns the file format, and it validates the value —
// an unsupported wire_api comes back as an error that names the fix instead
// of a silently dropped configuration.
func (s *Server) BindProvider(ctx context.Context, p domain.Provider) error {
	if p.AuthMode == domain.ProviderAuthChatGPT {
		// The official provider inherits the engine's own provider and login
		// state; there is nothing to bind.
		return fmt.Errorf("provider %s uses the engine's own login and has no endpoint to bind", p.ID)
	}
	ep, ok := p.EndpointFor(domain.DialectOpenAIResponses)
	if !ok {
		return fmt.Errorf("provider %s has no %s endpoint", p.ID, domain.DialectOpenAIResponses)
	}
	value := map[string]any{
		"name":                 p.Name,
		"base_url":             ep.BaseURL,
		"wire_api":             "responses",
		"requires_openai_auth": false,
		"env_key":              ProviderEnvKey(p.ID),
	}
	if p.Retries.Request > 0 {
		value["request_max_retries"] = p.Retries.Request
	}
	if p.Retries.Stream > 0 {
		value["stream_max_retries"] = p.Retries.Stream
	}
	if _, err := s.writeConfigValue(ctx, "model_providers."+p.ID, value, "upsert"); err != nil {
		return err
	}
	// Confirm the engine really holds the table; a write that reports success
	// but leaves nothing behind would send requests to the wrong endpoint.
	cfg, err := s.readConfig(ctx)
	if err != nil {
		return err
	}
	if _, ok := configModelProviders(cfg)[p.ID]; !ok {
		return fmt.Errorf("engine did not accept provider %s", p.ID)
	}
	return nil
}

// UnbindProvider removes a provider this engine no longer serves. The
// protocol has no delete for one table, so the whole table is rewritten
// without that entry; CoBot owns this file, so nothing else is in it.
func (s *Server) UnbindProvider(ctx context.Context, providerID string) error {
	engineDefault.Lock()
	defer engineDefault.Unlock()
	cfg, err := s.readConfig(ctx)
	if err != nil {
		return err
	}
	providers := configModelProviders(cfg)
	if _, ok := providers[providerID]; !ok {
		return nil
	}
	delete(providers, providerID)
	if _, err := s.writeConfigValue(ctx, "model_providers", providers, "replace"); err != nil {
		return err
	}
	return nil
}

// engineDefault guards the engine's default provider. Every engine of a profile
// shares one configuration file, so this is one lock and not one per server.
var engineDefault sync.Mutex

// asThreadProvider runs a call the engine answers only when its default
// provider is the one the thread runs with: starting or steering a turn,
// resuming a thread. It refuses the others with "This chat uses x, but your
// current provider is y", though a thread names its provider and one engine
// serves threads on several. So the default follows the thread for the length
// of the call and goes back after it; a turn that has started keeps its
// provider. The default stays on the engine's own account in between, because
// the model list and every other call without a thread read it.
func (s *Server) asThreadProvider(ctx context.Context, providerID string, call func() error) error {
	engineDefault.Lock()
	defer engineDefault.Unlock()
	if providerID == "" || providerID == builtinProvider {
		return call()
	}
	if _, err := s.writeConfigValue(ctx, "model_provider", providerID, "upsert"); err != nil {
		return fmt.Errorf("%w: %w", engine.ErrRejected, err)
	}
	defer func() {
		// The caller's deadline may be what ended the call; this has to happen anyway.
		if _, err := s.writeConfigValue(context.WithoutCancel(ctx), "model_provider", builtinProvider, "upsert"); err != nil && s.logger != nil {
			s.logger.Warn("engine default provider not put back", "error", err)
		}
	}()
	return call()
}

// threadProvider is the provider the engine said a thread runs with.
func (s *Server) threadProvider(threadID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.providers[threadID]
}

func (s *Server) setThreadProvider(threadID, providerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.providers == nil {
		s.providers = map[string]string{}
	}
	s.providers[threadID] = providerID
}

// SelectProvider makes the engine use the named provider.
//
// Codex keeps the choice in one place: the top level "model_provider" key.
// A provider that is only in the table is never used, and a turn then still
// goes to the engine's own endpoint — which is how a configured third-party
// key ends up not being used at all. The empty id means the engine's own
// provider, which is what it uses by default.
func (s *Server) SelectProvider(ctx context.Context, providerID string) error {
	engineDefault.Lock()
	defer engineDefault.Unlock()
	if providerID == "" {
		providerID = builtinProvider
	}
	if _, err := s.writeConfigValue(ctx, "model_provider", providerID, "upsert"); err != nil {
		return err
	}
	// Confirm the engine really holds the choice: a write that reports
	// success but leaves nothing behind would send requests to the wrong
	// endpoint.
	cfg, err := s.readConfig(ctx)
	if err != nil {
		return err
	}
	if got, _ := cfg["model_provider"].(string); got != providerID {
		return fmt.Errorf("engine did not accept the provider %q (it holds %q)", providerID, got)
	}
	return nil
}
