package integration

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/domain"
)

// providerBinder stands in for the engine that takes a model provider.
type providerBinder struct {
	mu       sync.Mutex
	bound    []domain.Provider
	selected string
}

func newProviderBinder() *providerBinder { return &providerBinder{} }

func (b *providerBinder) Dialects() []string { return []string{domain.DialectOpenAIResponses} }

func (b *providerBinder) BindProvider(ctx context.Context, p domain.Provider) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.bound = append(b.bound, p)
	return nil
}

func (b *providerBinder) UnbindProvider(ctx context.Context, id string) error { return nil }

func (b *providerBinder) SelectProvider(ctx context.Context, id string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.selected = id
	return nil
}

// A thread names the provider the profile runs with, and the model that
// belongs to it. The engine refuses a provider id it does not know, so the
// id only travels for a provider that is actually configured.
func TestThreadNamesTheProfileProviderAndItsModel(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()

	if _, err := h.app.SaveProvider(ctx, domain.Provider{
		ID: "gateway", Name: "Gateway", Kind: domain.ProviderKindCustom,
		AuthMode: domain.ProviderAuthAPIKey,
		Endpoints: []domain.ProviderEndpoint{
			{Dialect: domain.DialectOpenAIResponses, BaseURL: "https://gateway.example/v1"},
		},
		Models:       []domain.ProviderModel{{ID: "vendor-model"}},
		DefaultModel: "vendor-model",
	}, 0); err != nil {
		t.Fatalf("save the provider: %v", err)
	}
	if err := h.app.SetProviderCredential(ctx, "gateway", []byte("sk-test")); err != nil {
		t.Fatalf("store the key: %v", err)
	}
	settings, err := h.app.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	id := "gateway"
	if _, err := h.app.UpdateSettings(ctx, h.user, domain.SettingsPatch{DefaultProvider: &id}, settings.Revision); err != nil {
		t.Fatalf("run with the provider: %v", err)
	}

	team, bots := h.createTeam(t, []string{"fullstack-engineer"})
	fake := h.ensureFake(t, team.ID)
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{
		BotID: bots[1].ID, Text: "hi", RequestID: "provider-thread",
	}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 5*time.Second, "turn", func() bool { return len(fake.Turns()) == 1 })

	if got := fake.ThreadProviders; len(got) == 0 || got[0] != "gateway" {
		t.Fatalf("the thread named %v, want the profile's provider", got)
	}
	if got := fake.Turns()[0].Model; got != "vendor-model" {
		t.Fatalf("the turn asked for %q, want the provider's own model", got)
	}
}

// Without a provider the thread names none, so the engine keeps its own
// account, and the model falls back to the profile's own setting.
func TestThreadWithoutAProviderNamesNone(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()

	team, bots := h.createTeam(t, []string{"fullstack-engineer"})
	fake := h.ensureFake(t, team.ID)
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{
		BotID: bots[1].ID, Text: "hi", RequestID: "no-provider-thread",
	}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 5*time.Second, "turn", func() bool { return len(fake.Turns()) == 1 })

	if got := fake.ThreadProviders; len(got) == 0 || got[0] != "" {
		t.Fatalf("the thread named %v, want none", got)
	}
}
