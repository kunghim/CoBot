package integration

import (
	"context"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// TestRiskEvaluationContinuesAndFallsBackToUser walks G03 end to end: inside
// the risk zone the model judges in a separate read-only session — a clean
// continue counts the wake and dispatches it, an unclean answer falls back
// to the user's card — while a wake beyond the hard limit never reaches a
// model at all and goes straight to the user.
func TestRiskEvaluationContinuesAndFallsBackToUser(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"backend-dev", "qa-engineer"})
	captain, member := bots[0], bots[1]
	fake := h.ensureFake(t, team.ID)

	settings, err := h.app.GetSettings(ctx)
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	limit := 3
	if _, err := h.app.UpdateSettings(ctx, h.user, domain.SettingsPatch{ChainLimit: &limit}, settings.Revision); err != nil {
		t.Fatalf("chain limit: %v", err)
	}

	send := func(from, to domain.Bot, text, key string) {
		t.Helper()
		if _, err := h.app.SendMessageFromBot(ctx, auth.NewPrincipalBot(from.ID, team.ID), to.ID, text, "", key); err != nil {
			t.Fatalf("send %s: %v", key, err)
		}
	}
	completeLast := func() {
		t.Helper()
		turn := fake.Turns()[len(fake.Turns())-1]
		fake.EmitTurnCompleted(turn.ThreadID, turn.TurnID, "completed", "好的")
	}

	// Chain root: user message (free), then two budget wakes.
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: captain.ID, Text: "开始协作"}); err != nil {
		t.Fatalf("user msg: %v", err)
	}
	waitUntil(t, 5*time.Second, "turn 1", func() bool { return len(fake.Turns()) == 1 })
	captainThread := fake.Turns()[0].ThreadID
	send(captain, member, "第一棒", "w1")
	completeLast()
	waitUntil(t, 5*time.Second, "turn 2", func() bool { return len(fake.Turns()) == 2 })
	memberThread := fake.Turns()[1].ThreadID
	send(member, captain, "第二棒", "w2")
	completeLast()
	waitUntil(t, 5*time.Second, "turn 3", func() bool { return len(fake.Turns()) == 3 })

	// Wake 3 enters the risk zone: the chain evaluates instead of dispatching.
	send(captain, member, "第三棒", "w3")
	waitUntil(t, 5*time.Second, "eval turn", func() bool { return len(fake.Turns()) == 4 })
	// The captain's own turn (second wake) ends while the evaluation judges;
	// the one-run-per-bot rule keeps everything else honest meanwhile.
	captainTurn := fake.Turns()[2]
	fake.EmitTurnCompleted(captainTurn.ThreadID, captainTurn.TurnID, "completed", "好的")
	evalTurn := fake.Turns()[3]
	if evalTurn.ThreadID == captainThread || evalTurn.ThreadID == memberThread {
		t.Fatal("risk evaluation must run on its own session, not a member's thread")
	}
	chains, err := sqlite.ListChains(ctx, h.app.DB, team.ID, "", 10)
	if err != nil || len(chains) != 1 {
		t.Fatalf("chains = %+v (err %v)", chains, err)
	}
	chain := chains[0]
	if chain.Status != domain.ChainEvaluating {
		t.Fatalf("chain = %s, want evaluating while the model judges", chain.Status)
	}

	// The eval's spend is booked to the team as risk_eval, never as a member.
	fake.EmitTokenUsage(evalTurn.ThreadID, evalTurn.TurnID, 100, 20, 5)
	// A clean continue (with prose around the JSON) lets the wake proceed.
	fake.EmitTurnCompleted(evalTurn.ThreadID, evalTurn.TurnID, "completed", `方向清晰 {"decision":"continue","reason":"范围明确"}`)
	waitUntil(t, 5*time.Second, "wake 3 dispatched after model continue", func() bool { return len(fake.Turns()) == 5 })
	chain, err = sqlite.GetChain(ctx, h.app.DB, chain.ID)
	if err != nil {
		t.Fatalf("chain after eval: %v", err)
	}
	if chain.Status != domain.ChainActive || chain.WakeCount != 3 {
		t.Fatalf("chain after continue = %s wakes %d, want active/3", chain.Status, chain.WakeCount)
	}
	usage, err := sqlite.GetUsage(ctx, h.app.DB, fake.Epoch(), evalTurn.ThreadID, evalTurn.TurnID)
	if err != nil {
		t.Fatalf("eval usage: %v", err)
	}
	if usage.Purpose != domain.UsagePurposeRiskEval || usage.BotID != "" {
		t.Fatalf("eval usage = purpose %q bot %q, want risk_eval with no member", usage.Purpose, usage.BotID)
	}
	// Wake 4 is sent from inside wake 3's run and is past the hard limit: no
	// model is consulted; the user decides.
	send(member, captain, "第四棒", "w4")
	completeLast() // wake 3's member turn
	time.Sleep(300 * time.Millisecond)
	if len(fake.Turns()) != 5 {
		t.Fatalf("over-limit wake started work without the user: %d turns", len(fake.Turns()))
	}
	cards, err := h.app.PendingApprovals(ctx, h.user, team.ID, "")
	if err != nil || len(cards) != 1 {
		chainNow, _ := sqlite.GetChain(ctx, h.app.DB, chain.ID)
		var dump string
		rows, _ := h.app.DB.Query(`SELECT substr(target_bot_id,1,4), status, substr(chain_id,1,4) FROM deliveries ORDER BY created_at`)
		if rows != nil {
			for rows.Next() {
				var tgt, st, ch string
				_ = rows.Scan(&tgt, &st, &ch)
				dump += "[" + tgt + " " + st + " " + ch + "]"
			}
			rows.Close()
		}
		t.Fatalf("cards = %d (err %v), chain=%s wakes=%d limit=%d deliveries=%s captain=%s member=%s",
			len(cards), err, chainNow.Status, chainNow.WakeCount, chainNow.LimitCount, dump, captain.ID[:4], member.ID[:4])
	}
	if cards[0].Type != domain.ApprovalChain || cards[0].Reason != "over_limit" {
		t.Fatalf("card = %s/%s, want chain/over_limit", cards[0].Type, cards[0].Reason)
	}
	if _, err := h.app.DecideApproval(ctx, h.user, cards[0].ID, domain.ChainOptionContinue, "", cards[0].Revision); err != nil {
		t.Fatalf("user continue: %v", err)
	}
	waitUntil(t, 5*time.Second, "granted wake dispatched", func() bool { return len(fake.Turns()) == 6 })
	completeLast()

	// A second chain exercises the ask fallback: the model answers with
	// prose the product cannot act on, so the user decides.
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: captain.ID, Text: "再来一轮"}); err != nil {
		t.Fatalf("second user msg: %v", err)
	}
	waitUntil(t, 5*time.Second, "chain 2 turn 1", func() bool { return len(fake.Turns()) == 7 })
	send(captain, member, "新一轮第一棒", "c1")
	completeLast()
	waitUntil(t, 5*time.Second, "chain 2 turn 2", func() bool { return len(fake.Turns()) == 8 })
	send(member, captain, "新一轮第二棒", "c2")
	completeLast()
	waitUntil(t, 5*time.Second, "chain 2 turn 3", func() bool { return len(fake.Turns()) == 9 })
	send(captain, member, "新一轮第三棒", "c3")
	waitUntil(t, 5*time.Second, "chain 2 eval turn", func() bool { return len(fake.Turns()) == 10 })
	secondEval := fake.Turns()[9]
	fake.EmitTurnCompleted(secondEval.ThreadID, secondEval.TurnID, "completed", "这个我判断不了，交给你决定")
	waitUntil(t, 5*time.Second, "ask fallback card", func() bool {
		pending, err := h.app.PendingApprovals(ctx, h.user, team.ID, "")
		return err == nil && len(pending) == 1 && pending[0].Type == domain.ApprovalChain && pending[0].Reason == "risk_zone"
	})
	if len(fake.Turns()) != 10 {
		t.Fatalf("ask fallback dispatched anyway: %d turns", len(fake.Turns()))
	}
	pending, _ := h.app.PendingApprovals(ctx, h.user, team.ID, "")
	if _, err := h.app.DecideApproval(ctx, h.user, pending[0].ID, domain.ChainOptionContinue, "", pending[0].Revision); err != nil {
		t.Fatalf("fallback continue: %v", err)
	}
	waitUntil(t, 5*time.Second, "user-approved wake after fallback", func() bool { return len(fake.Turns()) == 11 })
}
