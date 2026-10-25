package integration

import (
	"context"
	"strings"
	"testing"

	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// The card the confirmed route dispatches goes out in the project manager's
// name: the member sees where it came from and can open it, and the project
// manager's conversation shows where it went.
func TestCardAfterUserConfirmIsFromLeader(t *testing.T) {
	h := newM2Harness(t)
	team, bots := h.createTeam(t, []string{"fullstack-engineer"})
	pm, dev := bots[0], bots[1]
	d := relayDriver{t: t, h: h, team: team}
	req := d.start(pm, []domain.Stage{
		{Kind: domain.StageConfirm},
		{Name: "实现", Kind: domain.StageImplement, OwnerID: dev.ID},
		{Kind: domain.StageAccept},
	})
	d.confirm(req)

	card := d.open(req)
	if card.CreatedBy != pm.ID {
		t.Fatalf("card created by %q, want the project manager", card.CreatedBy)
	}
	var source string
	if err := h.app.DB.QueryRowContext(context.Background(),
		`SELECT source_bot_id FROM messages WHERE run_id = ? AND kind = ?`, card.ID, domain.MessageKindSystem).Scan(&source); err != nil {
		t.Fatal(err)
	}
	if source != pm.ID {
		t.Fatalf("card message source %q, want the project manager", source)
	}
	messages, err := sqlite.ListMessages(context.Background(), h.app.DB, pm.ID, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if !hasMessageText(messages, "你确认了需求「回归」") {
		t.Fatal("the project manager's conversation has no note of the user's confirmation")
	}
}

// The confirm text lists the route as an ordered list the user reads, one
// stage per line with the current one marked.
func TestConfirmTextRouteIsAnOrderedList(t *testing.T) {
	h := newM2Harness(t)
	team, bots := h.createTeam(t, []string{"fullstack-engineer"})
	pm, dev := bots[0], bots[1]
	d := relayDriver{t: t, h: h, team: team}
	d.start(pm, []domain.Stage{
		{Kind: domain.StageConfirm},
		{Name: "实现", Kind: domain.StageImplement, OwnerID: dev.ID},
		{Kind: domain.StageAccept},
	})
	messages, err := sqlite.ListMessages(context.Background(), h.app.DB, pm.ID, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range messages {
		if text := m.RenderText(); strings.Contains(text, "### 链路") {
			if !strings.Contains(text, "1. 用户确认（用户） ← 当前") || strings.Contains(text, "▶") {
				t.Fatalf("confirm text route =\n%s", text)
			}
			return
		}
	}
	t.Fatal("no confirm text posted to the project manager")
}

// A revision the user raises at a later confirmation sends the producing
// stage a rework card in the project manager's name.
func TestUserReviseReworkIsFromLeader(t *testing.T) {
	h := newM2Harness(t)
	team, bots := h.createTeam(t, []string{"fullstack-engineer"})
	pm, dev := bots[0], bots[1]
	d := relayDriver{t: t, h: h, team: team}
	req := d.start(pm, []domain.Stage{
		{Kind: domain.StageConfirm},
		{Name: "实现", Kind: domain.StageImplement, OwnerID: dev.ID},
		{Kind: domain.StageConfirm},
		{Kind: domain.StageAccept},
	})
	d.confirm(req)
	d.handoff(d.open(req), domain.HandoffDone, "")

	ctx := context.Background()
	questions, err := sqlite.ListQuestions(ctx, h.app.DB, team.ID, domain.QuestionOpen, 20)
	if err != nil {
		t.Fatal(err)
	}
	second := domain.Question{}
	for _, q := range questions {
		if q.RequirementID == req.ID && q.Kind == domain.QuestionKindConfirm {
			second = q
			break
		}
	}
	if second.ID == "" {
		t.Fatal("no second confirmation")
	}
	if _, err := h.app.AnswerDecision(ctx, h.user, second.ID, domain.AcceptanceRevise, "", "端口换成 8080", second.Revision); err != nil {
		t.Fatal(err)
	}

	rework := d.open(req)
	if rework.OwnerID != dev.ID || rework.CreatedBy != pm.ID {
		t.Fatalf("rework card = %+v, want owner %q created by %q", rework, dev.ID, pm.ID)
	}
	var source string
	if err := h.app.DB.QueryRowContext(ctx,
		`SELECT source_bot_id FROM messages WHERE run_id = ? AND kind = ?`, rework.ID, domain.MessageKindSystem).Scan(&source); err != nil {
		t.Fatal(err)
	}
	if source != pm.ID {
		t.Fatalf("rework message source %q, want the project manager", source)
	}
	messages, err := sqlite.ListMessages(ctx, h.app.DB, pm.ID, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if !hasMessageText(messages, "提了修改意见：端口换成 8080") || !hasMessageText(messages, "改完后按链路继续") {
		t.Fatal("the project manager's conversation has no note of the user's revision")
	}
}

// Accepting the finished requirement records the verdict in the project
// manager's conversation.
func TestAcceptanceAcceptLeavesNote(t *testing.T) {
	h := newM2Harness(t)
	team, bots := h.createTeam(t, []string{"fullstack-engineer"})
	pm, dev := bots[0], bots[1]
	d := relayDriver{t: t, h: h, team: team}
	req := d.start(pm, []domain.Stage{
		{Kind: domain.StageConfirm},
		{Name: "实现", Kind: domain.StageImplement, OwnerID: dev.ID},
		{Kind: domain.StageAccept},
	})
	d.confirm(req)
	d.handoff(d.open(req), domain.HandoffDone, "")
	d.handoff(d.open(req), domain.HandoffDone, "")
	ctx := context.Background()
	questions, err := sqlite.ListQuestions(ctx, h.app.DB, team.ID, domain.QuestionOpen, 20)
	if err != nil {
		t.Fatal(err)
	}
	acceptance := domain.Question{}
	for _, q := range questions {
		if q.RequirementID == req.ID && q.Kind == domain.QuestionKindAcceptance {
			acceptance = q
			break
		}
	}
	if acceptance.ID == "" {
		t.Fatal("no acceptance question")
	}
	if _, err := h.app.AnswerDecision(ctx, h.user, acceptance.ID, domain.AcceptanceAccept, "", "", acceptance.Revision); err != nil {
		t.Fatal(err)
	}
	messages, err := sqlite.ListMessages(ctx, h.app.DB, pm.ID, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if !hasMessageText(messages, "你验收通过了需求「回归」") {
		t.Fatal("the project manager's conversation has no note of the acceptance")
	}
}

// Ignoring a member's question is recorded in its conversation.
func TestUserIgnoreQuestionLeavesNote(t *testing.T) {
	h := newM2Harness(t)
	team, bots := h.createTeam(t, []string{"fullstack-engineer"})
	dev := bots[1]
	ctx := context.Background()
	question, err := h.app.AskUser(ctx, auth.NewPrincipalBot(dev.ID, team.ID), app.AskUserInput{Question: "用哪个端口启动开发服务？"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.CancelQuestion(ctx, h.user, question.ID, question.Revision); err != nil {
		t.Fatal(err)
	}
	messages, err := sqlite.ListMessages(ctx, h.app.DB, dev.ID, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if !hasMessageText(messages, "你忽略了提问：用哪个端口启动开发服务？") {
		t.Fatal("the member's conversation has no note of the ignored question")
	}
}

// Ignoring a confirmation cancels its requirement: the project manager
// learns why with its next turn, without a wake, and the user keeps a record
// where the card was.
func TestCancelledRequirementNotifiesLeader(t *testing.T) {
	h := newM2Harness(t)
	team, bots := h.createTeam(t, []string{"fullstack-engineer"})
	pm, dev := bots[0], bots[1]
	d := relayDriver{t: t, h: h, team: team}
	req := d.start(pm, []domain.Stage{
		{Kind: domain.StageConfirm},
		{Name: "实现", Kind: domain.StageImplement, OwnerID: dev.ID},
		{Kind: domain.StageAccept},
	})
	d.confirm(req)
	if _, err := h.app.CancelRequirement(context.Background(), h.user, req.ID); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	messages, err := sqlite.ListMessages(ctx, h.app.DB, pm.ID, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if !hasMessageText(messages, "你取消了需求「回归」") {
		t.Fatal("the project manager's conversation has no record of the cancellation")
	}
	var note int
	if err := h.app.DB.QueryRowContext(ctx,
		`SELECT count(*) FROM messages WHERE bot_id = ? AND kind = ? AND source_bot_id = ?`, pm.ID, domain.MessageKindCollaboration, pm.ID).Scan(&note); err != nil {
		t.Fatal(err)
	}
	if note != 1 {
		t.Fatalf("leader collaboration notes = %d, want the resubmission note", note)
	}
	messages, err = sqlite.ListMessages(ctx, h.app.DB, pm.ID, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	_ = messages
	var noteText string
	if err := h.app.DB.QueryRowContext(ctx,
		`SELECT json_extract(blocks, '$[0].text') FROM messages WHERE bot_id = ? AND kind = ? AND source_bot_id = ?`, pm.ID, domain.MessageKindCollaboration, pm.ID).Scan(&noteText); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(noteText, "方向谈清楚后请重新提交需求") {
		t.Fatalf("cancel note = %q, want the neutral resubmission wording", noteText)
	}
}

// The straight-return offer on a confirm card follows the route: there has
// to be a checking stage between the work and the confirmation for a straight
// return to skip anything.
func TestConfirmCardSkipChecksFollowsTheRoute(t *testing.T) {
	h := newM2Harness(t)
	team, bots := h.createTeam(t, []string{"fullstack-engineer", "qa-engineer"})
	pm, dev, qa := bots[0], bots[1], bots[2]
	d := relayDriver{t: t, h: h, team: team}
	req := d.start(pm, []domain.Stage{
		{Kind: domain.StageConfirm},
		{Name: "实现", Kind: domain.StageImplement, OwnerID: dev.ID},
		{Name: "测试", Kind: domain.StageTest, OwnerID: qa.ID},
		{Kind: domain.StageConfirm},
		{Kind: domain.StageAccept},
	})
	ctx := context.Background()
	openConfirm := func() domain.Question {
		page, err := h.app.ListPage(ctx, h.user, app.PageRequest{Kind: "questions", TeamID: team.ID, State: "open"})
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range page["items"].([]any) {
			if q := item.(domain.Question); q.RequirementID == req.ID {
				return q
			}
		}
		t.Fatal("no open confirm question")
		return domain.Question{}
	}
	// Nothing sits between the document and the first confirmation.
	if first := openConfirm(); first.CanSkipChecks {
		t.Fatal("the first confirmation offers a straight return with nothing to skip")
	}
	d.confirm(req)
	d.handoff(d.open(req), domain.HandoffDone, "")
	d.handoff(d.open(req), domain.HandoffDone, "")
	// The test stage sits between the implementation and the second
	// confirmation: a straight return would skip it.
	if second := openConfirm(); !second.CanSkipChecks {
		t.Fatal("the second confirmation does not offer skipping the test stage")
	}
}

// A requirements review before the first confirmation is exactly what a
// straight return skips: the confirm card offers it, and a revision sent
// with return_to_me comes back past the review, straight to a new
// confirmation.
func TestConfirmAfterRequirementsReviewOffersSkippingIt(t *testing.T) {
	h := newM2Harness(t)
	team, bots := h.createTeam(t, []string{"fullstack-engineer", "code-reviewer"})
	pm, dev, reviewer := bots[0], bots[1], bots[2]
	d := relayDriver{t: t, h: h, team: team}
	req := d.start(pm, []domain.Stage{
		{Name: "需求评审", Kind: domain.StageReview, OwnerID: reviewer.ID, ReviewType: "requirements"},
		{Kind: domain.StageConfirm},
		{Name: "实现", Kind: domain.StageImplement, OwnerID: dev.ID},
		{Kind: domain.StageAccept},
	})
	ctx := context.Background()
	openConfirm := func() domain.Question {
		page, err := h.app.ListPage(ctx, h.user, app.PageRequest{Kind: "questions", TeamID: team.ID, State: "open"})
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range page["items"].([]any) {
			if q := item.(domain.Question); q.RequirementID == req.ID && q.Kind == domain.QuestionKindConfirm {
				return q
			}
		}
		t.Fatal("no open confirm question")
		return domain.Question{}
	}

	// The review sits between the document and the confirmation.
	d.handoff(d.open(req), domain.HandoffDone, "")
	first := openConfirm()
	if !first.CanSkipChecks {
		t.Fatal("the confirmation behind a requirements review does not offer skipping it")
	}

	// Revising with return_to_me sends the document to the project manager;
	// once it hands the fix back, the work skips the review and a fresh
	// confirmation waits.
	if _, err := h.app.AnswerDecision(ctx, h.user, first.ID, domain.AcceptanceRevise, domain.ReworkReturnToMe, "范围去掉导出", first.Revision); err != nil {
		t.Fatal(err)
	}
	draft := d.open(req)
	if draft.StageKind != domain.CardDraft || draft.OwnerID != pm.ID {
		t.Fatalf("rework card = %+v, want the project manager's draft", draft)
	}
	if _, err := h.app.Handoff(ctx, auth.NewPrincipalBot(pm.ID, team.ID), app.HandoffInput{
		CardID: draft.ID, Outcome: domain.HandoffDone, Summary: "文档已按要求修改", Document: "修改后的需求文档",
	}); err != nil {
		t.Fatal(err)
	}
	second := openConfirm()
	if second.StageIndex != 1 {
		t.Fatalf("fresh confirmation at stage %d, want 1", second.StageIndex)
	}
	for _, card := range func() []domain.Task {
		cards, err := sqlite.ListRequirementTasks(ctx, h.app.DB, req.ID)
		if err != nil {
			t.Fatal(err)
		}
		return cards
	}() {
		if !card.IsTerminal() && card.OwnerID == reviewer.ID {
			t.Fatal("the review stage ran again although the revision skipped it")
		}
	}
}

// hasMessageText reports whether one message renders to text containing the
// given line.
func hasMessageText(messages []domain.Message, text string) bool {
	for _, m := range messages {
		if strings.Contains(m.RenderText(), text) {
			return true
		}
	}
	return false
}
