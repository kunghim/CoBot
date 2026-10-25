package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/roles"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// The relay. A requirement is the project manager's document plus the route
// the user confirmed: an ordered list of stages. Each stage is a card for one
// member, or a confirmation for the user. Members hand cards off; CoBot moves
// the work along the route, so a model never chooses who is next. A checking
// stage (review, test) may send work back to the stage that produced it, a
// member may escalate to the project manager, and the route always ends with
// the project manager's acceptance and then the user's.

// relayEffect is what a relay step leaves to do once its transaction commits.
type relayEffect struct {
	wake   []string
	events []domain.Event
}

func (e *relayEffect) add(other relayEffect) {
	e.wake = append(e.wake, other.wake...)
	e.events = append(e.events, other.events...)
}

// publish records the events and wakes the members a step reached.
func (a *App) publishRelay(ctx context.Context, effect relayEffect) {
	for _, ev := range effect.events {
		_ = a.recordEvent(ctx, ev)
	}
	a.Hub.Publish()
	if a.Scheduler == nil {
		return
	}
	for _, id := range effect.wake {
		if id != "" {
			a.Scheduler.Wake(id)
		}
	}
}

// relayEntry says who moved the work on and why, for the card it creates.
type relayEntry struct {
	from      string // bot id, "" for the user
	chain     string
	sourceRun string
	returnTo  int    // -1, or the stage a rework card goes back to
	note      string // why this card exists: a send-back, a resume, the user's changes
}

var stageKindNames = map[string]string{
	domain.StageConfirm: "用户确认", domain.StageDesign: "产品设计", domain.StageReview: "评审",
	domain.StageTestCases: "测试用例", domain.StageImplement: "实现", domain.StageTest: "测试执行",
	domain.StageAccept: "验收汇报", domain.CardDraft: "需求文档", domain.CardEscalation: "处理上报",
}

var reviewTypeNames = map[string]string{
	"requirements": "需求", "design": "设计", "test_plan": "测试用例", "code": "代码", "general": "通用",
}

// StartRequirementInput is the project manager's requirement and route.
type StartRequirementInput struct {
	Title    string
	Document string
	Stages   []domain.Stage
}

func routeInvalid(reason string) error {
	return domain.InvalidRequest("error.requirement.route_invalid", map[string]any{"reason": reason})
}

// validateRoute checks the few rules CoBot keeps for every route; which
// stages a requirement needs is the project manager's call.
func validateRoute(ctx context.Context, q sqlite.Queryer, team domain.Team, stages []domain.Stage) ([]domain.Stage, error) {
	if len(stages) < 2 {
		return nil, routeInvalid("链路至少要有用户确认和验收汇报两环")
	}
	out := make([]domain.Stage, len(stages))
	copy(out, stages)
	first := 0
	if out[0].Kind == domain.StageReview && out[0].ReviewType == "requirements" {
		first = 1
	}
	if out[first].Kind != domain.StageConfirm {
		return nil, routeInvalid("第一环必须是用户确认（前面最多只能有一环需求评审）")
	}
	for i := range out {
		s := &out[i]
		s.Name = strings.TrimSpace(s.Name)
		if s.Name == "" {
			s.Name = stageKindNames[s.Kind]
		}
		last := i == len(out)-1
		switch s.Kind {
		case domain.StageAccept:
			if !last {
				return nil, routeInvalid("验收汇报只能是最后一环")
			}
			s.OwnerID = team.LeaderID
			continue
		case domain.StageConfirm:
			s.OwnerID = ""
			s.ReviewType = ""
		case domain.StageReview:
			if !roles.ValidReviewType(s.ReviewType) {
				return nil, routeInvalid(fmt.Sprintf("第 %d 环是评审，reviewType 只能是 %s", i+1, strings.Join(roles.ReviewTypes, "、")))
			}
		case domain.StageDesign, domain.StageTestCases, domain.StageImplement, domain.StageTest:
			s.ReviewType = ""
		default:
			return nil, routeInvalid(fmt.Sprintf("第 %d 环的 kind %q 不认识", i+1, s.Kind))
		}
		if last {
			return nil, routeInvalid("最后一环必须是项目经理的验收汇报（kind=accept）")
		}
		if s.Kind == domain.StageConfirm {
			continue
		}
		bot, err := sqlite.GetBot(ctx, q, s.OwnerID)
		if err != nil || bot.TeamID != team.ID {
			return nil, routeInvalid(fmt.Sprintf("第 %d 环「%s」的负责人不是团队成员", i+1, s.Name))
		}
		if bot.ID == team.LeaderID {
			return nil, routeInvalid(fmt.Sprintf("第 %d 环「%s」要交给成员做，项目经理只负责最后的验收汇报", i+1, s.Name))
		}
	}
	return out, nil
}

// StartRequirement records the project manager's requirement document and
// route and hands out the first stage, which is the user's confirmation.
func (a *App) StartRequirement(ctx context.Context, principal auth.Principal, in StartRequirementInput) (domain.Requirement, error) {
	cached, unlock, writeErr := beginWrite[domain.Requirement](ctx, a, principal)
	defer unlock()
	if writeErr != nil {
		return domain.Requirement{}, writeErr
	}
	if cached != nil {
		return *cached, nil
	}
	if principal.Kind != auth.KindBot {
		return domain.Requirement{}, domain.Forbidden("error.forbidden.bot_only", nil)
	}
	title, document := strings.TrimSpace(in.Title), strings.TrimSpace(in.Document)
	if title == "" || document == "" {
		return domain.Requirement{}, domain.InvalidRequest("error.requirement.document_required", nil)
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return domain.Requirement{}, err
	}
	defer tx.Rollback()
	bot, err := sqlite.GetBot(ctx, tx, principal.BotID)
	if err != nil {
		return domain.Requirement{}, sqlite.NormalizeError(err)
	}
	team, _, err := sqlite.GetTeam(ctx, tx, bot.TeamID)
	if err != nil {
		return domain.Requirement{}, sqlite.NormalizeError(err)
	}
	if team.LeaderID != bot.ID {
		return domain.Requirement{}, domain.Forbidden("error.requirement.leader_only", nil)
	}
	if team.Status != domain.TeamStatusActive {
		return domain.Requirement{}, domain.Conflict("error.team.not_active", map[string]any{"status": team.Status})
	}
	stages, err := validateRoute(ctx, tx, team, in.Stages)
	if err != nil {
		return domain.Requirement{}, err
	}
	req := domain.Requirement{
		ID: domain.NewID(), TeamID: team.ID, Title: title, Document: document, Stages: stages,
		Status: domain.RequirementActive, CreatedBy: bot.ID, ChainID: chainOfRunLocked(ctx, tx, bot.ID), Revision: 1,
	}
	if err := sqlite.InsertRequirement(ctx, tx, req); err != nil {
		return domain.Requirement{}, err
	}
	effect, err := a.enterStageLocked(ctx, tx, req, 0, relayEntry{from: bot.ID, chain: req.ChainID, sourceRun: principal.RunID, returnTo: -1})
	if err != nil {
		return domain.Requirement{}, err
	}
	if req, err = sqlite.GetRequirement(ctx, tx, req.ID); err != nil {
		return domain.Requirement{}, err
	}
	if err := a.saveWriteResult(ctx, tx, principal, req); err != nil {
		return domain.Requirement{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Requirement{}, err
	}
	a.publishRelay(ctx, effect)
	return req, nil
}

// enterStageLocked hands out one stage: a confirmation for the user, or a
// card for its owner.
func (a *App) enterStageLocked(ctx context.Context, tx *sql.Tx, req domain.Requirement, idx int, e relayEntry) (relayEffect, error) {
	var effect relayEffect
	if idx < 0 || idx >= len(req.Stages) {
		return effect, fmt.Errorf("stage %d outside the route of %s", idx, req.ID)
	}
	stage := req.Stages[idx]
	effect.events = append(effect.events, domain.Event{Type: domain.EventRequirementUpdated, TeamID: req.TeamID, ResourceID: req.ID})
	if stage.Kind == domain.StageConfirm {
		team, _, err := sqlite.GetTeam(ctx, tx, req.TeamID)
		if err != nil {
			return effect, err
		}
		text, err := a.confirmText(ctx, tx, req, idx, e.note)
		if err != nil {
			return effect, err
		}
		// The document is read in the conversation; the question only asks.
		posted, err := postDocumentLocked(ctx, tx, req.TeamID, team.LeaderID, text)
		if err != nil {
			return effect, err
		}
		ask := "请确认上面的内容。"
		if criteria := strings.TrimSpace(req.Stages[idx].Criteria); criteria != "" {
			ask = "请确认：" + criteria
		}
		question := domain.Question{
			ID: domain.NewID(), Kind: domain.QuestionKindConfirm, RequirementID: req.ID, StageIndex: idx,
			TeamID: req.TeamID, RequesterBotID: team.LeaderID, Question: ask, Options: []string{},
			Status: domain.QuestionOpen, Revision: 1,
		}
		if err := sqlite.InsertQuestion(ctx, tx, question); err != nil {
			return effect, err
		}
		effect.events = append(effect.events, posted, domain.Event{Type: domain.EventQuestionCreated, TeamID: req.TeamID, BotID: team.LeaderID, ResourceID: question.ID})
		return effect, nil
	}
	card := domain.Task{
		ID: domain.NewID(), TeamID: req.TeamID, OwnerID: stage.OwnerID, Title: stage.Name,
		AcceptanceCriteria: stage.Criteria, Status: domain.TaskTodo, ReviewType: stage.ReviewType,
		RequirementID: req.ID, StageIndex: idx, StageKind: stage.Kind, ReturnTo: e.returnTo, Revision: 1,
	}
	other, err := a.handOutCardLocked(ctx, tx, req, card, e)
	effect.add(other)
	return effect, err
}

// handOutCardLocked stores a card and delivers it to its owner.
func (a *App) handOutCardLocked(ctx context.Context, tx *sql.Tx, req domain.Requirement, card domain.Task, e relayEntry) (relayEffect, error) {
	var effect relayEffect
	card.CreatedBy = e.from
	if card.CreatedBy == "" {
		card.CreatedBy = domain.TaskAuthorUser
	}
	if err := sqlite.InsertTask(ctx, tx, card); err != nil {
		return effect, err
	}
	text, err := a.cardText(ctx, tx, req, card, e.note)
	if err != nil {
		return effect, err
	}
	if err := enqueueSystemMessageLocked(ctx, tx, systemMessage{
		teamID: req.TeamID, target: card.OwnerID, source: e.from, text: text, about: card.ID,
		taskID: card.ID, chainID: e.chain, sourceRun: e.sourceRun, kind: "card",
		card: map[string]any{"taskId": card.ID, "title": card.Title, "acceptanceCriteria": card.AcceptanceCriteria, "ownerId": card.OwnerID,
			"requirementId": req.ID, "requirementTitle": req.Title, "stage": stageLabel(req, card), "stageKind": card.StageKind},
	}); err != nil {
		return effect, err
	}
	effect.wake = append(effect.wake, card.OwnerID)
	effect.events = append(effect.events, domain.Event{Type: domain.EventTaskCreated, TeamID: req.TeamID, BotID: card.OwnerID, ResourceID: card.ID, Revision: card.Revision})
	return effect, nil
}

// leaderCardLocked hands the project manager a card outside the route: the
// requirement document to rework, or an escalation to handle.
func (a *App) leaderCardLocked(ctx context.Context, tx *sql.Tx, req domain.Requirement, kind string, stageIndex int, title string, e relayEntry) (relayEffect, error) {
	team, _, err := sqlite.GetTeam(ctx, tx, req.TeamID)
	if err != nil {
		return relayEffect{}, err
	}
	card := domain.Task{
		ID: domain.NewID(), TeamID: req.TeamID, OwnerID: team.LeaderID, Title: title, Status: domain.TaskTodo,
		RequirementID: req.ID, StageIndex: stageIndex, StageKind: kind, ReturnTo: e.returnTo, Revision: 1,
	}
	return a.handOutCardLocked(ctx, tx, req, card, e)
}

// producingStage is the stage whose output a checking stage looks at: the
// nearest earlier stage that makes something, or -1 for the requirement
// document itself.
func producingStage(req domain.Requirement, idx int) int {
	for j := idx - 1; j >= 0; j-- {
		if req.Stages[j].ProducesWork() {
			return j
		}
	}
	return -1
}

// reworkLocked sends work back to a stage (or to the requirement document)
// with the reason; returnTo is where the fixed work goes next, -1 for the
// route's own order.
func (a *App) reworkLocked(ctx context.Context, tx *sql.Tx, req domain.Requirement, target int, e relayEntry) (relayEffect, error) {
	if target < 0 {
		return a.leaderCardLocked(ctx, tx, req, domain.CardDraft, -1, "修改需求文档", e)
	}
	stage := req.Stages[target]
	card := domain.Task{
		ID: domain.NewID(), TeamID: req.TeamID, OwnerID: stage.OwnerID, Title: "返工：" + stage.Name,
		AcceptanceCriteria: stage.Criteria, Status: domain.TaskTodo, ReviewType: stage.ReviewType,
		RequirementID: req.ID, StageIndex: target, StageKind: stage.Kind, ReturnTo: e.returnTo, Revision: 1,
	}
	return a.handOutCardLocked(ctx, tx, req, card, e)
}

// HandoffInput is one handoff of a card.
type HandoffInput struct {
	CardID       string
	Outcome      string
	Summary      string
	ArtifactRefs []string
	Mode         string
	Suggestions  string
	Document     string
}

// Handoff records how a member hands a card off and moves the work along the
// route: done goes to the next stage (or back to the stage that sent it for
// rework), back sends the checked work to the stage that produced it,
// escalate hands the card to the project manager, and resume returns an
// escalated stage to its owner.
func (a *App) Handoff(ctx context.Context, principal auth.Principal, in HandoffInput) (domain.Task, error) {
	cached, unlock, writeErr := beginWrite[domain.Task](ctx, a, principal)
	defer unlock()
	if writeErr != nil {
		return domain.Task{}, writeErr
	}
	if cached != nil {
		return *cached, nil
	}
	if principal.Kind != auth.KindBot {
		return domain.Task{}, domain.Forbidden("error.forbidden.bot_only", nil)
	}
	summary := strings.TrimSpace(in.Summary)
	if summary == "" {
		return domain.Task{}, domain.InvalidRequest("error.card.summary_required", nil)
	}
	if in.ArtifactRefs == nil {
		in.ArtifactRefs = []string{}
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return domain.Task{}, err
	}
	defer tx.Rollback()
	card, err := sqlite.GetTask(ctx, tx, in.CardID)
	if err != nil {
		return domain.Task{}, sqlite.NormalizeError(err)
	}
	if card.TeamID != principal.TeamID || card.OwnerID != principal.BotID {
		return domain.Task{}, domain.Forbidden("error.card.owner_only", map[string]any{"title": card.Title})
	}
	if card.IsTerminal() {
		return domain.Task{}, domain.Conflict("error.card.finished", map[string]any{"title": card.Title})
	}
	req, err := sqlite.GetRequirement(ctx, tx, card.RequirementID)
	if err != nil {
		return domain.Task{}, sqlite.NormalizeError(err)
	}
	if req.Status != domain.RequirementActive {
		return domain.Task{}, domain.Conflict("error.requirement.not_active", map[string]any{"status": req.Status})
	}
	team, _, err := sqlite.GetTeam(ctx, tx, req.TeamID)
	if err != nil {
		return domain.Task{}, err
	}
	execution, err := collectExecutionLocked(ctx, tx, card, principal.RunID)
	if err != nil {
		return domain.Task{}, err
	}
	record := domain.Evidence{
		ID: domain.NewID(), TaskID: card.ID, AuthorID: principal.BotID, Summary: summary,
		ArtifactRefs: in.ArtifactRefs, Outcome: in.Outcome, Suggestions: strings.TrimSpace(in.Suggestions), Execution: execution,
	}
	e := relayEntry{from: principal.BotID, chain: chainOfRunLocked(ctx, tx, principal.BotID), sourceRun: principal.RunID, returnTo: -1}
	from := stageLabel(req, card)
	// The outcome is checked and its route chosen first; the handoff is
	// then recorded, so the card it hands out already sees it.
	var route func() (relayEffect, error)
	switch in.Outcome {
	case domain.HandoffDone:
		switch card.StageKind {
		case domain.CardEscalation:
			return domain.Task{}, domain.InvalidRequest("error.card.resume_escalation", nil)
		case domain.CardDraft:
			document := strings.TrimSpace(in.Document)
			if document == "" {
				return domain.Task{}, domain.InvalidRequest("error.card.document_required", nil)
			}
			if req, err = sqlite.UpdateRequirement(ctx, tx, req.ID, document, req.Status); err != nil {
				return domain.Task{}, err
			}
		}
		if card.StageKind == domain.StageAccept {
			route = func() (relayEffect, error) { return a.finishRouteLocked(ctx, tx, req, team, summary) }
			break
		}
		next := card.StageIndex + 1
		if card.ReturnTo >= 0 {
			next = card.ReturnTo
		}
		if record.Suggestions != "" {
			e.note = fmt.Sprintf("%s通过了，附带建议：%s", from, record.Suggestions)
		}
		route = func() (relayEffect, error) { return a.enterStageLocked(ctx, tx, req, next, e) }
	case domain.HandoffBack:
		if card.StageIndex < 0 || card.StageKind == domain.CardEscalation || !req.Stages[card.StageIndex].ChecksWork() {
			return domain.Task{}, domain.InvalidRequest("error.card.back_not_allowed", nil)
		}
		if in.Mode != domain.ReworkRerun && in.Mode != domain.ReworkReturnToMe {
			return domain.Task{}, domain.InvalidRequest("error.card.back_mode", nil)
		}
		record.Mode = in.Mode
		settings, err := sqlite.GetSettings(ctx, tx)
		if err != nil {
			return domain.Task{}, err
		}
		backs, err := sqlite.CountSendBacks(ctx, tx, req.ID, card.StageIndex)
		if err != nil {
			return domain.Task{}, err
		}
		if backs+1 > settings.ReviewRejectLimit {
			reasons, err := sendBackReasons(ctx, tx, req.ID, card.StageIndex)
			if err != nil {
				return domain.Task{}, err
			}
			reasons = append(reasons, summary)
			var b strings.Builder
			fmt.Fprintf(&b, "%s已经退回 %d 次，超过上限 %d 次，不再自动退回，交给你决定。历次退回原因：\n", from, backs+1, settings.ReviewRejectLimit)
			for i, reason := range reasons {
				fmt.Fprintf(&b, "%d. %s\n", i+1, reason)
			}
			b.WriteString("你可以说明处理意见后用 handoff outcome=resume 交回这一环继续；需要用户决定的先用 ask_user 问用户。")
			e.note = b.String()
			e.returnTo = card.ReturnTo
			title := "处理上报：" + req.Stages[card.StageIndex].Name
			route = func() (relayEffect, error) {
				return a.leaderCardLocked(ctx, tx, req, domain.CardEscalation, card.StageIndex, title, e)
			}
			break
		}
		target := producingStage(req, card.StageIndex)
		after := "改完后按链路继续往下走"
		if in.Mode == domain.ReworkReturnToMe {
			e.returnTo = card.StageIndex
			after = "改完后直接回到" + from
		}
		e.note = fmt.Sprintf("%s退回（第 %d 次，%s）：%s", from, backs+1, after, summary)
		route = func() (relayEffect, error) { return a.reworkLocked(ctx, tx, req, target, e) }
	case domain.HandoffEscalate:
		if principal.BotID == team.LeaderID {
			return domain.Task{}, domain.InvalidRequest("error.card.leader_cannot_escalate", nil)
		}
		e.note = fmt.Sprintf("%s上报：%s\n处理后用 handoff outcome=resume 交回这一环继续；需要用户决定的先用 ask_user 问用户。", from, summary)
		// A rework card escalated mid-way still goes back where it was
		// sent from once it is resumed and done.
		e.returnTo = card.ReturnTo
		route = func() (relayEffect, error) {
			return a.leaderCardLocked(ctx, tx, req, domain.CardEscalation, card.StageIndex, "处理上报："+card.Title, e)
		}
	case domain.HandoffResume:
		if card.StageKind != domain.CardEscalation {
			return domain.Task{}, domain.InvalidRequest("error.card.resume_not_escalation", nil)
		}
		e.note = "项目经理的处理意见：" + summary
		e.returnTo = card.ReturnTo
		route = func() (relayEffect, error) { return a.enterStageLocked(ctx, tx, req, card.StageIndex, e) }
	default:
		return domain.Task{}, domain.InvalidRequest("error.card.outcome", nil)
	}
	if err := sqlite.InsertEvidence(ctx, tx, record); err != nil {
		return domain.Task{}, err
	}
	updated, err := sqlite.UpdateTaskStatusTx(ctx, tx, card.ID, domain.TaskDone, 0)
	if err != nil {
		return domain.Task{}, err
	}
	effect, err := route()
	if err != nil {
		return domain.Task{}, err
	}
	if err := a.saveWriteResult(ctx, tx, principal, updated); err != nil {
		return domain.Task{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Task{}, err
	}
	effect.events = append(effect.events, domain.Event{Type: domain.EventTaskUpdated, TeamID: card.TeamID, BotID: principal.BotID, ResourceID: card.ID, Revision: updated.Revision,
		Payload: mustJSON(map[string]any{"change": "handoff", "outcome": in.Outcome})})
	a.publishRelay(ctx, effect)
	return updated, nil
}

// sendBackReasons lists the reasons one stage gave each time it sent work
// back, oldest first.
func sendBackReasons(ctx context.Context, q sqlite.Queryer, requirementID string, stageIndex int) ([]string, error) {
	rows, err := q.QueryContext(ctx, `SELECT e.summary FROM task_evidence e JOIN tasks t ON t.id = e.task_id
		WHERE t.requirement_id = ? AND t.stage_index = ? AND t.stage_kind != ? AND e.outcome = ? ORDER BY e.created_at`,
		requirementID, stageIndex, domain.CardEscalation, domain.HandoffBack)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if rows.Scan(&s) == nil {
			out = append(out, s)
		}
	}
	return out, rows.Err()
}

// finishRouteLocked ends the route: the project manager's report becomes
// the user's acceptance question, with every suggestion passed on.
func (a *App) finishRouteLocked(ctx context.Context, tx *sql.Tx, req domain.Requirement, team domain.Team, report string) (relayEffect, error) {
	var effect relayEffect
	text := report
	if suggestions, err := requirementSuggestions(ctx, tx, req.ID); err != nil {
		return effect, err
	} else if len(suggestions) > 0 {
		text += "\n\n遗留建议：\n- " + strings.Join(suggestions, "\n- ")
	}
	// The report is read in the conversation; the question only asks.
	posted, err := postDocumentLocked(ctx, tx, req.TeamID, team.LeaderID, fmt.Sprintf("## %s · 验收汇报\n\n%s", req.Title, text))
	if err != nil {
		return effect, err
	}
	question := domain.Question{
		ID: domain.NewID(), Kind: domain.QuestionKindAcceptance, RequirementID: req.ID, StageIndex: len(req.Stages) - 1,
		TeamID: req.TeamID, RequesterBotID: team.LeaderID, Question: fmt.Sprintf("需求「%s」已完成，请按上面的汇报验收。", req.Title), Options: []string{},
		Status: domain.QuestionOpen, Revision: 1,
	}
	if err := sqlite.InsertQuestion(ctx, tx, question); err != nil {
		return effect, err
	}
	if _, err := sqlite.UpdateRequirement(ctx, tx, req.ID, req.Document, domain.RequirementAccepting); err != nil {
		return effect, err
	}
	effect.events = append(effect.events, posted,
		domain.Event{Type: domain.EventQuestionCreated, TeamID: req.TeamID, BotID: team.LeaderID, ResourceID: question.ID},
		domain.Event{Type: domain.EventRequirementUpdated, TeamID: req.TeamID, ResourceID: req.ID})
	return effect, nil
}

// postDocumentLocked puts a document (the requirement to confirm, the report
// to accept) into the project manager's conversation, as a message from it,
// so it is read where the conversation is, before the question that asks for
// the decision. It stays in the history after the decision.
func postDocumentLocked(ctx context.Context, tx *sql.Tx, teamID, botID, text string) (domain.Event, error) {
	blocks, err := json.Marshal([]domain.TextBlock{{Type: "text", Text: text}})
	if err != nil {
		return domain.Event{}, err
	}
	message := domain.Message{
		ID: domain.NewID(), TeamID: teamID, BotID: botID,
		Kind: domain.MessageKindAssistant, Blocks: blocks, CreatedAt: time.Now().UTC(),
	}
	if err := sqlite.InsertMessage(ctx, tx, message); err != nil {
		return domain.Event{}, err
	}
	return domain.Event{Type: domain.EventMessageCreated, TeamID: teamID, BotID: botID, ResourceID: message.ID}, nil
}

func requirementSuggestions(ctx context.Context, q sqlite.Queryer, requirementID string) ([]string, error) {
	rows, err := q.QueryContext(ctx, `SELECT e.suggestions FROM task_evidence e JOIN tasks t ON t.id = e.task_id
		WHERE t.requirement_id = ? AND e.suggestions != '' ORDER BY e.created_at`, requirementID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if rows.Scan(&s) == nil {
			out = append(out, s)
		}
	}
	return out, rows.Err()
}

// AnswerDecision records the user's verdict on a confirm stage or on the
// finished requirement. Confirming moves the route on; asking for changes
// sends the work back to the stage that produced it (the requirement
// document for the first confirmation); with mode return_to_me the fixed
// work comes straight back to this confirmation instead of running the
// stages in between again. Accepting the finished requirement closes it;
// asking for changes there is new input for the project manager.
func (a *App) AnswerDecision(ctx context.Context, principal auth.Principal, questionID, decision, mode, comment string, expectedRevision int64) (domain.Question, error) {
	cached, unlock, writeErr := beginWrite[domain.Question](ctx, a, principal)
	defer unlock()
	if writeErr != nil {
		return domain.Question{}, writeErr
	}
	if cached != nil {
		return *cached, nil
	}
	if principal.Kind != auth.KindUser {
		return domain.Question{}, domain.Forbidden("error.forbidden.user_only", nil)
	}
	question, err := sqlite.GetQuestion(ctx, a.DB, questionID)
	if err != nil {
		return domain.Question{}, sqlite.NormalizeError(err)
	}
	if question.Kind != domain.QuestionKindConfirm && question.Kind != domain.QuestionKindAcceptance {
		return domain.Question{}, domain.InvalidRequest("error.question.not_acceptance", nil)
	}
	if question.Status != domain.QuestionOpen {
		return domain.Question{}, domain.Conflict("error.question.already_answered", map[string]any{"status": question.Status})
	}
	if mode != "" && mode != domain.ReworkRerun && mode != domain.ReworkReturnToMe {
		return domain.Question{}, domain.InvalidRequest("error.question.revise_mode", nil)
	}
	comment = strings.TrimSpace(comment)
	var answerText string
	switch decision {
	case domain.AcceptanceAccept:
		answerText = "通过"
		if comment != "" {
			answerText += "：" + comment
		}
	case domain.AcceptanceRevise:
		if comment == "" {
			return domain.Question{}, domain.InvalidRequest("error.question.revise_reason_required", nil)
		}
		answerText = "要求修改：" + comment
	default:
		return domain.Question{}, domain.InvalidRequest("error.question.acceptance_decision", nil)
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return domain.Question{}, err
	}
	defer tx.Rollback()
	req, err := sqlite.GetRequirement(ctx, tx, question.RequirementID)
	if err != nil {
		return domain.Question{}, sqlite.NormalizeError(err)
	}
	var effect relayEffect
	messageID := ""
	if question.Kind == domain.QuestionKindConfirm {
		if req.Status != domain.RequirementActive {
			return domain.Question{}, domain.Conflict("error.requirement.not_active", map[string]any{"status": req.Status})
		}
		team, _, err := sqlite.GetTeam(ctx, tx, req.TeamID)
		if err != nil {
			return domain.Question{}, err
		}
		// The user's decision is fresh input: it opens its own chain.
		chain, err := a.openChainLocked(ctx, tx, req.TeamID, question.ID, "")
		if err != nil {
			return domain.Question{}, err
		}
		if decision == domain.AcceptanceAccept {
			next := question.StageIndex + 1
			e := relayEntry{chain: chain.ID, returnTo: -1}
			// The confirmed route dispatches in the project manager's
			// name, so the member sees where the card came from and can
			// open it. A card that lands on the project manager itself
			// (the acceptance report) stays the user's doing.
			if owner := req.Stages[next].OwnerID; owner != "" && owner != team.LeaderID {
				e.from = team.LeaderID
			}
			var err error
			effect, err = a.enterStageLocked(ctx, tx, req, next, e)
			if err != nil {
				return domain.Question{}, err
			}
		} else {
			producer := producingStage(req, question.StageIndex)
			e := relayEntry{chain: chain.ID, returnTo: -1}
			// A member's rework goes out in the project manager's name;
			// a revision of the document itself stays the user's input.
			if producer >= 0 {
				e.from = team.LeaderID
			}
			after := "改完后按链路重新往下走，会再经过评审"
			if mode == domain.ReworkReturnToMe {
				e.returnTo = question.StageIndex
				after = "改完后直接回到用户确认"
			}
			e.note = "用户在「" + req.Stages[question.StageIndex].Name + "」提出修改意见（" + after + "）：" + comment
			effect, err = a.reworkLocked(ctx, tx, req, producer, e)
			if err != nil {
				return domain.Question{}, err
			}
		}
	} else {
		if _, err := sqlite.UpdateRequirement(ctx, tx, req.ID, req.Document, domain.RequirementDone); err != nil {
			return domain.Question{}, err
		}
		effect.events = append(effect.events, domain.Event{Type: domain.EventRequirementUpdated, TeamID: req.TeamID, ResourceID: req.ID})
		if decision == domain.AcceptanceRevise {
			if messageID, err = a.reviseAsNewInputLocked(ctx, tx, question, comment); err != nil {
				return domain.Question{}, err
			}
			effect.wake = append(effect.wake, question.RequesterBotID)
			effect.events = append(effect.events, domain.Event{Type: domain.EventMessageCreated, TeamID: question.TeamID, BotID: question.RequesterBotID, ResourceID: messageID})
		}
	}
	answered, err := sqlite.AnswerQuestion(ctx, tx, questionID, answerText, messageID, expectedRevision)
	if err != nil {
		return domain.Question{}, sqlite.NormalizeError(err)
	}
	if err := a.saveWriteResult(ctx, tx, principal, answered); err != nil {
		return domain.Question{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Question{}, err
	}
	effect.events = append(effect.events, domain.Event{Type: domain.EventQuestionAnswered, TeamID: question.TeamID, BotID: question.RequesterBotID, ResourceID: question.ID})
	a.publishRelay(ctx, effect)
	return answered, nil
}

// reviseAsNewInputLocked hands the user's changes to a finished requirement
// to the project manager as the user's own message.
func (a *App) reviseAsNewInputLocked(ctx context.Context, tx *sql.Tx, question domain.Question, comment string) (string, error) {
	blocks, err := json.Marshal([]domain.TextBlock{{Type: "text", Text: "验收未通过，需要修改：" + comment}})
	if err != nil {
		return "", err
	}
	message := domain.Message{
		ID: domain.NewID(), TeamID: question.TeamID, BotID: question.RequesterBotID,
		Kind: domain.MessageKindUser, Blocks: blocks, DeliveryStatus: domain.DeliveryQueued, RunID: question.ID,
		CreatedAt: time.Now().UTC(),
	}
	if err := sqlite.InsertMessage(ctx, tx, message); err != nil {
		return "", err
	}
	chain, err := a.openChainLocked(ctx, tx, question.TeamID, message.ID, "")
	if err != nil {
		return "", err
	}
	if err := sqlite.InsertDeliveryRow(ctx, tx, sqlite.DeliveryRecord{
		ID: domain.NewID(), MessageID: message.ID, TeamID: question.TeamID,
		TargetBotID: question.RequesterBotID, Status: domain.DeliveryQueued, ChainID: chain.ID, Kind: "question_answer",
	}); err != nil {
		return "", err
	}
	if _, ferr := sqlite.FindActiveRun(ctx, tx, question.RequesterBotID); ferr != nil {
		if !errors.Is(ferr, sqlite.ErrNotFound) {
			return "", ferr
		}
		if err := sqlite.SetBotRunState(ctx, tx, question.RequesterBotID, domain.BotStatusQueued, ""); err != nil {
			return "", err
		}
	}
	return message.ID, nil
}

// CancelRequirement stops a requirement for the user: its open cards and
// confirmations end, and nothing more of it is handed out. A member already
// working on a card finishes its turn, but its handoff is refused.
func (a *App) CancelRequirement(ctx context.Context, principal auth.Principal, requirementID string) (domain.Requirement, error) {
	cached, unlock, writeErr := beginWrite[domain.Requirement](ctx, a, principal)
	defer unlock()
	if writeErr != nil {
		return domain.Requirement{}, writeErr
	}
	if cached != nil {
		return *cached, nil
	}
	if principal.Kind != auth.KindUser {
		return domain.Requirement{}, domain.Forbidden("error.forbidden.user_only", nil)
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return domain.Requirement{}, err
	}
	defer tx.Rollback()
	req, err := sqlite.GetRequirement(ctx, tx, requirementID)
	if err != nil {
		return domain.Requirement{}, sqlite.NormalizeError(err)
	}
	if principal.Write != nil && principal.Write.ExpectedRevision > 0 && req.Revision != principal.Write.ExpectedRevision {
		expected := principal.Write.ExpectedRevision
		return domain.Requirement{}, domain.RevisionConflict("error.requirement.revision_conflict", map[string]any{"expected": expected, "actual": req.Revision})
	}
	if req.Status == domain.RequirementDone || req.Status == domain.RequirementCancelled {
		return domain.Requirement{}, domain.Conflict("error.requirement.not_active", map[string]any{"status": req.Status})
	}
	cards, err := sqlite.ListRequirementTasks(ctx, tx, req.ID)
	if err != nil {
		return domain.Requirement{}, err
	}
	var effect relayEffect
	var cancelled []string
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, card := range cards {
		if card.IsTerminal() {
			continue
		}
		cancelled = append(cancelled, card.ID)
		if _, err := sqlite.UpdateTaskStatusTx(ctx, tx, card.ID, domain.TaskCancelled, 0); err != nil {
			return domain.Requirement{}, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE deliveries SET status = ?, updated_at = ? WHERE task_id = ? AND status = ?`,
			domain.DeliveryCancelled, now, card.ID, domain.DeliveryQueued); err != nil {
			return domain.Requirement{}, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE messages SET delivery_status = ? WHERE run_id = ? AND delivery_status = ?`,
			domain.DeliveryCancelled, card.ID, domain.DeliveryQueued); err != nil {
			return domain.Requirement{}, err
		}
		effect.wake = append(effect.wake, card.OwnerID)
		effect.events = append(effect.events, domain.Event{Type: domain.EventTaskUpdated, TeamID: req.TeamID, ResourceID: card.ID})
	}
	if _, err := tx.ExecContext(ctx, `UPDATE questions SET status = ?, revision = revision + 1 WHERE requirement_id = ? AND status = ?`,
		domain.QuestionCancelled, req.ID, domain.QuestionOpen); err != nil {
		return domain.Requirement{}, err
	}
	if req, err = sqlite.UpdateRequirement(ctx, tx, req.ID, req.Document, domain.RequirementCancelled); err != nil {
		return domain.Requirement{}, err
	}
	if err := a.saveWriteResult(ctx, tx, principal, req); err != nil {
		return domain.Requirement{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Requirement{}, err
	}
	effect.events = append(effect.events, domain.Event{Type: domain.EventRequirementUpdated, TeamID: req.TeamID, ResourceID: req.ID})
	a.publishRelay(ctx, effect)
	a.interruptCancelledWork(ctx, cancelled)
	return req, nil
}

// interruptCancelledWork stops the turns members are spending on cards that
// were just cancelled, so cancelling takes effect now rather than when the
// member next hands something off. A turn that also carries other work (a
// message from the user, another requirement's card) is left running: only
// work that is entirely for the cancelled cards is stopped. It is best
// effort; a turn that cannot be interrupted simply finishes, and its handoff
// finds the card gone.
func (a *App) interruptCancelledWork(ctx context.Context, cardIDs []string) {
	if a.Scheduler == nil || len(cardIDs) == 0 {
		return
	}
	cancelled := make(map[string]bool, len(cardIDs))
	for _, id := range cardIDs {
		cancelled[id] = true
	}
	// The runs the cancelled cards' deliveries started, and what else each
	// of those runs was started for.
	rows, err := a.DB.QueryContext(ctx, `SELECT run_id, task_id FROM deliveries
		WHERE run_id IN (SELECT run_id FROM deliveries WHERE run_id != '' AND task_id IN (`+placeholders(len(cardIDs))+`))`, anyStrings(cardIDs)...)
	if err != nil {
		a.logWarn("list runs of cancelled cards", err)
		return
	}
	runs := map[string]bool{} // run id -> wholly for cancelled cards
	for rows.Next() {
		var runID, taskID string
		if err := rows.Scan(&runID, &taskID); err != nil {
			rows.Close()
			a.logWarn("read runs of cancelled cards", err)
			return
		}
		whole, seen := runs[runID]
		if !seen {
			whole = true
		}
		runs[runID] = whole && cancelled[taskID]
	}
	rows.Close()
	for runID, whole := range runs {
		if !whole {
			continue
		}
		if _, err := a.InterruptRun(ctx, runID); err != nil {
			// Not active any more, or not interruptible: nothing to stop.
			var de *domain.Error
			if !errors.As(err, &de) || de.Code != domain.CodeConflict {
				a.logWarn("interrupt run of cancelled card", err)
			}
		}
	}
}

func (a *App) logWarn(what string, err error) {
	if a.Logger != nil {
		a.Logger.Warn(what, "error", err)
	}
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func anyStrings(values []string) []any {
	out := make([]any, len(values))
	for i, v := range values {
		out[i] = v
	}
	return out
}

// ListRequirements is the user's view of a team's requirements.
func (a *App) ListRequirements(ctx context.Context, principal auth.Principal, teamID string) ([]domain.Requirement, error) {
	if principal.Kind != auth.KindUser {
		return nil, domain.Forbidden("error.forbidden.user_only", nil)
	}
	out, err := sqlite.ListRequirements(ctx, a.DB, teamID, "", 100)
	if out == nil {
		out = []domain.Requirement{}
	}
	return out, sqlite.NormalizeError(err)
}

// startAssignedTasks turns the cards a member was just handed to doing once
// its turn is accepted, and records that it took them up: picking a card up
// is what starting means, and the model is never asked to say so.
func (a *App) startAssignedTasks(ctx context.Context, runID, botID string) {
	rows, err := a.DB.QueryContext(ctx, `SELECT DISTINCT d.task_id FROM deliveries d JOIN tasks t ON t.id = d.task_id
		WHERE d.run_id = ? AND d.task_id != '' AND t.owner_id = ? AND t.status = ?`, runID, botID, domain.TaskTodo)
	if err != nil {
		return
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		tx, err := a.DB.BeginTx(ctx, nil)
		if err != nil {
			return
		}
		task, err := sqlite.GetTask(ctx, tx, id)
		if err != nil || task.Status != domain.TaskTodo {
			tx.Rollback()
			continue
		}
		updated, err := sqlite.UpdateTaskStatusTx(ctx, tx, id, domain.TaskDoing, 0)
		if err != nil || tx.Commit() != nil {
			tx.Rollback()
			continue
		}
		a.recordTaskUpdated(ctx, updated)
		_ = a.recordEvent(ctx, domain.Event{Type: domain.EventCardStarted, TeamID: updated.TeamID, BotID: botID, ResourceID: updated.ID,
			Payload: mustJSON(map[string]any{"title": updated.Title, "createdBy": updated.CreatedBy})})
	}
	a.Hub.Publish()
}

// recordTaskUpdated is a small helper for event publication.
func (a *App) recordTaskUpdated(ctx context.Context, task domain.Task) {
	_ = a.recordEvent(ctx, domain.Event{Type: domain.EventTaskUpdated, TeamID: task.TeamID, ResourceID: task.ID, Revision: task.Revision,
		Payload: mustJSON(map[string]any{"change": "status", "status": task.Status})})
	a.Hub.Publish()
}

// GetTaskDetail returns a card with its handoff records.
func (a *App) GetTaskDetail(ctx context.Context, principal auth.Principal, taskID string) (map[string]any, error) {
	if principal.Kind != auth.KindUser && principal.Kind != auth.KindBot {
		return nil, domain.Unauthorized("error.unauthorized.principal_required")
	}
	task, err := sqlite.GetTask(ctx, a.DB, taskID)
	if err != nil {
		return nil, sqlite.NormalizeError(err)
	}
	// A bot may only read its own team's cards (review B6).
	if principal.Kind == auth.KindBot && task.TeamID != principal.TeamID {
		return nil, domain.Forbidden("error.message.other_team", map[string]any{"taskId": taskID})
	}
	evidence, err := sqlite.ListEvidence(ctx, a.DB, taskID)
	if err != nil {
		return nil, err
	}
	if evidence == nil {
		evidence = []domain.Evidence{}
	}
	return map[string]any{"task": task, "evidence": evidence}, nil
}

// ---- system messages ----

// systemMessage is a message CoBot writes to a member, with the delivery that
// wakes it. about fills the message's run_id slot, which system messages use
// for the card or question they concern.
type systemMessage struct {
	teamID, target, source string
	text                   string
	about                  string
	taskID                 string
	chainID                string
	sourceRun              string
	kind                   string
	// card is the conversation's card for the message; the engine reads
	// only the text block.
	card map[string]any
}

func enqueueSystemMessageLocked(ctx context.Context, tx *sql.Tx, m systemMessage) error {
	parts := []domain.TextBlock{{Type: "text", Text: m.text}}
	if m.card != nil {
		card, err := json.Marshal(m.card)
		if err != nil {
			return err
		}
		parts = append(parts, domain.TextBlock{Type: domain.BlockTypeTask, Text: string(card)})
	}
	blocks, err := json.Marshal(parts)
	if err != nil {
		return err
	}
	message := domain.Message{
		ID: domain.NewID(), TeamID: m.teamID, BotID: m.target,
		Kind: domain.MessageKindSystem, Blocks: blocks, DeliveryStatus: domain.DeliveryQueued, RunID: m.about,
		SourceBotID: m.source,
	}
	if err := sqlite.InsertMessage(ctx, tx, message); err != nil {
		return err
	}
	return sqlite.InsertDeliveryRow(ctx, tx, sqlite.DeliveryRecord{
		ID: domain.NewID(), MessageID: message.ID, TeamID: m.teamID, TargetBotID: m.target,
		Status: domain.DeliveryQueued, ChainID: m.chainID, TaskID: m.taskID,
		SourceRunID: m.sourceRun, Kind: m.kind,
	})
}

// noteLocked leaves a member a note it reads with its next turn; it wakes
// nobody.
func noteLocked(ctx context.Context, tx *sql.Tx, teamID, target, source, about, text string) error {
	blocks, err := json.Marshal([]domain.TextBlock{{Type: "text", Text: text}})
	if err != nil {
		return err
	}
	return sqlite.InsertMessage(ctx, tx, domain.Message{
		ID: domain.NewID(), TeamID: teamID, BotID: target,
		Kind: domain.MessageKindCollaboration, Blocks: blocks, SourceBotID: source, RunID: about,
	})
}
