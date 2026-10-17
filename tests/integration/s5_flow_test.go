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

// The model only says what it means: a member is handed a task (which CoBot
// marks as started), delivers once, the leader sends it back with a reason,
// the member delivers again, the leader accepts, and the waiting task is
// released to its owner.
func TestDeliverAndReviewMoveTheTaskForTheModel(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"frontend-dev", "qa-engineer"})
	leader, dev, qa := bots[0], bots[1], bots[2]
	fake := h.ensureFake(t, team.ID)

	build, err := h.app.CreateTask(ctx, h.user, false, app.CreateTaskInput{TeamID: team.ID, OwnerID: dev.ID, Title: "做页面", AcceptanceCriteria: "能打开"})
	if err != nil {
		t.Fatal(err)
	}
	check, err := h.app.CreateTask(ctx, h.user, false, app.CreateTaskInput{TeamID: team.ID, OwnerID: qa.ID, Title: "测页面", AcceptanceCriteria: "测过", DependencyIDs: []string{build.ID}})
	if err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 5*time.Second, "developer handed the task", func() bool { return len(fake.Turns()) == 1 })
	waitUntil(t, 5*time.Second, "task started", func() bool {
		task, _ := sqlite.GetTask(ctx, h.app.DB, build.ID)
		return task.Status == domain.TaskDoing
	})
	devTurn := fake.Turns()[0]

	// Someone else's task cannot be delivered.
	if _, err := h.app.DeliverTask(ctx, auth.NewPrincipalBot(qa.ID, team.ID), build.ID, "我测过了", nil); err == nil {
		t.Fatal("a member delivered another member's task")
	}
	asDev := auth.NewPrincipalBot(dev.ID, team.ID)
	delivered, err := h.app.DeliverTask(ctx, asDev, build.ID, "index.html 双击打开；node check.js 通过", []string{"index.html"})
	if err != nil || delivered.Status != domain.TaskReview {
		t.Fatalf("deliver = %+v %v", delivered, err)
	}
	finishTurn(t, h, fake, devTurn, dev.ID)
	waitUntil(t, 5*time.Second, "leader woken", func() bool { return len(fake.Turns()) == 2 })
	notice := fake.Turns()[1]
	if !strings.Contains(notice.Text, "node check.js 通过") || !strings.Contains(notice.Text, "review_task") {
		t.Fatalf("leader notice:\n%s", notice.Text)
	}

	asLeader := auth.NewPrincipalBot(leader.ID, team.ID)
	if _, err := h.app.ReviewTask(ctx, asLeader, build.ID, false, ""); err == nil {
		t.Fatal("rejected without a reason")
	}
	back, err := h.app.ReviewTask(ctx, asLeader, build.ID, false, "删除按钮不起作用")
	if err != nil || back.Status != domain.TaskDoing {
		t.Fatalf("reject = %+v %v", back, err)
	}
	finishTurn(t, h, fake, notice, leader.ID)
	waitUntil(t, 5*time.Second, "developer gets the reason", func() bool { return len(fake.Turns()) == 3 })
	if again := fake.Turns()[2]; !strings.Contains(again.Text, "删除按钮不起作用") {
		t.Fatalf("rejection text:\n%s", again.Text)
	}
	if _, err := h.app.DeliverTask(ctx, asDev, build.ID, "修好了删除", nil); err != nil {
		t.Fatal(err)
	}
	finishTurn(t, h, fake, fake.Turns()[2], dev.ID)
	waitUntil(t, 5*time.Second, "leader woken again", func() bool { return len(fake.Turns()) == 4 })
	done, err := h.app.ReviewTask(ctx, asLeader, build.ID, true, "")
	if err != nil || done.Status != domain.TaskDone {
		t.Fatalf("accept = %+v %v", done, err)
	}
	finishTurn(t, h, fake, fake.Turns()[3], leader.ID)
	waitUntil(t, 5*time.Second, "tester handed its task", func() bool { return len(fake.Turns()) == 5 })
	if qaTurn := fake.Turns()[4]; !strings.Contains(qaTurn.Text, check.ID) {
		t.Fatalf("tester turn:\n%s", qaTurn.Text)
	}
}

// A thread is told the role, the team and the tools once; a turn carries
// only what changed since.
func TestTurnsCarryOnlyWhatChanged(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"frontend-dev"})
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
	if len(instructions) != 1 || !strings.Contains(instructions[0], "deliver_task") || !strings.Contains(instructions[0], dev.ID) || !strings.Contains(instructions[0], "协作约定") {
		t.Fatalf("thread instructions:\n%v", instructions)
	}
	if strings.Contains(first.Text, "协作约定") || strings.Contains(first.Text, "# 你的角色") || strings.Contains(first.Text, "成员：") {
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

// Notes left without waking a member are read in its next turn; a message
// to a member whose task still waits says so to the sender.
func TestNotesAndWaitingTasksReachTheRightSide(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"frontend-dev", "qa-engineer"})
	dev, qa := bots[1], bots[2]
	fake := h.ensureFake(t, team.ID)

	build, err := h.app.CreateTask(ctx, h.user, false, app.CreateTaskInput{TeamID: team.ID, OwnerID: dev.ID, Title: "做页面", AcceptanceCriteria: "能打开"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.CreateTask(ctx, h.user, false, app.CreateTaskInput{TeamID: team.ID, OwnerID: qa.ID, Title: "测页面", AcceptanceCriteria: "测过", DependencyIDs: []string{build.ID}}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 5*time.Second, "developer turn", func() bool { return len(fake.Turns()) == 1 })
	asDev := auth.NewPrincipalBot(dev.ID, team.ID)
	if waiting := h.app.WaitingTasksOf(ctx, team.ID, qa.ID); len(waiting) != 1 || !strings.Contains(waiting[0], "做页面") {
		t.Fatalf("waiting tasks of tester = %v", waiting)
	}
	if _, err := h.app.NotifyFromBot(ctx, asDev, qa.ID, "页面结构是单文件", "", "note-1"); err != nil {
		t.Fatal(err)
	}
	finishTurn(t, h, fake, fake.Turns()[0], dev.ID)

	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: qa.ID, Text: "你好"}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 5*time.Second, "tester turn", func() bool { return len(fake.Turns()) == 2 })
	text := fake.Turns()[1].Text
	for _, want := range []string{"页面结构是单文件", "测页面", "现在不要开工"} {
		if !strings.Contains(text, want) {
			t.Fatalf("tester turn lacks %q:\n%s", want, text)
		}
	}
}
