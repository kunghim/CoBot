package integration

import (
	"context"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/domain"
)

// A team that names its own provider runs with it; a team that does not
// follows the profile's default. The thread's own record is not consulted:
// it says which provider the thread had, not the one it should have.
func TestTeamProviderWinsOverTheProfileDefault(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()

	saveProvider(t, h, "first", "vendor-model")
	saveProvider(t, h, "second", "other-model")
	settings, err := h.app.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	id := "first"
	if _, err := h.app.UpdateSettings(ctx, h.user, domain.SettingsPatch{DefaultProvider: &id}, settings.Revision); err != nil {
		t.Fatalf("run with the first provider: %v", err)
	}

	team, bots := h.createTeam(t, []string{"fullstack-engineer"})
	fake := h.ensureFake(t, team.ID)

	// The team names its own provider.
	team, err = h.app.PatchTeamProvider(ctx, h.user, team.ID, "second", team.Revision)
	if err != nil {
		t.Fatalf("name the team's provider: %v", err)
	}
	if team.ProviderID != "second" {
		t.Fatalf("the team names %q", team.ProviderID)
	}

	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{
		BotID: bots[1].ID, Text: "hi", RequestID: "team-provider",
	}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 5*time.Second, "turn", func() bool { return len(fake.Turns()) == 1 })
	if got := fake.ThreadProviders; len(got) == 0 || got[0] != "second" {
		t.Fatalf("the thread named %v, want the team's own provider", got)
	}
	if got := fake.Turns()[0].Model; got != "other-model" {
		t.Fatalf("the turn asked for %q, want the team's provider's model", got)
	}
}

// A team that changes its provider moves its threads over. The engine
// ignores a provider for a thread it already holds, so the thread has to let
// go of it first.
func TestChangingATeamsProviderMovesItsThreads(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()

	saveProvider(t, h, "first", "vendor-model")
	saveProvider(t, h, "second", "other-model")
	settings, err := h.app.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	id := "first"
	if _, err := h.app.UpdateSettings(ctx, h.user, domain.SettingsPatch{DefaultProvider: &id}, settings.Revision); err != nil {
		t.Fatalf("run with the first provider: %v", err)
	}

	team, bots := h.createTeam(t, []string{"fullstack-engineer"})
	fake := h.ensureFake(t, team.ID)
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{
		BotID: bots[1].ID, Text: "first", RequestID: "before-switch",
	}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 5*time.Second, "first turn", func() bool { return len(fake.Turns()) == 1 })
	if got := fake.ThreadProviders; len(got) == 0 || got[0] != "first" {
		t.Fatalf("the thread named %v, want the profile's provider", got)
	}
	// The first turn has to end before the next one is delivered.
	first := fake.Turns()[0]
	fake.EmitTurnCompleted(first.ThreadID, first.TurnID, "completed", "done")

	team, err = h.app.PatchTeamProvider(ctx, h.user, team.ID, "second", team.Revision)
	if err != nil {
		t.Fatalf("change the team's provider: %v", err)
	}
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{
		BotID: bots[1].ID, Text: "second", RequestID: "after-switch",
	}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 5*time.Second, "second turn", func() bool { return len(fake.Turns()) == 2 })

	// Another provider is another vendor: its model cannot take the old
	// history as input, so the member starts a new thread pointed at the
	// old record (see handover_test.go), and the old thread is left alone.
	if got := fake.Archived(); len(got) != 0 {
		t.Fatalf("the old thread was archived: %v", got)
	}
	if got := fake.ThreadProviders; len(got) != 2 || got[1] != "second" {
		t.Fatalf("no new thread on the new provider: %v", got)
	}
	if second := fake.Turns()[1]; second.ThreadID == first.ThreadID {
		t.Fatal("the second turn ran on the old thread")
	}
}

// A team cannot name a provider that does not exist: the engine refuses an
// id it does not know, and the team would point at nothing.
func TestATeamCannotNameAProviderThatDoesNotExist(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()

	team, _ := h.createTeam(t, []string{"fullstack-engineer"})
	if _, err := h.app.PatchTeamProvider(ctx, h.user, team.ID, "not-configured", team.Revision); err == nil {
		t.Fatal("a team was allowed to name a provider that does not exist")
	}
}

func saveProvider(t *testing.T, h *m2Harness, id, model string) {
	t.Helper()
	if _, err := h.app.SaveProvider(context.Background(), domain.Provider{
		ID: id, Name: id, Kind: domain.ProviderKindCustom, AuthMode: domain.ProviderAuthAPIKey,
		Endpoints: []domain.ProviderEndpoint{
			{Dialect: domain.DialectOpenAIResponses, BaseURL: "https://" + id + ".example/v1"},
		},
		Models:       []domain.ProviderModel{{ID: model}},
		DefaultModel: model,
	}, 0); err != nil {
		t.Fatalf("save the provider %s: %v", id, err)
	}
}

// A check run while editing a provider with the key field left empty uses
// the key that provider keeps, but only toward an address the provider
// already has: the address comes from the caller, and a stored key must not
// go wherever a request asks it to.
func TestAStoredKeyOnlyGoesToItsOwnEndpoint(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	saveProvider(t, h, "gateway", "gateway-model")
	if err := h.app.Credentials.Put(ctx, "provider_gateway", []byte("sk-stored")); err != nil {
		t.Fatal(err)
	}

	key, err := h.app.StoredProviderKey(ctx, "gateway", "https://gateway.example/v1/")
	if err != nil || key != "sk-stored" {
		t.Fatalf("the provider's own endpoint got %q, %v", key, err)
	}
	if key, err := h.app.StoredProviderKey(ctx, "gateway", "https://elsewhere.example/v1"); err == nil || key != "" {
		t.Fatalf("another address got %q, %v; want a refusal", key, err)
	}
}
