package sqlite

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/testutil"
)

func TestWorkspaceIndexesUpgradeV33WithoutChangingHistoricalSteps(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join("..", "..", "..", ".tmp", "test-runs")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(root, "workspace-changes-upgrade-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "v33.sqlite")
	db, err := Open(path)
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
		if name < "00034" {
			data, err := fs.ReadFile(migrationsFS, path)
			if err != nil {
				t.Fatal(err)
			}
			oldSource[name] = string(data)
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
	chat := domain.Chat{ID: "old-chat", Title: "kept chat", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := SaveChat(ctx, db, chat, true); err != nil {
		t.Fatal(err)
	}
	if err := SaveChatRun(ctx, db, domain.ChatRun{ID: "old-run", ChatID: chat.ID, Status: "completed", StartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	step := domain.ChatStep{ID: "old-step", Kind: "file_change", Status: "completed", Detail: []byte(`{"changes":[{"path":"old.txt","kind":"update","diff":"+saved\n"}]}`)}
	if err := SaveChatStep(ctx, db, "old-run", step); err != nil {
		t.Fatal(err)
	}
	var before string
	if err := db.QueryRowContext(ctx, `SELECT data FROM chat_run_steps WHERE run_id='old-run'`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	current, err := NewMigrator(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := current.MigrateToLatest(ctx, backups); err != nil {
		t.Fatal(err)
	}
	version, err := current.CurrentVersion(ctx)
	if err != nil || version != MaxSupportedVersion {
		t.Fatalf("version=%d err=%v", version, err)
	}
	var after string
	if err := db.QueryRowContext(ctx, `SELECT data FROM chat_run_steps WHERE run_id='old-run'`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("historical step was rewritten")
	}
	if saved, err := GetChat(ctx, db, chat.ID); err != nil || saved.Title != chat.Title {
		t.Fatalf("chat=%+v err=%v", saved, err)
	}
	snapshots, err := filepath.Glob(filepath.Join(backups, "cobot-v33-*.sqlite"))
	if err != nil || len(snapshots) != 1 {
		t.Fatalf("backups=%v err=%v", snapshots, err)
	}
}
