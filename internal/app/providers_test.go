package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/engine"
	"github.com/kunghim/CoBot/internal/events"
	"github.com/kunghim/CoBot/internal/platform"
	"github.com/kunghim/CoBot/internal/secrets"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// fakeBinder records what the product asked the engine to do.
type fakeBinder struct {
	bound     []domain.Provider
	unbound   []string
	selected  string
	err       error
	selectErr error
	// selectedCh reports every selection attempt, so a test can wait for the
	// write that happens off the request path.
	selectedCh chan string
}

func newFakeBinder() *fakeBinder {
	return &fakeBinder{selectedCh: make(chan string, 8)}
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

func (f *fakeBinder) SelectProvider(ctx context.Context, id string) error {
	if f.selectedCh != nil {
		select {
		case f.selectedCh <- id:
		default:
		}
	}
	if f.selectErr != nil {
		return f.selectErr
	}
	f.selected = id
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
	if len(withoutOfficial(list)) != 0 {
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

// Every thread names its own provider, so the engine's configured default
// stays on its own account: that default is what calls without a thread
// see, the model list among them. Naming a default model in the settings
// puts the engine back there rather than onto the named provider. The write
// happens off the request path, so the test waits for it.
func TestSettingTheDefaultModelKeepsTheEngineOnItsOwnAccount(t *testing.T) {
	ctx := context.Background()
	binder := newFakeBinder()
	a := providerTestApp(t, binder)
	if _, err := a.SaveProvider(ctx, testProvider(), 0); err != nil {
		t.Fatal(err)
	}
	id := "cobot_deepseek"
	if _, err := a.UpdateSettings(ctx, auth.NewPrincipalUser(""), domain.SettingsPatch{
		DefaultProvider: &id,
	}, 0); err != nil {
		t.Fatalf("set the default provider: %v", err)
	}
	select {
	case got := <-binder.selectedCh:
		if got != "" {
			t.Fatalf("the engine was told %q, want its own account", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the engine was never told which provider to run with")
	}
	settings, err := a.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if settings.DefaultProvider != "cobot_deepseek" {
		t.Fatalf("the profile names %q", settings.DefaultProvider)
	}
}

// A dangling default provider is refused, and the engine is not switched to
// something that does not exist.
func TestSettingADanglingDefaultProviderIsRefused(t *testing.T) {
	ctx := context.Background()
	binder := &fakeBinder{}
	a := providerTestApp(t, binder)
	id := "not-configured"
	if _, err := a.UpdateSettings(ctx, auth.NewPrincipalUser(""), domain.SettingsPatch{
		DefaultProvider: &id,
	}, 0); err == nil {
		t.Fatal("a dangling default provider was accepted")
	}
	if binder.selected != "" {
		t.Fatalf("the engine was switched to %q", binder.selected)
	}
}

// An engine that will not take the provider does not undo a change the
// profile already accepted. The engine's own configuration is a fallback for
// the paths that do not name a provider; a thread always names its own.
func TestABusyEngineDoesNotUndoTheProfileChange(t *testing.T) {
	ctx := context.Background()
	binder := newFakeBinder()
	binder.selectErr = errors.New("the engine is busy")
	a := providerTestApp(t, binder)
	if _, err := a.SaveProvider(ctx, testProvider(), 0); err != nil {
		t.Fatal(err)
	}
	id := "cobot_deepseek"
	saved, err := a.UpdateSettings(ctx, auth.NewPrincipalUser(""), domain.SettingsPatch{
		DefaultProvider: &id,
	}, 0)
	if err != nil {
		t.Fatalf("the profile change was refused because the engine was busy: %v", err)
	}
	if saved.DefaultProvider != "cobot_deepseek" {
		t.Fatalf("the profile names %q", saved.DefaultProvider)
	}
	// The engine was asked, and refused: the write happens off the request
	// path, so the refusal lands after the change was accepted.
	select {
	case <-binder.selectedCh:
	case <-time.After(3 * time.Second):
		t.Fatal("the engine was never asked which provider to run with")
	}
}

// The official provider is the engine's own account: it has no endpoint and
// no key to hand over, and it still has to be storable.
func TestTheOfficialProviderCanBeStored(t *testing.T) {
	ctx := context.Background()
	binder := &fakeBinder{}
	a := providerTestApp(t, binder)
	view, err := a.SaveProvider(ctx, domain.Provider{
		ID: "openai", Name: "OpenAI", Kind: domain.ProviderKindOfficial,
		AuthMode: domain.ProviderAuthChatGPT,
	}, 0)
	if err != nil {
		t.Fatalf("the official provider could not be stored: %v", err)
	}
	if view.HasCredential {
		t.Fatal("the official provider reports a key")
	}
	if len(binder.bound) != 0 {
		t.Fatal("the engine was asked to bind the official provider")
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
	if len(withoutOfficial(list)) != 0 {
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
			if e.Dialect != domain.DialectOpenAIResponses {
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
	// An Anthropic key is no longer a vendor CoBot knows.
	if got := DetectProviderFromKey("sk-ant-api03-" + strings.Repeat("A", 92)); got != "" {
		t.Fatalf("an Anthropic key was guessed: %q", got)
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

// probeGateway answers like a provider: 200 for a good model, 402 for one
// that cannot be paid for.
func probeGateway(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(body, &payload)
		w.Header().Set("Content-Type", "application/json")
		if payload.Model == "broke" {
			w.WriteHeader(http.StatusPaymentRequired)
			_, _ = w.Write([]byte(`{"error":{"code":"insufficient_quota","message":"余额不足"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestProbeProviderRecordsTheOutcomeOnTheStoredModels(t *testing.T) {
	ctx := context.Background()
	gateway := probeGateway(t)
	a := providerTestApp(t, &fakeBinder{})

	p := testProvider()
	p.Endpoints = []domain.ProviderEndpoint{
		{Dialect: domain.DialectOpenAIResponses, BaseURL: gateway.URL},
	}
	p.Models = []domain.ProviderModel{{ID: "good"}, {ID: "broke"}}
	if _, err := a.SaveProvider(ctx, p, 0); err != nil {
		t.Fatal(err)
	}

	results, err := a.ProbeProvider(ctx, "cobot_deepseek")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("probed %d models, want 2", len(results))
	}
	if !results["good"].OK {
		t.Fatalf("the working model was reported as broken: %+v", results["good"])
	}
	if results["broke"].OK || results["broke"].Code != "quota" {
		t.Fatalf("the unpaid model was not reported as such: %+v", results["broke"])
	}

	// The outcome belongs to the stored models: a model that cannot run is
	// kept and marked, not dropped.
	stored, err := a.GetProvider(ctx, "cobot_deepseek")
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Models) != 2 {
		t.Fatalf("a model was dropped: %+v", stored.Models)
	}
	byID := map[string]domain.ProviderModel{}
	for _, m := range stored.Models {
		byID[m.ID] = m
	}
	if byID["good"].Status != domain.ModelStatusOK {
		t.Fatalf("the working model is marked %q", byID["good"].Status)
	}
	if byID["broke"].Status != domain.ModelStatusFailed || byID["broke"].Failure != "quota" {
		t.Fatalf("the broken model is marked %+v", byID["broke"])
	}
}

func TestProbeProviderRefusesAProviderWithNoOpenAIEndpoint(t *testing.T) {
	ctx := context.Background()
	a := providerTestApp(t, &fakeBinder{})

	// The stored state is arranged directly in the store, without
	// validation: a provider left without any endpoint exists only as the
	// leftover of the migration that removed the Claude dialect, and it must
	// refuse to be probed instead of pretending to check anything.
	p := testProvider()
	if _, err := a.SaveProvider(ctx, p, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := a.DB.ExecContext(ctx, `UPDATE model_providers SET endpoints_json = '[]' WHERE id = ?`, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ProbeProvider(ctx, "cobot_deepseek"); err == nil {
		t.Fatal("a provider that cannot be probed was probed anyway")
	}
}

// A provider that names a catalogue vendor by its id keeps the catalogue
// reference even when the caller left it out: without it the interface loses
// the vendor's mark and its localised name.
func TestSaveProviderKeepsTheCatalogueReference(t *testing.T) {
	ctx := context.Background()
	a := providerTestApp(t, &fakeBinder{})

	p := testProvider()
	p.ID = "deepseek"
	p.PresetID = ""
	if _, err := a.SaveProvider(ctx, p, 0); err != nil {
		t.Fatal(err)
	}
	stored, err := a.GetProvider(ctx, "deepseek")
	if err != nil {
		t.Fatal(err)
	}
	if stored.PresetID != "deepseek" {
		t.Fatalf("the catalogue reference is %q, want %q", stored.PresetID, "deepseek")
	}
}

// A gateway of the user's own keeps no catalogue reference.
func TestSaveProviderLeavesACustomProviderWithoutOne(t *testing.T) {
	ctx := context.Background()
	a := providerTestApp(t, &fakeBinder{})

	p := testProvider()
	p.ID = "cobot_gateway"
	if _, err := a.SaveProvider(ctx, p, 0); err != nil {
		t.Fatal(err)
	}
	stored, err := a.GetProvider(ctx, "cobot_gateway")
	if err != nil {
		t.Fatal(err)
	}
	if stored.PresetID != "" {
		t.Fatalf("a custom provider was given the catalogue reference %q", stored.PresetID)
	}
}

// withoutOfficial drops the ChatGPT account, which every profile lists.
func withoutOfficial(list []ProviderView) []ProviderView {
	out := []ProviderView{}
	for _, p := range list {
		if p.AuthMode != domain.ProviderAuthChatGPT {
			out = append(out, p)
		}
	}
	return out
}

func TestAMemberWithNoModelStaysOnTheSignedInAccount(t *testing.T) {
	ctx := context.Background()
	a := providerTestApp(t, &fakeBinder{})
	if _, err := a.SaveProvider(ctx, testProvider(), 0); err != nil {
		t.Fatal(err)
	}

	// An account change empties the status cache; the sign-in learned
	// before it still holds, so the member does not drift to the gateway.
	a.noteOfficialLogin(true)
	a.invalidateEngineStatus()
	choice, err := a.MemberModel(ctx, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if choice.EngineProvider != officialEngineProvider {
		t.Fatalf("signed-in account lost to %+v", choice)
	}

	// Known to be signed out, the next provider that can run takes over.
	a.noteOfficialLogin(false)
	choice, err = a.MemberModel(ctx, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if choice.ProviderID != "cobot_deepseek" {
		t.Fatalf("signed-out account still chosen: %+v", choice)
	}
}

func TestAnUncheckedAccountWithNoAlternativeIsNotProbed(t *testing.T) {
	a := providerTestApp(t, &fakeBinder{})
	// No other provider could run the turn, so there is nothing to ask the
	// engine: the account is the choice either way.
	choice, err := a.MemberModel(context.Background(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if choice.EngineProvider != officialEngineProvider {
		t.Fatalf("choice = %+v", choice)
	}
	if _, known := a.officialLoginState(); known {
		t.Fatal("the account was probed although nothing else could run")
	}
}

func TestRebindProvidersHandsStoredKeyProvidersToTheEngineAgain(t *testing.T) {
	ctx := context.Background()
	binder := &fakeBinder{}
	a := providerTestApp(t, binder)
	if _, err := a.SaveProvider(ctx, testProvider(), 0); err != nil {
		t.Fatal(err)
	}
	// An engine home reset apart from the database knows nothing.
	binder.bound = nil

	a.RebindProviders(ctx)
	if len(binder.bound) != 1 || binder.bound[0].ID != "cobot_deepseek" {
		t.Fatalf("bound after rebind = %+v, want only the key provider", binder.bound)
	}
}

func TestAThinkingLevelReachesOnlyTheModelsThatDeclaredIt(t *testing.T) {
	a := providerTestApp(t, newFakeBinder())
	ctx := context.Background()
	gateway := testProvider()
	gateway.Models = []domain.ProviderModel{
		{ID: "plain"},
		{ID: "thinker", SupportedEfforts: []string{"low", "high"}},
	}
	if _, err := a.SaveProvider(ctx, gateway, 0); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name   string
		choice ModelChoice
		model  string
		effort string
		want   string
	}{
		{"no level set", ModelChoice{ProviderID: gateway.ID, Model: "plain"}, "plain", "", ""},
		{"a gateway model that declared nothing gets the defaults", ModelChoice{ProviderID: gateway.ID, Model: "plain"}, "plain", "high", "high"},
		{"but not a level beyond them", ModelChoice{ProviderID: gateway.ID, Model: "plain"}, "plain", "xhigh", ""},
		{"a model that lists the level", ModelChoice{ProviderID: gateway.ID, Model: "thinker"}, "thinker", "high", "high"},
		{"a model that does not list this level", ModelChoice{ProviderID: gateway.ID, Model: "thinker"}, "thinker", "ultra", ""},
		{"the engine's own account", ModelChoice{}, "gpt", "ultra", "ultra"},
		{"a provider that is gone", ModelChoice{ProviderID: "gone", Model: "x"}, "x", "high", ""},
	} {
		if got := a.effortFor(ctx, c.choice, c.model, c.effort); got != c.want {
			t.Errorf("%s: effort = %q, want %q", c.name, got, c.want)
		}
	}
}
