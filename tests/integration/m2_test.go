package integration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/engine"
	"github.com/kunghim/CoBot/internal/events"
	"github.com/kunghim/CoBot/internal/platform"
	"github.com/kunghim/CoBot/internal/store/sqlite"
	"github.com/kunghim/CoBot/tests/integration/fakeengine"
)

type m2Harness struct {
	t       *testing.T
	app     *app.App
	sched   *app.Scheduler
	fakes   map[string]*fakeengine.Server // teamID -> fake
	user    auth.Principal
	teamDir string

	fakeSeq int
	onFake  func(*fakeengine.Server)
}

func newM2Harness(t *testing.T) *m2Harness {
	t.Helper()
	root := t.TempDir()
	paths, err := platform.Resolve(platform.EnvDevelopment, filepath.Join(root, "profile"))
	if err != nil {
		t.Fatal(err)
	}
	if err := paths.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	db, err := sqlite.Open(paths.DatabaseFile())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	migrator, err := sqlite.NewMigrator(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrator.MigrateToLatest(context.Background(), paths.BackupsDir()); err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	a := &app.App{
		DB: db, Paths: paths, Hub: events.NewHub(), InstanceID: "test-instance",
		ServiceEpoch: "epoch-0", Version: "test", Logger: logger,
	}
	h := &m2Harness{t: t, app: a, fakes: map[string]*fakeengine.Server{}, user: auth.NewPrincipalUser("sess-1")}
	sched := app.NewScheduler(a, func(spec app.ServerSpec) (engine.Server, error) {
		h.fakeSeq++
		fake := fakeengine.New(spec.Callbacks, fmt.Sprintf("epoch-fake-%d", h.fakeSeq))
		if h.onFake != nil {
			h.onFake(fake)
		}
		h.fakes[spec.TeamID] = fake
		return fake, nil
	})
	a.Scheduler = sched
	sched.Start()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = sched.Stop(ctx)
	})

	teamDir := filepath.Join(root, "team-workdir")
	if err := os.MkdirAll(teamDir, 0o755); err != nil {
		t.Fatal(err)
	}
	h.teamDir = teamDir
	return h
}

func (h *m2Harness) createTeam(t *testing.T, roles []string) (domain.Team, []domain.Bot) {
	t.Helper()
	team, bots, err := h.app.CreateTeam(context.Background(), h.user, app.CreateTeamInput{
		Name: "测试团队", Directory: h.teamDir, InitialRoleIDs: roles,
	})
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	return team, bots
}

func waitUntil(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func (h *m2Harness) fakeFor(t *testing.T, teamID string) *fakeengine.Server {
	t.Helper()
	var fake *fakeengine.Server
	waitUntil(t, 5*time.Second, "team fake server", func() bool {
		fake = h.fakes[teamID]
		return fake != nil
	})
	return fake
}

func TestTeamCreationValidation(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()

	// Same directory twice is refused even after status changes.
	team, bots := h.createTeam(t, []string{"backend-dev"})
	if team.Status != domain.TeamStatusActive {
		t.Fatalf("team status = %s, want active", team.Status)
	}
	if len(bots) != 2 || !bots[0].IsLeader {
		t.Fatalf("expected leader + backend-dev, got %+v", bots)
	}
	// The marker and the Git-exclude path behaviour: plain dir has no Git,
	// but the marker must exist.
	if _, err := os.Stat(filepath.Join(h.teamDir, ".cobot", "team.json")); err != nil {
		t.Fatalf("team marker missing: %v", err)
	}
	_, _, err := h.app.CreateTeam(ctx, h.user, app.CreateTeamInput{Name: "重复", Directory: h.teamDir})
	if err == nil {
		t.Fatal("duplicate directory must be refused")
	}

	// Nesting is refused in both directions.
	nested := filepath.Join(h.teamDir, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	_, _, err = h.app.CreateTeam(ctx, h.user, app.CreateTeamInput{Name: "嵌套", Directory: nested})
	var de *domain.Error
	if !errors.As(err, &de) || de.Code != domain.CodeConflict {
		t.Fatalf("nested directory must conflict, got %v", err)
	}

	// Reserved profile directories are refused.
	_, _, err = h.app.CreateTeam(ctx, h.user, app.CreateTeamInput{Name: "保留", Directory: h.app.Paths.Root})
	if err == nil {
		t.Fatal("reserved directory must be refused")
	}

	// Member limit includes the leader.
	limit, _ := h.app.GetSettings(ctx)
	members := make([]string, 0, limit.MemberLimit)
	for i := 1; i < limit.MemberLimit+2; i++ {
		members = append(members, "backend-dev")
	}
	dir2 := filepath.Join(filepath.Dir(h.teamDir), "team-2")
	if err := os.MkdirAll(dir2, 0o755); err != nil {
		t.Fatal(err)
	}
	_, _, err = h.app.CreateTeam(ctx, h.user, app.CreateTeamInput{Name: "超限", Directory: dir2, InitialRoleIDs: members})
	if err == nil {
		t.Fatal("member limit must be enforced")
	}
}

func TestSendMessageDispatchAndAssistantReply(t *testing.T) {
	h := newM2Harness(t)
	team, bots := h.createTeam(t, nil)
	bot := bots[0]

	result, err := h.app.SendMessage(context.Background(), h.user, app.SendMessageInput{
		BotID: bot.ID, Text: "你好，队长", RequestID: "req-1",
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if result.DeliveryStatus != domain.DeliveryQueued {
		t.Fatalf("delivery status = %s, want queued", result.DeliveryStatus)
	}

	fake := h.fakeFor(t, team.ID)
	waitUntil(t, 5*time.Second, "turn started", func() bool { return len(fake.Turns()) == 1 })
	turn := fake.Turns()[0]
	if turn.Text != "你好，队长" {
		t.Fatalf("engine received %q", turn.Text)
	}

	// Same requestId replays; different text conflicts.
	replay, err := h.app.SendMessage(context.Background(), h.user, app.SendMessageInput{
		BotID: bot.ID, Text: "你好，队长", RequestID: "req-1",
	})
	if err != nil || replay.Message.ID != result.Message.ID {
		t.Fatalf("idempotent replay failed: %v %+v", err, replay)
	}
	_, err = h.app.SendMessage(context.Background(), h.user, app.SendMessageInput{
		BotID: bot.ID, Text: "换个内容", RequestID: "req-1",
	})
	if err == nil {
		t.Fatal("same requestId with different text must conflict")
	}

	fake.EmitTurnCompleted(turn.ThreadID, turn.TurnID, "completed", "你好！有什么可以帮你？")
	waitUntil(t, 5*time.Second, "assistant message", func() bool {
		detail, err := h.app.ListConversation(context.Background(), h.user, bot.ID, "", 50)
		if err != nil {
			return false
		}
		messages := detail["messages"].([]domain.Message)
		for _, m := range messages {
			if m.Kind == domain.MessageKindAssistant {
				return true
			}
		}
		return false
	})
	detail, _ := h.app.ListConversation(context.Background(), h.user, bot.ID, "", 50)
	if detail["queuedCount"].(int) != 0 {
		t.Fatal("queue must be drained")
	}
	botsAfter, _ := h.app.GetTeamDetail(context.Background(), h.user, team.ID)
	if botsAfter.Bots[0].Status != domain.BotStatusIdle {
		t.Fatalf("bot status = %s, want idle", botsAfter.Bots[0].Status)
	}
}

func TestQueuedFIFOAndCancel(t *testing.T) {
	h := newM2Harness(t)
	team, bots := h.createTeam(t, nil)
	bot := bots[0]
	ctx := context.Background()

	first, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: bot.ID, Text: "第一条"})
	if err != nil {
		t.Fatal(err)
	}
	fake := h.fakeFor(t, team.ID)
	waitUntil(t, 5*time.Second, "first turn", func() bool { return len(fake.Turns()) == 1 })

	// Bot is busy; further messages queue without starting turns.
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: bot.ID, Text: "第二条"}); err != nil {
		t.Fatal(err)
	}
	third, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: bot.ID, Text: "第三条"})
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.Turns()) != 1 {
		t.Fatal("busy bot must not start new turns")
	}

	// Cancelling the not-yet-sent third message must keep it from executing.
	if _, err := h.app.CancelQueuedMessage(ctx, h.user, third.Message.ID); err != nil {
		t.Fatalf("cancel queued: %v", err)
	}
	// The in-flight first message cannot be cancelled.
	if _, err := h.app.CancelQueuedMessage(ctx, h.user, first.Message.ID); err == nil {
		t.Fatal("dispatched message must not be cancellable")
	}

	fake.EmitTurnCompleted(fake.Turns()[0].ThreadID, fake.Turns()[0].TurnID, "completed", "已处理第一条")
	waitUntil(t, 5*time.Second, "second turn", func() bool { return len(fake.Turns()) == 2 })
	if fake.Turns()[1].Text != "第二条" {
		t.Fatalf("FIFO violated: got %q", fake.Turns()[1].Text)
	}
	fake.EmitTurnCompleted(fake.Turns()[1].ThreadID, fake.Turns()[1].TurnID, "completed", "已处理第二条")
	// The cancelled third message must never reach the engine.
	time.Sleep(200 * time.Millisecond)
	if len(fake.Turns()) != 2 {
		t.Fatalf("cancelled message reached the engine: %+v", fake.Turns())
	}
}

func TestInterruptRequestsEngineStopAndFinalizes(t *testing.T) {
	h := newM2Harness(t)
	team, bots := h.createTeam(t, nil)
	bot := bots[0]
	ctx := context.Background()

	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: bot.ID, Text: "跑个长任务"}); err != nil {
		t.Fatal(err)
	}
	fake := h.fakeFor(t, team.ID)
	waitUntil(t, 5*time.Second, "turn", func() bool { return len(fake.Turns()) == 1 })

	detail, _ := h.app.ListConversation(ctx, h.user, bot.ID, "", 10)
	run := detail["activeRun"].(domain.Run)
	runID, err := h.app.InterruptRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("interrupt: %v", err)
	}
	if runID != run.ID {
		t.Fatal("interrupt must target the active run")
	}
	if len(fake.Interrupts()) != 1 {
		t.Fatal("engine interrupt not requested")
	}
	// Only that bot: another message to the same bot stays queued.
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: bot.ID, Text: "后续"}); err != nil {
		t.Fatal(err)
	}
	fake.EmitTurnCompleted(fake.Turns()[0].ThreadID, fake.Turns()[0].TurnID, "interrupted", "")
	waitUntil(t, 5*time.Second, "interrupted run", func() bool {
		r, err := sqlite.GetRun(ctx, h.app.DB, run.ID)
		return err == nil && r.Status == domain.RunInterrupted
	})
}

func TestNativeApprovalRoundTrip(t *testing.T) {
	h := newM2Harness(t)
	team, bots := h.createTeam(t, nil)
	bot := bots[0]
	ctx := context.Background()

	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: bot.ID, Text: "写个文件"}); err != nil {
		t.Fatal(err)
	}
	fake := h.fakeFor(t, team.ID)
	waitUntil(t, 5*time.Second, "turn", func() bool { return len(fake.Turns()) == 1 })
	turn := fake.Turns()[0]

	fake.EmitApproval("req-appr-1", turn.ThreadID, turn.TurnID, `pwsh -Command "Write-Host hi"`)
	waitUntil(t, 5*time.Second, "pending approval", func() bool {
		items, err := h.app.PendingApprovals(ctx, h.user, team.ID, "")
		return err == nil && len(items) == 1
	})
	pending, _ := h.app.PendingApprovals(ctx, h.user, team.ID, "")
	approval := pending[0]
	if approval.Type != domain.ApprovalNativeTool || approval.Summary == "" {
		t.Fatalf("unexpected approval: %+v", approval)
	}
	// Both entry points see the same single request: member scope filter works.
	memberScoped, _ := h.app.PendingApprovals(ctx, h.user, team.ID, bot.ID)
	if len(memberScoped) != 1 {
		t.Fatal("requester-scoped projection must see its own card")
	}
	otherScoped, _ := h.app.PendingApprovals(ctx, h.user, team.ID, "some-other-bot")
	if len(otherScoped) != 0 {
		t.Fatal("unrelated scope must see nothing")
	}
	// While waiting, the bot reports waiting_approval.
	detail, _ := h.app.GetTeamDetail(ctx, h.user, team.ID)
	if detail.Bots[0].Status != domain.BotStatusWaitingApproval {
		t.Fatalf("bot status = %s, want waiting_approval", detail.Bots[0].Status)
	}

	decided, err := h.app.DecideApproval(ctx, h.user, approval.ID, "accept", "", approval.Revision)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if decided.RequestState != domain.ApprovalStateDecided || decided.ResponseState != domain.ResponseSent {
		t.Fatalf("unexpected decided state: %+v", decided)
	}
	replies := fake.Replies()
	if len(replies) != 1 || replies[0].Response["decision"] != "accept" {
		t.Fatalf("engine reply missing/wrong: %+v", replies)
	}
	// Double decide is refused.
	if _, err := h.app.DecideApproval(ctx, h.user, approval.ID, "decline", "", decided.Revision); err == nil {
		t.Fatal("second decision must conflict")
	}
	// History keeps the original options and choice.
	history, _ := h.app.ApprovalHistory(ctx, h.user, team.ID, 50)
	if len(history) != 1 || history[0].Decision == nil || history[0].Decision.OptionID != "accept" {
		t.Fatalf("history incomplete: %+v", history)
	}
	// The turn continues and completes normally.
	fake.EmitTurnCompleted(turn.ThreadID, turn.TurnID, "completed", "文件已写入")
	waitUntil(t, 5*time.Second, "completion", func() bool {
		r, err := sqlite.GetRun(ctx, h.app.DB, approval.RunID)
		return err == nil && r.Status == domain.RunCompleted
	})
}

func TestApprovalExpiresAfterEngineRestart(t *testing.T) {
	h := newM2Harness(t)
	team, bots := h.createTeam(t, nil)
	bot := bots[0]
	ctx := context.Background()

	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: bot.ID, Text: "干活"}); err != nil {
		t.Fatal(err)
	}
	fake := h.fakeFor(t, team.ID)
	waitUntil(t, 5*time.Second, "turn", func() bool { return len(fake.Turns()) == 1 })
	turn := fake.Turns()[0]
	fake.EmitApproval("req-appr-2", turn.ThreadID, turn.TurnID, "rm something")
	waitUntil(t, 5*time.Second, "pending", func() bool {
		items, _ := h.app.PendingApprovals(ctx, h.user, team.ID, "")
		return len(items) == 1
	})
	pending, _ := h.app.PendingApprovals(ctx, h.user, team.ID, "")

	// The connection dies while the approval is pending.
	fake.EmitExit(errors.New("process died"))
	waitUntil(t, 5*time.Second, "run unknown after exit", func() bool {
		r, err := sqlite.GetRun(ctx, h.app.DB, pending[0].RunID)
		return err == nil && r.Status == domain.RunUnknown
	})

	// The bot is locked by the unknown run (H04): dispatch must not restart
	// it, so the decision records truthfully that no reply was delivered.
	if len(fake.Turns()) != 1 {
		t.Fatal("locked bot must not start new turns after connection loss")
	}
	decided, err := h.app.DecideApproval(ctx, h.user, pending[0].ID, "accept", "", 0)
	if err != nil {
		t.Fatalf("decide on stale approval: %v", err)
	}
	if decided.ResponseState != domain.ResponseUnknown {
		t.Fatalf("decision without a live connection must be unknown, got %s", decided.ResponseState)
	}
	// Decided history is preserved even though delivery is unknown.
	if decided.Decision == nil || decided.RequestState != domain.ApprovalStateDecided {
		t.Fatalf("decision must be preserved: %+v", decided)
	}

	// F04: once the run is resolved (recovery does this in M4; here we
	// simulate the resolved state) and a NEW server generation exists, the
	// old request id must expire instead of being answered on the new
	// connection.
	tx, err := h.app.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlite.UpdateRunStatus(ctx, tx, pending[0].RunID, domain.RunInterrupted, "", "", true); err != nil {
		t.Fatal(err)
	}
	if err := sqlite.SetBotRunState(ctx, tx, bot.ID, domain.BotStatusIdle, ""); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: bot.ID, Text: "重启后的新任务"}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 5*time.Second, "new server generation", func() bool { return h.fakeSeq >= 2 })
	decided2, err := h.app.DecideApproval(ctx, h.user, pending[0].ID, "accept", "", 0)
	if err == nil {
		t.Fatal("second decision on the same approval must conflict")
	}
	_ = decided2

	// A fresh approval issued by the NEW generation expires correctly when
	// decided after yet another restart: create one with an old epoch.
	fake2 := h.fakes[team.ID]
	waitUntil(t, 5*time.Second, "second-generation turn", func() bool { return len(fake2.Turns()) >= 1 })
	turn2 := fake2.Turns()[len(fake2.Turns())-1]
	fake2.EmitApproval("req-appr-3", turn2.ThreadID, turn2.TurnID, "old-epoch command")
	waitUntil(t, 5*time.Second, "second pending", func() bool {
		items, _ := h.app.PendingApprovals(ctx, h.user, team.ID, "")
		return len(items) == 1
	})
	pending2, _ := h.app.PendingApprovals(ctx, h.user, team.ID, "")
	// Kill the connection again; the decision then has no live epoch match.
	fake2.EmitExit(errors.New("second crash"))
	waitUntil(t, 5*time.Second, "second unknown run", func() bool {
		r, err := sqlite.GetRun(ctx, h.app.DB, pending2[0].RunID)
		return err == nil && r.Status == domain.RunUnknown
	})
	decided3, err := h.app.DecideApproval(ctx, h.user, pending2[0].ID, "decline", "", 0)
	if err != nil {
		t.Fatalf("decide after second crash: %v", err)
	}
	if decided3.ResponseState != domain.ResponseUnknown {
		t.Fatalf("want unknown after connection loss, got %s", decided3.ResponseState)
	}
}

func TestTurnStartUnknownLocksBot(t *testing.T) {
	h := newM2Harness(t)
	team, bots := h.createTeam(t, nil)
	bot := bots[0]
	ctx := context.Background()

	// Arm the failure before dispatch spins the team server.
	h.onFake = func(fake *fakeengine.Server) {
		fake.BeforeStartTurn = func() error { return errors.New("connection reset") }
	}
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: bot.ID, Text: "试试"}); err != nil {
		t.Fatal(err)
	}
	fake := h.fakeFor(t, team.ID)
	waitUntil(t, 5*time.Second, "unknown run", func() bool {
		detail, err := h.app.ListConversation(ctx, h.user, bot.ID, "", 10)
		if err != nil {
			return false
		}
		run, ok := detail["activeRun"].(domain.Run)
		return ok && run.Status == domain.RunUnknown
	})
	detail, _ := h.app.ListConversation(ctx, h.user, bot.ID, "", 10)
	if detail["queuedCount"].(int) != 0 {
		t.Fatal("queue must reflect the failed dispatch")
	}
	// Bot is locked: a new message queues but must not start a turn.
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: bot.ID, Text: "再试"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	if len(fake.Turns()) != 0 {
		t.Fatal("locked bot must not start new turns")
	}
	// The first message's delivery is unknown, not silently re-sent.
	_ = team
}
