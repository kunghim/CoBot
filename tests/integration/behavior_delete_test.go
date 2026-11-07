package integration

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// Deleting is one list of statements in the store, written by hand. These tests
// read the columns from the database itself, so a table added later cannot be
// forgotten quietly.

// ownerColumns lists, per table, the columns that name a team or a member.
func ownerColumns(t *testing.T, db *sql.DB) map[string][]string {
	t.Helper()
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' AND name != 'goose_db_version'`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, name)
	}
	rows.Close()
	out := map[string][]string{}
	for _, table := range tables {
		columns, err := db.Query(`SELECT name FROM pragma_table_info(?)`, table)
		if err != nil {
			t.Fatal(err)
		}
		for columns.Next() {
			var name string
			if err := columns.Scan(&name); err != nil {
				t.Fatal(err)
			}
			if name == "team_id" || name == "owner_id" || strings.HasSuffix(name, "bot_id") {
				out[table] = append(out[table], name)
			}
		}
		columns.Close()
	}
	return out
}

func rowsNaming(t *testing.T, db *sql.DB, table, column, id string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM `+table+` WHERE `+column+` = ?`, id).Scan(&n); err != nil {
		t.Fatalf("%s.%s: %v", table, column, err)
	}
	return n
}

// fillTeam leaves rows in as many tables as a team's life touches: a finished
// card with evidence, an open requirement, a question, an approval, a chain,
// usage and a memory.
func fillTeam(t *testing.T, h *m2Harness, team domain.Team, pm, dev domain.Bot) {
	t.Helper()
	ctx := context.Background()
	fake := h.ensureFake(t, team.ID)
	f := reviewFlow{t: t, h: h, fake: fake}

	card, _ := h.handCard(t, team, pm, dev, "做页面", "能打开")
	turn := f.waitTurn(dev, "做页面")
	fake.EmitTokenUsage(turn.ThreadID, turn.TurnID, 10, 5, 1)
	fake.EmitApproval("7", turn.ThreadID, turn.TurnID, "go test")
	waitUntil(t, 5*time.Second, "approval card", func() bool {
		var n int
		_ = h.app.DB.QueryRow(`SELECT count(*) FROM approvals WHERE team_id = ?`, team.ID).Scan(&n)
		return n > 0
	})
	f.call(dev, "handoff", map[string]any{"cardId": card.ID, "outcome": "done", "summary": "做好了", "idempotencyKey": "fill-handoff"})
	if _, err := h.app.AskUser(ctx, auth.NewPrincipalBot(dev.ID, team.ID), app.AskUserInput{Question: "还要什么？", Options: []string{"没了"}}); err != nil {
		t.Fatal(err)
	}
	f.finish(dev, turn, "好了")

	tx, err := h.app.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := sqlite.InsertMemory(ctx, tx, domain.Memory{ID: domain.NewID(), TeamID: team.ID, BotID: dev.ID, Body: "记住这个", NormKey: "记住这个", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func assertFilled(t *testing.T, db *sql.DB, teamID string, tables ...string) {
	t.Helper()
	for _, table := range tables {
		if n := rowsNaming(t, db, table, "team_id", teamID); n == 0 {
			t.Fatalf("the test left no row in %s, so it proves nothing about it", table)
		}
	}
}

func TestDeletingATeamLeavesNoRowNamingIt(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"fullstack-engineer"})
	pm, dev := bots[0], bots[1]
	fillTeam(t, h, team, pm, dev)
	other, otherBots := h.createTeamWithDir(t, "另一个团队", "other-team", nil)

	db := h.app.DB
	assertFilled(t, db, team.ID, "bots", "messages", "deliveries", "runs", "tasks", "requirements", "questions", "approvals", "chains", "usage_ledger", "memories", "events", "directory_bindings")

	// A team with open work cannot be archived; one whose folder is gone can
	// be deleted as it stands, which is the case with the most left to clean.
	if err := os.Rename(h.teamDir, h.teamDir+"-away"); err != nil {
		t.Fatal(err)
	}
	h.app.CheckTeamDirectories(ctx)
	if _, err := h.app.DeleteTeam(ctx, h.user, team.ID, 0); err != nil {
		t.Fatalf("delete: %v", err)
	}

	for table, columns := range ownerColumns(t, db) {
		for _, column := range columns {
			for _, id := range []string{team.ID, pm.ID, dev.ID} {
				if n := rowsNaming(t, db, table, column, id); n != 0 {
					t.Errorf("%s.%s keeps %d rows of the deleted team", table, column, n)
				}
			}
		}
	}
	// Tables that reach the team only through another row must not be left
	// pointing at nothing.
	rows, err := db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var table, parent string
		var rowid, fk sql.NullInt64
		if err := rows.Scan(&table, &rowid, &parent, &fk); err != nil {
			t.Fatal(err)
		}
		t.Errorf("%s has a row pointing at a missing %s", table, parent)
	}
	if n := rowsNaming(t, db, "bots", "team_id", other.ID); n != len(otherBots) {
		t.Errorf("the other team has %d members left, want %d", n, len(otherBots))
	}
}

// Deleting a member removes what is its own, but what it told others stays in
// their history, and the ledger stays because team budgets count from it.
func TestDeletingAMemberKeepsOthersHistoryAndTheLedger(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"fullstack-engineer", "qa-engineer"})
	pm, dev, qa := bots[0], bots[1], bots[2]
	fake := h.ensureFake(t, team.ID)

	turn := startTurn(t, h, fake, dev, "做点事")
	fake.EmitTokenUsage(turn.ThreadID, turn.TurnID, 10, 5, 1)
	waitUntil(t, 5*time.Second, "usage recorded", func() bool { return rowsNaming(t, h.app.DB, "usage_ledger", "bot_id", dev.ID) > 0 })
	f := reviewFlow{t: t, h: h, fake: fake}
	f.call(dev, "send_message", map[string]any{"targetBotId": qa.ID, "text": "测试注意这里", "idempotencyKey": "keep-note"})
	f.finish(dev, turn, "好")
	waitUntil(t, 5*time.Second, "member idle", func() bool { return botStatus(t, h, dev.ID) == domain.BotStatusIdle })
	if n := rowsNaming(t, h.app.DB, "messages", "source_bot_id", dev.ID); n == 0 {
		t.Fatal("the note was not recorded")
	}

	if _, err := h.app.DeleteBot(ctx, h.user, dev.ID, 0); err != nil {
		t.Fatalf("delete member: %v", err)
	}

	db := h.app.DB
	for _, table := range []string{"bots"} {
		if n := rowsNaming(t, db, table, "id", dev.ID); n != 0 {
			t.Errorf("%s keeps the member", table)
		}
	}
	for table, column := range map[string]string{"messages": "bot_id", "runs": "bot_id", "run_steps": "bot_id", "provider_sessions": "bot_id", "deliveries": "target_bot_id"} {
		if n := rowsNaming(t, db, table, column, dev.ID); n != 0 {
			t.Errorf("%s keeps %d rows of the deleted member", table, n)
		}
	}
	if n := rowsNaming(t, db, "usage_ledger", "bot_id", dev.ID); n == 0 {
		t.Error("the usage ledger must stay: team budgets count from it")
	}
	if n := rowsNaming(t, db, "messages", "source_bot_id", dev.ID); n == 0 {
		t.Error("what the member said to a colleague must stay in the colleague's history")
	}
	if n := rowsNaming(t, db, "bots", "id", pm.ID); n != 1 {
		t.Error("the project manager is gone")
	}
	if deleted := fake.DeletedThreads(); len(deleted) != 1 || deleted[0] != turn.ThreadID {
		t.Errorf("deleted threads = %v, want the member's own %s", deleted, turn.ThreadID)
	}
}
