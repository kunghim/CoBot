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

// reviewFlow drives a team turn by turn: each member's turn is found by the
// member's own engine thread.
type reviewFlow struct {
	t    *testing.T
	h    *m2Harness
	fake *fakeengine.Server
}

func (f reviewFlow) thread(bot domain.Bot) string {
	session, err := sqlite.GetProviderSession(context.Background(), f.h.app.DB, bot.ID)
	if err != nil {
		return ""
	}
	return session.ProviderThreadID
}

// turnsOf lists the turns the engine accepted for a member so far.
func (f reviewFlow) turnsOf(bot domain.Bot) []fakeengine.StartedTurn {
	thread := f.thread(bot)
	var out []fakeengine.StartedTurn
	for _, turn := range f.fake.Turns() {
		if thread != "" && turn.ThreadID == thread {
			out = append(out, turn)
		}
	}
	return out
}

// waitTurn waits for a member turn whose input contains every part, and
// returns it.
func (f reviewFlow) waitTurn(bot domain.Bot, parts ...string) fakeengine.StartedTurn {
	f.t.Helper()
	var found fakeengine.StartedTurn
	waitUntil(f.t, 5*time.Second, bot.Name+" turn with "+strings.Join(parts, " / "), func() bool {
		for _, turn := range f.turnsOf(bot) {
			all := true
			for _, part := range parts {
				all = all && strings.Contains(turn.Text, part)
			}
			if all {
				found = turn
				return true
			}
		}
		return false
	})
	waitRunning(f.t, f.h, bot.ID)
	return found
}

// finish ends a member's current turn with a reply and waits until it is
// recorded.
func (f reviewFlow) finish(bot domain.Bot, turn fakeengine.StartedTurn, reply string) {
	f.t.Helper()
	run, err := sqlite.FindActiveRun(context.Background(), f.h.app.DB, bot.ID)
	if err != nil {
		f.t.Fatalf("%s has no live run: %v", bot.Name, err)
	}
	f.fake.EmitTurnCompleted(turn.ThreadID, turn.TurnID, "completed", reply)
	waitUntil(f.t, 5*time.Second, bot.Name+" turn recorded", func() bool {
		r, err := sqlite.GetRun(context.Background(), f.h.app.DB, run.ID)
		return err == nil && r.Status == domain.RunCompleted
	})
}

func (f reviewFlow) task(id string) domain.Task {
	f.t.Helper()
	task, err := sqlite.GetTask(context.Background(), f.h.app.DB, id)
	if err != nil {
		f.t.Fatal(err)
	}
	return task
}

func (f reviewFlow) call(bot domain.Bot, tool string, args map[string]any) map[string]any {
	f.t.Helper()
	status, body := f.h.callTool(f.t, f.h.botToken(f.t, bot), tool, args)
	if status != 200 {
		f.t.Fatalf("%s %s = %d %v", bot.Name, tool, status, body)
	}
	data, _ := body["data"].(map[string]any)
	if data["status"] == "rejected" {
		f.t.Fatalf("%s %s rejected: %v", bot.Name, tool, data)
	}
	return data
}

// createdTaskID reads the id of the task a create_task call returned.
func createdTaskID(t *testing.T, data map[string]any) string {
	t.Helper()
	payload, _ := data["data"].(map[string]any)
	task, _ := payload["task"].(map[string]any)
	id, _ := task["id"].(string)
	if id == "" {
		t.Fatalf("no task in %v", data)
	}
	return id
}

// A task marked for review goes to the reviewer on delivery, with the
// checklist of its type; the reviewer's verdict decides it, the leader is
// only woken once the reviewer sent it back more often than allowed, and the
// finished requirement goes to the user for acceptance with the leader's
// report.
func TestReviewerDecidesAndTheUserAcceptsTheRequirement(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	settings, err := h.app.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	limit := 1
	if _, err := h.app.UpdateSettings(ctx, h.user, domain.SettingsPatch{ReviewRejectLimit: &limit}, settings.Revision); err != nil {
		t.Fatal(err)
	}
	team, bots := h.createTeam(t, []string{"backend-dev", "code-reviewer"})
	captain, dev, reviewer := bots[0], bots[1], bots[2]
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: captain.ID, Text: "方案确认，开始吧"}); err != nil {
		t.Fatal(err)
	}
	f := reviewFlow{t: t, h: h, fake: h.fakeFor(t, team.ID)}
	plan := f.waitTurn(captain, "方案确认")

	created := f.call(captain, "create_task", map[string]any{
		"title": "实现登录接口", "ownerId": dev.ID, "acceptanceCriteria": "返回 200", "reviewType": "code", "idempotencyKey": "s7-task",
	})
	taskID := createdTaskID(t, created)
	task := f.task(taskID)
	if task.ReviewerID != reviewer.ID || task.ReviewType != "code" || task.RequirementID == "" {
		t.Fatalf("task = %+v", task)
	}
	f.finish(captain, plan, "已派活")
	captainTurns := len(f.turnsOf(captain))

	deliver := func(round int) {
		turn := f.waitTurn(dev, "实现登录接口")
		if round > 0 {
			turn = f.waitTurn(dev, "评审未通过")
		}
		f.call(dev, "deliver_task", map[string]any{"taskId": taskID, "summary": "实现了接口，curl 返回 200", "idempotencyKey": "s7-deliver-" + string(rune('a'+round))})
		f.finish(dev, turn, "")
	}
	review := func(text string) fakeengine.StartedTurn {
		var turn fakeengine.StartedTurn
		waitUntil(t, 5*time.Second, "review request", func() bool {
			turns := f.turnsOf(reviewer)
			if len(turns) == 0 || !strings.Contains(turns[len(turns)-1].Text, text) {
				return false
			}
			turn = turns[len(turns)-1]
			return true
		})
		waitRunning(t, h, reviewer.ID)
		return turn
	}

	// First delivery: the reviewer gets it with the code checklist.
	deliver(0)
	first := review("请评审")
	if !strings.Contains(first.Text, "代码评审检查") || !strings.Contains(first.Text, "返回 200") {
		t.Fatalf("review request lacks checklist or criteria: %q", first.Text)
	}
	f.call(reviewer, "review_task", map[string]any{"taskId": taskID, "decision": "reject", "comment": "缺少错误处理", "idempotencyKey": "s7-r1"})
	f.finish(reviewer, first, "")
	if got := f.task(taskID); got.Status != domain.TaskDoing || got.RejectCount != 1 {
		t.Fatalf("after first send-back: %+v", got)
	}

	// Second delivery and send-back: past the limit, the leader decides.
	deliver(1)
	second := review("第 1 次打回后的重新交付")
	f.call(reviewer, "review_task", map[string]any{"taskId": taskID, "decision": "reject", "comment": "还是没有处理超时", "idempotencyKey": "s7-r2"})
	f.finish(reviewer, second, "")
	if got := f.task(taskID); got.Status != domain.TaskReview || got.RejectCount != 2 {
		t.Fatalf("after escalation: %+v", got)
	}
	if len(f.turnsOf(captain)) != captainTurns {
		// Nothing so far may have woken the leader.
		t.Fatalf("leader woken before the escalation: %d turns", len(f.turnsOf(captain)))
	}
	escalation := f.waitTurn(captain, "超过 1 次", "缺少错误处理", "还是没有处理超时")
	status, body := h.callTool(t, h.botToken(t, reviewer), "review_task", map[string]any{"taskId": taskID, "decision": "accept", "idempotencyKey": "s7-r3"})
	if data, _ := body["data"].(map[string]any); status != 200 || data["status"] != "rejected" {
		t.Fatalf("the reviewer decided an escalated task: %d %v", status, body)
	}

	// The leader accepts; the requirement is finished and reported.
	f.call(captain, "review_task", map[string]any{"taskId": taskID, "decision": "accept", "idempotencyKey": "s7-lead"})
	f.finish(captain, escalation, "")
	report := f.waitTurn(captain, "这次需求的任务都已结束", "实现登录接口")
	f.finish(captain, report, "登录接口已完成并通过验收：curl 返回 200。")

	questions, err := sqlite.ListQuestions(ctx, h.app.DB, team.ID, domain.QuestionOpen, 10)
	if err != nil || len(questions) != 1 {
		t.Fatalf("questions = %+v, %v", questions, err)
	}
	acceptance := questions[0]
	if acceptance.Kind != domain.QuestionKindAcceptance || !strings.Contains(acceptance.Question, "登录接口已完成") || acceptance.RequirementID != task.RequirementID {
		t.Fatalf("acceptance = %+v", acceptance)
	}
	// An acceptance never holds the leader back.
	if blocked, err := sqlite.HasBlockingQuestion(ctx, h.app.DB, captain.ID, ""); err != nil || blocked {
		t.Fatalf("acceptance blocks the leader: %v %v", blocked, err)
	}

	// Asking for changes needs the changes, and wakes the leader with them.
	_, err = h.app.AnswerAcceptance(ctx, h.user, acceptance.ID, domain.AcceptanceRevise, "  ", acceptance.Revision)
	var de *domain.Error
	if !errors.As(err, &de) || de.MessageKey != "error.question.revise_reason_required" {
		t.Fatalf("revise without changes = %v", err)
	}
	if _, err := h.app.AnswerQuestion(ctx, h.user, acceptance.ID, "随便答", acceptance.Revision); err == nil {
		t.Fatal("an acceptance answered as a plain question")
	}
	if _, err := h.app.AnswerAcceptance(ctx, h.user, acceptance.ID, domain.AcceptanceRevise, "登录失败时要提示原因", acceptance.Revision); err != nil {
		t.Fatal(err)
	}
	f.waitTurn(captain, "用户验收未通过", "登录失败时要提示原因")
}

// Without a reviewer the leader accepts the delivery as before, and the
// user's acceptance closes the requirement without waking anyone.
func TestUserAcceptanceClosesARequirement(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"backend-dev"})
	captain, dev := bots[0], bots[1]
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: captain.ID, Text: "做吧"}); err != nil {
		t.Fatal(err)
	}
	f := reviewFlow{t: t, h: h, fake: h.fakeFor(t, team.ID)}
	plan := f.waitTurn(captain, "做吧")
	// A review type with no reviewer in the team leaves acceptance to the leader.
	taskID := createdTaskID(t, f.call(captain, "create_task", map[string]any{
		"title": "改文案", "ownerId": dev.ID, "acceptanceCriteria": "文案已更新", "reviewType": "general", "idempotencyKey": "s7b-task",
	}))
	if got := f.task(taskID); got.ReviewerID != "" {
		t.Fatalf("a reviewer was assigned in a team without one: %+v", got)
	}
	status, body := h.callTool(t, h.botToken(t, captain), "create_task", map[string]any{
		"title": "x", "ownerId": dev.ID, "acceptanceCriteria": "y", "reviewType": "poetry", "idempotencyKey": "s7b-bad",
	})
	if data, _ := body["data"].(map[string]any); status != 200 || data["status"] != "rejected" {
		t.Fatalf("unknown review type accepted: %d %v", status, body)
	}
	f.finish(captain, plan, "")

	work := f.waitTurn(dev, "改文案")
	f.call(dev, "deliver_task", map[string]any{"taskId": taskID, "summary": "改好了", "idempotencyKey": "s7b-deliver"})
	f.finish(dev, work, "")
	notice := f.waitTurn(captain, "交付了任务「改文案」")
	f.call(captain, "review_task", map[string]any{"taskId": taskID, "decision": "accept", "idempotencyKey": "s7b-accept"})
	f.finish(captain, notice, "")
	report := f.waitTurn(captain, "这次需求的任务都已结束")
	f.finish(captain, report, "文案已更新。")

	questions, err := sqlite.ListQuestions(ctx, h.app.DB, team.ID, domain.QuestionOpen, 10)
	if err != nil || len(questions) != 1 || questions[0].Kind != domain.QuestionKindAcceptance {
		t.Fatalf("questions = %+v, %v", questions, err)
	}
	turns := len(f.fake.Turns())
	answered, err := h.app.AnswerAcceptance(ctx, h.user, questions[0].ID, domain.AcceptanceAccept, "", questions[0].Revision)
	if err != nil || answered.Status != domain.QuestionAnswered || answered.AnswerText != "验收通过" {
		t.Fatalf("accept = %+v, %v", answered, err)
	}
	time.Sleep(200 * time.Millisecond)
	if len(f.fake.Turns()) != turns {
		t.Fatal("accepting woke a member")
	}
}
