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
	bot := domain.Bot{ID: botID, TeamID: teamID, Name: "owner", RoleID: "fullstack-engineer", IsLeader: true}
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

// A requirement keeps its route, and its cards and handoff records keep the
// stage they belong to and what the engine reported was run.
func TestRequirementCardsAndHandoffsRoundTrip(t *testing.T) {
	ctx := context.Background()
	db := openMigrated(t)
	teamID, botID := seedTeam(t, db)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	req := domain.Requirement{
		ID: domain.NewID(), TeamID: teamID, Title: "待办页面", Document: "能添加和删除",
		Stages: []domain.Stage{
			{Name: "确认", Kind: domain.StageConfirm},
			{Name: "实现", Kind: domain.StageImplement, OwnerID: botID, Criteria: "能打开"},
			{Name: "评审", Kind: domain.StageReview, OwnerID: botID, ReviewType: "code"},
			{Name: "验收", Kind: domain.StageAccept, OwnerID: botID},
		},
		Status: domain.RequirementActive, CreatedBy: botID, Revision: 1,
	}
	if err := InsertRequirement(ctx, tx, req); err != nil {
		t.Fatal(err)
	}
	card := domain.Task{
		ID: domain.NewID(), TeamID: teamID, OwnerID: botID, Title: "实现", Status: domain.TaskTodo,
		RequirementID: req.ID, StageIndex: 1, StageKind: domain.StageImplement, ReturnTo: 2, Revision: 1, CreatedBy: botID,
	}
	if err := InsertTask(ctx, tx, card); err != nil {
		t.Fatal(err)
	}
	exit := 0
	if err := InsertEvidence(ctx, tx, domain.Evidence{
		ID: domain.NewID(), TaskID: card.ID, AuthorID: botID, Summary: "做好了", ArtifactRefs: []string{"index.html"},
		Outcome: domain.HandoffBack, Mode: domain.ReworkRerun,
		Execution: &domain.Execution{Commands: []domain.ExecutedCommand{{Command: "npm test", ExitCode: &exit}}, Files: []string{"index.html"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	got, err := GetRequirement(ctx, db, req.ID)
	if err != nil || len(got.Stages) != 4 || got.Stages[2].ReviewType != "code" || got.Status != domain.RequirementActive {
		t.Fatalf("requirement = %+v, %v", got, err)
	}
	cards, err := ListRequirementTasks(ctx, db, req.ID)
	if err != nil || len(cards) != 1 || cards[0].StageIndex != 1 || cards[0].ReturnTo != 2 || cards[0].StageKind != domain.StageImplement {
		t.Fatalf("cards = %+v, %v", cards, err)
	}
	records, err := ListEvidence(ctx, db, card.ID)
	if err != nil || len(records) != 1 || records[0].Execution == nil || *records[0].Execution.Commands[0].ExitCode != 0 {
		t.Fatalf("records = %+v, %v", records, err)
	}
	backs, err := CountSendBacks(ctx, db, req.ID, 1)
	if err != nil || backs != 1 {
		t.Fatalf("send-backs = %d, %v", backs, err)
	}
}
