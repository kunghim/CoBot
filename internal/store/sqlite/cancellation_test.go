package sqlite

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Cancellation must release native statements as well as the sql.DB pool
// slot. A leaked statement poisons later reads/checkpoints and, on Windows,
// keeps the database file locked even after DB.Close.
func TestCancelledReadsReleaseDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cancel.sqlite")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for range 300 {
		ctx, cancel := context.WithCancel(context.Background())
		finished := make(chan struct{})
		go func() { cancel(); close(finished) }()
		var n int
		_ = db.QueryRowContext(ctx, "SELECT 1").Scan(&n)
		<-finished
		if err := db.QueryRow("SELECT 1").Scan(&n); err != nil {
			t.Fatal("cancelled read poisoned connection", err)
		}
	}
	var busy, frames, checkpointed int
	if err := db.QueryRow("PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &frames, &checkpointed); err != nil || busy != 0 {
		t.Fatalf("checkpoint: %d / %v", busy, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, path+".closed"); err != nil {
		t.Fatal("database handle leaked after close", err)
	}
}
