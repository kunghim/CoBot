package integration

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
	"github.com/kunghim/CoBot/tests/integration/fakeengine"
)

const handoverHeading = "接着之前的会话继续工作"

// sendAndFinish gives a member one message, waits for its turn and ends it
// with a reply.
func sendAndFinish(t *testing.T, h *m2Harness, fake *fakeengine.Server, bot domain.Bot, text string) fakeengine.StartedTurn {
	t.Helper()
	before := len(fake.Turns())
	if _, err := h.app.SendMessage(context.Background(), h.user, app.SendMessageInput{
		BotID: bot.ID, Text: text, RequestID: "handover-" + bot.ID + "-" + text,
	}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 5*time.Second, text+" turn", func() bool { return len(fake.Turns()) == before+1 })
	turn := fake.Turns()[before]
	fake.EmitTurnCompleted(turn.ThreadID, turn.TurnID, "completed", "做完了："+text)
	waitUntil(t, 5*time.Second, text+" done", func() bool { return botIdle(t, h, bot.ID) })
	return turn
}

func chooseModel(t *testing.T, h *m2Harness, botID, providerID, model string) {
	t.Helper()
	revision := botRevision(t, h, botID)
	if _, err := h.app.SetBotModel(context.Background(), h.user, botID, providerID, model, revision); err != nil {
		t.Fatalf("choose %s/%s: %v", providerID, model, err)
	}
}

// saveVendorProvider stores a vendor's own provider (not a gateway).
func saveVendorProvider(t *testing.T, h *m2Harness, id string, models ...string) {
	t.Helper()
	kept := make([]domain.ProviderModel, 0, len(models))
	for _, m := range models {
		kept = append(kept, domain.ProviderModel{ID: m})
	}
	if _, err := h.app.SaveProvider(context.Background(), domain.Provider{
		ID: id, Name: id, Kind: domain.ProviderKindOfficial, AuthMode: domain.ProviderAuthAPIKey,
		Endpoints: []domain.ProviderEndpoint{{Dialect: domain.DialectOpenAIResponses, BaseURL: "https://" + id + ".example/v1"}},
		Models:    kept, DefaultModel: models[0],
	}, 0); err != nil {
		t.Fatalf("save the provider %s: %v", id, err)
	}
}

// A member moved to another vendor hears about the old record on the first
// turn of its new thread only; the turns after run on the new thread as
// usual.
func TestAHandoverIsToldOnTheFirstTurnOnly(t *testing.T) {
	h := newM2Harness(t)
	saveVendorProvider(t, h, "alpha", "alpha-model")
	saveVendorProvider(t, h, "beta", "beta-model")
	team, bots := h.createTeam(t, []string{"fullstack-engineer"})
	fake := h.ensureFake(t, team.ID)
	engineer := bots[1]

	chooseModel(t, h, engineer.ID, "alpha", "alpha-model")
	first := sendAndFinish(t, h, fake, engineer, "one")
	if strings.Contains(first.Text, handoverHeading) {
		t.Fatal("a member's very first thread has nothing to take over")
	}

	chooseModel(t, h, engineer.ID, "beta", "beta-model")
	second := sendAndFinish(t, h, fake, engineer, "two")
	if second.ThreadID == first.ThreadID {
		t.Fatal("another vendor ran on the old thread")
	}
	for _, want := range []string{
		handoverHeading,
		fakeengine.TranscriptPath(first.ThreadID),
		"之前是 alpha（alpha-model），现在是 beta（beta-model）",
		"上一轮的结果：正常结束",
		"你最后的回复：「做完了：one」",
	} {
		if !strings.Contains(second.Text, want) {
			t.Fatalf("the handover lacks %q:\n%s", want, second.Text)
		}
	}
	if strings.Index(second.Text, handoverHeading) > strings.LastIndex(second.Text, "two") {
		t.Fatal("the handover comes after the message")
	}
	if got := fake.Archived(); len(got) != 0 {
		t.Fatalf("the old thread was archived, which moves its record: %v", got)
	}

	third := sendAndFinish(t, h, fake, engineer, "three")
	if third.ThreadID != second.ThreadID || strings.Contains(third.Text, handoverHeading) {
		t.Fatalf("the handover was told twice or the thread moved: %s vs %s", third.ThreadID, second.ThreadID)
	}
}

// Within one vendor's own provider another model reads the same history,
// so the thread stays. A gateway fronts many vendors and a model's name
// does not say whose it is, so there another model is a new thread.
func TestAnotherModelKeepsTheThreadOnlyWithinAVendor(t *testing.T) {
	h := newM2Harness(t)
	saveVendorProvider(t, h, "vendor", "vendor-pro", "vendor-flash")
	if _, err := h.app.SaveProvider(context.Background(), domain.Provider{
		ID: "gateway", Name: "gateway", Kind: domain.ProviderKindCustom, AuthMode: domain.ProviderAuthAPIKey,
		Endpoints: []domain.ProviderEndpoint{{Dialect: domain.DialectOpenAIResponses, BaseURL: "https://gateway.example/v1"}},
		Models:    []domain.ProviderModel{{ID: "gpt-x"}, {ID: "claude-y"}}, DefaultModel: "gpt-x",
	}, 0); err != nil {
		t.Fatal(err)
	}
	team, bots := h.createTeam(t, []string{"fullstack-engineer", "qa-engineer"})
	fake := h.ensureFake(t, team.ID)
	engineer, tester := bots[1], bots[2]

	chooseModel(t, h, engineer.ID, "vendor", "vendor-pro")
	a := sendAndFinish(t, h, fake, engineer, "a")
	chooseModel(t, h, engineer.ID, "vendor", "vendor-flash")
	b := sendAndFinish(t, h, fake, engineer, "b")
	if b.ThreadID != a.ThreadID || strings.Contains(b.Text, handoverHeading) || b.Model != "vendor-flash" {
		t.Fatalf("a model change within a vendor left the thread: %s → %s, model %q", a.ThreadID, b.ThreadID, b.Model)
	}

	chooseModel(t, h, tester.ID, "gateway", "gpt-x")
	c := sendAndFinish(t, h, fake, tester, "c")
	chooseModel(t, h, tester.ID, "gateway", "claude-y")
	d := sendAndFinish(t, h, fake, tester, "d")
	if d.ThreadID == c.ThreadID || !strings.Contains(d.Text, fakeengine.TranscriptPath(c.ThreadID)) {
		t.Fatalf("a model change on a gateway kept the thread: %s → %s", c.ThreadID, d.ThreadID)
	}
}

// A thread the engine can no longer load is replaced by a new one, and the
// member is pointed at the old record rather than starting from nothing.
func TestAThreadThatCannotBeLoadedAgainIsHandedOver(t *testing.T) {
	h := newM2Harness(t)
	team, bots := h.createTeam(t, []string{"fullstack-engineer"})
	fake := h.ensureFake(t, team.ID)
	engineer := bots[1]

	first := sendAndFinish(t, h, fake, engineer, "before")
	fake.Unload(first.ThreadID)
	fake.ResumeErr = errors.New("thread not found")
	second := sendAndFinish(t, h, fake, engineer, "after")
	if second.ThreadID == first.ThreadID {
		t.Fatal("the thread that failed to load took the turn")
	}
	if !strings.Contains(second.Text, handoverHeading) || !strings.Contains(second.Text, fakeengine.TranscriptPath(first.ThreadID)) {
		t.Fatalf("the new thread was not handed over:\n%s", second.Text)
	}
}

func botIdle(t *testing.T, h *m2Harness, botID string) bool {
	t.Helper()
	bot, err := sqlite.GetBot(context.Background(), h.app.DB, botID)
	return err == nil && bot.Status == domain.BotStatusIdle
}

func botRevision(t *testing.T, h *m2Harness, botID string) int64 {
	t.Helper()
	bot, err := sqlite.GetBot(context.Background(), h.app.DB, botID)
	if err != nil {
		t.Fatal(err)
	}
	return bot.Revision
}

// Each thread is told to call the tools the way its vendor's model can:
// the ChatGPT account in code mode, another vendor as plain calls in the
// mcp__cobot namespace.
func TestAThreadIsToldToCallToolsTheWayItsVendorCan(t *testing.T) {
	h := newM2Harness(t)
	team, bots := h.createTeam(t, []string{"fullstack-engineer", "qa-engineer"})
	fake := h.ensureFake(t, team.ID)
	engineer, tester := bots[1], bots[2]

	// With no provider configured, a member runs on the ChatGPT account.
	sendAndFinish(t, h, fake, engineer, "official")
	saveVendorProvider(t, h, "vendor", "vendor-model")
	chooseModel(t, h, tester.ID, "vendor", "vendor-model")
	sendAndFinish(t, h, fake, tester, "vendor")
	if got := fake.ThreadProviders; len(got) != 2 || got[0] != "openai" || got[1] != "vendor" {
		t.Fatalf("the threads ran on %v", got)
	}
	got := fake.Instructions()
	if len(got) != 2 {
		t.Fatalf("%d threads started", len(got))
	}
	if !strings.Contains(got[0], "tools.mcp__cobot__") {
		t.Fatal("the ChatGPT account's thread lost the code-mode spelling")
	}
	if strings.Contains(got[1], "tools.mcp__cobot__") || !strings.Contains(got[1], "mcp__cobot 这个命名空间") {
		t.Fatal("another vendor's thread is told the code-mode spelling")
	}
}

// A card left open after the turn that worked on it is reminded, and once
// more on a fresh thread handed over from the old one; then it is left for
// the user. A turn the user spent talking to the member about something
// else counts for nothing.
func TestAnOpenCardIsRemindedTwiceAndOnlyAfterItsOwnTurn(t *testing.T) {
	h := newM2Harness(t)
	team, bots := h.createTeam(t, []string{"fullstack-engineer"})
	fake := h.ensureFake(t, team.ID)
	pm, dev := bots[0], bots[1]

	h.handCard(t, team, pm, dev, "做页面", "能打开")
	waitUntil(t, 5*time.Second, "card turn", func() bool { return len(fake.Turns()) == 1 })
	finishTurn(t, h, fake, fake.Turns()[0], dev.ID)
	waitUntil(t, 5*time.Second, "reminder turn", func() bool { return len(fake.Turns()) == 2 })
	if !strings.Contains(fake.Turns()[1].Text, "这一轮结束了，但卡片还没有交接") {
		t.Fatalf("the second turn is not the reminder:\n%s", fake.Turns()[1].Text)
	}
	finishTurn(t, h, fake, fake.Turns()[1], dev.ID)
	waitUntil(t, 5*time.Second, "second reminder", func() bool { return len(fake.Turns()) == 3 })
	second := fake.Turns()[2]
	if second.ThreadID == fake.Turns()[1].ThreadID {
		t.Fatal("the second reminder ran on the thread that kept failing")
	}
	for _, want := range []string{handoverHeading, fakeengine.TranscriptPath(fake.Turns()[1].ThreadID), "上次提醒后还是没有交出"} {
		if !strings.Contains(second.Text, want) {
			t.Fatalf("the second reminder lacks %q:\n%s", want, second.Text)
		}
	}
	finishTurn(t, h, fake, second, dev.ID)

	sendAndFinish(t, h, fake, dev, "随便聊聊")
	time.Sleep(300 * time.Millisecond)
	if n := len(fake.Turns()); n != 4 {
		t.Fatalf("%d turns; the card was reminded a third time", n)
	}
}

// Stopping a chain cancels the handoff that was waiting, so the
// requirement stays open with nobody on it; the conversation of the member
// the step was for says where it stopped and how to go on.
func TestAStoppedChainSaysWhereTheRequirementStopped(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	settings, err := h.app.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	one := 1
	if _, err := h.app.UpdateSettings(ctx, h.user, domain.SettingsPatch{ChainLimit: &one}, settings.Revision); err != nil {
		t.Fatal(err)
	}
	team, bots := h.createTeam(t, []string{"fullstack-engineer"})
	fake := h.ensureFake(t, team.ID)
	pm, dev := bots[0], bots[1]
	f := reviewFlow{t: t, h: h, fake: fake}

	card, req := h.handCard(t, team, pm, dev, "做页面", "能打开")
	turn := f.waitTurn(dev, "做页面")
	f.call(dev, "handoff", map[string]any{"cardId": card.ID, "outcome": "done", "summary": "做好了", "idempotencyKey": "stop-notice"})
	f.finish(dev, turn, "")
	waitUntil(t, 5*time.Second, "budget card", func() bool {
		pending, err := h.app.PendingApprovals(ctx, h.user, team.ID, "")
		return err == nil && len(pending) == 1 && pending[0].Reason == "over_limit"
	})
	pending, _ := h.app.PendingApprovals(ctx, h.user, team.ID, "")
	if _, err := h.app.DecideApproval(ctx, h.user, pending[0].ID, domain.ChainOptionStop, "", pending[0].Revision); err != nil {
		t.Fatalf("stop: %v", err)
	}
	messages, err := sqlite.ListMessages(ctx, h.app.DB, pm.ID, "", 20)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range messages {
		if m.Kind == domain.MessageKindSystem && strings.Contains(m.RenderText(), "已按你的选择停止这条链") && strings.Contains(m.RenderText(), req.Title) {
			found = true
		}
	}
	if !found {
		t.Fatalf("no notice in the project manager's conversation: %+v", messages)
	}
}

// A reminder wakes the member within the chain that brought it the card:
// the chain's budget counts it like any other wake.
func TestAReminderCountsAgainstTheChainBudget(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	settings, err := h.app.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	one := 1
	if _, err := h.app.UpdateSettings(ctx, h.user, domain.SettingsPatch{ChainLimit: &one}, settings.Revision); err != nil {
		t.Fatal(err)
	}
	team, bots := h.createTeam(t, []string{"fullstack-engineer"})
	fake := h.ensureFake(t, team.ID)
	pm, dev := bots[0], bots[1]

	h.handCard(t, team, pm, dev, "做页面", "能打开")
	waitUntil(t, 5*time.Second, "card turn", func() bool { return len(fake.Turns()) == 1 })
	finishTurn(t, h, fake, fake.Turns()[0], dev.ID)
	waitUntil(t, 5*time.Second, "budget card", func() bool {
		pending, err := h.app.PendingApprovals(ctx, h.user, team.ID, "")
		return err == nil && len(pending) == 1 && pending[0].Reason == "over_limit"
	})
	time.Sleep(300 * time.Millisecond)
	if n := len(fake.Turns()); n != 1 {
		t.Fatalf("%d turns; the reminder went past the chain's budget", n)
	}
}
