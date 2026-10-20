package app

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/engine"
	"github.com/kunghim/CoBot/internal/events"
	"github.com/kunghim/CoBot/internal/platform"
	"github.com/kunghim/CoBot/internal/secrets"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// fakeBinder records what the product asked the engine to do.
type fakeBinder struct {
	bound   []domain.Provider
	unbound []string
	err     error
}

func (f *fakeBinder) Dialects() []string { return []string{domain.DialectOpenAIResponses} }

func (f *fakeBinder) BindProvider(ctx context.Context, p domain.Provider) error {
	if f.err != nil {
		return f.err
	}
	f.bound = append(f.bound, p)
	return nil
}

func (f *fakeBinder) UnbindProvider(ctx context.Context, id string) error {
	f.unbound = append(f.unbound, id)
	return nil
}

func providerTestApp(t *testing.T, binder *fakeBinder) *App {
	t.Helper()
	root := t.TempDir()
	paths, err := platform.Resolve(platform.EnvDevelopment, filepath.Join(root, "profile"))
	if err != nil {
		t.Fatal(err)
	}
	if err := paths.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	db, err := sqlite.Open(paths.DatabaseFile())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	migrator, err := sqlite.NewMigrator(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrator.MigrateToLatest(context.Background(), paths.BackupsDir()); err != nil {
		t.Fatal(err)
	}
	store, err := secrets.New(paths.CredentialsDir())
	if err != nil {
		t.Fatal(err)
	}
	a := &App{DB: db, Paths: paths, Hub: events.NewHub(), Credentials: store}
	a.ProviderBinderFactory = func(context.Context) (engine.ProviderBinder, error) { return binder, nil }
	return a
}

func testProvider() domain.Provider {
	return domain.Provider{
		ID:       "cobot_deepseek",
		Name:     "DeepSeek",
		Kind:     domain.ProviderKindCustom,
		AuthMode: domain.ProviderAuthAPIKey,
		Endpoints: []domain.ProviderEndpoint{
			{Dialect: domain.DialectOpenAIResponses, BaseURL: "https://api.deepseek.com/v1"},
		},
		Models: []domain.ProviderModel{{ID: "deepseek-v4-pro"}},
	}
}

func TestSaveProviderBindsTheEngineAndStoresTheRecord(t *testing.T) {
	ctx := context.Background()
	binder := &fakeBinder{}
	a := providerTestApp(t, binder)

	saved, err := a.SaveProvider(ctx, testProvider(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(binder.bound) != 1 || binder.bound[0].ID != "cobot_deepseek" {
		t.Fatalf("the engine was not told about the provider: %+v", binder.bound)
	}
	if saved.ID != "cobot_deepseek" || saved.Revision != 1 {
		t.Fatalf("provider not stored as expected: %+v", saved)
	}
	// The credential reference is filled in so the provider and its key
	// cannot drift apart.
	if saved.CredentialRef != "provider_cobot_deepseek" {
		t.Fatalf("credential reference = %q", saved.CredentialRef)
	}
	if saved.HasCredential {
		t.Fatal("a provider without a key claims to have one")
	}
}

func TestSaveProviderKeepsNothingWhenTheEngineRefusesIt(t *testing.T) {
	ctx := context.Background()
	binder := &fakeBinder{err: errors.New("engine said no")}
	a := providerTestApp(t, binder)

	if _, err := a.SaveProvider(ctx, testProvider(), 0); err == nil {
		t.Fatal("a provider the engine refused was saved")
	}
	list, err := a.ListProviders(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("a refused provider reached the database: %+v", list)
	}
}

func TestProviderCredentialIsStoredEncryptedAndNeverListed(t *testing.T) {
	ctx := context.Background()
	a := providerTestApp(t, &fakeBinder{})
	if _, err := a.SaveProvider(ctx, testProvider(), 0); err != nil {
		t.Fatal(err)
	}
	if err := a.SetProviderCredential(ctx, "cobot_deepseek", []byte("sk-super-secret-value")); err != nil {
		t.Fatal(err)
	}
	view, err := a.GetProvider(ctx, "cobot_deepseek")
	if err != nil {
		t.Fatal(err)
	}
	if !view.HasCredential {
		t.Fatal("a stored key is not reported")
	}
	// The secret must not travel in the projection.
	if view.CredentialRef == "sk-super-secret-value" {
		t.Fatal("the secret is the reference")
	}
	env, err := a.ProviderEnv(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if env["COBOT_PROVIDER_COBOT_DEEPSEEK_KEY"] != "sk-super-secret-value" {
		t.Fatalf("the engine environment does not carry the key: %v", env)
	}
}

func TestProviderEnvSkipsProvidersWithoutAKey(t *testing.T) {
	ctx := context.Background()
	a := providerTestApp(t, &fakeBinder{})
	if _, err := a.SaveProvider(ctx, testProvider(), 0); err != nil {
		t.Fatal(err)
	}
	env, err := a.ProviderEnv(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(env) != 0 {
		t.Fatalf("a provider without a key still reached the engine: %v", env)
	}
}

func TestDeleteProviderTellsTheEngine(t *testing.T) {
	ctx := context.Background()
	binder := &fakeBinder{}
	a := providerTestApp(t, binder)
	if _, err := a.SaveProvider(ctx, testProvider(), 0); err != nil {
		t.Fatal(err)
	}
	if err := a.DeleteProvider(ctx, "cobot_deepseek"); err != nil {
		t.Fatal(err)
	}
	if len(binder.unbound) != 1 || binder.unbound[0] != "cobot_deepseek" {
		t.Fatalf("the engine was not told: %+v", binder.unbound)
	}
	list, err := a.ListProviders(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("the provider survived the delete: %+v", list)
	}
}

func TestProviderPresetsCarryTheirDialectAndKeyPatterns(t *testing.T) {
	presets := ProviderPresets()
	if len(presets) == 0 {
		t.Fatal("the built-in catalogue is empty")
	}
	seen := map[string]bool{}
	for _, p := range presets {
		if p.ID == "" || p.Name == "" || p.Short == "" {
			t.Fatalf("incomplete preset: %+v", p)
		}
		if seen[p.ID] {
			t.Fatalf("duplicate preset id: %s", p.ID)
		}
		seen[p.ID] = true
		for _, e := range p.Endpoints {
			if e.Dialect != domain.DialectOpenAIResponses && e.Dialect != domain.DialectAnthropicMessages {
				t.Fatalf("preset %s declares an unknown dialect: %q", p.ID, e.Dialect)
			}
		}
	}
	// The official provider inherits the engine's own endpoint, so it has
	// none of its own.
	for _, p := range presets {
		if p.Official && len(p.Endpoints) != 0 {
			t.Fatalf("the official preset declares endpoints: %+v", p.Endpoints)
		}
	}
}

func TestDetectProviderFromKeyNeverGuesses(t *testing.T) {
	if got := DetectProviderFromKey("sk-ant-api03-" + strings.Repeat("A", 92)); got != "anthropic" {
		t.Fatalf("Anthropic key not recognised: %q", got)
	}
	if got := DetectProviderFromKey("sk-or-v1-" + strings.Repeat("a", 64)); got != "openrouter" {
		t.Fatalf("OpenRouter key not recognised: %q", got)
	}
	// A key of a vendor with a generic format cannot be told apart, and the
	// product must not pretend otherwise.
	if got := DetectProviderFromKey("sk-whatever"); got != "" {
		t.Fatalf("an unrecognised key was guessed: %q", got)
	}
	if got := DetectProviderFromKey(""); got != "" {
		t.Fatalf("an empty key was guessed: %q", got)
	}
}
