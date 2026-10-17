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
// wake past it waits for the user, a continue grants a fresh budget of the
// configured size, and stopping cancels the chain's undelivered work.
func TestChainBudgetGatesBotWakes(t *testing.T) {
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
		t.Fatalf("set chain limit: %v", err)
	}

	// The user opens the causal chain; this wake is user-caused and free.
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: captain.ID, Text: "开始协作"}); err != nil {
		t.Fatalf("user message: %v", err)
	}
	waitUntil(t, 5*time.Second, "captain turn 1", func() bool { return len(fake.Turns()) == 1 })

	// pass hands the ball from one member's running turn to the other.
	pass := func(from, to domain.Bot, text, key string, turns int) {
		t.Helper()
		if _, err := h.app.SendMessageFromBot(ctx, auth.NewPrincipalBot(from.ID, team.ID), to.ID, text, "", key); err != nil {
			t.Fatalf("%s send: %v", key, err)
		}
		last := fake.Turns()[len(fake.Turns())-1]
		fake.EmitTurnCompleted(last.ThreadID, last.TurnID, "completed", "已交接")
		if turns > 0 {
			waitUntil(t, 5*time.Second, key+" turn", func() bool { return len(fake.Turns()) == turns })
		}
	}
	// Wakes 1-3 are inside the budget: no model judges them, no card asks.
	pass(captain, member, "第一棒", "req-w1", 2)
	pass(member, captain, "第二棒", "req-w2", 3)
	pass(captain, member, "第三棒", "req-w3", 4)

	// Wake 4 is past the limit: the chain waits on one card.
	pass(member, captain, "第四棒", "req-w4", 0)
	waitUntil(t, 5*time.Second, "over-limit card", func() bool {
		pending, err := h.app.PendingApprovals(ctx, h.user, team.ID, "")
		return err == nil && len(pending) == 1 && pending[0].Reason == "over_limit"
	})
	chains, err := sqlite.ListChains(ctx, h.app.DB, team.ID, "", 10)
	if err != nil || len(chains) != 1 {
		t.Fatalf("chains = %+v (err %v), want exactly the user-opened chain", chains, err)
	}
	chain := chains[0]
	if chain.WakeCount != 3 || chain.Status != domain.ChainAwaitingApproval {
		t.Fatalf("chain before decision = wakes %d status %s limit %d", chain.WakeCount, chain.Status, chain.LimitCount)
	}
	pending, _ := h.app.PendingApprovals(ctx, h.user, team.ID, "")
	card := pending[0]

	// Continue: the bound wake runs and the chain gets a fresh budget of 3.
	if _, err := h.app.DecideApproval(ctx, h.user, card.ID, domain.ChainOptionContinue, "", card.Revision); err != nil {
		t.Fatalf("continue: %v", err)
	}
	waitUntil(t, 5*time.Second, "captain turn (wake 4)", func() bool { return len(fake.Turns()) == 5 })
	chain, err = sqlite.GetChain(ctx, h.app.DB, chain.ID)
	if err != nil {
		t.Fatalf("chain after continue: %v", err)
	}
	if chain.WakeCount != 4 || chain.Status != domain.ChainActive || chain.Epoch != 2 || chain.LimitCount != 6 {
		t.Fatalf("chain after continue = wakes %d status %s epoch %d limit %d, want 4/active/2/6", chain.WakeCount, chain.Status, chain.Epoch, chain.LimitCount)
	}

	// Wakes 5 and 6 use the new budget without asking; 7 asks again.
	pass(captain, member, "第五棒", "req-w5", 6)
	pass(member, captain, "第六棒", "req-w6", 7)
	if pending, _ := h.app.PendingApprovals(ctx, h.user, team.ID, ""); len(pending) != 0 {
		t.Fatalf("a wake inside the granted budget asked again: %+v", pending)
	}
	pass(captain, member, "第七棒", "req-w7", 0)
	waitUntil(t, 5*time.Second, "second over-limit card", func() bool {
		pending, err := h.app.PendingApprovals(ctx, h.user, team.ID, "")
		return err == nil && len(pending) == 1 && pending[0].Reason == "over_limit"
	})
	pending, _ = h.app.PendingApprovals(ctx, h.user, team.ID, "")
	overCard := pending[0]

	// Stopping cancels the chain's undelivered work.
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
		t.Fatal("stop must cancel the chain's undelivered seventh wake")
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

// seedMemory stores a memory row as earlier builds' remember tool did; the
// model can no longer write memories, but the ones kept are still shown,
// deleted and handed to the member.
func seedMemory(t *testing.T, h *m2Harness, teamID, botID, body string) domain.Memory {
	t.Helper()
	m := domain.Memory{ID: domain.NewID(), TeamID: teamID, BotID: botID, Body: body, NormKey: domain.NormalizeMemoryBody(body)}
	tx, err := h.app.DB.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := sqlite.InsertMemory(context.Background(), tx, m); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return m
}

// TestMemoriesStayWithTheirOwner pins G04: a member's memories are listed for
// it alone, and a deletion hides one immediately.
func TestMemoriesStayWithTheirOwner(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"backend-dev", "qa-engineer"})
	alice, bob := bots[1], bots[2]
	mem := seedMemory(t, h, team.ID, alice.ID, "部署前先跑 go test ./...")

	if items, err := h.app.ListMemories(ctx, h.user, bob.ID, 10); err != nil || len(items) != 0 {
		t.Fatalf("bob's memories = %+v (err %v), want none", items, err)
	}
	if items, err := h.app.ListMemories(ctx, h.user, alice.ID, 10); err != nil || len(items) != 1 {
		t.Fatalf("alice's memories = %+v (err %v), want hers", items, err)
	}
	if err := h.app.DeleteMemory(ctx, h.user, mem.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if items, err := h.app.ListMemories(ctx, h.user, alice.ID, 10); err != nil || len(items) != 0 {
		t.Fatalf("after delete = %+v (err %v), want empty", items, err)
	}
}

// TestThreadInstructionsCarryOwnMemories pins G05: a member's new thread is
// told its own memories, never a teammate's, within the item and character
// bounds.
func TestThreadInstructionsCarryOwnMemories(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"backend-dev", "qa-engineer"})
	member, teammate := bots[1], bots[2]
	body := strings.Repeat("记忆内容片段 ", 120) // ~1440 chars each
	for i := 0; i < 10; i++ {
		seedMemory(t, h, team.ID, member.ID, fmt.Sprintf("%s #%d", body, i))
	}
	seedMemory(t, h, team.ID, member.ID, "提交前先运行 gofmt 检查")
	seedMemory(t, h, team.ID, teammate.ID, "队友自己的习惯")

	fake := h.ensureFake(t, team.ID)
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: member.ID, Text: "开始"}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 5*time.Second, "member thread", func() bool { return len(fake.Instructions()) > 0 })
	text := fake.Instructions()[len(fake.Instructions())-1]
	if !strings.Contains(text, "gofmt") || strings.Contains(text, "队友自己的习惯") {
		t.Fatalf("instructions should carry only the member's memories:\n%s", text)
	}
	if got := strings.Count(text, "  #") + strings.Count(text, "gofmt"); got > domain.MemorySearchMaxItems {
		t.Fatalf("memories in instructions = %d, want at most %d", got, domain.MemorySearchMaxItems)
	}
	if n := utf8.RuneCountInString(text); n > domain.MemorySearchMaxChars+4000 {
		t.Fatalf("instructions grew to %d characters", n)
	}
}
