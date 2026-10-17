package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/kunghim/CoBot/internal/domain"
)

// openMigrated returns a migrated database for store tests.
func openMigrated(t *testing.T) *sql.DB {
	t.Helper()
	ctx := context.Background()
	db, err := Open(filepath.Join(t.TempDir(), "cobot.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	m, err := NewMigrator(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.MigrateToLatest(ctx, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	return db
}

// seedTeam inserts a team with one owner so tasks can reference real rows.
func seedTeam(t *testing.T, db *sql.DB) (teamID, botID string) {
	t.Helper()
	ctx := context.Background()
	teamID, botID = domain.NewID(), domain.NewID()
	team := domain.Team{ID: teamID, Name: "store-test", ProfileInstance: "test", LeaderID: botID, Status: domain.TeamStatusActive}
	bot := domain.Bot{ID: botID, TeamID: teamID, Name: "owner", RoleID: "backend-dev", IsLeader: true}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := InsertTeam(ctx, tx, team, filepath.Join(t.TempDir(), "dir"), "key-"+teamID, []domain.Bot{bot}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return teamID, botID
}

// seedTask inserts one task owned by the seeded bot.
func seedTask(t *testing.T, db *sql.DB, teamID, botID, title string) string {
	t.Helper()
	ctx := context.Background()
	id := domain.NewID()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	task := domain.Task{
		ID: id, TeamID: teamID, OwnerID: botID, Title: title,
		Status: domain.TaskTodo, Revision: 1, CreatedBy: domain.TaskAuthorUser,
	}
	if err := InsertTask(ctx, tx, task); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return id
}

// addEdge records one dependency edge in its own transaction.
func addEdge(t *testing.T, db *sql.DB, taskID, dependsOn string) {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := AddDependency(context.Background(), tx, taskID, dependsOn); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

// TestHasCycleFromDetectsBackEdge pins the direction the product relies on:
// once A depends on B, offering B a dependency on A must be seen as a cycle.
func TestHasCycleFromDetectsBackEdge(t *testing.T) {
	ctx := context.Background()
	db := openMigrated(t)
	teamID, botID := seedTeam(t, db)
	a := seedTask(t, db, teamID, botID, "A")
	b := seedTask(t, db, teamID, botID, "B")

	addEdge(t, db, a, b)
	got, err := HasCycleFrom(ctx, db, b, a)
	if err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Fatal("B -> A must be a cycle once A -> B exists")
	}

	// A fresh pair is not a cycle, so ordinary dependencies still work.
	c := seedTask(t, db, teamID, botID, "C")
	addEdge(t, db, c, a)
	d := seedTask(t, db, teamID, botID, "D")
	got, err = HasCycleFrom(ctx, db, d, c)
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Fatal("D -> C must not be a cycle")
	}
}

// TestHasCycleFromIndirect covers the longer path: A -> B -> C, so C -> A
// closes a three-node loop.
func TestHasCycleFromIndirect(t *testing.T) {
	ctx := context.Background()
	db := openMigrated(t)
	teamID, botID := seedTeam(t, db)
	a := seedTask(t, db, teamID, botID, "A")
	b := seedTask(t, db, teamID, botID, "B")
	c := seedTask(t, db, teamID, botID, "C")

	addEdge(t, db, a, b)
	addEdge(t, db, b, c)
	got, err := HasCycleFrom(ctx, db, c, a)
	if err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Fatal("C -> A must close the A -> B -> C loop")
	}
}
