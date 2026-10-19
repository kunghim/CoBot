package integration

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/engine"
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
	return f.waitTurnAfter(bot, 0, parts...)
}

// waitTurnAfter is waitTurn among the member's turns after the first skip.
func (f reviewFlow) waitTurnAfter(bot domain.Bot, skip int, parts ...string) fakeengine.StartedTurn {
	f.t.Helper()
	var found fakeengine.StartedTurn
	waitUntil(f.t, 5*time.Second, bot.Name+" turn with "+strings.Join(parts, " / "), func() bool {
		turns := f.turnsOf(bot)
		for i := skip; i < len(turns); i++ {
			all := true
			for _, part := range parts {
				all = all && strings.Contains(turns[i].Text, part)
			}
			if all {
				found = turns[i]
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

// refused calls a tool expecting a refusal with the given message key.
func (f reviewFlow) refused(bot domain.Bot, tool string, args map[string]any, key string) {
	f.t.Helper()
	status, body := f.h.callTool(f.t, f.h.botToken(f.t, bot), tool, args)
	data, _ := body["data"].(map[string]any)
	refusal, _ := data["error"].(map[string]any)
	if status != 200 || data["status"] != "rejected" || refusal["messageKey"] != key {
		f.t.Fatalf("%s %s = %d %v, want refusal %s", bot.Name, tool, status, body, key)
	}
}

// card is the open card a member holds.
func (f reviewFlow) card(bot domain.Bot) domain.Task {
	f.t.Helper()
	var found domain.Task
	waitUntil(f.t, 5*time.Second, bot.Name+" holds a card", func() bool {
		cards, err := sqlite.ListTasks(context.Background(), f.h.app.DB, bot.TeamID, bot.ID, 50)
		if err != nil {
			return false
		}
		for _, card := range cards {
			if !card.IsTerminal() {
				found = card
				return true
			}
		}
		return false
	})
	return found
}

// openQuestion is the team's one open question of a kind.
func (f reviewFlow) openQuestion(teamID, kind string) domain.Question {
	f.t.Helper()
	var found domain.Question
	waitUntil(f.t, 5*time.Second, "open "+kind+" question", func() bool {
		questions, err := sqlite.ListQuestions(context.Background(), f.h.app.DB, teamID, domain.QuestionOpen, 10)
		if err != nil {
			return false
		}
		for _, q := range questions {
			if q.Kind == kind {
				found = q
				return true
			}
		}
		return false
	})
	return found
}

func relayRoute(reviewer, dev domain.Bot) []map[string]any {
	return []map[string]any{
		{"name": "需求评审", "kind": "review", "ownerId": reviewer.ID, "reviewType": "requirements", "criteria": "需求可验证"},
		{"name": "用户确认需求", "kind": "confirm", "criteria": "需求和链路"},
		{"name": "实现登录接口", "kind": "implement", "ownerId": dev.ID, "criteria": "返回 200"},
		{"name": "代码评审", "kind": "review", "ownerId": reviewer.ID, "reviewType": "code"},
		{"name": "验收汇报", "kind": "accept"},
	}
}

// A requirement runs its route: the requirement review, the user's
// confirmation (whose changes go back to the project manager's document),
// the implementation, a code review that sends work back until the limit
// escalates it to the project manager, who resumes it, and the final
// acceptance carrying the reviewer's suggestions.
func TestRequirementRunsItsRouteToAcceptance(t *testing.T) {
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
	team, bots := h.createTeam(t, []string{"fullstack-engineer", "code-reviewer"})
	pm, dev, reviewer := bots[0], bots[1], bots[2]
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: pm.ID, Text: "做一个登录接口"}); err != nil {
		t.Fatal(err)
	}
	f := reviewFlow{t: t, h: h, fake: h.fakeFor(t, team.ID)}
	talk := f.waitTurn(pm, "做一个登录接口")
	f.call(pm, "start_requirement", map[string]any{
		"title": "登录接口", "document": "# 目标\n提供登录接口，成功返回 200。", "stages": relayRoute(reviewer, dev), "idempotencyKey": "s7-start",
	})
	f.finish(pm, talk, "已提交需求")
	pmTurns := len(f.turnsOf(pm))

	// The requirement review reaches the reviewer with the document, the
	// route and the requirements checklist.
	review := f.waitTurn(reviewer, "【接力卡片】需求评审", "提供登录接口", "▶ 1. 需求评审", "评审清单")
	card := f.card(reviewer)
	if card.StageIndex != 0 || card.StageKind != domain.StageReview || card.ReviewType != "requirements" {
		t.Fatalf("review card = %+v", card)
	}
	waitUntil(t, 5*time.Second, "card started", func() bool { return f.task(card.ID).Status == domain.TaskDoing })
	f.call(reviewer, "handoff", map[string]any{"cardId": card.ID, "outcome": "done", "summary": "需求清楚", "idempotencyKey": "s7-rr1"})
	f.finish(reviewer, review, "")

	// The user asks for changes: the document goes back to the project
	// manager, and the revised one is reviewed again.
	confirm := f.openQuestion(team.ID, domain.QuestionKindConfirm)
	if confirm.StageIndex != 1 || !strings.Contains(confirm.Question, "提供登录接口") {
		t.Fatalf("confirm = %+v", confirm)
	}
	if _, err := h.app.AnswerQuestion(ctx, h.user, confirm.ID, "随便答", confirm.Revision); err == nil {
		t.Fatal("a confirmation answered as a plain question")
	}
	if _, err := h.app.AnswerDecision(ctx, h.user, confirm.ID, domain.AcceptanceRevise, "", "失败时要返回原因", confirm.Revision); err != nil {
		t.Fatal(err)
	}
	draft := f.waitTurnAfter(pm, pmTurns, "修改需求文档", "失败时要返回原因")
	draftCard := f.card(pm)
	if draftCard.StageKind != domain.CardDraft {
		t.Fatalf("draft card = %+v", draftCard)
	}
	f.refused(pm, "handoff", map[string]any{"cardId": draftCard.ID, "outcome": "done", "summary": "改好了", "idempotencyKey": "s7-d0"}, "error.card.document_required")
	f.call(pm, "handoff", map[string]any{"cardId": draftCard.ID, "outcome": "done", "summary": "加了失败原因",
		"document": "# 目标\n提供登录接口，成功返回 200，失败返回原因。", "idempotencyKey": "s7-d1"})
	f.finish(pm, draft, "")
	reviewerTurns := len(f.turnsOf(reviewer))
	again := f.waitTurnAfter(reviewer, 1, "失败返回原因")
	f.call(reviewer, "handoff", map[string]any{"cardId": f.card(reviewer).ID, "outcome": "done", "summary": "可以", "idempotencyKey": "s7-rr2"})
	f.finish(reviewer, again, "")
	confirm = f.openQuestion(team.ID, domain.QuestionKindConfirm)
	if _, err := h.app.AnswerDecision(ctx, h.user, confirm.ID, domain.AcceptanceAccept, "", "", confirm.Revision); err != nil {
		t.Fatal(err)
	}

	// Implementation, then a code review that sends it back once.
	work := f.waitTurn(dev, "【接力卡片】实现登录接口", "返回 200")
	devCard := f.card(dev)
	// What the engine reports the member ran travels with the handoff.
	f.fake.Emit(engine.Notification{Method: "item/completed", ThreadID: work.ThreadID, TurnID: work.TurnID,
		Step: &engine.Step{ID: "build-1", Kind: engine.StepCommand, Status: engine.StepCompleted, Detail: json.RawMessage(`{"command":"go build ./...","exitCode":0}`)}})
	waitUntil(t, 5*time.Second, "command step recorded", func() bool {
		var n int
		_ = h.app.DB.QueryRowContext(ctx, `SELECT count(*) FROM run_steps WHERE bot_id = ? AND kind = ?`, dev.ID, engine.StepCommand).Scan(&n)
		return n == 1
	})
	f.refused(dev, "handoff", map[string]any{"cardId": devCard.ID, "outcome": "back", "summary": "x", "mode": "rerun", "idempotencyKey": "s7-x"}, "error.card.back_not_allowed")
	f.call(dev, "handoff", map[string]any{"cardId": devCard.ID, "outcome": "done", "summary": "实现了，curl 返回 200", "idempotencyKey": "s7-i1"})
	f.finish(dev, work, "")
	codeReview := f.waitTurnAfter(reviewer, reviewerTurns, "【接力卡片】代码评审", "实现了，curl 返回 200", "运行：go build ./...（退出码 0）")
	codeCard := f.card(reviewer)
	f.refused(reviewer, "handoff", map[string]any{"cardId": codeCard.ID, "outcome": "back", "summary": "缺少错误处理", "idempotencyKey": "s7-c0"}, "error.card.back_mode")
	f.call(reviewer, "handoff", map[string]any{"cardId": codeCard.ID, "outcome": "back", "summary": "缺少错误处理", "mode": "rerun", "idempotencyKey": "s7-c1"})
	f.finish(reviewer, codeReview, "")
	rework := f.waitTurn(dev, "返工：实现登录接口", "缺少错误处理")
	f.call(dev, "handoff", map[string]any{"cardId": f.card(dev).ID, "outcome": "done", "summary": "补了错误处理", "idempotencyKey": "s7-i2"})
	f.finish(dev, rework, "")

	// The second send-back is past the limit: the project manager decides.
	secondReview := f.waitTurn(reviewer, "补了错误处理")
	f.call(reviewer, "handoff", map[string]any{"cardId": f.card(reviewer).ID, "outcome": "back", "summary": "还是没有处理超时", "mode": "rerun", "idempotencyKey": "s7-c2"})
	f.finish(reviewer, secondReview, "")
	escalation := f.waitTurn(pm, "处理上报：代码评审", "超过上限 1 次", "缺少错误处理", "还是没有处理超时")
	escalationCard := f.card(pm)
	f.refused(pm, "handoff", map[string]any{"cardId": escalationCard.ID, "outcome": "done", "summary": "x", "idempotencyKey": "s7-e0"}, "error.card.resume_escalation")
	f.call(pm, "handoff", map[string]any{"cardId": escalationCard.ID, "outcome": "resume", "summary": "超时不在本次范围，评审通过即可", "idempotencyKey": "s7-e1"})
	f.finish(pm, escalation, "")

	// The reviewer passes with a suggestion; the acceptance stage reaches the
	// project manager, whose report goes to the user with the suggestion.
	resumed := f.waitTurn(reviewer, "项目经理的处理意见：超时不在本次范围")
	f.call(reviewer, "handoff", map[string]any{"cardId": f.card(reviewer).ID, "outcome": "done", "summary": "通过", "suggestions": "以后补超时处理", "idempotencyKey": "s7-c3"})
	f.finish(reviewer, resumed, "")
	accept := f.waitTurn(pm, "【接力卡片】验收汇报", "以后补超时处理", "补了错误处理", "更早的几轮交接没有列出")
	// Each stage shows only its latest handoff: the first implementation and
	// the send-backs it got were superseded.
	if strings.Contains(accept.Text, "实现了，curl 返回 200") || strings.Contains(accept.Text, "缺少错误处理") {
		t.Fatalf("the acceptance card repeats superseded rounds:\n%s", accept.Text)
	}
	f.call(pm, "handoff", map[string]any{"cardId": f.card(pm).ID, "outcome": "done", "summary": "登录接口已完成：curl 返回 200。", "idempotencyKey": "s7-a"})
	f.finish(pm, accept, "")

	acceptance := f.openQuestion(team.ID, domain.QuestionKindAcceptance)
	if !strings.Contains(acceptance.Question, "登录接口已完成") || !strings.Contains(acceptance.Question, "以后补超时处理") {
		t.Fatalf("acceptance = %+v", acceptance)
	}
	req, err := sqlite.GetRequirement(ctx, h.app.DB, acceptance.RequirementID)
	if err != nil || req.Status != domain.RequirementAccepting || !strings.Contains(req.Document, "失败返回原因") {
		t.Fatalf("requirement = %+v, %v", req, err)
	}
	// An acceptance never holds the project manager back.
	if blocked, err := sqlite.HasBlockingQuestion(ctx, h.app.DB, pm.ID, ""); err != nil || blocked {
		t.Fatalf("acceptance blocks the project manager: %v %v", blocked, err)
	}
	_, err = h.app.AnswerDecision(ctx, h.user, acceptance.ID, domain.AcceptanceRevise, "", "  ", acceptance.Revision)
	var de *domain.Error
	if !errors.As(err, &de) || de.MessageKey != "error.question.revise_reason_required" {
		t.Fatalf("revise without changes = %v", err)
	}
	turns := len(f.fake.Turns())
	if _, err := h.app.AnswerDecision(ctx, h.user, acceptance.ID, domain.AcceptanceAccept, "", "", acceptance.Revision); err != nil {
		t.Fatal(err)
	}
	if req, _ := sqlite.GetRequirement(ctx, h.app.DB, acceptance.RequirementID); req.Status != domain.RequirementDone {
		t.Fatalf("requirement after acceptance = %s", req.Status)
	}
	time.Sleep(200 * time.Millisecond)
	if len(f.fake.Turns()) != turns {
		t.Fatal("accepting woke a member")
	}

	// Each handoff left its record, and picking a card up was recorded for
	// the activity page.
	var started int
	if err := h.app.DB.QueryRowContext(ctx, `SELECT count(*) FROM events WHERE type = ?`, domain.EventCardStarted).Scan(&started); err != nil || started < 8 {
		t.Fatalf("card.started events = %d, %v", started, err)
	}
}

// CoBot keeps only a few rules for a route, a member may not start one,
// a note never wakes anyone, and the user may cancel a requirement.
func TestRouteRulesNotesAndCancel(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"fullstack-engineer", "code-reviewer"})
	pm, dev, reviewer := bots[0], bots[1], bots[2]
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: pm.ID, Text: "改个文案"}); err != nil {
		t.Fatal(err)
	}
	f := reviewFlow{t: t, h: h, fake: h.fakeFor(t, team.ID)}
	talk := f.waitTurn(pm, "改个文案")
	start := func(key string, stages []map[string]any) map[string]any {
		return map[string]any{"title": "改文案", "document": "把按钮文案改成登录", "stages": stages, "idempotencyKey": key}
	}
	bad := map[string][]map[string]any{
		"no-confirm": {{"kind": "implement", "ownerId": dev.ID}, {"kind": "accept"}},
		"no-accept":  {{"kind": "confirm"}, {"kind": "implement", "ownerId": dev.ID}},
		"leader":     {{"kind": "confirm"}, {"kind": "implement", "ownerId": pm.ID}, {"kind": "accept"}},
		"stranger":   {{"kind": "confirm"}, {"kind": "implement", "ownerId": "nobody"}, {"kind": "accept"}},
		"no-type":    {{"kind": "confirm"}, {"kind": "review", "ownerId": reviewer.ID}, {"kind": "accept"}},
	}
	for key, stages := range bad {
		f.refused(pm, "start_requirement", start("s7b-"+key, stages), "error.requirement.route_invalid")
	}
	f.refused(dev, "start_requirement", start("s7b-dev", []map[string]any{{"kind": "confirm"}, {"kind": "accept"}}), "error.requirement.leader_only")
	f.call(pm, "start_requirement", start("s7b-ok", []map[string]any{{"kind": "confirm"}, {"name": "改文案", "kind": "implement", "ownerId": dev.ID}, {"kind": "accept"}}))
	f.finish(pm, talk, "")

	confirm := f.openQuestion(team.ID, domain.QuestionKindConfirm)
	if _, err := h.app.AnswerDecision(ctx, h.user, confirm.ID, domain.AcceptanceAccept, "", "", confirm.Revision); err != nil {
		t.Fatal(err)
	}
	work := f.waitTurn(dev, "【接力卡片】改文案")
	card := f.card(dev)

	// A note is stored for the recipient's next turn and wakes nobody.
	reviewerTurns := len(f.turnsOf(reviewer))
	f.call(dev, "send_message", map[string]any{"targetBotIds": []string{reviewer.ID}, "text": "顺便说一下，文案在 header 里", "idempotencyKey": "s7b-note"})
	time.Sleep(200 * time.Millisecond)
	if len(f.turnsOf(reviewer)) != reviewerTurns {
		t.Fatal("a note woke its recipient")
	}

	// The user cancels: the card ends, and its handoff is refused.
	reqs, err := h.app.ListRequirements(ctx, h.user, team.ID)
	if err != nil || len(reqs) != 1 {
		t.Fatalf("requirements = %+v, %v", reqs, err)
	}
	if _, err := h.app.CancelRequirement(ctx, h.user, reqs[0].ID); err != nil {
		t.Fatal(err)
	}
	if got := f.task(card.ID); got.Status != domain.TaskCancelled {
		t.Fatalf("card after cancel = %s", got.Status)
	}
	f.refused(dev, "handoff", map[string]any{"cardId": card.ID, "outcome": "done", "summary": "改好了", "idempotencyKey": "s7b-late"}, "error.card.finished")
	f.finish(dev, work, "")
	if _, err := h.app.CancelRequirement(ctx, h.user, reqs[0].ID); err == nil {
		t.Fatal("a cancelled requirement cancelled again")
	}
}

// A user who asks for changes on a confirmation can have the fix come
// straight back, without the review in between running again.
func TestConfirmChangesCanComeStraightBack(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"fullstack-engineer", "code-reviewer"})
	pm, dev, reviewer := bots[0], bots[1], bots[2]
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: pm.ID, Text: "加个导出按钮"}); err != nil {
		t.Fatal(err)
	}
	f := reviewFlow{t: t, h: h, fake: h.fakeFor(t, team.ID)}
	talk := f.waitTurn(pm, "加个导出按钮")
	f.call(pm, "start_requirement", map[string]any{
		"title": "导出", "document": "导出 CSV", "stages": relayRoute(reviewer, dev), "idempotencyKey": "s7c-start",
	})
	f.finish(pm, talk, "")
	review := f.waitTurn(reviewer, "【接力卡片】需求评审")
	f.call(reviewer, "handoff", map[string]any{"cardId": f.card(reviewer).ID, "outcome": "done", "summary": "可以", "idempotencyKey": "s7c-r"})
	f.finish(reviewer, review, "")
	reviewerTurns := len(f.turnsOf(reviewer))
	pmTurns := len(f.turnsOf(pm))

	confirm := f.openQuestion(team.ID, domain.QuestionKindConfirm)
	if _, err := h.app.AnswerDecision(ctx, h.user, confirm.ID, domain.AcceptanceRevise, "bogus", "要支持 Excel", confirm.Revision); err == nil {
		t.Fatal("an unknown destination for the changes was accepted")
	}
	if _, err := h.app.AnswerDecision(ctx, h.user, confirm.ID, domain.AcceptanceRevise, domain.ReworkReturnToMe, "要支持 Excel", confirm.Revision); err != nil {
		t.Fatal(err)
	}
	draft := f.waitTurnAfter(pm, pmTurns, "修改需求文档", "要支持 Excel", "直接回到第 2 环「用户确认需求」")
	f.call(pm, "handoff", map[string]any{"cardId": f.card(pm).ID, "outcome": "done", "summary": "加了 Excel", "document": "导出 CSV 和 Excel", "idempotencyKey": "s7c-d"})
	f.finish(pm, draft, "")

	again := f.openQuestion(team.ID, domain.QuestionKindConfirm)
	if again.ID == confirm.ID || !strings.Contains(again.Question, "导出 CSV 和 Excel") {
		t.Fatalf("the revised document did not come back for confirmation: %+v", again)
	}
	time.Sleep(200 * time.Millisecond)
	if n := len(f.turnsOf(reviewer)); n != reviewerTurns {
		t.Fatalf("the review ran again: reviewer turns %d -> %d", reviewerTurns, n)
	}
}
