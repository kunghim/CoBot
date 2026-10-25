package integration

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
	"github.com/kunghim/CoBot/tests/integration/fakeengine"
)

// Messages that queue up while a member works are taken into its next turn
// together: one turn, one wake on the chain, every delivery accepted.
func TestQueuedMemberMessagesMergeIntoOneTurn(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"fullstack-engineer"})
	captain, member := bots[0], bots[1]
	fake := h.ensureFake(t, team.ID)

	// The member is busy with the user's request.
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: member.ID, Text: "先做登录页"}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 5*time.Second, "member busy", func() bool { return len(fake.Turns()) == 1 })
	memberTurn := fake.Turns()[0]

	// The captain, inside its own run, pings the member three times.
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: captain.ID, Text: "协调一下"}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 5*time.Second, "captain running", func() bool { return len(fake.Turns()) == 2 })
	for i, text := range []string{"接口定了", "记得写测试", "完成后告诉我"} {
		if _, err := h.app.SendMessageFromBot(ctx, auth.NewPrincipalBot(captain.ID, team.ID), member.ID, text, "", "req-batch-"+string(rune('a'+i))); err != nil {
			t.Fatal(err)
		}
	}

	// The member finishes its turn: the three pings start ONE turn.
	fake.EmitTurnCompleted(memberTurn.ThreadID, memberTurn.TurnID, "completed", "登录页做好了")
	waitUntil(t, 5*time.Second, "merged turn", func() bool { return len(fake.Turns()) == 3 })
	merged := fake.Turns()[2]
	if merged.ThreadID != memberTurn.ThreadID {
		t.Fatalf("third turn is not the member's: %+v", merged)
	}
	for _, want := range []string{"收到了 3 条消息", "【来自 项目经理】", "接口定了", "记得写测试", "完成后告诉我"} {
		if !strings.Contains(merged.Text, want) {
			t.Fatalf("merged input lacks %q:\n%s", want, merged.Text)
		}
	}
	time.Sleep(300 * time.Millisecond)
	if n := len(fake.Turns()); n != 3 {
		t.Fatalf("turns = %d, want the pings in one turn", n)
	}

	var runIDs, accepted int
	if err := h.app.DB.QueryRow(`SELECT count(DISTINCT d.run_id), sum(d.status = ?) FROM deliveries d JOIN messages m ON m.id = d.message_id WHERE d.target_bot_id = ? AND m.source_bot_id = ?`,
		domain.DeliveryAccepted, member.ID, captain.ID).Scan(&runIDs, &accepted); err != nil {
		t.Fatal(err)
	}
	if runIDs != 1 || accepted != 3 {
		t.Fatalf("deliveries: %d runs, %d accepted; want 1 run, 3 accepted", runIDs, accepted)
	}
	chains, err := sqlite.ListChains(ctx, h.app.DB, team.ID, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, chain := range chains {
		if chain.OriginBotID == captain.ID && chain.WakeCount != 1 {
			t.Fatalf("the merged turn counted %d wakes, want 1", chain.WakeCount)
		}
	}
}

// A user message to a member that is working joins its running turn; no
// new turn starts for it.
func TestUserMessageSteersRunningTurn(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, nil)
	bot := bots[0]
	fake := h.ensureFake(t, team.ID)

	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: bot.ID, Text: "写一个登录页"}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 5*time.Second, "turn running", func() bool {
		run, err := sqlite.FindActiveRun(ctx, h.app.DB, bot.ID)
		return err == nil && run.Status == domain.RunRunning
	})
	turn := fake.Turns()[0]

	result, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: bot.ID, Text: "顺便加上记住我"})
	if err != nil {
		t.Fatal(err)
	}
	if result.DeliveryStatus != domain.DeliveryAccepted {
		t.Fatalf("steered message status = %s", result.DeliveryStatus)
	}
	steers := fake.Steers()
	if len(steers) != 1 || steers[0].TurnID != turn.TurnID || steers[0].Text != "顺便加上记住我" {
		t.Fatalf("steers = %+v", steers)
	}
	fake.EmitTurnCompleted(turn.ThreadID, turn.TurnID, "completed", "都做好了")
	time.Sleep(300 * time.Millisecond)
	if n := len(fake.Turns()); n != 1 {
		t.Fatalf("turns = %d: the steered message started its own turn", n)
	}
}

// When the engine refuses (the turn just ended), user messages queue and the
// ones that waited run together in the next turn.
func TestRefusedSteerQueuesAndUserMessagesMerge(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, nil)
	bot := bots[0]
	fake := h.ensureFake(t, team.ID)
	fake.SteerErr = errors.New("no active turn")

	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: bot.ID, Text: "第一件事"}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 5*time.Second, "turn running", func() bool {
		run, err := sqlite.FindActiveRun(ctx, h.app.DB, bot.ID)
		return err == nil && run.Status == domain.RunRunning
	})
	for _, text := range []string{"第二件事", "第三件事"} {
		result, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: bot.ID, Text: text})
		if err != nil || result.DeliveryStatus != domain.DeliveryQueued {
			t.Fatalf("refused steer: %+v %v", result, err)
		}
	}
	turn := fake.Turns()[0]
	fake.EmitTurnCompleted(turn.ThreadID, turn.TurnID, "completed", "第一件做完")
	waitUntil(t, 5*time.Second, "next turn", func() bool { return len(fake.Turns()) == 2 })
	next := fake.Turns()[1].Text
	if !strings.Contains(next, "收到了 2 条消息") || !strings.Contains(next, "第二件事") || !strings.Contains(next, "第三件事") || !strings.Contains(next, "【来自 用户】") {
		t.Fatalf("queued user messages were not merged:\n%s", next)
	}
}

// One send_message reaches several members; it is left for each
// recipient's next turn and wakes nobody.
func TestBroadcastNotesWakeNobody(t *testing.T) {
	h := newM2Harness(t)
	team, bots := h.createTeam(t, []string{"fullstack-engineer", "qa-engineer"})
	captain, backend, qa := bots[0], bots[1], bots[2]
	token := h.botToken(t, captain)
	fake := h.ensureFake(t, team.ID)

	if status, body := h.callTool(t, token, "send_message", map[string]any{"targetBotIds": []string{backend.ID, qa.ID}, "text": "接口文档已更新", "idempotencyKey": "s4-note"}); status != 200 {
		t.Fatalf("notify = %d %v", status, body)
	}
	time.Sleep(300 * time.Millisecond)
	if n := len(fake.Turns()); n != 0 {
		t.Fatalf("a notify-only message woke %d turns", n)
	}
	var deliveries int
	if err := h.app.DB.QueryRow(`SELECT count(*) FROM deliveries d JOIN messages m ON m.id = d.message_id WHERE m.source_bot_id = ?`, captain.ID).Scan(&deliveries); err != nil || deliveries != 0 {
		t.Fatalf("notify-only created %d deliveries (%v)", deliveries, err)
	}

	if status, body := h.callTool(t, token, "send_message", map[string]any{"targetBotIds": []string{backend.ID, qa.ID, backend.ID}, "text": "开始联调", "idempotencyKey": "s4-all"}); status != 200 {
		t.Fatalf("broadcast = %d %v", status, body)
	}
	// The user wakes both; the notes reach each with its turn.
	for _, bot := range []domain.Bot{backend, qa} {
		if _, err := h.app.SendMessage(context.Background(), h.user, app.SendMessageInput{BotID: bot.ID, Text: "看下留言"}); err != nil {
			t.Fatal(err)
		}
	}
	waitUntil(t, 5*time.Second, "both woken", func() bool { return len(fake.Turns()) == 2 })
	for _, turn := range fake.Turns() {
		if !strings.Contains(turn.Text, "开始联调") || !strings.Contains(turn.Text, "接口文档已更新") {
			t.Fatalf("turn input lacks the message or the earlier note:\n%s", turn.Text)
		}
	}
	// A retried broadcast is idempotent per recipient.
	if status, _ := h.callTool(t, token, "send_message", map[string]any{"targetBotIds": []string{backend.ID, qa.ID}, "text": "开始联调", "idempotencyKey": "s4-all"}); status != 200 {
		t.Fatalf("retry status %d", status)
	}
	var messages int
	if err := h.app.DB.QueryRow(`SELECT count(*) FROM messages WHERE source_bot_id = ? AND blocks LIKE '%开始联调%'`, captain.ID).Scan(&messages); err != nil || messages != 2 {
		t.Fatalf("broadcast messages = %d (%v), want one per recipient", messages, err)
	}
}

// Opening a team starts its engine before any message, and starts no turn.
func TestWarmTeamStartsEngineAhead(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, _ := h.createTeam(t, nil)
	if h.app.EngineEpochOfTeam(team.ID) != "" {
		t.Fatal("engine running before warm-up")
	}
	result, err := h.app.WarmTeam(ctx, h.user, team.ID)
	if err != nil || result["warming"] != true {
		t.Fatalf("warm = %v %v", result, err)
	}
	waitUntil(t, 5*time.Second, "engine warm", func() bool { return h.app.EngineEpochOfTeam(team.ID) != "" })
	if turns := h.fakeFor(t, team.ID).Turns(); len(turns) != 0 {
		t.Fatalf("warm-up started turns: %+v", turns)
	}
	// Warming again is a no-op on a running engine.
	epoch := h.app.EngineEpochOfTeam(team.ID)
	if _, err := h.app.WarmTeam(ctx, h.user, team.ID); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if h.app.EngineEpochOfTeam(team.ID) != epoch {
		t.Fatal("a second warm-up replaced the engine")
	}
}

// A member handing its card off wakes the next stage's owner with the next
// card, sent in the member's name; the member's own reply does not.
func TestHandoffWakesTheNextStage(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"fullstack-engineer"})
	leader, member := bots[0], bots[1]
	fake := h.ensureFake(t, team.ID)

	card, _ := h.handCard(t, team, leader, member, "登录接口", "返回 200")
	waitUntil(t, 5*time.Second, "member handed the card", func() bool { return len(fake.Turns()) == 1 })
	memberTurn := fake.Turns()[0]
	as := auth.NewPrincipalBot(member.ID, team.ID)
	// Picking the card up marks it as started; the member does not.
	waitUntil(t, 5*time.Second, "card started", func() bool {
		started, _ := sqlite.GetTask(ctx, h.app.DB, card.ID)
		return started.Status == domain.TaskDoing
	})
	if _, err := h.app.Handoff(ctx, as, app.HandoffInput{CardID: card.ID, Outcome: domain.HandoffDone, Summary: "接口已实现，go test ./... 通过"}); err != nil {
		t.Fatal(err)
	}
	fake.EmitTurnCompleted(memberTurn.ThreadID, memberTurn.TurnID, "completed", "已交接")

	waitUntil(t, 5*time.Second, "leader woken", func() bool { return len(fake.Turns()) == 2 })
	next := fake.Turns()[1]
	if !strings.Contains(next.Text, "【接力卡片】验收汇报") || !strings.Contains(next.Text, "接口已实现，go test ./... 通过") || !strings.Contains(next.Text, "handoff") {
		t.Fatalf("leader input:\n%s", next.Text)
	}
	// Card messages carry their card in run_id; the notes of the user's
	// decisions do not, so they stay out of this count.
	var leaderMessages int
	if err := h.app.DB.QueryRow(`SELECT count(*) FROM messages WHERE bot_id = ? AND kind = 'system' AND run_id != ''`, leader.ID).Scan(&leaderMessages); err != nil || leaderMessages != 1 {
		t.Fatalf("leader cards = %d (%v)", leaderMessages, err)
	}
	// The leader's conversation shows the card as coming from the member.
	page, err := h.app.ListPage(ctx, h.user, app.PageRequest{Kind: "messages", BotID: leader.ID})
	if err != nil {
		t.Fatal(err)
	}
	var sawCard bool
	for _, item := range page["messages"].([]any) {
		if message := item.(domain.Message); message.Kind == domain.MessageKindSystem {
			sawCard = message.SourceBotID == member.ID
		}
	}
	if !sawCard {
		t.Fatal("leader page: the card does not name the member")
	}
	// The record between the two members carries both cards: the first one
	// the confirmed route dispatched in the project manager's name, and the
	// acceptance card the member handed off in its own name.
	record, err := h.app.ListPage(ctx, h.user, app.PageRequest{Kind: "collaboration", TeamID: team.ID, BotID: member.ID, PeerBotID: leader.ID})
	if err != nil {
		t.Fatal(err)
	}
	items := record["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("exchange record = %+v", items)
	}
	first, second := items[0].(domain.Message), items[1].(domain.Message)
	if first.Kind != domain.MessageKindSystem || first.SourceBotID != leader.ID ||
		second.Kind != domain.MessageKindSystem || second.SourceBotID != member.ID {
		t.Fatalf("exchange record = %s then %s (%s, %s)", first.SourceBotID, second.SourceBotID, first.Kind, second.Kind)
	}
}

// A member's later turns go straight to its loaded thread; only a thread the
// engine no longer has is resumed first, and one it cannot resume is replaced.
func TestLoadedThreadIsNotResumedBeforeEachTurn(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, nil)
	bot := bots[0]
	fake := h.ensureFake(t, team.ID)

	turn := func(text string, want int) fakeengine.StartedTurn {
		t.Helper()
		if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: bot.ID, Text: text}); err != nil {
			t.Fatal(err)
		}
		waitUntil(t, 5*time.Second, "turn started", func() bool { return len(fake.Turns()) == want })
		started := fake.Turns()[want-1]
		fake.EmitTurnCompleted(started.ThreadID, started.TurnID, "completed", "好")
		waitUntil(t, 5*time.Second, "turn finished", func() bool {
			_, err := sqlite.FindActiveRun(ctx, h.app.DB, bot.ID)
			return errors.Is(err, sqlite.ErrNotFound)
		})
		return started
	}
	first := turn("第一件事", 1)
	second := turn("第二件事", 2)
	if second.ThreadID != first.ThreadID || fake.Resumes() != 0 {
		t.Fatalf("second turn: thread %s→%s, resumes %d", first.ThreadID, second.ThreadID, fake.Resumes())
	}
	fake.Unload(first.ThreadID)
	third := turn("第三件事", 3)
	if third.ThreadID != first.ThreadID || fake.Resumes() != 1 {
		t.Fatalf("after unload: thread %s, resumes %d", third.ThreadID, fake.Resumes())
	}
	// A thread the engine cannot resume is replaced by a new one, told the
	// full instructions again.
	fake.Unload(first.ThreadID)
	fake.ResumeErr = errors.New("thread not found")
	threads := len(fake.Instructions())
	fourth := turn("第四件事", 4)
	if fourth.ThreadID == first.ThreadID || len(fake.Instructions()) != threads+1 {
		t.Fatalf("after failed resume: thread %s, threads started %d→%d", fourth.ThreadID, threads, len(fake.Instructions()))
	}
}
