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
)

// Messages that queue up while a member works are taken into its next turn
// together: one turn, one wake on the chain, every delivery accepted.
func TestQueuedMemberMessagesMergeIntoOneTurn(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"backend-dev"})
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
	for _, want := range []string{"收到了 3 条消息", "【来自 队长】", "接口定了", "记得写测试", "完成后告诉我"} {
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
