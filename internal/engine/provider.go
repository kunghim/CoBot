package engine

import (
	"context"
	"encoding/json"

	"github.com/kunghim/CoBot/internal/domain"
)

// ProviderBinder is the capability of an engine that can run a CoBot model
// provider. An engine declares the wire dialects it speaks and takes a
// provider record; CoBot never translates protocols itself, it only records
// which dialect each engine gets.
type ProviderBinder interface {
	// Dialects lists the wire dialects this engine can speak, e.g.
	// domain.DialectOpenAIResponses.
	Dialects() []string
	// BindProvider makes the provider reachable for this engine. It must
	// fail loudly: an engine that silently falls back to its own default
	// endpoint would send a user's third-party key to the wrong host.
	BindProvider(ctx context.Context, p domain.Provider) error
	// UnbindProvider removes a provider this engine no longer serves.
	UnbindProvider(ctx context.Context, providerID string) error
	// SelectProvider makes the engine use the named provider from now on. An
	// empty id puts the engine's own provider back.
	//
	// This is separate from BindProvider on purpose: a provider that is only
	// in the engine's table is never used. The engine keeps the choice in
	// one place, and a profile that says "run with this provider" has to
	// put it there, or every turn still goes to the engine's own endpoint.
	SelectProvider(ctx context.Context, providerID string) error
}

// AuthProvider is the capability of engines that need the user to authorize
// them. A provider that carries its own API key does not need it: CoBot
// stores the secret and hands it to the engine through a channel the engine
// owns.
type AuthProvider interface {
	AccountRead(ctx context.Context) (json.RawMessage, error)
	AccountLoginStart(ctx context.Context) (LoginInfo, error)
	AccountLoginCancel(ctx context.Context, loginID string) error
}
