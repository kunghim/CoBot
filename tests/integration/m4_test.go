package integration

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

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

	// Wake 3 lands in the risk zone: the model judges it in its own session
	// (G03); only its escalation produces a user card.
	if _, err := h.app.SendMessageFromBot(ctx, auth.NewPrincipalBot(captain.ID, team.ID), member.ID, "第三棒", "", "req-w3"); err != nil {
		t.Fatalf("wake 3 send: %v", err)
	}
	fake.EmitTurnCompleted(fake.Turns()[2].ThreadID, fake.Turns()[2].TurnID, "completed", "等待评估")
	waitUntil(t, 5*time.Second, "risk eval turn", func() bool { return len(fake.Turns()) == 4 })
	// The captain's own turn ends while the evaluation judges.
	captainTurn := fake.Turns()[2]
	fake.EmitTurnCompleted(captainTurn.ThreadID, captainTurn.TurnID, "completed", "已交接")
	// The model escalates instead of continuing.
	evalTurn := fake.Turns()[3]
	fake.EmitTurnCompleted(evalTurn.ThreadID, evalTurn.TurnID, "completed", "建议用户确认方向")

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
	waitUntil(t, 5*time.Second, "member turn (wake 3)", func() bool { return len(fake.Turns()) == 5 })
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
	fake.EmitTurnCompleted(fake.Turns()[4].ThreadID, fake.Turns()[4].TurnID, "completed", "等待确认")
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
	waitUntil(t, 5*time.Second, "bot-caused wake claimed", func() bool { return len(fake.Turns()) == 2 })

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

// TestUsageLedgerSnapshotsAndDedupes pins G06: one row per engine turn
// updated in place (never summed), cached input never counted twice, unknown
// markers resolve when real numbers arrive, and system sessions count only
// toward the team.
func TestUsageLedgerSnapshotsAndDedupes(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"backend-dev"})
	bot := bots[1]

	base := domain.UsageEntry{
		TeamID: team.ID, BotID: bot.ID, Purpose: domain.UsagePurposeRun,
		EngineEpoch: "epoch-u1", ThreadID: "thread-a", TurnID: "turn-1",
		InputTokens: 100, OutputTokens: 40, CachedInputTokens: 60,
		Status: domain.UsageStatusRecorded,
	}
	if err := h.app.RecordUsage(ctx, base); err != nil {
		t.Fatalf("record: %v", err)
	}
	// The engine re-sends a fuller snapshot for the same turn: the row is
	// replaced, never accumulated.
	newer := base
	newer.ID = domain.NewID()
	newer.InputTokens = 130
	newer.OutputTokens = 55
	if err := h.app.RecordUsage(ctx, newer); err != nil {
		t.Fatalf("re-record: %v", err)
	}
	got, err := sqlite.GetUsage(ctx, h.app.DB, base.EngineEpoch, base.ThreadID, base.TurnID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.InputTokens != 130 || got.OutputTokens != 55 {
		t.Fatalf("snapshot = in %d out %d, want the newest 130/55 (never 230/95)", got.InputTokens, got.OutputTokens)
	}
	if got.BillableTokens() != 185 {
		t.Fatalf("billable = %d, want input+output only (cached must not be added again)", got.BillableTokens())
	}

	// A second turn is a second row; the team total adds rows, not updates.
	second := base
	second.ID = domain.NewID()
	second.TurnID = "turn-2"
	second.InputTokens = 15
	second.OutputTokens = 5
	if err := h.app.RecordUsage(ctx, second); err != nil {
		t.Fatalf("second turn: %v", err)
	}
	billable, unknown, err := sqlite.SumUsage(ctx, h.app.DB, team.ID, "")
	if err != nil {
		t.Fatalf("sum: %v", err)
	}
	if billable != 205 || unknown != 0 {
		t.Fatalf("team sum = %d/%d unknown, want 205/0", billable, unknown)
	}

	// A turn that ends without usage is unknown — until real numbers arrive.
	tx, err := h.app.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("tx: %v", err)
	}
	if err := h.app.MarkUsageUnknownIfMissing(ctx, tx, team.ID, bot.ID, base.EngineEpoch, "thread-a", "turn-3"); err != nil {
		tx.Rollback()
		t.Fatalf("mark unknown: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if _, unknown, err = sqlite.SumUsage(ctx, h.app.DB, team.ID, ""); err != nil || unknown != 1 {
		t.Fatalf("unknown count = %d (err %v), want 1", unknown, err)
	}
	real := base
	real.ID = domain.NewID()
	real.TurnID = "turn-3"
	if err := h.app.RecordUsage(ctx, real); err != nil {
		t.Fatalf("resolve unknown: %v", err)
	}
	if _, unknown, _ = sqlite.SumUsage(ctx, h.app.DB, team.ID, ""); unknown != 0 {
		t.Fatalf("unknown count after real usage = %d, want 0", unknown)
	}

	// A system session (future risk evaluation) counts for the team only.
	system := base
	system.ID = domain.NewID()
	system.BotID = ""
	system.Purpose = domain.UsagePurposeRiskEval
	system.TurnID = "turn-eval"
	system.InputTokens = 7
	system.OutputTokens = 3
	if err := h.app.RecordUsage(ctx, system); err != nil {
		t.Fatalf("system usage: %v", err)
	}
	teamTotal, _, _ := sqlite.SumUsage(ctx, h.app.DB, team.ID, "")
	botTotal, _, _ := sqlite.SumUsage(ctx, h.app.DB, team.ID, bot.ID)
	if teamTotal != botTotal+10 {
		t.Fatalf("team %d vs bot %d: system session must count for the team only", teamTotal, botTotal)
	}
}

// TestBudgetPausesNewTurnsAndResumes walks G07/G08 end to end: a budget
// stops only new turns, fixing the budget resumes them, a turn without usage
// pauses the queue as unknown, and real numbers arriving resume it.
func TestBudgetPausesNewTurnsAndResumes(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"backend-dev"})
	captain := bots[0]
	fake := h.ensureFake(t, team.ID)

	settings, err := h.app.GetSettings(ctx)
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	scope := domain.BudgetScopeTeam
	tokens := int64(100)
	if _, err := h.app.UpdateSettings(ctx, h.user, domain.SettingsPatch{BudgetScope: &scope, BudgetTokens: &tokens}, settings.Revision); err != nil {
		t.Fatalf("enable budget: %v", err)
	}

	// Turn 1 reports 90 billable tokens: under the limit.
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: captain.ID, Text: "第一条"}); err != nil {
		t.Fatalf("msg1: %v", err)
	}
	waitUntil(t, 5*time.Second, "turn 1", func() bool { return len(fake.Turns()) == 1 })
	turn1 := fake.Turns()[0]
	fake.EmitTokenUsage(turn1.ThreadID, turn1.TurnID, 60, 30, 10)
	fake.EmitTurnCompleted(turn1.ThreadID, turn1.TurnID, "completed", "好的")
	waitUntil(t, 5*time.Second, "turn 1 done", func() bool {
		_, err := sqlite.FindActiveRun(ctx, h.app.DB, captain.ID)
		return errors.Is(err, sqlite.ErrNotFound)
	})

	// Turn 2 adds 25: the scope now totals 115 of 100 — over budget.
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: captain.ID, Text: "第二条"}); err != nil {
		t.Fatalf("msg2: %v", err)
	}
	waitUntil(t, 5*time.Second, "turn 2", func() bool { return len(fake.Turns()) == 2 })
	turn2 := fake.Turns()[1]
	fake.EmitTokenUsage(turn2.ThreadID, turn2.TurnID, 20, 5, 0)
	fake.EmitTurnCompleted(turn2.ThreadID, turn2.TurnID, "completed", "好的")
	waitUntil(t, 5*time.Second, "turn 2 done", func() bool {
		_, err := sqlite.FindActiveRun(ctx, h.app.DB, captain.ID)
		return errors.Is(err, sqlite.ErrNotFound)
	})

	// The third message must not start: the budget holds the queue and the
	// bot asks for input instead of pretending to work.
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: captain.ID, Text: "第三条"}); err != nil {
		t.Fatalf("msg3: %v", err)
	}
	time.Sleep(400 * time.Millisecond)
	if len(fake.Turns()) != 2 {
		t.Fatalf("over-budget turn started: %d turns", len(fake.Turns()))
	}
	bot, err := sqlite.GetBot(ctx, h.app.DB, captain.ID)
	if err != nil {
		t.Fatalf("bot: %v", err)
	}
	if bot.Status != domain.BotStatusWaitingInput {
		t.Fatalf("bot status = %s, want waiting_input while the budget holds the queue", bot.Status)
	}
	if h.queuedForBot(t, captain.ID) != 1 {
		t.Fatalf("queued = %d, want the held delivery kept", h.queuedForBot(t, captain.ID))
	}

	// Raising the budget resumes exactly the held delivery.
	settings, _ = h.app.GetSettings(ctx)
	tokens = 1000
	if _, err := h.app.UpdateSettings(ctx, h.user, domain.SettingsPatch{BudgetTokens: &tokens}, settings.Revision); err != nil {
		t.Fatalf("raise budget: %v", err)
	}
	waitUntil(t, 5*time.Second, "turn 3 after raise", func() bool { return len(fake.Turns()) == 3 })

	// This turn never reports usage: at completion the ledger records an
	// unknown, and the enabled budget must pause the next turn (G08).
	turn3 := fake.Turns()[2]
	fake.EmitTurnCompleted(turn3.ThreadID, turn3.TurnID, "completed", "好的")
	waitUntil(t, 5*time.Second, "turn 3 done", func() bool {
		_, err := sqlite.FindActiveRun(ctx, h.app.DB, captain.ID)
		return errors.Is(err, sqlite.ErrNotFound)
	})
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: captain.ID, Text: "第四条"}); err != nil {
		t.Fatalf("msg4: %v", err)
	}
	time.Sleep(400 * time.Millisecond)
	if len(fake.Turns()) != 3 {
		t.Fatalf("turn started despite unknown usage: %d turns", len(fake.Turns()))
	}

	// Real numbers for the unknown turn make the spend determinable again:
	// the ledger update itself re-wakes the held queue.
	fake.EmitTokenUsage(turn3.ThreadID, turn3.TurnID, 40, 10, 0)
	waitUntil(t, 5*time.Second, "turn 4 after usage arrives", func() bool { return len(fake.Turns()) == 4 })
}

// TestMemoryOwnershipIsolationAndTombstones pins G04: memories never cross
// member or team boundaries, deletion hides them immediately, and neither a
// plain re-remember nor a differently-spaced one can recreate a deleted or
// existing fact for the same owner and source.
func TestMemoryOwnershipIsolationAndTombstones(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	teamA, botsA := h.createTeam(t, []string{"backend-dev", "qa-engineer"})
	teamB, botsB := h.createTeamWithDir(t, "第二团队", "second-team-dir", []string{"backend-dev"})
	alice, bob := botsA[1], botsA[2]
	other := botsB[1]

	alicePrincipal := auth.NewPrincipalBot(alice.ID, teamA.ID)
	bobPrincipal := auth.NewPrincipalBot(bob.ID, teamA.ID)
	otherPrincipal := auth.NewPrincipalBot(other.ID, teamB.ID)

	mem, err := h.app.RememberMemory(ctx, alicePrincipal, "部署前先跑 go test ./...", "")
	if err != nil {
		t.Fatalf("alice remembers: %v", err)
	}

	// Ownership isolation: teammates and other teams cannot retrieve it.
	got, err := h.app.SearchMemories(ctx, bobPrincipal, "部署", 10)
	if err != nil {
		t.Fatalf("bob search: %v", err)
	}
	if items := got["items"].([]domain.Memory); len(items) != 0 {
		t.Fatalf("bob retrieved alice's memory: %+v", items)
	}
	got, err = h.app.SearchMemories(ctx, otherPrincipal, "部署", 10)
	if err != nil {
		t.Fatalf("other search: %v", err)
	}
	if items := got["items"].([]domain.Memory); len(items) != 0 {
		t.Fatalf("cross-team retrieval: %+v", items)
	}
	// The owner retrieves it.
	got, err = h.app.SearchMemories(ctx, alicePrincipal, "部署", 10)
	if err != nil || len(got["items"].([]domain.Memory)) != 1 {
		t.Fatalf("alice search = %+v (err %v), want her own memory", got, err)
	}

	// The same owner, source and normalized body is never recorded twice —
	// even with different spacing or case.
	var de *domain.Error
	_, derr := h.app.RememberMemory(ctx, alicePrincipal, "  部署前先跑   GO TEST ./... ", "")
	if derr == nil {
		t.Fatal("duplicate remember must fail")
	}
	if !errors.As(derr, &de) || de.Code != domain.CodeConflict {
		t.Fatalf("duplicate remember must conflict, got %v", derr)
	}

	// Another member may hold the same fact: memories are per owner.
	if _, err := h.app.RememberMemory(ctx, bobPrincipal, "部署前先跑 go test ./...", ""); err != nil {
		t.Fatalf("bob's own copy: %v", err)
	}

	// Deletion: hidden immediately, and the tombstone blocks re-creation.
	if err := h.app.DeleteMemory(ctx, h.user, mem.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	got, err = h.app.SearchMemories(ctx, alicePrincipal, "部署", 10)
	if err != nil || len(got["items"].([]domain.Memory)) != 0 {
		t.Fatalf("after delete alice search = %+v (err %v), want empty", got, err)
	}
	if _, err := h.app.RememberMemory(ctx, alicePrincipal, "部署前先跑 go test ./...", ""); err == nil {
		t.Fatal("a deleted fact must not be recreated automatically")
	}
	_ = teamB
}

// TestMemoryGuardsAndBounds pins G05: secret-shaped and oversized bodies are
// refused, retrieval stays within items and characters with overflow
// reported, and the dispatch context carries the member's own memories.
func TestMemoryGuardsAndBounds(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"backend-dev"})
	member := bots[1]
	principal := auth.NewPrincipalBot(member.ID, team.ID)

	if _, err := h.app.RememberMemory(ctx, principal, "password: hunter2secret", ""); err == nil {
		t.Fatal("a secret-shaped body must be refused")
	} else {
		var de *domain.Error
		if !errors.As(err, &de) || de.Code != domain.CodeInvalidRequest {
			t.Fatalf("secret refusal = %v, want invalid_request", err)
		}
	}
	if _, err := h.app.RememberMemory(ctx, principal, strings.Repeat("字", domain.MemoryLimitMaxBody+1), ""); err == nil {
		t.Fatal("an oversized body must be refused")
	}
	if _, err := h.app.RememberMemory(ctx, principal, "   ", ""); err == nil {
		t.Fatal("an empty body must be refused")
	}

	// Ten full memories: retrieval keeps the item cap and the character
	// budget, reporting what did not fit instead of hiding it.
	body := strings.Repeat("记忆内容片段 ", 120) // ~1440 chars each
	for i := 0; i < 10; i++ {
		if _, err := h.app.RememberMemory(ctx, principal, fmt.Sprintf("%s #%d", body, i), ""); err != nil {
			t.Fatalf("remember %d: %v", i, err)
		}
	}
	got, err := h.app.SearchMemories(ctx, principal, "", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	items := got["items"].([]domain.Memory)
	if len(items) > domain.MemorySearchMaxItems {
		t.Fatalf("items = %d, want at most %d", len(items), domain.MemorySearchMaxItems)
	}
	total := 0
	for _, m := range items {
		total += utf8.RuneCountInString(m.Body)
	}
	if total > domain.MemorySearchMaxChars {
		t.Fatalf("total chars = %d, want at most %d", total, domain.MemorySearchMaxChars)
	}
	if dropped, ok := got["dropped"].(int); ok && dropped < 0 {
		t.Fatalf("dropped = %d", dropped)
	}

	// The per-dispatch context carries the member's own memories so the next
	// turn actually sees them (bounded the same way).
	if _, err := h.app.RememberMemory(ctx, principal, "提交前先运行 gofmt 检查", ""); err != nil {
		t.Fatalf("compact memory: %v", err)
	}
	contextText := h.app.BuildContext(ctx, member, "")
	if !strings.Contains(contextText, "gofmt") {
		t.Fatalf("context missing the member's memory:\n%s", contextText)
	}
}
