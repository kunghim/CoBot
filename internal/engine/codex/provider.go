package codex

import (
	"context"
	"fmt"
	"strings"

	"github.com/kunghim/CoBot/internal/domain"
)

// Dialects lists the wire dialects this engine speaks. Codex 0.161 accepts
// only the OpenAI Responses dialect: a chat-completions provider is rejected
// by the engine outright rather than silently downgraded to its own default
// endpoint, which would send a user's third-party key to the wrong host.
func (s *Server) Dialects() []string { return []string{domain.DialectOpenAIResponses} }

// ProviderEnvKey is the environment variable that carries a provider's API
// key into the engine. The name is stable per provider so a key can be
// rotated without renaming the configuration, and the secret itself never
// reaches config.toml — only this variable name does.
func ProviderEnvKey(providerID string) string {
	return "COBOT_PROVIDER_" + sanitizeEnvName(providerID) + "_KEY"
}

func sanitizeEnvName(id string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(id) {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

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
