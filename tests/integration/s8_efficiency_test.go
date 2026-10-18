package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// Tasks handed to a busy member reach it together in one turn, and the
// assignment of a task it already delivered costs no turn at all.
func TestAssignmentsShareATurnAndDeliveredOnesAreDropped(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"backend-dev"})
	captain, dev := bots[0], bots[1]
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: dev.ID, Text: "先看看项目"}); err != nil {
		t.Fatal(err)
	}
	f := reviewFlow{t: t, h: h, fake: h.fakeFor(t, team.ID)}
	busy := f.waitTurn(dev, "先看看项目")

	// Three tasks arrive while the member works.
	ids := map[string]string{}
	for _, title := range []string{"任务甲", "任务乙", "任务丙"} {
		ids[title] = createdTaskID(t, f.call(captain, "create_task", map[string]any{
			"title": title, "ownerId": dev.ID, "acceptanceCriteria": "完成", "idempotencyKey": "s8-" + title,
		}))
	}
	// It sees them among its tasks and delivers the first one right away.
	f.call(dev, "deliver_task", map[string]any{"taskId": ids["任务甲"], "summary": "顺手做完了", "idempotencyKey": "s8-deliver"})
	f.finish(dev, busy, "")

	// The other two arrive in one turn; the delivered one in none.
	both := f.waitTurn(dev, "你被指派了 2 个任务", "任务乙", "任务丙")
	if strings.Contains(both.Text, "你被指派了任务：任务甲") {
		t.Fatalf("the delivered task was handed over again: %q", both.Text)
	}
	f.finish(dev, both, "")
	time.Sleep(300 * time.Millisecond)
	if n := len(f.turnsOf(dev)); n != 2 {
		t.Fatalf("member turns = %d, want 2", n)
	}
	for _, title := range []string{"任务乙", "任务丙"} {
		if got := f.task(ids[title]); got.Status != domain.TaskDoing {
			t.Fatalf("%s = %s, want doing once its turn started", title, got.Status)
		}
	}
	var stale string
	if err := h.app.DB.QueryRowContext(ctx, `SELECT status FROM deliveries WHERE task_id = ? AND kind = 'task_assignment'`, ids["任务甲"]).Scan(&stale); err != nil || stale != domain.DeliveryCancelled {
		t.Fatalf("stale assignment = %q, %v", stale, err)
	}
}

// Deliveries that reach a busy leader are accepted in one turn.
func TestDeliveryNoticesShareTheLeadersTurn(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"backend-dev", "frontend-dev"})
	captain, back, front := bots[0], bots[1], bots[2]
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: captain.ID, Text: "开工"}); err != nil {
		t.Fatal(err)
	}
	f := reviewFlow{t: t, h: h, fake: h.fakeFor(t, team.ID)}
	plan := f.waitTurn(captain, "开工")
	backTask := createdTaskID(t, f.call(captain, "create_task", map[string]any{"title": "后端接口", "ownerId": back.ID, "acceptanceCriteria": "返回 200", "idempotencyKey": "s8b-back"}))
	frontTask := createdTaskID(t, f.call(captain, "create_task", map[string]any{"title": "前端页面", "ownerId": front.ID, "acceptanceCriteria": "能打开", "idempotencyKey": "s8b-front"}))

	// Both members deliver while the leader is still in its planning turn.
	for _, m := range []struct {
		bot   domain.Bot
		task  string
		title string
	}{{back, backTask, "后端接口"}, {front, frontTask, "前端页面"}} {
		turn := f.waitTurn(m.bot, m.title)
		f.call(m.bot, "deliver_task", map[string]any{"taskId": m.task, "summary": "做好了", "idempotencyKey": "s8b-" + m.title})
		f.finish(m.bot, turn, "")
	}
	f.finish(captain, plan, "")
	review := f.waitTurn(captain, "有 2 个任务交付了", "后端接口", "前端页面")
	f.call(captain, "review_task", map[string]any{"taskId": backTask, "decision": "accept", "idempotencyKey": "s8b-a1"})
	f.call(captain, "review_task", map[string]any{"taskId": frontTask, "decision": "accept", "idempotencyKey": "s8b-a2"})
	f.finish(captain, review, "")
	// The next leader turn is the requirement report, not another notice.
	report := f.waitTurn(captain, "这次需求的任务都已结束")
	if strings.Contains(report.Text, "交付了任务") {
		t.Fatalf("a delivery notice took its own turn: %q", report.Text)
	}
}

// A team engine with nothing to do is shut down after the idle timeout; the
// next message starts a fresh one and resumes the member's thread.
func TestIdleTeamEngineStopsAndComesBack(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	h.app.Scheduler.IdleEngineTimeout = 300 * time.Millisecond
	team, bots := h.createTeam(t, nil)
	captain := bots[0]
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: captain.ID, Text: "第一轮"}); err != nil {
		t.Fatal(err)
	}
	first := h.fakeFor(t, team.ID)
	f := reviewFlow{t: t, h: h, fake: first}
	turn := f.waitTurn(captain, "第一轮")

	// A running turn keeps the engine however long it takes.
	time.Sleep(2500 * time.Millisecond)
	if h.app.EngineEpochOfTeam(team.ID) != first.Epoch() {
		t.Fatal("the engine was stopped under a running turn")
	}
	f.finish(captain, turn, "好")
	waitUntil(t, 10*time.Second, "idle engine stopped", func() bool {
		return h.app.EngineEpochOfTeam(team.ID) == "" && first.ShutdownCalls > 0
	})

	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: captain.ID, Text: "第二轮"}); err != nil {
		t.Fatal(err)
	}
	second := h.fakeGeneration(t, team.ID, first)
	waitUntil(t, 5*time.Second, "turn on the new engine", func() bool { return len(second.Turns()) == 1 })
	session, err := sqlite.GetProviderSession(ctx, h.app.DB, captain.ID)
	if err != nil || session.ProviderThreadID != turn.ThreadID || second.Resumes() != 1 {
		t.Fatalf("thread not resumed: session=%+v resumes=%d err=%v", session, second.Resumes(), err)
	}
	if !strings.Contains(second.Turns()[0].Text, "第二轮") {
		t.Fatalf("turn = %q", second.Turns()[0].Text)
	}
}

// A new turn waits when what is left of the budget would not cover about
// what the member's recent turns cost, before any overspend.
func TestBudgetReserveHoldsATurnTheRestCannotCover(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, nil)
	captain := bots[0]
	fake := h.ensureFake(t, team.ID)
	settings, err := h.app.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	scope, tokens := domain.BudgetScopeBot, int64(100)
	if _, err := h.app.UpdateSettings(ctx, h.user, domain.SettingsPatch{BudgetScope: &scope, BudgetTokens: &tokens}, settings.Revision); err != nil {
		t.Fatal(err)
	}
	// The first turn has no history to estimate from and starts; it spends 60.
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: captain.ID, Text: "第一条"}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 5*time.Second, "turn 1", func() bool { return len(fake.Turns()) == 1 })
	turn := fake.Turns()[0]
	fake.EmitTokenUsage(turn.ThreadID, turn.TurnID, 50, 10, 0)
	fake.EmitTurnCompleted(turn.ThreadID, turn.TurnID, "completed", "好")
	waitUntil(t, 5*time.Second, "turn 1 done", func() bool {
		_, err := sqlite.FindActiveRun(ctx, h.app.DB, captain.ID)
		return err != nil
	})
	// 60 spent + about 60 for the next turn exceeds 100: the turn waits.
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: captain.ID, Text: "第二条"}); err != nil {
		t.Fatal(err)
	}
	var reason string
	waitUntil(t, 5*time.Second, "reserve pause recorded", func() bool {
		return h.app.DB.QueryRowContext(ctx, `SELECT resource_id FROM events WHERE type = ? ORDER BY seq DESC LIMIT 1`, domain.EventBudgetPaused).Scan(&reason) == nil
	})
	if reason != "reserve" || len(fake.Turns()) != 1 {
		t.Fatalf("reason = %q, turns = %d", reason, len(fake.Turns()))
	}
	// Room for one more such turn lets it start.
	settings, _ = h.app.GetSettings(ctx)
	tokens = 130
	if _, err := h.app.UpdateSettings(ctx, h.user, domain.SettingsPatch{BudgetTokens: &tokens}, settings.Revision); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 5*time.Second, "turn 2 after raise", func() bool { return len(fake.Turns()) == 2 })
}
