package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/kunghim/CoBot/internal/domain"
)

// TestClaudeEndpointDropMigratesProviders covers version 34: the Anthropic
// Messages dialect is gone, so providers lose their Claude endpoint, and one
// left without any endpoint is disabled instead of deleted — its row and key
// stay, and the settings list asks the user to remove it.
func TestClaudeEndpointDropMigratesProviders(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "v33.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	old, err := migratorBefore(t, db, "00035")
	if err != nil {
		t.Fatal(err)
	}
	backups := filepath.Join(dir, "backups")
	if err := old.MigrateToLatest(ctx, backups); err != nil {
		t.Fatal(err)
	}

	seed := func(id, auth, endpoints string, enabled int) {
		t.Helper()
		_, err := db.ExecContext(ctx,
			`INSERT INTO model_providers
				(id, name, kind, auth_mode, credential_ref, endpoints_json, models_json, default_model, retries_json, preset_id, enabled, revision, created_at, updated_at)
			 VALUES (?, 'Seed', 'custom', ?, 'provider_'+?, ?, '[]', '', '{}', '', ?, 1, '2026-10-25T00:00:00Z', '2026-10-25T00:00:00Z')`,
			id, auth, id, endpoints, enabled)
		if err != nil {
			t.Fatal(err)
		}
	}
	seed("mixed", "api_key",
		`[{"dialect":"anthropic-messages","baseUrl":"https://api.deepseek.com/anthropic"},{"dialect":"openai-responses","baseUrl":"https://api.deepseek.com/v1"}]`, 1)
	seed("claude-only", "api_key",
		`[{"dialect":"anthropic-messages","baseUrl":"https://api.anthropic.com"}]`, 1)
	// The ChatGPT account was seeded by an earlier migration with no
	// endpoints on purpose; it must not be caught by the "nothing left" rule.

	current, err := NewMigrator(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := current.MigrateToLatest(ctx, backups); err != nil {
		t.Fatal(err)
	}

	mixed, err := GetProvider(ctx, db, "mixed")
	if err != nil {
		t.Fatal(err)
	}
	if len(mixed.Endpoints) != 1 || mixed.Endpoints[0].Dialect != domain.DialectOpenAIResponses {
		t.Fatalf("the surviving endpoint did not survive: %+v", mixed.Endpoints)
	}
	if !mixed.Enabled {
		t.Fatal("a provider that still has an endpoint was disabled")
	}

	legacy, err := GetProvider(ctx, db, "claude-only")
	if err != nil {
		t.Fatal(err)
	}
	if len(legacy.Endpoints) != 0 {
		t.Fatalf("the removed dialect did not go away: %+v", legacy.Endpoints)
	}
	if legacy.Enabled {
		t.Fatal("a provider left without any endpoint was left usable")
	}

	var officialEnabled int
	if err := db.QueryRowContext(ctx, `SELECT enabled FROM model_providers WHERE id = 'openai'`).Scan(&officialEnabled); err != nil {
		t.Fatal(err)
	}
	if officialEnabled != 1 {
		t.Fatal("the ChatGPT account was disabled by the endpoint drop")
	}
}
