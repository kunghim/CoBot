package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// TestChainBudgetGatesBotWakes walks the whole G01/G02 accounting loop with a
// chain limit of 3: bot-caused wakes count against one shared budget, the
// risk-zone wake waits for the user, a continue grants exactly that wake, and
// stopping cancels the chain's undelivered work.
func TestChainBudgetGatesBotWakes(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"backend-dev", "qa-engineer"})
	captain, member := bots[0], bots[1]
	fake := h.ensureFake(t, team.ID)

	// A compact budget: risk zone for limit 3 is wake 3 alone.
	settings, err := h.app.GetSettings(ctx)
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	limit := 3
	if _, err := h.app.UpdateSettings(ctx, h.user, domain.SettingsPatch{ChainLimit: &limit}, settings.Revision); err != nil {
		t.Fatalf("set chain limit: %v", err)
	}

	// The user opens the causal chain; this wake is user-caused and free.
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: captain.ID, Text: "开始协作"}); err != nil {
		t.Fatalf("user message: %v", err)
	}
	waitUntil(t, 5*time.Second, "captain turn 1", func() bool { return len(fake.Turns()) == 1 })

	// Wake 1: the captain pings the member from inside its own run.
	if _, err := h.app.SendMessageFromBot(ctx, auth.NewPrincipalBot(captain.ID, team.ID), member.ID, "第一棒", "", "req-w1"); err != nil {
		t.Fatalf("wake 1 send: %v", err)
	}
	fake.EmitTurnCompleted(fake.Turns()[0].ThreadID, fake.Turns()[0].TurnID, "completed", "已交接")
	waitUntil(t, 5*time.Second, "member turn (wake 1)", func() bool { return len(fake.Turns()) == 2 })

	// Wake 2: the member passes it back.
	if _, err := h.app.SendMessageFromBot(ctx, auth.NewPrincipalBot(member.ID, team.ID), captain.ID, "第二棒", "", "req-w2"); err != nil {
		t.Fatalf("wake 2 send: %v", err)
	}
	fake.EmitTurnCompleted(fake.Turns()[1].ThreadID, fake.Turns()[1].TurnID, "completed", "已交接")
	waitUntil(t, 5*time.Second, "captain turn (wake 2)", func() bool { return len(fake.Turns()) == 3 })

	// Wake 3 lands in the risk zone: it must not start; the user gets one
	// confirmation card for the chain instead.
	if _, err := h.app.SendMessageFromBot(ctx, auth.NewPrincipalBot(captain.ID, team.ID), member.ID, "第三棒", "", "req-w3"); err != nil {
		t.Fatalf("wake 3 send: %v", err)
	}
	fake.EmitTurnCompleted(fake.Turns()[2].ThreadID, fake.Turns()[2].TurnID, "completed", "等待确认")
	time.Sleep(200 * time.Millisecond)
	if len(fake.Turns()) != 3 {
		t.Fatalf("risk-zone wake started a turn: %d turns", len(fake.Turns()))
	}

	chains, err := sqlite.ListChains(ctx, h.app.DB, team.ID, "", 10)
	if err != nil || len(chains) != 1 {
		t.Fatalf("chains = %+v (err %v), want exactly the user-opened chain", chains, err)
	}
	chain := chains[0]
	if chain.WakeCount != 2 || chain.Status != domain.ChainAwaitingApproval {
		var deliveryStates string
		rows, qerr := h.app.DB.Query(`SELECT substr(target_bot_id,1,4), status, substr(chain_id,1,4) FROM deliveries ORDER BY created_at`)
		if qerr == nil {
			for rows.Next() {
				var tgt, st, ch string
				_ = rows.Scan(&tgt, &st, &ch)
				deliveryStates += fmt.Sprintf("[%s %s %s] ", tgt, st, ch)
			}
			rows.Close()
		}
		t.Fatalf("chain before decision = wakes %d status %s limit %d riskStart %d | captain=%s member=%s | deliveries %s",
			chain.WakeCount, chain.Status, chain.LimitCount, chain.RiskStart, captain.ID[:4], member.ID[:4], deliveryStates)
	}

	pending, err := h.app.PendingApprovals(ctx, h.user, team.ID, "")
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending approvals = %+v (err %v), want the one chain card", pending, err)
	}
	card := pending[0]
	if card.Type != domain.ApprovalChain || card.Reason != "risk_zone" {
		t.Fatalf("card = type %s reason %s, want chain/risk_zone", card.Type, card.Reason)
	}

	// Continue: exactly the bound wake is counted, the chain returns to
	// active with a new authorization epoch, and the member runs.
	if _, err := h.app.DecideApproval(ctx, h.user, card.ID, domain.ChainOptionContinue, "", card.Revision); err != nil {
		t.Fatalf("continue: %v", err)
	}
	waitUntil(t, 5*time.Second, "member turn (wake 3)", func() bool { return len(fake.Turns()) == 4 })
	chain, err = sqlite.GetChain(ctx, h.app.DB, chain.ID)
	if err != nil {
		t.Fatalf("chain after continue: %v", err)
	}
	if chain.WakeCount != 3 || chain.Status != domain.ChainActive || chain.Epoch != 2 {
		t.Fatalf("chain after continue = wakes %d status %s epoch %d, want 3/active/2", chain.WakeCount, chain.Status, chain.Epoch)
	}

	// Wake 4 exceeds the limit: another card, marked with the true trigger.
	if _, err := h.app.SendMessageFromBot(ctx, auth.NewPrincipalBot(member.ID, team.ID), captain.ID, "第四棒", "", "req-w4"); err != nil {
		t.Fatalf("wake 4 send: %v", err)
	}
	fake.EmitTurnCompleted(fake.Turns()[3].ThreadID, fake.Turns()[3].TurnID, "completed", "等待确认")
	waitUntil(t, 5*time.Second, "over-limit card", func() bool {
		pending, err := h.app.PendingApprovals(ctx, h.user, team.ID, "")
		return err == nil && len(pending) == 1 && pending[0].Reason == "over_limit"
	})
	pending, _ = h.app.PendingApprovals(ctx, h.user, team.ID, "")
	overCard := pending[0]

	// Stopping cancels the chain's undelivered work and interrupts what is
	// still running for this chain.
	if _, err := h.app.DecideApproval(ctx, h.user, overCard.ID, domain.ChainOptionStop, "", overCard.Revision); err != nil {
		t.Fatalf("stop: %v", err)
	}
	chain, err = sqlite.GetChain(ctx, h.app.DB, chain.ID)
	if err != nil {
		t.Fatalf("chain after stop: %v", err)
	}
	if chain.Status != domain.ChainStopped || chain.StopReason != "user_stopped" {
		t.Fatalf("chain after stop = %s/%s, want stopped/user_stopped", chain.Status, chain.StopReason)
	}
	var cancelled int
	err = h.app.DB.QueryRow(`SELECT COUNT(*) FROM deliveries WHERE chain_id = ? AND status = ?`,
		chain.ID, domain.DeliveryCancelled).Scan(&cancelled)
	if err != nil {
		t.Fatalf("count cancelled: %v", err)
	}
	if cancelled == 0 {
		t.Fatal("stop must cancel the chain's undelivered fourth wake")
	}
}

// TestUserMessageOpensNewChainWithoutClearingOld proves a second user message
// starts its own budget and never resets the chain still in flight (G02).
func TestUserMessageOpensNewChainWithoutClearingOld(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"backend-dev", "qa-engineer"})
	captain, member := bots[0], bots[1]
	fake := h.ensureFake(t, team.ID)

	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: captain.ID, Text: "第一条"}); err != nil {
		t.Fatalf("first user message: %v", err)
	}
	waitUntil(t, 5*time.Second, "captain turn", func() bool { return len(fake.Turns()) == 1 })
	if _, err := h.app.SendMessageFromBot(ctx, auth.NewPrincipalBot(captain.ID, team.ID), member.ID, "唤醒", "", "req-w1"); err != nil {
		t.Fatalf("bot send: %v", err)
	}

	// A second user message while the first chain still has a wake in
	// flight: new chain, old chain untouched.
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: member.ID, Text: "第二条"}); err != nil {
		t.Fatalf("second user message: %v", err)
	}
	chains, err := sqlite.ListChains(ctx, h.app.DB, team.ID, "", 10)
	if err != nil || len(chains) != 2 {
		t.Fatalf("chains = %d (err %v), want 2", len(chains), err)
	}
	// The bot-caused wake belongs to the first chain only.
	var first domain.Chain
	for _, c := range chains {
		if c.OriginBotID == "" {
			first = c
		}
	}
	if first.ID == "" || first.WakeCount != 1 {
		t.Fatalf("first chain = %+v, want the user-opened chain with one counted wake", first)
	}
}

// TestChainWakeDeduplicatesDispatchKey pins the store rule behind G01: the
// same dispatch key can never count twice on one chain.
func TestChainWakeDeduplicatesDispatchKey(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"backend-dev"})
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: bots[0].ID, Text: "hi"}); err != nil {
		t.Fatalf("message: %v", err)
	}
	chains, err := sqlite.ListChains(ctx, h.app.DB, team.ID, "", 1)
	if err != nil || len(chains) != 1 {
		t.Fatalf("chains = %+v (err %v)", chains, err)
	}
	chainID := chains[0].ID

	tx, err := h.app.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("tx: %v", err)
	}
	defer tx.Rollback()
	first, err := sqlite.CountChainWake(ctx, tx, chainID, "key-1", "d1", bots[0].ID)
	if err != nil || !first {
		t.Fatalf("first count = %v/%v, want true", first, err)
	}
	if err := sqlite.IncrementChainWake(ctx, tx, chainID); err != nil {
		t.Fatalf("increment: %v", err)
	}
	second, err := sqlite.CountChainWake(ctx, tx, chainID, "key-1", "d1", bots[0].ID)
	if err != nil || second {
		t.Fatalf("second count = %v/%v, want false (already counted)", second, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	chain, err := sqlite.GetChain(ctx, h.app.DB, chainID)
	if err != nil {
		t.Fatalf("get chain: %v", err)
	}
	if chain.WakeCount != 1 {
		t.Fatalf("wake count = %d, want 1 (duplicate key must not count)", chain.WakeCount)
	}
}
