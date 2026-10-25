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
