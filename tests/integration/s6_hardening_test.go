package integration

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/engine"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// A link a member wrote opens documents, but a program or script in the team
// folder is only shown in the file manager: opening it would run it outside
// the engine's sandbox and every approval.
func TestFileLinksNeverLaunchProgramsOrScripts(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, _ := h.createTeam(t, []string{"fullstack-engineer"})
	for _, name := range []string{"page.html", "run.cmd", "setup.EXE", "go.ps1", "link.lnk"} {
		if err := os.WriteFile(filepath.Join(team.Directory, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	type launch struct {
		path   string
		reveal bool
	}
	var launched []launch
	h.app.OpenLocal = func(path string, reveal bool) error {
		launched = append(launched, launch{path, reveal})
		return nil
	}

	if _, err := h.app.OpenTeamPath(ctx, h.user, team.ID, "page.html", false); err != nil {
		t.Fatalf("a document opens: %v", err)
	}
	for _, name := range []string{"run.cmd", "setup.EXE", "go.ps1", "link.lnk"} {
		_, err := h.app.OpenTeamPath(ctx, h.user, team.ID, name, false)
		var de *domain.Error
		if !errors.As(err, &de) || de.MessageKey != "error.path.open_executable" {
			t.Fatalf("%s must not be opened, got %v", name, err)
		}
		if _, err := h.app.OpenTeamPath(ctx, h.user, team.ID, name, true); err != nil {
			t.Fatalf("%s can still be shown in the file manager: %v", name, err)
		}
	}
	if len(launched) != 5 || launched[0].reveal {
		t.Fatalf("launches = %+v", launched)
	}
	for _, l := range launched[1:] {
		if !l.reveal {
			t.Fatalf("a program was opened: %+v", l)
		}
	}
}

// waitRunning returns the member's live run once the engine accepted its turn.
func waitRunning(t *testing.T, h *m2Harness, botID string) domain.Run {
	t.Helper()
	var run domain.Run
	waitUntil(t, 5*time.Second, "running turn", func() bool {
		r, err := sqlite.FindActiveRun(context.Background(), h.app.DB, botID)
		run = r
		return err == nil && r.Status == domain.RunRunning && r.ProviderTurnID != ""
	})
	return run
}

// A turn whose end was never recorded is settled from the engine's own
// record when the user interrupts it, so the member does not stay "working".
func TestInterruptSettlesATurnTheEngineAlreadyEnded(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, nil)
	bot := bots[0]
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: bot.ID, Text: "第一条"}); err != nil {
		t.Fatal(err)
	}
	fake := h.fakeFor(t, team.ID)
	run := waitRunning(t, h, bot.ID)
	turn := fake.Turns()[0]
	// The engine finished the turn, but its completion never reached CoBot.
	fake.SetTurnFact(turn.ThreadID, turn.TurnID, "completed")
	fake.InterruptErr = errors.New("no active turn to interrupt")

	got, err := h.app.InterruptRun(ctx, run.ID)
	if err != nil || got != run.ID {
		t.Fatalf("interrupt = %q, %v", got, err)
	}
	settled, err := sqlite.GetRun(ctx, h.app.DB, run.ID)
	if err != nil || settled.Status != domain.RunCompleted {
		t.Fatalf("run = %+v, %v", settled, err)
	}
	// The member takes the next message as a new turn.
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: bot.ID, Text: "第二条"}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 5*time.Second, "next message dispatched", func() bool { return len(fake.Turns()) == 2 })

	// A turn the engine still runs is not settled by a failed interrupt.
	run = waitRunning(t, h, bot.ID)
	if _, err := h.app.InterruptRun(ctx, run.ID); err == nil {
		t.Fatal("a failed interrupt of a running turn must be reported")
	}
	if r, _ := sqlite.GetRun(ctx, h.app.DB, run.ID); r.Status != domain.RunRunning {
		t.Fatalf("live run changed to %s", r.Status)
	}
}

// A turn's end that cannot be written at once is retried, not dropped.
func TestFinishedTurnIsRecordedAfterAFailedWrite(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, nil)
	bot := bots[0]
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: bot.ID, Text: "做点事"}); err != nil {
		t.Fatal(err)
	}
	fake := h.fakeFor(t, team.ID)
	run := waitRunning(t, h, bot.ID)
	turn := fake.Turns()[0]

	// The reply cannot be stored while the table is away.
	if _, err := h.app.DB.ExecContext(ctx, `ALTER TABLE messages RENAME TO messages_away`); err != nil {
		t.Fatal(err)
	}
	fake.EmitTurnCompleted(turn.ThreadID, turn.TurnID, "completed", "做完了")
	if r, _ := sqlite.GetRun(ctx, h.app.DB, run.ID); r.Status != domain.RunRunning {
		t.Fatalf("run settled without its reply: %s", r.Status)
	}
	if _, err := h.app.DB.ExecContext(ctx, `ALTER TABLE messages_away RENAME TO messages`); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 10*time.Second, "settled after retry", func() bool {
		r, err := sqlite.GetRun(ctx, h.app.DB, run.ID)
		return err == nil && r.Status == domain.RunCompleted
	})
	var replies int
	if err := h.app.DB.QueryRowContext(ctx, `SELECT count(*) FROM messages WHERE run_id = ? AND kind = ?`, run.ID, domain.MessageKindAssistant).Scan(&replies); err != nil || replies != 1 {
		t.Fatalf("replies = %d, %v", replies, err)
	}
}

// An approval that cannot become a card is declined, so the engine never
// waits forever on a request nobody can see.
func TestUnrecordableApprovalIsDeclined(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, nil)
	bot := bots[0]
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: bot.ID, Text: "跑命令"}); err != nil {
		t.Fatal(err)
	}
	fake := h.fakeFor(t, team.ID)
	waitRunning(t, h, bot.ID)
	turn := fake.Turns()[0]

	if _, err := h.app.DB.ExecContext(ctx, `ALTER TABLE approvals RENAME TO approvals_away`); err != nil {
		t.Fatal(err)
	}
	fake.EmitApproval("lost-card", turn.ThreadID, turn.TurnID, "npm test")
	if _, err := h.app.DB.ExecContext(ctx, `ALTER TABLE approvals_away RENAME TO approvals`); err != nil {
		t.Fatal(err)
	}
	replies := fake.Replies()
	if len(replies) != 1 || replies[0].Response["decision"] != domain.DecisionDeclineEngine {
		t.Fatalf("replies = %+v", replies)
	}
}

// A turn the engine refused failed outright: no recovery card, the member is
// free again, and the next message gets a turn of its own.
func TestRefusedTurnFailsWithoutARecoveryCard(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, nil)
	bot := bots[0]
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: bot.ID, Text: "预热"}); err != nil {
		t.Fatal(err)
	}
	fake := h.fakeFor(t, team.ID)
	first := waitRunning(t, h, bot.ID)
	fake.EmitTurnCompleted(fake.Turns()[0].ThreadID, fake.Turns()[0].TurnID, "completed", "好")
	waitUntil(t, 5*time.Second, "first turn settled", func() bool {
		r, err := sqlite.GetRun(ctx, h.app.DB, first.ID)
		return err == nil && r.Status == domain.RunCompleted
	})

	fake.StartTurnErr = fmt.Errorf("turn/start: %w: invalid effort", engine.ErrRejected)
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: bot.ID, Text: "会被拒绝"}); err != nil {
		t.Fatal(err)
	}
	var failed domain.Run
	waitUntil(t, 5*time.Second, "refused run failed", func() bool {
		err := h.app.DB.QueryRowContext(ctx, `SELECT id, status, error_key FROM runs WHERE bot_id = ? AND id != ? ORDER BY started_at DESC LIMIT 1`, bot.ID, first.ID).Scan(&failed.ID, &failed.Status, &failed.ErrorKey)
		return err == nil && failed.Status == domain.RunFailed
	})
	if failed.ErrorKey != "error.engine.turn_rejected" {
		t.Fatalf("error key = %q", failed.ErrorKey)
	}
	pending, err := h.app.PendingApprovals(ctx, h.user, team.ID, "")
	if err != nil || len(pending) != 0 {
		t.Fatalf("a refused turn raised cards: %+v, %v", pending, err)
	}

	fake.StartTurnErr = nil
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: bot.ID, Text: "再来"}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 5*time.Second, "member takes the next turn", func() bool { return len(fake.Turns()) == 2 })
}

// Cancelling a task releases the tasks that waited on it and tells the
// leader, instead of parking them for good.
func TestCancelledDependencyReleasesDependentsAndTellsTheLeader(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"fullstack-engineer", "qa-engineer"})
	captain, dev, qa := bots[0], bots[1], bots[2]
	token := h.botToken(t, captain)
	taskID := func(title string) string {
		var id string
		if err := h.app.DB.QueryRowContext(ctx, `SELECT id FROM tasks WHERE team_id = ? AND title = ?`, team.ID, title).Scan(&id); err != nil {
			t.Fatalf("task %s: %v", title, err)
		}
		return id
	}
	if status, body := h.callTool(t, token, "create_task", map[string]any{"title": "实现接口", "ownerId": dev.ID, "acceptanceCriteria": "返回 200", "idempotencyKey": "s6-dev"}); status != 200 {
		t.Fatalf("create dev task = %d %v", status, body)
	}
	devTask := taskID("实现接口")
	if status, body := h.callTool(t, token, "create_task", map[string]any{"title": "测试接口", "ownerId": qa.ID, "acceptanceCriteria": "用例通过", "dependencyIds": []string{devTask}, "idempotencyKey": "s6-qa"}); status != 200 {
		t.Fatalf("create qa task = %d %v", status, body)
	}
	qaTask := taskID("测试接口")
	fake := h.fakeFor(t, team.ID)
	waitUntil(t, 5*time.Second, "dev woken", func() bool { return len(fake.Turns()) == 1 })
	fake.EmitTurnCompleted(fake.Turns()[0].ThreadID, fake.Turns()[0].TurnID, "completed", "")

	task, err := sqlite.GetTask(ctx, h.app.DB, devTask)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.UpdateTaskStatus(ctx, h.user, devTask, domain.TaskCancelled, "", task.Revision); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	var qaTurn, leaderTurn bool
	waitUntil(t, 5*time.Second, "qa and leader woken", func() bool {
		for _, turn := range fake.Turns() {
			if strings.Contains(turn.Text, "你被指派了任务：测试接口") {
				qaTurn = true
			}
			if strings.Contains(turn.Text, "已被取消") && strings.Contains(turn.Text, qaTask) {
				leaderTurn = true
			}
		}
		return qaTurn && leaderTurn
	})
}
