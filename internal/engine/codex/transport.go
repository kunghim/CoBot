package codex

import (
	"context"
	"fmt"
)

// httpProvider is the official account reached without a WebSocket. Codex
// opens one by default and falls back to HTTPS only after the handshake has
// timed out, which on some networks costs minutes at the start of every
// connection. The built-in provider cannot be changed ("Built-in providers
// cannot be overridden"), so an engine started HTTP-only runs the official
// threads on this provider instead: the same endpoint and login, with
// WebSockets off. Verified against Codex 0.167 (#93).
const httpProvider = "cobot-openai-http"

// httpProviderTable is the configuration of httpProvider.
var httpProviderTable = map[string]any{
	"name":                 "OpenAI (HTTP only)",
	"base_url":             "https://chatgpt.com/backend-api/codex",
	"wire_api":             "responses",
	"requires_openai_auth": true,
	"supports_websockets":  false,
}

// HTTPOnly reports whether this process runs the official account without
// WebSockets; it is fixed when the process starts.
func (s *Server) HTTPOnly() bool { return s.httpOnly }

// engineProvider is the provider id the engine is told for a thread. In an
// HTTP-only engine the official account (named or left to the default) is
// httpProvider; every other provider is itself.
func (s *Server) engineProvider(id string) string {
	if s.httpOnly && (id == "" || id == builtinProvider) {
		return httpProvider
	}
	return id
}

// productProvider is the provider id CoBot keeps for what the engine reports:
// httpProvider is the official account, so a thread that moves between the
// two transports is still the same vendor's and keeps its history.
func productProvider(id string) string {
	if id == httpProvider {
		return builtinProvider
	}
	return id
}

// bindHTTPProvider puts httpProvider in the engine's configuration. The file
// is shared by every engine of the profile, and a thread can only take a
// turn while the default names its provider, so the table has to be in the
// file and not only in this process's overrides.
func (s *Server) bindHTTPProvider(ctx context.Context) error {
	engineDefault.Lock()
	defer engineDefault.Unlock()
	if _, err := s.writeConfigValue(ctx, "model_providers."+httpProvider, httpProviderTable, "upsert"); err != nil {
		return fmt.Errorf("register the HTTP-only provider: %w", err)
	}
	cfg, err := s.readConfig(ctx)
	if err != nil {
		return err
	}
	if _, ok := configModelProviders(cfg)[httpProvider]; !ok {
		return fmt.Errorf("engine did not accept the HTTP-only provider")
	}
	return nil
}
