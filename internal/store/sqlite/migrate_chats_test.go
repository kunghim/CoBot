package sqlite

import (
	"context"
	"encoding/json"
	"github.com/kunghim/CoBot/internal/domain"
	"path/filepath"
	"testing"
)

func TestChatUpgradePreservesV28Team(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "v28.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	old, err := migratorBefore(t, db, "00029")
	if err != nil {
		t.Fatal(err)
	}
	backups := filepath.Join(dir, "backups")
	if err := old.MigrateToLatest(ctx, backups); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO teams(id,name,profile_instance,status,created_at,updated_at) VALUES('existing-team','Existing team','test-profile','active','2026-10-23','2026-10-23')`); err != nil {
		t.Fatal(err)
	}
	current, err := NewMigrator(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := current.MigrateToLatest(ctx, backups); err != nil {
		t.Fatal(err)
	}
	var name, status string
	if err := db.QueryRowContext(ctx, `SELECT name,status FROM teams WHERE id='existing-team'`).Scan(&name, &status); err != nil {
		t.Fatal(err)
	}
	if name != "Existing team" || status != "active" {
		t.Fatalf("existing team changed: %q %q", name, status)
	}
	var chats int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM chats`).Scan(&chats); err != nil || chats != 0 {
		t.Fatalf("chat schema not initialized empty: count=%d err=%v", chats, err)
	}
	version, err := current.CurrentVersion(ctx)
	if err != nil || version != MaxSupportedVersion {
		t.Fatalf("upgrade version=%d err=%v", version, err)
	}
	files, err := filepath.Glob(filepath.Join(backups, "*"))
	if err != nil || len(files) == 0 {
		t.Fatalf("upgrade backup missing: %v", err)
	}
}

func TestChatNavigationUpgradePreservesExistingHistoryAndArchive(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "v29.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	old, err := migratorBefore(t, db, "00030")
	if err != nil {
		t.Fatal(err)
	}
	if err = old.MigrateToLatest(ctx, filepath.Join(dir, "backups")); err != nil {
		t.Fatal(err)
	}
	data := `{"id":"existing-chat","title":"Keep me","revision":7,"workspace":"original-folder","model":"original-model"}`
	if _, err = db.ExecContext(ctx, `INSERT INTO chats(id,activity_at,data) VALUES('existing-chat','2026-10-23',?)`, data); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO teams(id,name,profile_instance,status,created_at,updated_at) VALUES('archived-team','Keep archive','test','archived','2026-10-23','2026-10-23')`); err != nil {
		t.Fatal(err)
	}
	current, err := NewMigrator(db)
	if err != nil {
		t.Fatal(err)
	}
	if err = current.MigrateToLatest(ctx, filepath.Join(dir, "backups")); err != nil {
		t.Fatal(err)
	}
	c, err := GetChat(ctx, db, "existing-chat")
	if err != nil || c.Title != "Keep me" || c.Revision != 7 || c.Workspace != "original-folder" || c.Model != "original-model" || c.Pinned || c.ArchivedAt != "" || c.ManualOrder == 0 {
		t.Fatal("chat changed", c, err)
	}
	n, err := ChatNavigation(ctx, db)
	if err != nil || n.Revision != 1 || n.Sort != "recent" || len(n.Sections) != 0 {
		t.Fatal("navigation defaults", n, err)
	}
	var status string
	if err = db.QueryRowContext(ctx, `SELECT status FROM teams WHERE id='archived-team'`).Scan(&status); err != nil || status != "archived" {
		t.Fatal("archive changed", err)
	}
	var roundtrip domain.Chat
	if err = json.Unmarshal([]byte(data), &roundtrip); err != nil || roundtrip.ID != c.ID {
		t.Fatal(err)
	}
}
