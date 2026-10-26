package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// seedQuotaAccount fakes the ChatGPT account's latest rate limits and clears
// it when the test ends.
func seedQuotaAccount(t *testing.T, raw string) {
	t.Helper()
	accountRateLimits.mu.Lock()
	accountRateLimits.raw = json.RawMessage(raw)
	accountRateLimits.mu.Unlock()
	t.Cleanup(func() {
		accountRateLimits.mu.Lock()
		accountRateLimits.raw = nil
		accountRateLimits.mu.Unlock()
	})
}

func moveBotToProvider(t *testing.T, a *App, botID, providerID, model string) {
	t.Helper()
	tx, err := a.DB.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlite.SetBotModel(context.Background(), tx, botID, providerID, model); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

// The account quota belongs to the ChatGPT account: a member resolved to
// another vendor's model, by its own choice or through the default model,
// does not see it.
func TestBotStatsRateLimitsFollowTheProvider(t *testing.T) {
	ctx := context.Background()
	binder := newFakeBinder()
	a := providerTestApp(t, binder)
	seedTeam(t, a, "t1", "lead", "dev", "qa")
	seedQuotaAccount(t, `{"primary":{"used_percent":7}}`)
	user := auth.NewPrincipalUser("")

	// Nothing configured: the member runs on the engine's own account and
	// its quota shows.
	stats, err := a.BotStats(ctx, user, "dev")
	if err != nil {
		t.Fatal(err)
	}
	if stats.RateLimits == nil {
		t.Fatal("an account member sees no quota")
	}

	// Another member's own choice of a third-party provider hides it.
	if _, err := a.SaveProvider(ctx, testProvider(), 0); err != nil {
		t.Fatal(err)
	}
	moveBotToProvider(t, a, "qa", "cobot_deepseek", "deepseek-v4-pro")
	stats, err = a.BotStats(ctx, user, "qa")
	if err != nil {
		t.Fatal(err)
	}
	if stats.RateLimits != nil {
		t.Fatal("a member on another vendor still sees the account quota")
	}

	// A member on the default model of a third-party provider hides it too.
	model := "deepseek-v4-pro"
	if _, err := a.UpdateSettings(ctx, user, domain.SettingsPatch{DefaultProvider: &[]string{"cobot_deepseek"}[0], DefaultModel: &model}, 0); err != nil {
		t.Fatalf("set the default provider: %v", err)
	}
	stats, err = a.BotStats(ctx, user, "dev")
	if err != nil {
		t.Fatal(err)
	}
	if stats.RateLimits != nil {
		t.Fatal("a member on the default third-party model still sees the account quota")
	}
}

// The team's own provider candidate sits between the member's choice and
// the settings default, exactly as it does for a turn.
func TestBotStatsRateLimitsFollowTheTeamProvider(t *testing.T) {
	ctx := context.Background()
	binder := newFakeBinder()
	a := providerTestApp(t, binder)
	seedQuotaAccount(t, `{"primary":{"used_percent":7}}`)
	if _, err := a.SaveProvider(ctx, testProvider(), 0); err != nil {
		t.Fatal(err)
	}

	var bots []domain.Bot
	for _, id := range []string{"lead", "dev"} {
		bots = append(bots, domain.Bot{ID: id, TeamID: "t2", Name: id, RoleID: "fullstack-engineer", IsLeader: id == "lead"})
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	team := domain.Team{ID: "t2", Name: "t2", ProfileInstance: "p", LeaderID: "lead", Status: domain.TeamStatusActive, ProviderID: "cobot_deepseek"}
	if err := sqlite.InsertTeam(ctx, tx, team, "C:/t2", "t2", bots); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	stats, err := a.BotStats(ctx, auth.NewPrincipalUser(""), "dev")
	if err != nil {
		t.Fatal(err)
	}
	if stats.RateLimits != nil {
		t.Fatal("a member running on the team's third-party provider still sees the account quota")
	}
}

// A team listing reports how each member's latest finished run ended, for
// surfaces that ask whether its last session failed.
func TestTeamListingCarriesLastRunStatus(t *testing.T) {
	ctx := context.Background()
	binder := newFakeBinder()
	a := providerTestApp(t, binder)
	seedTeam(t, a, "t9", "lead", "dev")
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	insert := func(id, bot, status, started string) {
		t.Helper()
		if _, err := tx.ExecContext(ctx, `INSERT INTO runs (id, team_id, bot_id, status, engine_epoch, started_at, ended_at) VALUES (?, 't9', ?, ?, 'e', ?, '2026-10-26T00:00:00Z')`,
			id, bot, status, started); err != nil {
			t.Fatal(err)
		}
	}
	insert("r1", "dev", "completed", "2026-10-25T10:00:00Z")
	insert("r2", "dev", "failed", "2026-10-25T11:00:00Z")
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	page, err := a.ListPage(ctx, auth.NewPrincipalUser(""), PageRequest{Kind: "teams", TeamID: "t9"})
	_ = page
	team, err := sqlite.GetTeamByID(ctx, a.DB, "t9")
	if err != nil {
		t.Fatal(err)
	}
	view, err := teamWithBotsOn(ctx, a.DB, team)
	if err != nil {
		t.Fatal(err)
	}
	for _, bot := range view.Bots {
		if bot.ID == "dev" && bot.LastRunStatus != "failed" {
			t.Fatalf("dev lastRunStatus = %q, want failed", bot.LastRunStatus)
		}
	}
}
