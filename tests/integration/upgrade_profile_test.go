package integration

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/kunghim/CoBot/internal/platform"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// The release check supplies a profile made by the previous tag's own
// migration code. Ordinary unit runs do not fabricate an old-version fixture.
func TestPreviousReleaseProfileUpgrades(t *testing.T) {
	root := os.Getenv("COBOT_UPGRADE_PROFILE")
	if root == "" {
		t.Skip("run node scripts/check-upgrade.mjs <published-tag> to supply the previous release profile")
	}
	raw, err := os.ReadFile(filepath.Join(root, "upgrade-fixture.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		SchemaVersion int64 `json:"schemaVersion"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil || fixture.SchemaVersion < 1 {
		t.Fatalf("invalid previous release fixture: %v", err)
	}
	// Both starts use the same synthetic profile, never the user's profiles.
	for attempt := 0; attempt < 2; attempt++ {
		core := startCore(t, platform.EnvDevelopment, root)
		c := newClient(t, core.Endpoint)
		authorize(t, core, c)
		resp, body := c.do("GET", "/api/v1/settings", nil, nil)
		if resp.StatusCode != 200 {
			t.Fatalf("upgraded settings: %d %v", resp.StatusCode, body)
		}
		settings := body["data"].(map[string]any)
		mode := "dark"
		if attempt > 0 {
			mode = "light"
		}
		if settings["language"] != "en" || settings["theme"] != "harbor" || settings["colorMode"] != mode {
			t.Fatal("appearance did not survive migration/restart")
		}
		team, directory, err := sqlite.GetTeam(context.Background(), core.App.DB, "upgrade-team")
		if err != nil || team.Name != "升级保留团队" || team.LeaderID != "upgrade-bot" || team.Status != "archived" || directory != filepath.Join(root, "synthetic-team") {
			t.Fatalf("team did not survive: %+v %v", team, err)
		}
		bot, err := sqlite.GetBot(context.Background(), core.App.DB, "upgrade-bot")
		if err != nil || bot.TeamID != team.ID || !bot.IsLeader || bot.Name != "Upgrade member" {
			t.Fatalf("member did not survive: %+v %v", bot, err)
		}
		resp, body = c.do("GET", "/api/v1/bots/upgrade-bot/messages", nil, nil)
		if resp.StatusCode != 200 {
			t.Fatalf("upgraded conversation: %d %v", resp.StatusCode, body)
		}
		message, err := sqlite.GetMessage(context.Background(), core.App.DB, "upgrade-message")
		if err != nil || message.RenderText() != "Synthetic history survives upgrade / 升级后保留" {
			t.Fatalf("history did not survive: %v", err)
		}
		if attempt == 0 {
			resp, body = c.patchSettings(map[string]any{"colorMode": "light"}, int64(settings["revision"].(float64)))
			if resp.StatusCode != 200 {
				t.Fatalf("upgraded settings are not writable: %d %v", resp.StatusCode, body)
			}
		}
		migrator, err := sqlite.NewMigrator(core.App.DB)
		if err != nil {
			t.Fatal(err)
		}
		version, err := migrator.CurrentVersion(context.Background())
		if err != nil || version != sqlite.MaxSupportedVersion {
			t.Fatalf("upgraded schema: %d %v", version, err)
		}
		if fixture.SchemaVersion < version {
			backups, err := filepath.Glob(filepath.Join(root, "backups", "*.sqlite"))
			if err != nil || len(backups) == 0 {
				t.Fatalf("pre-migration backup missing: %v", err)
			}
		}
		if err := core.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
}
