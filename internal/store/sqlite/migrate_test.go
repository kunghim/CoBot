package sqlite

import (
	"context"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/testutil"
)

func TestMigrateFreshDatabase(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "cobot.sqlite")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	m, err := NewMigrator(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.MigrateToLatest(ctx, t.TempDir()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	v, err := m.CurrentVersion(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if v != MaxSupportedVersion {
		t.Fatalf("version = %d, want %d", v, MaxSupportedVersion)
	}
	// Second start must be idempotent: no re-migration, no error.
	if err := m.MigrateToLatest(ctx, t.TempDir()); err != nil {
		t.Fatalf("re-migrate: %v", err)
	}
	settings, err := GetSettings(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if settings.MemberLimit != 6 || settings.Language != "zh-CN" {
		t.Fatalf("unexpected defaults: %+v", settings)
	}
}

func TestUpgradeTakesBackupAndPreservesData(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "cobot.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Simulate an old library running only the first schema version, with
	// business data present before the upgrade.
	onlyV1 := testutil.NewMapFS(map[string]string{
		"00001_initial.sql": string(readMigration(t, "00001_initial.sql")),
	})
	m1, err := NewMigratorWithFS(db, onlyV1)
	if err != nil {
		t.Fatal(err)
	}
	backups := filepath.Join(dir, "backups")
	if err := m1.MigrateToLatest(ctx, backups); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO events (id, instance_id, type, created_at)
		VALUES ('e-seed', 'i-seed', 'seeded', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}

	// Upgrading to a later schema version must snapshot first and keep data.
	upgradeFS := testutil.NewMapFS(map[string]string{
		"00001_initial.sql": string(readMigration(t, "00001_initial.sql")),
		"00002_legacy.sql": "-- +goose Up\n" +
			"CREATE TABLE legacy_probe (id TEXT PRIMARY KEY, body TEXT NOT NULL);\n" +
			"INSERT INTO legacy_probe (id, body) VALUES ('m1', 'kept');\n" +
			"-- +goose Down\nDROP TABLE legacy_probe;\n",
	})
	m2, err := NewMigratorWithFS(db, upgradeFS)
	if err != nil {
		t.Fatal(err)
	}
	if err := m2.MigrateToLatest(ctx, backups); err != nil {
		t.Fatalf("upgrade: %v", err)
	}

	var body string
	if err := db.QueryRow(`SELECT body FROM legacy_probe WHERE id='m1'`).Scan(&body); err != nil || body != "kept" {
		t.Fatalf("legacy row lost: %v %q", err, body)
	}
	var seeded string
	if err := db.QueryRow(`SELECT type FROM events WHERE id='e-seed'`).Scan(&seeded); err != nil || seeded != "seeded" {
		t.Fatalf("pre-existing business data lost: %v %q", err, seeded)
	}
	files, err := filepath.Glob(filepath.Join(backups, "cobot-v*.sqlite"))
	if err != nil || len(files) != 1 {
		t.Fatalf("expected exactly one pre-upgrade backup, got %v (%v)", files, err)
	}
}

func readMigration(t *testing.T, name string) []byte {
	t.Helper()
	data, err := fs.ReadFile(Migrations(), "migrations/"+name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestMigrateFailureLeavesSchemaServedAndDoesNotAutoDown(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "cobot.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	base, err := fs.ReadFile(Migrations(), "migrations/00001_initial.sql")
	if err != nil {
		t.Fatal(err)
	}
	good := base
	badFS := testutil.NewMapFS(map[string]string{
		"00001_initial.sql": string(good),
		"00002_broken.sql":  "-- +goose Up\nTHIS IS NOT SQL;\n",
	})
	m, err := NewMigratorWithFS(db, badFS)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.MigrateToLatest(ctx, t.TempDir()); err == nil {
		t.Fatal("broken migration must surface an error")
	}
	// Version stayed at 1 and the applied schema keeps serving.
	v, err := m.CurrentVersion(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if v != 1 {
		t.Fatalf("version after failure = %d, want 1", v)
	}
	if _, err := GetSettings(ctx, db); err != nil {
		t.Fatalf("settings unreadable after failed migration: %v", err)
	}
}

func TestGuardRejectsNewerDatabase(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "cobot.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m, err := NewMigrator(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.MigrateToLatest(ctx, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO goose_db_version (version_id, tstamp, is_applied) VALUES (99, CURRENT_TIMESTAMP, 1)`); err != nil {
		t.Fatal(err)
	}
	if err := m.Guard(ctx); err == nil {
		t.Fatal("a newer schema version must be refused")
	}
	if err := m.MigrateToLatest(ctx, t.TempDir()); err == nil {
		t.Fatal("migrate must also refuse a newer database")
	}
}

func TestUpdateSettingsConcurrencyAndValidation(t *testing.T) {
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "cobot.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m, err := NewMigrator(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.MigrateToLatest(ctx, t.TempDir()); err != nil {
		t.Fatal(err)
	}

	lang := "en"
	saved, err := UpdateSettings(ctx, db, domain.SettingsPatch{Language: &lang}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Revision != 2 || saved.Language != "en" {
		t.Fatalf("unexpected saved settings: %+v", saved)
	}

	// Stale revision is rejected without touching the row.
	if _, err := UpdateSettings(ctx, db, domain.SettingsPatch{Language: &lang}, 1); err == nil {
		t.Fatal("stale expectedRevision must conflict")
	}
	// Invalid values are rejected.
	badLang := "fr"
	if _, err := UpdateSettings(ctx, db, domain.SettingsPatch{Language: &badLang}, 2); err == nil {
		t.Fatal("unsupported language must be rejected")
	}
	limit := 999
	if _, err := UpdateSettings(ctx, db, domain.SettingsPatch{MemberLimit: &limit}, 2); err == nil {
		t.Fatal("out-of-range member limit must be rejected")
	}

	// Events recorded inside the same transaction replay in order.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := InsertEvent(ctx, tx, "test-instance", domain.Event{Type: domain.EventSettingsUpdated, ResourceID: "settings"}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	evs, err := ListEvents(ctx, db, domain.Scope{}, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) == 0 || evs[0].Type != domain.EventSettingsUpdated {
		t.Fatalf("expected settings event, got %+v", evs)
	}
}
