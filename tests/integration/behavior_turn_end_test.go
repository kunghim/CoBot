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

// What a member does next when a turn ends is decided in a fixed order: the
// queue, then its open questions, then a reminder about an open card. These
// tests hold that behavior still while the code around it moves.

func botStatus(t *testing.T, h *m2Harness, botID string) string {
	t.Helper()
	bot, err := sqlite.GetBot(context.Background(), h.app.DB, botID)
	if err != nil {
		t.Fatal(err)
	}
	return bot.Status
}

// settlesAt waits for a status and checks it holds once the wake that follows a
// finished turn has had its effect.
func settlesAt(t *testing.T, h *m2Harness, botID, want string) {
	t.Helper()
	waitUntil(t, 5*time.Second, "member "+want, func() bool { return botStatus(t, h, botID) == want })
	time.Sleep(200 * time.Millisecond)
	if got := botStatus(t, h, botID); got != want {
		t.Fatalf("member status = %s after settling at %s", got, want)
	}
}

func startTurn(t *testing.T, h *m2Harness, fake *fakeengine.Server, bot domain.Bot, text string) fakeengine.StartedTurn {
	t.Helper()
	before := len(fake.Turns())
	if _, err := h.app.SendMessage(context.Background(), h.user, app.SendMessageInput{BotID: bot.ID, Text: text}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 5*time.Second, text+" turn", func() bool { return len(fake.Turns()) > before })
	// The run has its turn id only once the start is acknowledged.
	waitUntil(t, 5*time.Second, text+" acknowledged", func() bool {
		run, err := sqlite.FindActiveRun(context.Background(), h.app.DB, bot.ID)
		return err == nil && run.ProviderTurnID != ""
	})
	return fake.Turns()[before]
}

func TestTurnEndWithNothingElseLeavesTheMemberIdle(t *testing.T) {
	for _, status := range []string{domain.RunCompleted, domain.RunFailed, domain.RunInterrupted} {
		t.Run(status, func(t *testing.T) {
			h := newM2Harness(t)
			team, bots := h.createTeam(t, []string{"fullstack-engineer"})
			fake := h.ensureFake(t, team.ID)
			dev := bots[1]
			turn := startTurn(t, h, fake, dev, "做点事")
			fake.EmitTurnCompleted(turn.ThreadID, turn.TurnID, status, "结果")
			settlesAt(t, h, dev.ID, domain.BotStatusIdle)
			if got := h.runStatus(t, dev.ID); got != status {
				t.Fatalf("run status = %s, want %s", got, status)
			}
		})
	}
}

// A message from the user joins the running turn, so what queues behind one is
// other work, such as a card.
func TestTurnEndTakesTheNextQueuedWork(t *testing.T) {
	h := newM2Harness(t)
	team, bots := h.createTeam(t, []string{"fullstack-engineer"})
	fake := h.ensureFake(t, team.ID)
	pm, dev := bots[0], bots[1]
	turn := startTurn(t, h, fake, dev, "第一件")
	h.handCard(t, team, pm, dev, "做页面", "能打开")
	fake.EmitTurnCompleted(turn.ThreadID, turn.TurnID, domain.RunCompleted, "好")
	waitUntil(t, 5*time.Second, "next turn", func() bool { return len(fake.Turns()) == 2 })
	settlesAt(t, h, dev.ID, domain.BotStatusWorking)
}

// A member with an unanswered question of its own waits for the user.
func TestTurnEndWithAnOpenQuestionWaitsForTheUser(t *testing.T) {
	h := newM2Harness(t)
	team, bots := h.createTeam(t, []string{"fullstack-engineer"})
	fake := h.ensureFake(t, team.ID)
	dev := bots[1]
	turn := startTurn(t, h, fake, dev, "做点事")
	if _, err := h.app.AskUser(context.Background(), auth.NewPrincipalBot(dev.ID, team.ID), app.AskUserInput{Question: "用哪种方式？", Options: []string{"甲", "乙"}}); err != nil {
		t.Fatal(err)
	}
	fake.EmitTurnCompleted(turn.ThreadID, turn.TurnID, domain.RunCompleted, "等你回答")
	settlesAt(t, h, dev.ID, domain.BotStatusWaitingInput)
}

// A recruitment request is not counted when the turn ends, only by the dispatch
// its wake runs right after; the member ends up waiting either way. The two
// places differ on purpose of this test: it pins today's result, not a design.
func TestTurnEndWithAnOpenRecruitmentWaitsForTheUser(t *testing.T) {
	h := newM2Harness(t)
	team, bots := h.createTeam(t, nil)
	fake := h.ensureFake(t, team.ID)
	pm := bots[0]
	turn := startTurn(t, h, fake, pm, "招人")
	requestMember(t, h, pm, []string{"fullstack-engineer"}, "实现环节没人做")
	fake.EmitTurnCompleted(turn.ThreadID, turn.TurnID, domain.RunCompleted, "等你同意")
	settlesAt(t, h, pm.ID, domain.BotStatusWaitingInput)
}

// Work that is queued but held by an open question does not start, and the
// member shows as waiting rather than queued.
func TestTurnEndWithBlockedWorkWaitsForTheUser(t *testing.T) {
	h := newM2Harness(t)
	team, bots := h.createTeam(t, []string{"fullstack-engineer"})
	fake := h.ensureFake(t, team.ID)
	pm, dev := bots[0], bots[1]
	turn := startTurn(t, h, fake, dev, "做点事")
	if _, err := h.app.AskUser(context.Background(), auth.NewPrincipalBot(dev.ID, team.ID), app.AskUserInput{Question: "用哪种方式？", Options: []string{"甲", "乙"}}); err != nil {
		t.Fatal(err)
	}
	h.handCard(t, team, pm, dev, "做页面", "能打开")
	if h.queuedForBot(t, dev.ID) == 0 {
		t.Fatal("the card should be queued behind the running turn")
	}
	fake.EmitTurnCompleted(turn.ThreadID, turn.TurnID, domain.RunCompleted, "等你回答")
	settlesAt(t, h, dev.ID, domain.BotStatusWaitingInput)
	if n := len(fake.Turns()); n != 1 {
		t.Fatalf("%d turns; the card must wait for the answer", n)
	}
}

// A recruitment card waits for the user just as a question does, and the member
// says so from the moment its turn ends, not only after the next dispatch.
func TestTurnEndWithAnOpenRecruitmentCardWaitsForTheUser(t *testing.T) {
	h := newM2Harness(t)
	team, bots := h.createTeam(t, []string{"fullstack-engineer"})
	fake := h.ensureFake(t, team.ID)
	pm := bots[0]
	turn := startTurn(t, h, fake, pm, "加个人")
	requestMember(t, h, pm, []string{"qa-engineer"}, "需要测试")
	fake.EmitTurnCompleted(turn.ThreadID, turn.TurnID, domain.RunCompleted, "等你决定")
	settlesAt(t, h, pm.ID, domain.BotStatusWaitingInput)
}

// Only a turn that completed is reminded about its card: a failed or stopped
// one has its own reason for not handing it on.
func TestOnlyACompletedTurnIsRemindedAboutItsCard(t *testing.T) {
	cases := map[string]int{domain.RunCompleted: 2, domain.RunFailed: 1, domain.RunInterrupted: 1}
	for status, turns := range cases {
		t.Run(status, func(t *testing.T) {
			h := newM2Harness(t)
			team, bots := h.createTeam(t, []string{"fullstack-engineer"})
			fake := h.ensureFake(t, team.ID)
			pm, dev := bots[0], bots[1]
			h.handCard(t, team, pm, dev, "做页面", "能打开")
			waitUntil(t, 5*time.Second, "card turn", func() bool { return len(fake.Turns()) == 1 })
			turn := fake.Turns()[0]
			waitUntil(t, 5*time.Second, "acknowledged", func() bool {
				run, err := sqlite.FindActiveRun(context.Background(), h.app.DB, dev.ID)
				return err == nil && run.ProviderTurnID != ""
			})
			fake.EmitTurnCompleted(turn.ThreadID, turn.TurnID, status, "")
			waitUntil(t, 5*time.Second, "turn over", func() bool { return h.runStatusAtLeast(t, dev.ID, turn) })
			time.Sleep(300 * time.Millisecond)
			if n := len(fake.Turns()); n != turns {
				t.Fatalf("%d turns after a %s turn, want %d", n, status, turns)
			}
		})
	}
}

// runStatusAtLeast reports that the given turn's run has ended.
func (h *m2Harness) runStatusAtLeast(t *testing.T, botID string, turn fakeengine.StartedTurn) bool {
	t.Helper()
	var ended int
	err := h.app.DB.QueryRow(`SELECT count(*) FROM runs WHERE bot_id = ? AND provider_turn_id = ? AND status NOT IN (?, ?)`,
		botID, turn.TurnID, domain.RunStarting, domain.RunRunning).Scan(&ended)
	if err != nil {
		t.Fatal(err)
	}
	return ended > 0
}

// A message still in line never reached the engine: editing it changes the
// words the next turn carries, and nothing else.
func TestEditingAQueuedMessageChangesWhatTheNextTurnCarries(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, nil)
	bot := bots[0]
	fake := h.ensureFake(t, team.ID)
	fake.SteerErr = errors.New("no active turn")
	first := startTurn(t, h, fake, bot, "第一条")
	queued, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: bot.ID, Text: "排队的原话"})
	if err != nil {
		t.Fatal(err)
	}
	if queued.DeliveryStatus != domain.DeliveryQueued {
		t.Fatalf("delivery = %s, want queued", queued.DeliveryStatus)
	}
	if _, err := h.app.EditMessage(ctx, h.user, queued.Message.ID, app.MessageEditText{RequestID: "edit-queued", Text: "改过的话"}); err != nil {
		t.Fatal(err)
	}
	fake.EmitTurnCompleted(first.ThreadID, first.TurnID, domain.RunCompleted, "好")
	waitUntil(t, 5*time.Second, "second turn", func() bool { return len(fake.Turns()) == 2 })
	if got := fake.Turns()[1].Text; !strings.Contains(got, "改过的话") || strings.Contains(got, "排队的原话") {
		t.Fatalf("second turn carried %q", got)
	}
}
