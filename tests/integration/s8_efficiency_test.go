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

// Cards handed to a busy member reach it together in one turn, even from
// different requirements, and a card it already handed off costs no turn.
func TestCardsShareATurnAndHandedOffOnesAreDropped(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"fullstack-engineer"})
	pm, dev := bots[0], bots[1]
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: dev.ID, Text: "先看看项目"}); err != nil {
		t.Fatal(err)
	}
	f := reviewFlow{t: t, h: h, fake: h.fakeFor(t, team.ID)}
	busy := f.waitTurn(dev, "先看看项目")

	// Three cards of three requirements arrive while the member works.
	ids := map[string]string{}
	for _, title := range []string{"卡片甲", "卡片乙", "卡片丙"} {
		card, _ := h.handCard(t, team, pm, dev, title, "完成")
		ids[title] = card.ID
	}
	// It sees one among its cards and hands it off right away.
	f.call(dev, "handoff", map[string]any{"cardId": ids["卡片甲"], "outcome": "done", "summary": "顺手做完了", "idempotencyKey": "s8-handoff"})
	f.finish(dev, busy, "")

	// The other two arrive in one turn; the handed-off one in none.
	both := f.waitTurn(dev, "你收到了 2 张接力卡片", "卡片乙", "卡片丙")
	if strings.Contains(both.Text, "【接力卡片】卡片甲") {
		t.Fatalf("the handed-off card was handed over again: %q", both.Text)
	}
	f.finish(dev, both, "")
	// The turn ended with both cards still open: one reminder names both,
	// one more follows on a fresh thread, and then nothing.
	reminder := f.waitTurn(dev, "这一轮结束了，但卡片还没有交接", "卡片乙", "卡片丙")
	f.finish(dev, reminder, "")
	again := f.waitTurn(dev, "上次提醒后还是没有交出", "卡片乙", "卡片丙")
	f.finish(dev, again, "")
	time.Sleep(300 * time.Millisecond)
	// The fresh thread is the member's too; only the project manager's
	// turns are someone else's.
	memberTurns := 0
	for _, turn := range f.fake.Turns() {
		if turn.ThreadID != f.thread(pm) {
			memberTurns++
		}
	}
	if memberTurns != 4 {
		t.Fatalf("member turns = %d, want 4", memberTurns)
	}
	for _, title := range []string{"卡片乙", "卡片丙"} {
		if got := f.task(ids[title]); got.Status != domain.TaskDoing {
			t.Fatalf("%s = %s, want doing once its turn started", title, got.Status)
		}
	}
	var stale string
	if err := h.app.DB.QueryRowContext(ctx, `SELECT status FROM deliveries WHERE task_id = ? AND kind = 'card'`, ids["卡片甲"]).Scan(&stale); err != nil || stale != domain.DeliveryCancelled {
		t.Fatalf("stale card delivery = %q, %v", stale, err)
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
