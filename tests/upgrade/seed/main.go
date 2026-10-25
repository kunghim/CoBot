// Command seed creates synthetic upgrade data using a released version's
// own store and migrations. The runner copies this file into that source tree.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

func main() {
	root := flag.String("data-root", "", "explicit synthetic data root")
	flag.Parse()
	if *root == "" {
		panic("an explicit data root is required")
	}
	if err := seed(*root); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func seed(root string) error {
	ctx := context.Background()
	// Refuse existing directories, even empty ones: this utility only creates
	// a new fixture and must never overwrite a profile supplied by mistake.
	if err := os.Mkdir(root, 0o700); err != nil {
		return fmt.Errorf("create fresh fixture root: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "data"), 0o700); err != nil {
		return err
	}
	db, err := sqlite.Open(filepath.Join(root, "data", "cobot.sqlite"))
	if err != nil {
		return err
	}
	defer db.Close()
	migrator, err := sqlite.NewMigrator(db)
	if err != nil {
		return err
	}
	if err := migrator.MigrateToLatest(ctx, filepath.Join(root, "backups")); err != nil {
		return err
	}
	lang, mode, theme := "en", "dark", "harbor"
	// An explicit missing engine prevents discovery of a developer's real Codex.
	engine := filepath.Join(root, "synthetic-missing-engine")
	if _, err := sqlite.UpdateSettings(ctx, db, domain.SettingsPatch{
		Language: &lang, ColorMode: &mode, Theme: &theme, EnginePath: &engine,
	}, 0); err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := sqlite.SetMeta(ctx, tx, "profileInstanceId", "upgrade-profile"); err != nil {
		return err
	}
	directory := filepath.Join(root, "synthetic-team")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	// Archived teams never dispatch a model turn during startup verification.
	if err := sqlite.InsertTeam(ctx, tx, domain.Team{
		ID: "upgrade-team", Name: "升级保留团队", ProfileInstance: "upgrade-profile",
		LeaderID: "upgrade-bot", Status: domain.TeamStatusArchived,
	}, directory, directory, []domain.Bot{{
		ID: "upgrade-bot", TeamID: "upgrade-team", Name: "Upgrade member",
		RoleID: "pm", IsLeader: true,
	}}); err != nil {
		return err
	}
	if err := sqlite.InsertMessage(ctx, tx, domain.Message{
		ID: "upgrade-message", TeamID: "upgrade-team", BotID: "upgrade-bot",
		Kind: domain.MessageKindAssistant, Blocks: json.RawMessage(`[{"type":"text","text":"Synthetic history survives upgrade / 升级后保留"}]`),
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	version, err := migrator.CurrentVersion(ctx)
	if err != nil {
		return err
	}
	manifest, err := json.Marshal(map[string]any{"schemaVersion": version})
	if err != nil {
		return err
	}
	// Keep startup's optional asset sync offline as well.
	if err := os.WriteFile(filepath.Join(root, "config.json"), []byte(`{"assetsBaseUrl":"http://127.0.0.1:9/assets/"}`), 0o600); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, "upgrade-fixture.json"), manifest, 0o600)
}
