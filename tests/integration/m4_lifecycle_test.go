package integration

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// TestBotDeleteRefusesOpenWorkAndPurges: the captain and any
// member with unfinished work are refused by name; a member that finished and
// handed over its work is removed completely — its conversation, runs and
// engine session — while the task it handed over stays with its new owner
// and its role stays available to every team.
func TestBotDeleteRefusesOpenWorkAndPurges(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"fullstack-engineer", "qa-engineer"})
	leader, owner, target := bots[0], bots[1], bots[2]
	fake := h.ensureFake(t, team.ID)

	// The captain is never directly deletable.
	_, err := h.app.DeleteBot(ctx, h.user, leader.ID, 0)
	var de *domain.Error
	if !errors.As(err, &de) || de.Code != domain.CodeConflict {
		t.Fatalf("leader delete = %v, want conflict leader_delete", err)
	}

	// A card the member holds blocks the delete with named counts.
	_, req := h.handCard(t, team, leader, owner, "在办卡片", "不能留下无主卡片")
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
		t.Fatalf("delete before the card ended = %v, want conflict open_work (card still held)", err)
	}

	// Cancelling the requirement ends the card: the member may go.
	if _, err := h.app.CancelRequirement(ctx, h.user, req.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	deleted, err := h.app.DeleteBot(ctx, h.user, owner.ID, 0)
	if err != nil {
		t.Fatalf("delete work-free member: %v", err)
	}
	if deleted.ID != owner.ID {
		t.Fatalf("deleted = %s, want %s", deleted.ID, owner.ID)
	}

	// The member and what was its own are gone.
	members, err := sqlite.ListBots(ctx, h.app.DB, team.ID)
	if err != nil || len(members) != 2 {
		t.Fatalf("members after delete = %d (err %v), want leader + target", len(members), err)
	}
	for _, query := range []string{
		`SELECT COUNT(*) FROM bots WHERE id = ?`,
		`SELECT COUNT(*) FROM messages WHERE bot_id = ?`,
		`SELECT COUNT(*) FROM runs WHERE bot_id = ?`,
		`SELECT COUNT(*) FROM provider_sessions WHERE bot_id = ?`,
	} {
		args := []any{owner.ID}
		if strings.Count(query, "?") == 2 {
			args = append(args, owner.ID)
		}
		var n int
		if err := h.app.DB.QueryRow(query, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("%s left %d rows of the deleted member", query, n)
		}
	}
	// The role is a shared definition, not member data: it stays in the
	// library and can staff this team again.
	again, err := h.app.AddBot(ctx, h.user, team.ID, owner.RoleID)
	if err != nil || again.RoleID != owner.RoleID {
		t.Fatalf("re-adding the deleted member's role = %+v, %v", again, err)
	}
	library, err := h.app.RoleLibrary(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, role := range library {
		if role["id"] == owner.RoleID {
			found = true
		}
	}
	if !found {
		t.Fatal("deleting a member must never remove its role from the library")
	}

	// A live (unknown) execution also blocks deletion: it is unfinished work
	// that must be resolved, not deleted around.
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

// TestArchiveOnlyIdleTeam: archiving is the one gate. A pending
// approval, an unfinished task, a running member and a queued message each
// refuse it; once the team is idle the archive succeeds, records its time,
// shuts the team engine down and refuses new work. Restore brings it back.
func TestArchiveOnlyIdleTeam(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"fullstack-engineer", "qa-engineer"})
	captain, owner := bots[0], bots[1]
	fake := h.ensureFake(t, team.ID)
	refused := func(why string) {
		t.Helper()
		_, err := h.app.ArchiveTeam(ctx, h.user, team.ID, 0)
		var de *domain.Error
		if !errors.As(err, &de) || de.MessageKey != "error.team.open_work" {
			t.Fatalf("archive with %s = %v, want open_work refusal", why, err)
		}
	}

	// A pending approval blocks the archive.
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: captain.ID, Text: "跑一下测试"}); err != nil {
		t.Fatalf("command msg: %v", err)
	}
	waitUntil(t, 5*time.Second, "captain turn", func() bool { return len(fake.Turns()) == 1 })
	asking := fake.Turns()[0]
	fake.EmitApproval("archive-gate", asking.ThreadID, asking.TurnID, "go test ./...")
	waitUntil(t, 5*time.Second, "approval card", func() bool { cards, _ := h.app.PendingApprovals(ctx, h.user, team.ID, ""); return len(cards) == 1 })
	cards, _ := h.app.PendingApprovals(ctx, h.user, team.ID, "")
	refused("a pending approval")
	if _, err := h.app.DecideApproval(ctx, h.user, cards[0].ID, "decline", "", cards[0].Revision); err != nil {
		t.Fatalf("decline: %v", err)
	}
	fake.EmitTurnCompleted(asking.ThreadID, asking.TurnID, "completed", "declined")
	waitUntil(t, 5*time.Second, "captain run ended", func() bool {
		_, err := sqlite.FindActiveRun(ctx, h.app.DB, captain.ID)
		return errors.Is(err, sqlite.ErrNotFound)
	})

	// An open card blocks the archive until its requirement is cancelled.
	_, req := h.handCard(t, team, captain, owner, "归档前卡片", "先收尾")
	waitUntil(t, 5*time.Second, "owner assignment turn", func() bool { return len(fake.Turns()) >= 2 })
	fake.EmitTurnCompleted(fake.Turns()[1].ThreadID, fake.Turns()[1].TurnID, "completed", "收到")
	waitUntil(t, 5*time.Second, "owner run ended", func() bool {
		_, err := sqlite.FindActiveRun(ctx, h.app.DB, owner.ID)
		return errors.Is(err, sqlite.ErrNotFound)
	})
	refused("an unfinished task")
	if _, err := h.app.CancelRequirement(ctx, h.user, req.ID); err != nil {
		t.Fatalf("cancel requirement: %v", err)
	}

	// A running member blocks the archive.
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: captain.ID, Text: "正在忙"}); err != nil {
		t.Fatalf("busy msg: %v", err)
	}
	waitUntil(t, 5*time.Second, "captain turn", func() bool { return len(fake.Turns()) >= 3 })
	refused("a running member")
	fake.EmitTurnCompleted(fake.Turns()[2].ThreadID, fake.Turns()[2].TurnID, "completed", "好了")
	waitUntil(t, 5*time.Second, "captain run ended", func() bool {
		_, err := sqlite.FindActiveRun(ctx, h.app.DB, captain.ID)
		return errors.Is(err, sqlite.ErrNotFound)
	})

	// A queued message blocks the archive. The finished turns reported no
	// usage, so a budget parks the next delivery in the queue reliably.
	settings, _ := h.app.GetSettings(ctx)
	scope := domain.BudgetScopeTeam
	tiny := int64(1)
	if _, err := h.app.UpdateSettings(ctx, h.user, domain.SettingsPatch{BudgetScope: &scope, BudgetTokens: &tiny}, settings.Revision); err != nil {
		t.Fatalf("enable budget: %v", err)
	}
	held, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: captain.ID, Text: "被预算挡住"})
	if err != nil {
		t.Fatalf("held msg: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	if h.queuedForBot(t, captain.ID) == 0 {
		t.Fatal("expected the budget to hold a queued delivery")
	}
	refused("a queued message")
	if _, err := h.app.CancelQueuedMessage(ctx, h.user, held.Message.ID); err != nil {
		t.Fatalf("cancel queued: %v", err)
	}

	// Idle: the archive succeeds and shuts the team engine down.
	archived, err := h.app.ArchiveTeam(ctx, h.user, team.ID, 0)
	if err != nil || archived.Status != domain.TeamStatusArchived || archived.ArchivedAt == "" {
		t.Fatalf("archive = %+v (err %v), want archived with a time", archived, err)
	}
	if fake.ShutdownCalls < 1 {
		t.Fatal("archiving must shut the team engine down")
	}
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: captain.ID, Text: "归档后不该派发"}); err == nil {
		t.Fatal("an archived team must refuse new messages")
	}
	var de *domain.Error
	if _, _, err := h.app.CreateTeam(ctx, h.user, app.CreateTeamInput{Name: "占用", Directory: h.teamDir}); !errors.As(err, &de) || de.MessageKey != "error.team.directory_archived" {
		t.Fatalf("creating in an archived team's folder = %v, want directory_archived", err)
	}

	restored, err := h.app.RestoreTeam(ctx, h.user, team.ID, 0)
	if err != nil || restored.Status != domain.TeamStatusActive || restored.ArchivedAt != "" {
		t.Fatalf("restore = %+v (err %v), want active", restored, err)
	}
}

// TestDeleteRemovesTeamCompletely: deleting an archived team needs no second
// census and leaves nothing behind — no team, member, conversation or event
// rows, no binding, no .cobot folder — while the project's own files stay
// and the folder is free for a new team. An active team cannot be deleted.
func TestDeleteRemovesTeamCompletely(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"fullstack-engineer"})
	userFile := filepath.Join(h.teamDir, "README.md")
	if err := os.WriteFile(userFile, []byte("project"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.DeleteTeam(ctx, h.user, team.ID, 0); err == nil {
		t.Fatal("deleting an active team must be refused")
	}
	if _, err := h.app.ArchiveTeam(ctx, h.user, team.ID, 0); err != nil {
		t.Fatalf("archive: %v", err)
	}
	if _, err := h.app.DeleteTeam(ctx, h.user, team.ID, 0); err != nil {
		t.Fatalf("delete: %v", err)
	}

	for _, table := range []string{"teams", "bots", "messages", "events", "directory_bindings"} {
		column := "team_id"
		if table == "teams" {
			column = "id"
		}
		var n int
		if err := h.app.DB.QueryRow("SELECT COUNT(*) FROM "+table+" WHERE "+column+" = ?", team.ID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("%s keeps %d rows of the deleted team", table, n)
		}
	}
	var sessions int
	if err := h.app.DB.QueryRow("SELECT COUNT(*) FROM provider_sessions WHERE bot_id = ?", bots[0].ID).Scan(&sessions); err != nil || sessions != 0 {
		t.Fatalf("provider sessions left = %d (%v)", sessions, err)
	}
	if _, err := os.Stat(filepath.Join(h.teamDir, ".cobot")); !os.IsNotExist(err) {
		t.Fatalf(".cobot must be removed, stat err = %v", err)
	}
	if data, err := os.ReadFile(userFile); err != nil || string(data) != "project" {
		t.Fatalf("project files must stay: %q (%v)", data, err)
	}
	again, _, err := h.app.CreateTeam(ctx, h.user, app.CreateTeamInput{Name: "重建团队", Directory: h.teamDir})
	if err != nil || again.Status != domain.TeamStatusActive {
		t.Fatalf("a new team in the freed folder = %+v, %v", again, err)
	}
}

// TestClaimedFolderIsRefused: one project belongs to one team for
// life. A folder whose marker names a team this instance cannot see is
// refused by name and never taken over, while a stale marker left by a team
// this instance deleted claims nothing.
func TestClaimedFolderIsRefused(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()

	writeMarker := func(dir, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(dir, ".cobot"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, ".cobot", "team.json"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	refusal := func(dir string) *domain.Error {
		t.Helper()
		_, _, err := h.app.CreateTeam(ctx, h.user, app.CreateTeamInput{Name: "认领测试", Directory: dir})
		var de *domain.Error
		if !errors.As(err, &de) || de.Code != domain.CodeConflict {
			t.Fatalf("creating in %s = %v, want a conflict", dir, err)
		}
		return de
	}

	named := filepath.Join(filepath.Dir(h.teamDir), "claimed-named")
	writeMarker(named, `{"instanceId":"other-instance","teamId":"t1","teamName":"别处的团队"}`)
	if de := refusal(named); de.MessageKey != "error.team.directory_claimed_external" || de.Params["teamName"] != "别处的团队" {
		t.Fatalf("named claim refusal = %s %+v", de.MessageKey, de.Params)
	}
	unnamed := filepath.Join(filepath.Dir(h.teamDir), "claimed-unnamed")
	writeMarker(unnamed, `not json`)
	if de := refusal(unnamed); de.MessageKey != "error.team.directory_claimed_unknown" {
		t.Fatalf("unreadable claim refusal = %s", de.MessageKey)
	}
	if data, _ := os.ReadFile(filepath.Join(unnamed, ".cobot", "team.json")); string(data) != "not json" {
		t.Fatal("a refused claim must never be overwritten")
	}

	stale := filepath.Join(filepath.Dir(h.teamDir), "stale-marker")
	writeMarker(stale, `{"instanceId":"`+h.app.InstanceID+`","teamId":"deleted-long-ago"}`)
	team, _, err := h.app.CreateTeam(ctx, h.user, app.CreateTeamInput{Name: "旧标记", Directory: stale})
	if err != nil {
		t.Fatalf("a stale marker of this instance must not block: %v", err)
	}
	var written app.TeamMarker
	data, _ := os.ReadFile(filepath.Join(stale, ".cobot", "team.json"))
	if err := json.Unmarshal(data, &written); err != nil || written.TeamID != team.ID || written.TeamName != "旧标记" {
		t.Fatalf("marker after create = %s (%v)", data, err)
	}
}

// TestTeamFollowsItsFolder pins the lifelong binding: a team whose folder is
// gone becomes directory_missing and refuses work; once the folder is back
// at the same path it resumes by itself, and a lost marker is rewritten.
func TestTeamFollowsItsFolder(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"fullstack-engineer"})
	status := func() string {
		t.Helper()
		current, _, err := sqlite.GetTeam(ctx, h.app.DB, team.ID)
		if err != nil {
			t.Fatal(err)
		}
		return current.Status
	}

	away := h.teamDir + "-away"
	if err := os.Rename(h.teamDir, away); err != nil {
		t.Fatal(err)
	}
	h.app.CheckTeamDirectories(ctx)
	if got := status(); got != domain.TeamStatusDirectoryMissing {
		t.Fatalf("status with the folder gone = %s", got)
	}
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: bots[1].ID, Text: "hi", RequestID: "missing-folder"}); err == nil {
		t.Fatal("a team without its folder must refuse work")
	}

	if err := os.Rename(away, h.teamDir); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(h.teamDir, ".cobot", "team.json")); err != nil {
		t.Fatal(err)
	}
	h.app.CheckTeamDirectories(ctx)
	if got := status(); got != domain.TeamStatusActive {
		t.Fatalf("status with the folder back = %s", got)
	}
	var marker app.TeamMarker
	data, err := os.ReadFile(filepath.Join(h.teamDir, ".cobot", "team.json"))
	if err != nil || json.Unmarshal(data, &marker) != nil || marker.TeamID != team.ID {
		t.Fatalf("lost marker must be rewritten: %s (%v)", data, err)
	}
}

// TestCreateTeamIsAtomic: creating a team is one transaction. When the
// folder cannot be prepared nothing is left — no team, member or binding
// row, and the user's own file is untouched — and the same request can be
// sent again once the folder is fixed.
func TestCreateTeamIsAtomic(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	dir := filepath.Join(filepath.Dir(h.teamDir), "blocked-folder")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	blocker := filepath.Join(dir, ".cobot")
	if err := os.WriteFile(blocker, []byte("a user file, not a folder"), 0o644); err != nil {
		t.Fatal(err)
	}
	input := app.CreateTeamInput{Name: "原子创建", Directory: dir, InitialRoleIDs: []string{"fullstack-engineer"}, RequestID: "atomic-create"}
	_, _, err := h.app.CreateTeam(ctx, h.user, input)
	var de *domain.Error
	if !errors.As(err, &de) || de.MessageKey != "error.team.prepare_failed" {
		t.Fatalf("create in an unpreparable folder = %v, want prepare_failed", err)
	}
	for _, table := range []string{"teams", "bots", "directory_bindings"} {
		var n int
		if err := h.app.DB.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("%s keeps %d rows after a failed create", table, n)
		}
	}
	if data, err := os.ReadFile(blocker); err != nil || string(data) != "a user file, not a folder" {
		t.Fatalf("the user's file must stay: %q (%v)", data, err)
	}

	if err := os.Remove(blocker); err != nil {
		t.Fatal(err)
	}
	team, _, err := h.app.CreateTeam(ctx, h.user, input)
	if err != nil || team.Status != domain.TeamStatusActive {
		t.Fatalf("the same request after fixing the folder = %+v, %v", team, err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".cobot", "team.json")); err != nil {
		t.Fatalf("marker after create: %v", err)
	}
}
