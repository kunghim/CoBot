package integration

import (
	"context"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/domain"
)

// Each member runs with the model it chose, from any enabled provider; a
// member that chose nothing follows the default model the settings name.
func TestMembersRunWithTheModelTheyChose(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()

	saveProvider(t, h, "first", "first-model")
	saveProvider(t, h, "second", "second-model")
	setDefaultModel(t, h, "first", "first-model")

	team, bots := h.createTeam(t, []string{"fullstack-engineer", "qa-engineer"})
	fake := h.ensureFake(t, team.ID)
	engineer, tester := bots[1], bots[2]

	chosen, err := h.app.SetBotModel(ctx, h.user, engineer.ID, "second", "second-model", engineer.Revision)
	if err != nil {
		t.Fatalf("choose the engineer's model: %v", err)
	}
	if chosen.ProviderID != "second" || chosen.Model != "second-model" {
		t.Fatalf("the member records %q/%q", chosen.ProviderID, chosen.Model)
	}

	for i, bot := range []domain.Bot{engineer, tester} {
		if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{
			BotID: bot.ID, Text: "hi", RequestID: "member-model-" + bot.ID,
		}); err != nil {
			t.Fatal(err)
		}
		waitUntil(t, 5*time.Second, "turn", func() bool { return len(fake.Turns()) == i+1 })
	}
	turns := fake.Turns()
	models := map[string]string{}
	for _, turn := range turns {
		models[turn.ThreadID] = turn.Model
	}
	if got := fake.ThreadProviders; len(got) != 2 || got[0] != "second" || got[1] != "first" {
		t.Fatalf("the threads named %v, want the engineer's own provider and the default", got)
	}
	if turns[0].Model != "second-model" || turns[1].Model != "first-model" {
		t.Fatalf("the turns asked for %q and %q", turns[0].Model, turns[1].Model)
	}
}

// A provider that is switched off is out of every picker: a member cannot
// choose it, and a member that ran on it moves to the default model.
func TestADisabledProviderIsNotOnOffer(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()

	saveProvider(t, h, "first", "first-model")
	saveProvider(t, h, "second", "second-model")
	setDefaultModel(t, h, "first", "first-model")

	team, bots := h.createTeam(t, []string{"fullstack-engineer"})
	fake := h.ensureFake(t, team.ID)
	engineer := bots[1]
	engineer, err := h.app.SetBotModel(ctx, h.user, engineer.ID, "second", "second-model", engineer.Revision)
	if err != nil {
		t.Fatal(err)
	}

	second, err := h.app.GetProvider(ctx, "second")
	if err != nil {
		t.Fatal(err)
	}
	if !second.Enabled {
		t.Fatal("a provider the user added is not on offer")
	}
	if _, err := h.app.SetProviderEnabled(ctx, "second", false, second.Revision); err != nil {
		t.Fatalf("switch the provider off: %v", err)
	}

	if _, err := h.app.SetBotModel(ctx, h.user, engineer.ID, "second", "second-model", engineer.Revision); err == nil {
		t.Fatal("a member chose a model of a disabled provider")
	}
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{
		BotID: engineer.ID, Text: "hi", RequestID: "disabled-provider",
	}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 5*time.Second, "turn", func() bool { return len(fake.Turns()) == 1 })
	if got := fake.ThreadProviders; len(got) == 0 || got[0] != "first" {
		t.Fatalf("the thread named %v, want the default once the member's provider is off", got)
	}
	if got := fake.Turns()[0].Model; got != "first-model" {
		t.Fatalf("the turn asked for %q, want the default model", got)
	}
}

// A member cannot choose a model its provider does not keep.
func TestAMemberCannotChooseAModelTheProviderDoesNotKeep(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	saveProvider(t, h, "first", "first-model")
	_, bots := h.createTeam(t, []string{"fullstack-engineer"})
	if _, err := h.app.SetBotModel(ctx, h.user, bots[1].ID, "first", "not-kept", bots[1].Revision); err == nil {
		t.Fatal("a member chose a model the provider does not keep")
	}
	if _, err := h.app.SetBotModel(ctx, h.user, bots[1].ID, "gone", "x", bots[1].Revision); err == nil {
		t.Fatal("a member chose a provider that does not exist")
	}
}

// A thread on the ChatGPT account is not loaded again on every turn. The
// engine names that account "openai"; a thread that asked for none would
// look like it was switching provider each time and be archived and
// resumed before every turn.
func TestAnOfficialThreadIsNotReloadedOnEveryTurn(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()

	team, bots := h.createTeam(t, []string{"fullstack-engineer"})
	fake := h.ensureFake(t, team.ID)
	for i, text := range []string{"first", "second", "third"} {
		if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{
			BotID: bots[1].ID, Text: text, RequestID: "official-" + text,
		}); err != nil {
			t.Fatal(err)
		}
		waitUntil(t, 5*time.Second, text+" turn", func() bool { return len(fake.Turns()) == i+1 })
		turn := fake.Turns()[i]
		fake.EmitTurnCompleted(turn.ThreadID, turn.TurnID, "completed", "done")
	}
	if got := fake.Archived(); len(got) != 0 {
		t.Fatalf("the official thread was archived %d times", len(got))
	}
	if got := fake.Resumes(); got != 0 {
		t.Fatalf("the official thread was resumed %d times while still loaded", got)
	}
}

func setDefaultModel(t *testing.T, h *m2Harness, providerID, model string) {
	t.Helper()
	ctx := context.Background()
	settings, err := h.app.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.UpdateSettings(ctx, h.user, domain.SettingsPatch{DefaultProvider: &providerID, DefaultModel: &model}, settings.Revision); err != nil {
		t.Fatalf("name the default model: %v", err)
	}
}

// A member moved back to the default after a model of its own runs the
// engine's default model again. A turn that names no model keeps whatever
// its thread last ran with, so the default has to be named.
func TestAMemberBackOnTheDefaultLeavesItsOwnModel(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()

	team, bots := h.createTeam(t, []string{"fullstack-engineer"})
	fake := h.ensureFake(t, team.ID)
	engineer := bots[1]
	engineer, err := h.app.SetBotModel(ctx, h.user, engineer.ID, "openai", "chosen-model", engineer.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: engineer.ID, Text: "first", RequestID: "own-model"}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 5*time.Second, "first turn", func() bool { return len(fake.Turns()) == 1 })
	first := fake.Turns()[0]
	fake.EmitTurnCompleted(first.ThreadID, first.TurnID, "completed", "done")
	if first.Model != "chosen-model" {
		t.Fatalf("the first turn asked for %q", first.Model)
	}

	if _, err := h.app.SetBotModel(ctx, h.user, engineer.ID, "", "", engineer.Revision); err != nil {
		t.Fatalf("back to the default: %v", err)
	}
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: engineer.ID, Text: "second", RequestID: "back-to-default"}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 5*time.Second, "second turn", func() bool { return len(fake.Turns()) == 2 })
	if got := fake.Turns()[1].Model; got != "fake-model" {
		t.Fatalf("the turn after going back asked for %q, want the engine's default", got)
	}
}
