package sqlite

import (
	"context"
	"errors"
	"testing"

	"github.com/kunghim/CoBot/internal/domain"
)

// Events of a team are stored while it exists; one that arrives after the team
// was deleted (a settlement finishing late) stores nothing. Events of no team
// are always stored.
func TestInsertEventSkipsADeletedTeam(t *testing.T) {
	db := openMigrated(t)
	ctx := context.Background()
	teamID, botID := seedTeam(t, db)
	insert := func(ev domain.Event) error {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if _, err := InsertEvent(ctx, tx, "test-instance", ev); err != nil {
			return err
		}
		return tx.Commit()
	}
	count := func() (n int) {
		if err := db.QueryRow(`SELECT count(*) FROM events WHERE team_id = ?`, teamID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if err := insert(domain.Event{Type: domain.EventRunUpdated, TeamID: teamID, BotID: botID, ResourceID: "run"}); err != nil || count() != 1 {
		t.Fatalf("live team: %v, %d rows", err, count())
	}
	if err := insert(domain.Event{Type: domain.EventSettingsUpdated, ResourceID: "settings"}); err != nil {
		t.Fatalf("no team: %v", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := PurgeTeamTx(ctx, tx, teamID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := insert(domain.Event{Type: domain.EventRunUpdated, TeamID: teamID, BotID: botID, ResourceID: "run"}); !errors.Is(err, ErrTeamGone) {
		t.Fatalf("deleted team: %v", err)
	}
	if n := count(); n != 0 {
		t.Fatalf("deleted team keeps %d events", n)
	}
}
