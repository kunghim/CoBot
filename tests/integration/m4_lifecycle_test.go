package integration

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// TestBotDeleteRefusesOpenWorkAndKeepsHistory walks B08: the captain and any
// member with unfinished work are refused by name, a member that finished and
// handed over its work deletes cleanly, and deletion tombstones the row
// without erasing history.
func TestBotDeleteRefusesOpenWorkAndKeepsHistory(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"backend-dev", "qa-engineer"})
	leader, owner, target := bots[0], bots[1], bots[2]
	fake := h.ensureFake(t, team.ID)

	// The captain is never directly deletable.
	_, err := h.app.DeleteBot(ctx, h.user, leader.ID, 0)
	var de *domain.Error
	if !errors.As(err, &de) || de.Code != domain.CodeConflict {
		t.Fatalf("leader delete = %v, want conflict leader_delete", err)
	}

	// An owned, unfinished task blocks the delete with named counts.
	if _, err := h.app.CreateTask(ctx, h.user, false, app.CreateTaskInput{
		TeamID: team.ID, OwnerID: owner.ID, Title: "在办任务", AcceptanceCriteria: "不能留下无主任务",
	}); err != nil {
		t.Fatalf("create task: %v", err)
	}
	_, err = h.app.DeleteBot(ctx, h.user, owner.ID, 0)
	if !errors.As(err, &de) || de.Code != domain.CodeConflict {
		t.Fatalf("delete with open task = %v, want conflict open_work", err)
	}

	// Finish the assignment turn so the owner has no live run.
	waitUntil(t, 5*time.Second, "assignment turn", func() bool { return len(fake.Turns()) == 1 })
	fake.EmitTurnCompleted(fake.Turns()[0].ThreadID, fake.Turns()[0].TurnID, "completed", "收到")
	waitUntil(t, 5*time.Second, "assignment run ended", func() bool {
		_, err := sqlite.FindActiveRun(ctx, h.app.DB, owner.ID)
		return errors.Is(err, sqlite.ErrNotFound)
	})
	_, err = h.app.DeleteBot(ctx, h.user, owner.ID, 0)
	if !errors.As(err, &de) || de.Code != domain.CodeConflict {
		t.Fatalf("delete before hand-over = %v, want conflict open_work (task still owned)", err)
	}

	// Hand the task over: the owner's work is finished, the member may go.
	transfer, err := h.app.OfferTransfer(ctx, h.user, taskIDOf(t, h, owner.ID), target.ID, "交给测试")
	if err != nil {
		t.Fatalf("offer: %v", err)
	}
	if _, err := h.app.AcceptTransfer(ctx, h.user, transfer.ID); err != nil {
		t.Fatalf("accept: %v", err)
	}
	deleted, err := h.app.DeleteBot(ctx, h.user, owner.ID, 0)
	if err != nil {
		t.Fatalf("delete work-free member: %v", err)
	}
	if deleted.ID != owner.ID {
		t.Fatalf("deleted = %s, want %s", deleted.ID, owner.ID)
	}

	// The projection drops the member; the history rows remain.
	members, err := sqlite.ListBots(ctx, h.app.DB, team.ID)
	if err != nil || len(members) != 2 {
		t.Fatalf("members after delete = %d (err %v), want leader + target", len(members), err)
	}
	var history int
	if err := h.app.DB.QueryRow(`SELECT COUNT(*) FROM messages WHERE bot_id = ?`, owner.ID).Scan(&history); err != nil {
		t.Fatalf("history count: %v", err)
	}
	if history == 0 {
		t.Fatal("deleting a member must keep its message history")
	}

	// A live (unknown) execution also blocks deletion: it is unfinished work
	// that must be resolved, not deleted around.
	waitUntil(t, 5*time.Second, "target assignment turn", func() bool { return len(fake.Turns()) >= 2 })
	fake.EmitTurnCompleted(fake.Turns()[1].ThreadID, fake.Turns()[1].TurnID, "completed", "收到")
	waitUntil(t, 5*time.Second, "target run ended", func() bool {
		_, err := sqlite.FindActiveRun(ctx, h.app.DB, target.ID)
		return errors.Is(err, sqlite.ErrNotFound)
	})
	fake.StartTurnErr = errors.New("engine refuses to start")
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: target.ID, Text: "触发未知执行"}); err != nil {
		t.Fatalf("msg: %v", err)
	}
	waitUntil(t, 5*time.Second, "unknown run", func() bool {
		run, err := sqlite.FindActiveRun(ctx, h.app.DB, target.ID)
		return err == nil && run.Status == domain.RunUnknown
	})
	_, err = h.app.DeleteBot(ctx, h.user, target.ID, 0)
	if !errors.As(err, &de) || de.Code != domain.CodeConflict {
		t.Fatalf("delete with unknown run = %v, want conflict open_work", err)
	}
	fake.StartTurnErr = nil
}

// taskIDOf finds the single task owned by a bot, for the hand-over above.
func taskIDOf(t *testing.T, h *m2Harness, ownerID string) string {
	t.Helper()
	var id string
	if err := h.app.DB.QueryRow(`SELECT id FROM tasks WHERE owner_id = ? ORDER BY created_at DESC LIMIT 1`, ownerID).Scan(&id); err != nil {
		t.Fatalf("find task: %v", err)
	}
	return id
}

// TestTeamArchiveRestoreDeleteGateOnWork walks B09: pending approvals and
// running executions block an archive, an archived team freezes dispatch
// while keeping queued work, restore resumes exactly that work, open tasks
// block deletion, and a clean archived team soft-deletes with bindings
// removed but user files kept.
func TestTeamArchiveRestoreDeleteGateOnWork(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"backend-dev", "qa-engineer"})
	captain, owner := bots[0], bots[1]
	fake := h.ensureFake(t, team.ID)

	// A pending approval blocks the archive.
	approval, err := h.app.RequestMember(ctx, auth.NewPrincipalBot(captain.ID, team.ID), "qa-engineer", "需要测试人手")
	if err != nil {
		t.Fatalf("request member: %v", err)
	}
	if _, err := h.app.ArchiveTeam(ctx, h.user, team.ID, 0); err == nil {
		t.Fatal("archive with a pending approval must be refused")
	}
	if _, err := h.app.DecideApproval(ctx, h.user, approval.ID, "reject", "", approval.Revision); err != nil {
		t.Fatalf("reject: %v", err)
	}

	// An unfinished task may exist (it survives the archive) but must block
	// deletion later; its assignment turn must be over before archiving.
	if _, err := h.app.CreateTask(ctx, h.user, false, app.CreateTaskInput{
		TeamID: team.ID, OwnerID: owner.ID, Title: "归档前任务", AcceptanceCriteria: "归档保留任务",
	}); err != nil {
		t.Fatalf("create task: %v", err)
	}
	waitUntil(t, 5*time.Second, "owner assignment turn", func() bool { return len(fake.Turns()) >= 1 })
	fake.EmitTurnCompleted(fake.Turns()[0].ThreadID, fake.Turns()[0].TurnID, "completed", "收到")
	waitUntil(t, 5*time.Second, "owner run ended", func() bool {
		_, err := sqlite.FindActiveRun(ctx, h.app.DB, owner.ID)
		return errors.Is(err, sqlite.ErrNotFound)
	})

	// A running execution blocks the archive; after it ends, the archive
	// succeeds — the open task does not block it.
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: captain.ID, Text: "正在忙"}); err != nil {
		t.Fatalf("busy msg: %v", err)
	}
	waitUntil(t, 5*time.Second, "captain turn", func() bool { return len(fake.Turns()) >= 2 })
	if _, err := h.app.ArchiveTeam(ctx, h.user, team.ID, 0); err == nil {
		t.Fatal("archive while a run is active must be refused")
	}
	fake.EmitTurnCompleted(fake.Turns()[1].ThreadID, fake.Turns()[1].TurnID, "completed", "好了")
	waitUntil(t, 5*time.Second, "captain run ended", func() bool {
		_, err := sqlite.FindActiveRun(ctx, h.app.DB, captain.ID)
		return errors.Is(err, sqlite.ErrNotFound)
	})
	// The two finished turns reported no usage, so enabling a budget holds
	// new turns as unknown spend — a reliable way to park a delivery in the
	// queue without racing the dispatcher.
	settings, _ := h.app.GetSettings(ctx)
	scope := domain.BudgetScopeTeam
	tiny := int64(1)
	if _, err := h.app.UpdateSettings(ctx, h.user, domain.SettingsPatch{BudgetScope: &scope, BudgetTokens: &tiny}, settings.Revision); err != nil {
		t.Fatalf("enable budget: %v", err)
	}
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: captain.ID, Text: "被预算挡住"}); err != nil {
		t.Fatalf("held msg: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	if h.queuedForBot(t, captain.ID) == 0 {
		t.Fatal("expected the budget to hold a queued delivery")
	}

	archived, err := h.app.ArchiveTeam(ctx, h.user, team.ID, 0)
	if err != nil || archived.Status != domain.TeamStatusArchived {
		t.Fatalf("archive = %+v (err %v), want archived", archived, err)
	}

	// Archived: new messages are refused, the queued delivery stays put.
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: captain.ID, Text: "归档后不该派发"}); err == nil {
		t.Fatal("an archived team must refuse new messages")
	}
	if h.queuedForBot(t, captain.ID) == 0 {
		t.Fatal("the archived team must keep its queued delivery")
	}

	// Deletion requires the archive step and a work-free team.
	if _, err := h.app.DeleteTeam(ctx, h.user, team.ID, 0); err == nil {
		t.Fatal("deleting a team with an open task must be refused")
	}

	// Restore resumes exactly the held work once the budget allows it.
	settings, _ = h.app.GetSettings(ctx)
	generous := int64(1_000_000)
	if _, err := h.app.UpdateSettings(ctx, h.user, domain.SettingsPatch{BudgetTokens: &generous}, settings.Revision); err != nil {
		t.Fatalf("raise budget: %v", err)
	}
	// Usage from the finished turns never arrived (fake engine), so clear the
	// unknown pause by recording snapshots for those turns before restore.
	for _, turn := range fake.Turns() {
		fake.EmitTokenUsage(turn.ThreadID, turn.TurnID, 5, 1, 0)
	}
	restored, err := h.app.RestoreTeam(ctx, h.user, team.ID, 0)
	if err != nil || restored.Status != domain.TeamStatusActive {
		t.Fatalf("restore = %+v (err %v), want active", restored, err)
	}
	turnsBefore := len(fake.Turns())
	waitUntil(t, 5*time.Second, "held delivery dispatched after restore", func() bool {
		return len(fake.Turns()) > turnsBefore
	})
}

// TestTeamDeleteSoftRemovesAndKeepsUserFiles: a clean archived team deletes
// (B09) — projections drop it, bindings go away, application rows stay for
// audit, and the on-disk marker is never touched.
func TestTeamDeleteSoftRemovesAndKeepsUserFiles(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()

	// Deleting a live team is refused: archive first.
	team, _ := h.createTeam(t, []string{"backend-dev"})
	if _, err := h.app.DeleteTeam(ctx, h.user, team.ID, 0); err == nil {
		t.Fatal("deleting an active team must be refused")
	}
	if _, err := h.app.ArchiveTeam(ctx, h.user, team.ID, 0); err != nil {
		t.Fatalf("archive: %v", err)
	}
	if _, err := h.app.DeleteTeam(ctx, h.user, team.ID, 0); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Projections drop the team and its members.
	visible, err := h.app.ListTeams(ctx, h.user)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, item := range visible {
		if item.Team.ID == team.ID {
			t.Fatal("deleted team must not appear in the list")
		}
	}
	members, err := sqlite.ListBots(ctx, h.app.DB, team.ID)
	if err != nil || len(members) != 0 {
		t.Fatalf("members after delete = %d (err %v), want 0", len(members), err)
	}
	// The directory binding is gone...
	bindings, err := sqlite.ListDirectoryBindings(ctx, h.app.DB)
	if err != nil {
		t.Fatalf("bindings: %v", err)
	}
	for _, b := range bindings {
		if b.TeamID == team.ID {
			t.Fatal("deleted team must release its directory binding")
		}
	}
	// ...but the user's marker file on disk is untouched.
	markerPath := filepath.Join(h.teamDir, ".cobot", "team.json")
	if _, err := os.Stat(markerPath); err != nil {
		t.Fatalf("marker must survive team deletion: %v", err)
	}
}

// TestMarkerAdoptIsExplicit pins B04: a directory that already carries a
// team marker — copied, moved, or from an unknown instance — is refused
// until the user explicitly adopts it, and adopting rewrites the marker for
// the new owner instance.
func TestMarkerAdoptIsExplicit(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()

	foreignDir := filepath.Join(filepath.Dir(h.teamDir), "marker-adopt-dir")
	if err := os.MkdirAll(filepath.Join(foreignDir, ".cobot"), 0o755); err != nil {
		t.Fatal(err)
	}
	marker := []byte(`{"instanceId":"foreign-instance","teamId":"copied-team"}`)
	if err := os.WriteFile(filepath.Join(foreignDir, ".cobot", "team.json"), marker, 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, err := h.app.CreateTeam(ctx, h.user, app.CreateTeamInput{
		Name: "接管测试", Directory: foreignDir, InitialRoleIDs: []string{"backend-dev"},
	})
	var de *domain.Error
	if !errors.As(err, &de) || de.Code != domain.CodeInvalidRequest {
		t.Fatalf("creating over a foreign marker = %v, want invalid marker_adopt_required", err)
	}
	if de.Params["instanceId"] != "foreign-instance" {
		t.Fatalf("refusal params = %+v, want the marker's instance id", de.Params)
	}

	// The explicit choice adopts the directory and rewrites the claim.
	team, _, err := h.app.CreateTeam(ctx, h.user, app.CreateTeamInput{
		Name: "接管测试", Directory: foreignDir, InitialRoleIDs: []string{"backend-dev"},
		AdoptMarker: true,
	})
	if err != nil {
		t.Fatalf("adopt create: %v", err)
	}
	if team.Status == domain.TeamStatusDeleted {
		t.Fatal("adopted team must be live")
	}
	data, err := os.ReadFile(filepath.Join(foreignDir, ".cobot", "team.json"))
	if err != nil {
		t.Fatalf("read adopted marker: %v", err)
	}
	var written app.TeamMarker
	if err := json.Unmarshal(data, &written); err != nil {
		t.Fatalf("parse marker: %v", err)
	}
	if written.InstanceID != h.app.InstanceID {
		t.Fatalf("adopted marker instance = %q, want this profile %q", written.InstanceID, h.app.InstanceID)
	}
}
