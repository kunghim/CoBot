package sqlite

import (
	"context"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/testutil"
)

func TestDeliveryContextUpgradePreservesV7WorkAndBacksUp(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "v7.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	files, err := fs.Glob(migrationsFS, "migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	oldSource := map[string]string{}
	for _, path := range files {
		name := strings.TrimPrefix(path, "migrations/")
		if name < "00008" {
			raw, err := fs.ReadFile(migrationsFS, path)
			if err != nil {
				t.Fatal(err)
			}
			oldSource[name] = string(raw)
		}
	}
	old, err := NewMigratorWithFS(db, testutil.NewMapFS(oldSource))
	if err != nil {
		t.Fatal(err)
	}
	backups := filepath.Join(dir, "backups")
	if err := old.MigrateToLatest(ctx, backups); err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	team := domain.Team{ID: "team", Name: "kept team", LeaderID: "bot", ProfileInstance: "instance"}
	bot := domain.Bot{ID: "bot", TeamID: team.ID, Name: "kept bot", RoleID: "leader", Status: domain.BotStatusIdle, IsLeader: true}
	if err := InsertTeamStaged(ctx, tx, team, dir, dir, []domain.Bot{bot}); err != nil {
		t.Fatal(err)
	}
	task := domain.Task{ID: "task", TeamID: team.ID, OwnerID: bot.ID, Title: "kept task", Status: domain.TaskTodo, Revision: 1}
	if err := InsertTask(ctx, tx, task); err != nil {
		t.Fatal(err)
	}
	for _, m := range []domain.Message{
		{ID: "assignment", TeamID: team.ID, BotID: bot.ID, Kind: domain.MessageKindSystem, RunID: task.ID, Blocks: []byte(`[]`)},
		{ID: "answer", TeamID: team.ID, BotID: bot.ID, Kind: domain.MessageKindUser, RunID: "question", Blocks: []byte(`[]`)},
		{ID: "ordinary", TeamID: team.ID, BotID: bot.ID, Kind: domain.MessageKindUser, RunID: task.ID, Blocks: []byte(`[]`)},
	} {
		if err := InsertMessage(ctx, tx, m); err != nil {
			t.Fatal(err)
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		if _, err := tx.ExecContext(ctx, `INSERT INTO deliveries(id,message_id,team_id,target_bot_id,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, "d-"+m.ID, m.ID, team.ID, bot.ID, domain.DeliveryQueued, now, now); err != nil {
			t.Fatal(err)
		}
	}
	if err := InsertQuestion(ctx, tx, domain.Question{ID: "question", TeamID: team.ID, RequesterBotID: bot.ID, TaskID: task.ID, Question: "kept question", Status: domain.QuestionAnswered, Revision: 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE questions SET answer_message_id='answer' WHERE id='question'`); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	current, err := NewMigrator(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := current.MigrateToLatest(ctx, backups); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string][]string{"d-assignment": {"task", "task_assignment"}, "d-answer": {"task", "question_answer"}, "d-ordinary": {"", "message"}} {
		delivery, err := GetDelivery(ctx, db, id)
		if err != nil || delivery.TaskID != want[0] || delivery.Kind != want[1] {
			t.Fatalf("backfill %s: %+v / %v", id, delivery, err)
		}
	}
	snapshots, err := filepath.Glob(filepath.Join(backups, "cobot-v7-*.sqlite"))
	if err != nil || len(snapshots) != 1 {
		t.Fatal("missing consistent v7 backup", err)
	}
	backup, err := Open(snapshots[0])
	if err != nil {
		t.Fatal(err)
	}
	defer backup.Close()
	var count int
	if err := backup.QueryRow(`SELECT count(*) FROM messages`).Scan(&count); err != nil || count != 3 {
		t.Fatal("backup lost pre-upgrade business facts", err)
	}
}

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

// TestUpgradePurgesSoftDeletedTeams: migration 11 removes teams that earlier
// builds soft-deleted, with every row of theirs, and keeps live teams intact.
func TestUpgradePurgesSoftDeletedTeams(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "v10.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	files, err := fs.Glob(migrationsFS, "migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	oldSource := map[string]string{}
	for _, path := range files {
		name := strings.TrimPrefix(path, "migrations/")
		if name < "00011" {
			raw, err := fs.ReadFile(migrationsFS, path)
			if err != nil {
				t.Fatal(err)
			}
			oldSource[name] = string(raw)
		}
	}
	old, err := NewMigratorWithFS(db, testutil.NewMapFS(oldSource))
	if err != nil {
		t.Fatal(err)
	}
	backups := filepath.Join(dir, "backups")
	if err := old.MigrateToLatest(ctx, backups); err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for _, id := range []string{"gone", "kept"} {
		team := domain.Team{ID: id, Name: id, LeaderID: id + "-bot", ProfileInstance: "instance"}
		bot := domain.Bot{ID: id + "-bot", TeamID: id, Name: id, RoleID: "leader", Status: domain.BotStatusIdle, IsLeader: true}
		folder := filepath.Join(dir, id)
		if err := InsertTeamStaged(ctx, tx, team, folder, folder, []domain.Bot{bot}); err != nil {
			t.Fatal(err)
		}
		task := domain.Task{ID: id + "-task", TeamID: id, OwnerID: bot.ID, Title: id, Status: domain.TaskTodo, Revision: 1}
		if err := InsertTask(ctx, tx, task); err != nil {
			t.Fatal(err)
		}
		message := domain.Message{ID: id + "-msg", TeamID: id, BotID: bot.ID, Kind: domain.MessageKindUser, Blocks: []byte(`[]`)}
		if err := InsertMessage(ctx, tx, message); err != nil {
			t.Fatal(err)
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		if _, err := tx.ExecContext(ctx, `INSERT INTO deliveries(id,message_id,team_id,target_bot_id,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, id+"-d", message.ID, id, bot.ID, domain.DeliveryCancelled, now, now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE teams SET status = 'deleted' WHERE id = 'gone'`); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	current, err := NewMigrator(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := current.MigrateToLatest(ctx, backups); err != nil {
		t.Fatal(err)
	}
	count := func(query, id string) int {
		t.Helper()
		var n int
		if err := db.QueryRow(query, id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	for _, query := range []string{
		`SELECT COUNT(*) FROM teams WHERE id = ?`,
		`SELECT COUNT(*) FROM bots WHERE team_id = ?`,
		`SELECT COUNT(*) FROM tasks WHERE team_id = ?`,
		`SELECT COUNT(*) FROM messages WHERE team_id = ?`,
		`SELECT COUNT(*) FROM deliveries WHERE team_id = ?`,
		`SELECT COUNT(*) FROM directory_bindings WHERE team_id = ?`,
	} {
		if n := count(query, "gone"); n != 0 {
			t.Fatalf("%s left %d rows of the soft-deleted team", query, n)
		}
		if n := count(query, "kept"); n != 1 {
			t.Fatalf("%s kept %d rows of the live team, want 1", query, n)
		}
	}
}
