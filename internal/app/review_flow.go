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

// Review and acceptance. A task the leader marks for review goes to a
// reviewer member on delivery, with the checklist of its review type; the
// reviewer's verdict decides it, and the leader is only woken when the
// reviewer has sent it back too often. When every task of a requirement (the
// tasks one confirmed plan created) has ended, the leader reports the result,
// and that report becomes the user's acceptance question.

// systemMessage is a message CoBot writes to a member, with the delivery that
// wakes it. about fills the message's run_id slot, which system messages use
// for the task or requirement they concern.
type systemMessage struct {
	teamID, target, source string
	text                   string
	about                  string
	taskID                 string
	chainID                string
	sourceRun              string
	kind                   string
}

func enqueueSystemMessageLocked(ctx context.Context, tx *sql.Tx, m systemMessage) error {
	blocks, err := json.Marshal([]domain.TextBlock{{Type: "text", Text: m.text}})
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

// liveReviewerLocked is the reviewer a task's delivery goes to: its assigned
// reviewer while that member still belongs to the team, else none.
func (a *App) liveReviewerLocked(ctx context.Context, tx *sql.Tx, task domain.Task) (string, error) {
	if task.ReviewerID == "" {
		return "", nil
	}
	bot, err := sqlite.GetBot(ctx, tx, task.ReviewerID)
	if errors.Is(err, sqlite.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if bot.TeamID != task.TeamID {
		return "", nil
	}
	return bot.ID, nil
}

// rejectsExhausted reports whether the reviewer has sent the task back more
// often than allowed, so the leader decides it now.
func rejectsExhausted(ctx context.Context, q sqlite.Queryer, task domain.Task) bool {
	settings, err := sqlite.GetSettings(ctx, q)
	if err != nil {
		return false
	}
	return task.RejectCount > settings.ReviewRejectLimit
}

// enqueueReviewRequestLocked hands a delivery to its reviewer with what the
// reviewer needs in one message: the task, its completion criteria, the
// delivery and the checklist of its review type.
func (a *App) enqueueReviewRequestLocked(ctx context.Context, tx *sql.Tx, principal auth.Principal, task domain.Task, reviewerID, summary string, artifactRefs []string) error {
	owner, err := sqlite.GetBot(ctx, tx, task.OwnerID)
	if err != nil {
		return err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "请评审：%s 交付了任务「%s」（任务 ID：%s）。\n", owner.Name, task.Title, task.ID)
	if task.Description != "" {
		fmt.Fprintf(&b, "任务要求：%s\n", task.Description)
	}
	fmt.Fprintf(&b, "完成标准：%s\n", task.AcceptanceCriteria)
	fmt.Fprintf(&b, "交付说明：%s\n", summary)
	if len(artifactRefs) > 0 {
		fmt.Fprintf(&b, "产出文件：%s\n", strings.Join(artifactRefs, "、"))
	}
	if task.RejectCount > 0 {
		fmt.Fprintf(&b, "这是第 %d 次打回后的重新交付。\n", task.RejectCount)
	}
	if checklist := roles.Checklist(task.ReviewType); checklist != "" {
		b.WriteString("\n" + checklist + "\n")
	}
	b.WriteString("\n看过产出后用 review_task 给出结论：通过选 accept；不通过选 reject，并在 comment 写明缺什么、怎样算改好。")
	return enqueueSystemMessageLocked(ctx, tx, systemMessage{
		teamID: task.TeamID, target: reviewerID, source: principal.BotID, text: b.String(), about: task.ID,
		taskID: task.ID, chainID: chainOfRunLocked(ctx, tx, principal.BotID), sourceRun: principal.RunID, kind: "review_request",
	})
}

// reviewSummary is how a reviewer's verdict reads in the task's record.
func reviewSummary(accept bool, comment string) string {
	verdict := "评审通过"
	if !accept {
		verdict = "评审打回"
	}
	if comment == "" {
		return verdict
	}
	return verdict + "：" + comment
}

// noteReviewPassedLocked leaves the leader a note that a reviewer accepted a
// task. It wakes nobody: the leader reads it with its next turn.
func (a *App) noteReviewPassedLocked(ctx context.Context, tx *sql.Tx, principal auth.Principal, team domain.Team, task domain.Task, comment string) error {
	if team.LeaderID == "" || team.LeaderID == principal.BotID {
		return nil
	}
	text := fmt.Sprintf("评审通过：任务「%s」（任务 ID：%s）已完成。", task.Title, task.ID)
	if comment != "" {
		text += "评审意见：" + comment
	}
	blocks, err := json.Marshal([]domain.TextBlock{{Type: "text", Text: text}})
	if err != nil {
		return err
	}
	return sqlite.InsertMessage(ctx, tx, domain.Message{
		ID: domain.NewID(), TeamID: task.TeamID, BotID: team.LeaderID,
		Kind: domain.MessageKindCollaboration, Blocks: blocks, SourceBotID: principal.BotID, RunID: task.ID,
	})
}

// enqueueReviewEscalationLocked wakes the leader for a task its reviewer sent
// back more often than allowed, with every reason so far. It reports the
// leader to wake.
func (a *App) enqueueReviewEscalationLocked(ctx context.Context, tx *sql.Tx, principal auth.Principal, team domain.Team, task domain.Task, limit int) (string, error) {
	if team.LeaderID == "" {
		return "", nil
	}
	evidence, err := sqlite.ListEvidence(ctx, tx, task.ID)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "任务「%s」（任务 ID：%s）被评审员打回已超过 %d 次，不再自动退回负责人，需要你来决定。历次打回原因：\n", task.Title, task.ID, limit)
	n := 0
	for _, item := range evidence {
		if item.ReviewResult == "reject" {
			n++
			fmt.Fprintf(&b, "%d. %s\n", n, item.Summary)
		}
	}
	b.WriteString("任务现在仍待验收：可以用 review_task 直接通过，或打回并写明怎么改（打回后重新计数）；如果是需求本身不清楚，先问用户。")
	if err := enqueueSystemMessageLocked(ctx, tx, systemMessage{
		teamID: task.TeamID, target: team.LeaderID, source: principal.BotID, text: b.String(), about: task.ID,
		taskID: task.ID, chainID: chainOfRunLocked(ctx, tx, principal.BotID), sourceRun: principal.RunID, kind: "review_escalation",
	}); err != nil {
		return "", err
	}
	return team.LeaderID, nil
}

var requirementStatusNames = map[string]string{domain.TaskDone: "已完成", domain.TaskCancelled: "已取消"}

// reportRequirementIfFinishedLocked asks the leader to report a requirement
// to the user once its last task ended. The leader's reply to this message
// becomes the user's acceptance question (see createAcceptanceLocked). Each
// requirement is reported once; nothing is reported when every task was
// cancelled. It reports the leader to wake.
func (a *App) reportRequirementIfFinishedLocked(ctx context.Context, tx *sql.Tx, task domain.Task) (string, error) {
	if task.RequirementID == "" || !task.IsTerminal() {
		return "", nil
	}
	tasks, err := sqlite.ListRequirementTasks(ctx, tx, task.RequirementID)
	if err != nil {
		return "", err
	}
	done := false
	for _, t := range tasks {
		if !t.IsTerminal() {
			return "", nil
		}
		done = done || t.Status == domain.TaskDone
	}
	if !done {
		return "", nil
	}
	var reported int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM deliveries d JOIN messages m ON m.id = d.message_id WHERE d.kind = 'delivery_report' AND m.run_id = ?`, task.RequirementID).Scan(&reported); err != nil {
		return "", err
	}
	if reported > 0 {
		return "", nil
	}
	team, _, err := sqlite.GetTeam(ctx, tx, task.TeamID)
	if err != nil {
		return "", err
	}
	if team.LeaderID == "" {
		return "", nil
	}
	var b strings.Builder
	b.WriteString("这次需求的任务都已结束：\n")
	for _, t := range tasks {
		owner, _ := sqlite.GetBot(ctx, tx, t.OwnerID)
		fmt.Fprintf(&b, "- 「%s」（负责人：%s）%s\n", t.Title, owner.Name, requirementStatusNames[t.Status])
	}
	b.WriteString("请向用户汇报交付结果：做了什么、怎么验证的、怎么使用、还有什么待决事项。你这一轮的回复就是交付说明，系统会据此请用户验收，不要再用 ask_user 询问是否验收。")
	if err := enqueueSystemMessageLocked(ctx, tx, systemMessage{
		teamID: task.TeamID, target: team.LeaderID, text: b.String(), about: task.RequirementID, kind: "delivery_report",
	}); err != nil {
		return "", err
	}
	return team.LeaderID, nil
}

// createAcceptanceLocked turns the reply of a leader's turn that reported a
// requirement into the user's acceptance question. It runs in the settlement
// of that turn and returns the questions it created.
func (a *App) createAcceptanceLocked(ctx context.Context, tx *sql.Tx, run domain.Run, reply string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT m.run_id FROM deliveries d JOIN messages m ON m.id = d.message_id WHERE d.run_id = ? AND d.kind = 'delivery_report'`, run.ID)
	if err != nil {
		return nil, err
	}
	var requirements []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil && id != "" {
			requirements = append(requirements, id)
		}
	}
	rows.Close()
	summary := strings.TrimSpace(reply)
	if summary == "" {
		summary = "这次需求的任务都已结束，队长没有给出交付说明，可以查看各任务的交付与评审记录。"
	}
	var created []string
	for _, requirement := range requirements {
		var open int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM questions WHERE kind = ? AND requirement_id = ?`, domain.QuestionKindAcceptance, requirement).Scan(&open); err != nil {
			return nil, err
		}
		if open > 0 {
			continue
		}
		question := domain.Question{
			ID: domain.NewID(), Kind: domain.QuestionKindAcceptance, RequirementID: requirement,
			TeamID: run.TeamID, RequesterBotID: run.BotID, RunID: run.ID,
			Question: summary, Options: []string{}, Status: domain.QuestionOpen, Revision: 1,
		}
		if err := sqlite.InsertQuestion(ctx, tx, question); err != nil {
			return nil, err
		}
		created = append(created, question.ID)
	}
	return created, nil
}

// AnswerAcceptance records the user's verdict on a finished requirement.
// Accepting closes it and wakes nobody; asking for changes requires the
// changes and wakes the leader with them, as fresh user input.
func (a *App) AnswerAcceptance(ctx context.Context, principal auth.Principal, questionID, decision, comment string, expectedRevision int64) (domain.Question, error) {
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
	if question.Kind != domain.QuestionKindAcceptance {
		return domain.Question{}, domain.InvalidRequest("error.question.not_acceptance", nil)
	}
	if question.Status != domain.QuestionOpen {
		return domain.Question{}, domain.Conflict("error.question.already_answered", map[string]any{"status": question.Status})
	}
	comment = strings.TrimSpace(comment)
	var answerText string
	switch decision {
	case domain.AcceptanceAccept:
		answerText = "验收通过"
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
	messageID := ""
	if decision == domain.AcceptanceRevise {
		text := fmt.Sprintf("用户验收未通过，要求修改：%s\n请按意见安排后续任务；这批任务结束后，系统会再请用户验收。", comment)
		blocks, err := json.Marshal([]domain.TextBlock{{Type: "text", Text: text}})
		if err != nil {
			return domain.Question{}, err
		}
		message := domain.Message{
			ID: domain.NewID(), TeamID: question.TeamID, BotID: question.RequesterBotID,
			Kind: domain.MessageKindUser, Blocks: blocks, DeliveryStatus: domain.DeliveryQueued, RunID: question.ID,
			CreatedAt: time.Now().UTC(),
		}
		if err := sqlite.InsertMessage(ctx, tx, message); err != nil {
			return domain.Question{}, err
		}
		// The changes are fresh user input: a new chain, and the tasks the
		// leader creates for them form a new requirement to accept.
		chain, err := a.openChainLocked(ctx, tx, question.TeamID, message.ID, "")
		if err != nil {
			return domain.Question{}, err
		}
		if err := sqlite.InsertDeliveryRow(ctx, tx, sqlite.DeliveryRecord{
			ID: domain.NewID(), MessageID: message.ID, TeamID: question.TeamID,
			TargetBotID: question.RequesterBotID, Status: domain.DeliveryQueued, ChainID: chain.ID,
			Kind: "question_answer",
		}); err != nil {
			return domain.Question{}, err
		}
		if _, ferr := sqlite.FindActiveRun(ctx, tx, question.RequesterBotID); ferr != nil {
			if !errors.Is(ferr, sqlite.ErrNotFound) {
				return domain.Question{}, ferr
			}
			if err := sqlite.SetBotRunState(ctx, tx, question.RequesterBotID, domain.BotStatusQueued, ""); err != nil {
				return domain.Question{}, err
			}
		}
		messageID = message.ID
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
	_ = a.recordEvent(ctx, domain.Event{Type: domain.EventQuestionAnswered, TeamID: question.TeamID, BotID: question.RequesterBotID, ResourceID: question.ID})
	if messageID != "" {
		_ = a.recordEvent(ctx, domain.Event{Type: domain.EventMessageCreated, TeamID: question.TeamID, BotID: question.RequesterBotID, ResourceID: messageID})
	}
	a.Hub.Publish()
	if messageID != "" && a.Scheduler != nil {
		a.Scheduler.Wake(question.RequesterBotID)
	}
	return answered, nil
}
