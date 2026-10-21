package integration

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/engine"
	"github.com/kunghim/CoBot/internal/store/sqlite"
	"github.com/kunghim/CoBot/tests/integration/fakeengine"
)

// finishTurn completes a started turn and waits until the member is idle.
func finishTurn(t *testing.T, h *m2Harness, fake *fakeengine.Server, turn fakeengine.StartedTurn, botID string) {
	t.Helper()
	fake.EmitTurnCompleted(turn.ThreadID, turn.TurnID, "completed", "好")
	waitUntil(t, 5*time.Second, "turn finished", func() bool {
		_, err := sqlite.FindActiveRun(context.Background(), h.app.DB, botID)
		return errors.Is(err, sqlite.ErrNotFound)
	})
}

// A thread is told the role, the team and the tools once; a turn carries
// only what changed since.
func TestTurnsCarryOnlyWhatChanged(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"fullstack-engineer"})
	dev := bots[1]
	fake := h.ensureFake(t, team.ID)

	turn := func(text string, want int) fakeengine.StartedTurn {
		t.Helper()
		if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: dev.ID, Text: text}); err != nil {
			t.Fatal(err)
		}
		waitUntil(t, 5*time.Second, "turn started", func() bool { return len(fake.Turns()) == want })
		started := fake.Turns()[want-1]
		finishTurn(t, h, fake, started, dev.ID)
		return started
	}
	first := turn("第一件事", 1)
	instructions := fake.Instructions()
	if len(instructions) != 1 || !strings.Contains(instructions[0], "handoff") || !strings.Contains(instructions[0], dev.ID) || !strings.Contains(instructions[0], "你在一个 CoBot 团队中工作") {
		t.Fatalf("thread instructions:\n%v", instructions)
	}
	if strings.Contains(first.Text, "处理卡片") || strings.Contains(first.Text, "# 你的角色") || strings.Contains(first.Text, "成员：") {
		t.Fatalf("first turn repeats the instructions:\n%s", first.Text)
	}
	if second := turn("第二件事", 2); second.Text != "第二件事" {
		t.Fatalf("an unchanged team still sent context:\n%s", second.Text)
	}
	newcomer, err := h.app.AddBot(ctx, h.user, team.ID, "qa-engineer")
	if err != nil {
		t.Fatal(err)
	}
	third := turn("第三件事", 3)
	if !strings.Contains(third.Text, "团队成员有变化") || !strings.Contains(third.Text, newcomer.ID) {
		t.Fatalf("roster change not told:\n%s", third.Text)
	}
	if fourth := turn("第四件事", 4); fourth.Text != "第四件事" {
		t.Fatalf("roster change told twice:\n%s", fourth.Text)
	}
}

// A Codex sub-agent a member spawns works inside the member's run: its steps
// join the member's process, its approvals reach the user as the member's,
// its usage counts to the member, and its turn ending does not end the
// member's turn.
func TestSubAgentWorkBelongsToTheMember(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, nil)
	bot := bots[0]
	fake := h.ensureFake(t, team.ID)
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: bot.ID, Text: "让子智能体查测试"}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 5*time.Second, "turn", func() bool { return len(fake.Turns()) == 1 })
	turn := fake.Turns()[0]
	fake.SpawnSubAgent("child-thread", turn.ThreadID)
	child := func(n engine.Notification) {
		n.ThreadID, n.TurnID = "child-thread", "child-turn"
		fake.Emit(n)
	}
	child(engine.Notification{Method: "turn/started"})
	child(engine.Notification{Method: "item/completed", Step: &engine.Step{ID: "cmd-1", Kind: engine.StepCommand, Status: engine.StepCompleted, Detail: json.RawMessage(`{"command":"go test"}`)}})
	usage := `{"inputTokens":700,"cachedInputTokens":0,"cacheWriteInputTokens":0,"outputTokens":30,"reasoningOutputTokens":0,"totalTokens":730}`
	child(engine.Notification{Method: "thread/tokenUsage/updated", TokenUsage: json.RawMessage(`{"threadId":"child-thread","turnId":"child-turn","tokenUsage":{"last":` + usage + `,"total":` + usage + `}}`)})
	fake.EmitApproval("sub-approval", "child-thread", "child-turn", "rm -rf build")
	child(engine.Notification{Method: "turn/completed", TurnStatus: "completed", Raw: json.RawMessage(`{"threadId":"child-thread","turn":{"id":"child-turn","status":"completed"}}`)})

	runID := h.runStatusID(t, bot.ID)
	waitUntil(t, 5*time.Second, "sub-agent step stored", func() bool {
		step, err := sqlite.GetRunStep(ctx, h.app.DB, runID, "sub-d-thread:cmd-1")
		return err == nil && strings.Contains(string(step.Detail), "child-thread")
	})
	var pending int
	if err := h.app.DB.QueryRow(`SELECT count(*) FROM approvals WHERE requester_bot_id = ? AND run_id = ? AND request_state = 'pending'`, bot.ID, runID).Scan(&pending); err != nil || pending != 1 {
		t.Fatalf("sub-agent approval cards = %d (%v)", pending, err)
	}
	if usage, err := sqlite.GetUsage(ctx, h.app.DB, fake.Epoch(), "child-thread", "child-turn"); err != nil || usage.BotID != bot.ID || usage.InputTokens != 700 {
		t.Fatalf("sub-agent usage = %+v %v", usage, err)
	}
	if run, err := sqlite.FindActiveRun(ctx, h.app.DB, bot.ID); err != nil || run.ID != runID {
		t.Fatalf("the sub-agent ending ended the member's turn: %+v %v", run, err)
	}
}

// A card Codex stopped waiting for leaves the screen; a decided card is left
// as decided. The turn's diff is stored as one step.
func TestSettledRequestsCloseCardsAndTheTurnDiffIsKept(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, nil)
	bot := bots[0]
	fake := h.ensureFake(t, team.ID)
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: bot.ID, Text: "改代码"}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 5*time.Second, "turn", func() bool { return len(fake.Turns()) == 1 })
	turn := fake.Turns()[0]
	fake.EmitApproval("req-a", turn.ThreadID, turn.TurnID, "npm install")
	fake.EmitApproval("req-b", turn.ThreadID, turn.TurnID, "npm test")
	stateOf := func(requestID string) string {
		var state string
		_ = h.app.DB.QueryRow(`SELECT request_state FROM approvals WHERE engine_request_id = ?`, `"`+requestID+`"`).Scan(&state)
		return state
	}
	waitUntil(t, 5*time.Second, "cards", func() bool { return stateOf("req-a") == "pending" && stateOf("req-b") == "pending" })
	if _, err := h.app.DB.Exec(`UPDATE approvals SET request_state = 'decided' WHERE engine_request_id = ?`, `"req-b"`); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"req-a", "req-b"} {
		fake.Emit(engine.Notification{Method: "serverRequest/resolved", ThreadID: turn.ThreadID, RequestID: json.RawMessage(`"` + id + `"`)})
	}
	waitUntil(t, 5*time.Second, "unanswered card closed", func() bool { return stateOf("req-a") == domain.ApprovalStateInvalidated })
	if got := stateOf("req-b"); got != "decided" {
		t.Fatalf("decided card changed to %q", got)
	}

	fake.Emit(engine.Notification{Method: "turn/diff/updated", ThreadID: turn.ThreadID, TurnID: turn.TurnID,
		Step: &engine.Step{ID: "turn-diff", Kind: engine.StepDiff, Status: engine.StepCompleted, Detail: json.RawMessage(`{"diff":"diff --git a/x b/x","files":1}`)}})
	waitUntil(t, 5*time.Second, "diff step", func() bool {
		step, err := sqlite.GetRunStep(ctx, h.app.DB, h.runStatusID(t, bot.ID), "turn-diff")
		return err == nil && step.Kind == engine.StepDiff
	})
}

// A member's statistics come from what the engine reported: turns, steps,
// usage, the context in use and the account's limits.
func TestSessionStatsReadWhatTheEngineReported(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, nil)
	bot := bots[0]
	fake := h.ensureFake(t, team.ID)
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: bot.ID, Text: "跑测试"}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 5*time.Second, "turn", func() bool { return len(fake.Turns()) == 1 })
	turn := fake.Turns()[0]
	emit := func(n engine.Notification) {
		n.ThreadID, n.TurnID = turn.ThreadID, turn.TurnID
		fake.Emit(n)
	}
	emit(engine.Notification{Method: "item/started", Step: &engine.Step{ID: "cmd-1", Kind: engine.StepCommand, Status: engine.StepInProgress, Detail: json.RawMessage(`{"command":"go test"}`)}})
	time.Sleep(20 * time.Millisecond)
	emit(engine.Notification{Method: "item/completed", Step: &engine.Step{ID: "cmd-1", Kind: engine.StepCommand, Status: engine.StepCompleted, Detail: json.RawMessage(`{"command":"go test","exitCode":0}`)}})
	usage := `{"inputTokens":5000,"cachedInputTokens":4000,"cacheWriteInputTokens":0,"outputTokens":200,"reasoningOutputTokens":0,"totalTokens":5200}`
	emit(engine.Notification{Method: "thread/tokenUsage/updated", TokenUsage: json.RawMessage(`{"threadId":"` + turn.ThreadID + `","turnId":"` + turn.TurnID + `","tokenUsage":{"last":` + usage + `,"total":` + usage + `,"modelContextWindow":258400}}`)})
	fake.Emit(engine.Notification{Method: "account/rateLimits/updated", Raw: json.RawMessage(`{"rateLimits":{"primary":{"usedPercent":3,"windowDurationMins":300},"secondary":{"usedPercent":79,"windowDurationMins":10080}}}`)})
	fake.EmitTurnCompleted(turn.ThreadID, turn.TurnID, "completed", "通过")
	waitUntil(t, 5*time.Second, "turn ended", func() bool {
		_, err := sqlite.FindActiveRun(ctx, h.app.DB, bot.ID)
		return errors.Is(err, sqlite.ErrNotFound)
	})

	stats, err := h.app.BotStats(ctx, h.user, bot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Turns != 1 || stats.Steps != 1 || stats.ToolMs <= 0 || stats.Tokens.Input != 5000 || stats.Tokens.CachedRead != 4000 || stats.Tokens.Output != 200 {
		t.Fatalf("stats = %+v", stats)
	}
	if stats.Context.Used != 5200 || stats.Context.Window != 258400 || !strings.Contains(string(stats.RateLimits), `"usedPercent":79`) {
		t.Fatalf("context/limits = %+v %s", stats.Context, stats.RateLimits)
	}
}

// The team's reasoning effort reaches every member's next turn; empty
// leaves the model's own default.
func TestTeamReasoningEffortReachesTheTurn(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, nil)
	fake := h.ensureFake(t, team.ID)
	if _, err := h.app.PatchTeamReasoningEffort(ctx, h.user, team.ID, "HIGH!", 0); err == nil {
		t.Fatal("an invalid effort was accepted")
	}
	updated, err := h.app.PatchTeamReasoningEffort(ctx, h.user, team.ID, "high", 0)
	if err != nil || updated.ReasoningEffort != "high" {
		t.Fatalf("patch = %+v %v", updated, err)
	}
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: bots[0].ID, Text: "想清楚再做"}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 5*time.Second, "turn", func() bool { return len(fake.Turns()) == 1 })
	if got := fake.Turns()[0].Effort; got != "high" {
		t.Fatalf("turn effort = %q", got)
	}
}

// A turn's usage is the sum of all its model calls, measured by how much the
// thread total grew; a repeated report adds nothing.
func TestTurnUsageSumsEveryModelCall(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, nil)
	bot := bots[0]
	fake := h.ensureFake(t, team.ID)
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: bot.ID, Text: "做事"}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 5*time.Second, "turn", func() bool { return len(fake.Turns()) == 1 })
	turn := fake.Turns()[0]
	report := func(last, total string) {
		raw := json.RawMessage(`{"threadId":"` + turn.ThreadID + `","turnId":"` + turn.TurnID + `","tokenUsage":{"last":` + last + `,"total":` + total + `,"modelContextWindow":258400}}`)
		fake.Emit(engine.Notification{Method: "thread/tokenUsage/updated", ThreadID: turn.ThreadID, TurnID: turn.TurnID, TokenUsage: raw})
	}
	report(`{"inputTokens":1000,"cachedInputTokens":800,"cacheWriteInputTokens":0,"outputTokens":50,"reasoningOutputTokens":10,"totalTokens":1050}`,
		`{"inputTokens":1000,"cachedInputTokens":800,"cacheWriteInputTokens":0,"outputTokens":50,"reasoningOutputTokens":10,"totalTokens":1050}`)
	second := `{"inputTokens":3200,"cachedInputTokens":2500,"cacheWriteInputTokens":100,"outputTokens":130,"reasoningOutputTokens":30,"totalTokens":3330}`
	report(`{"inputTokens":2200,"cachedInputTokens":1700,"cacheWriteInputTokens":100,"outputTokens":80,"reasoningOutputTokens":20,"totalTokens":2280}`, second)
	report(`{"inputTokens":2200,"cachedInputTokens":1700,"cacheWriteInputTokens":100,"outputTokens":80,"reasoningOutputTokens":20,"totalTokens":2280}`, second)

	usage, err := sqlite.GetUsage(ctx, h.app.DB, fake.Epoch(), turn.ThreadID, turn.TurnID)
	if err != nil {
		t.Fatal(err)
	}
	if usage.InputTokens != 3200 || usage.CachedInputTokens != 2500 || usage.CacheWriteInputTokens != 100 || usage.OutputTokens != 130 || usage.ReasoningOutputTokens != 30 {
		t.Fatalf("turn usage = %+v", usage)
	}
}

// Notes left without waking a member are read in its next turn, with the
// cards it holds.
func TestNotesReachTheNextTurn(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"fullstack-engineer", "qa-engineer"})
	pm, dev, qa := bots[0], bots[1], bots[2]
	fake := h.ensureFake(t, team.ID)

	h.handCard(t, team, pm, dev, "做页面", "能打开")
	waitUntil(t, 5*time.Second, "developer turn", func() bool { return len(fake.Turns()) == 1 })
	asDev := auth.NewPrincipalBot(dev.ID, team.ID)
	if _, err := h.app.NotifyFromBot(ctx, asDev, qa.ID, "页面结构是单文件", "", "note-1"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if len(fake.Turns()) != 1 {
		t.Fatal("a note woke its recipient")
	}
	finishTurn(t, h, fake, fake.Turns()[0], dev.ID)
	// The card is still open, so the developer is reminded once; that next
	// turn lists the card it still holds.
	waitUntil(t, 5*time.Second, "developer reminder", func() bool { return len(fake.Turns()) == 2 })
	if text := fake.Turns()[1].Text; !strings.Contains(text, "# 你手上的卡片") || !strings.Contains(text, "做页面") || !strings.Contains(text, "还没有交接") {
		t.Fatalf("the reminder turn lacks the card:\n%s", text)
	}
	finishTurn(t, h, fake, fake.Turns()[1], dev.ID)

	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: qa.ID, Text: "你好"}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 5*time.Second, "tester turn", func() bool { return len(fake.Turns()) == 3 })
	if text := fake.Turns()[2].Text; !strings.Contains(text, "页面结构是单文件") || !strings.Contains(text, "不需要回复") {
		t.Fatalf("tester turn lacks the note:\n%s", text)
	}
}
