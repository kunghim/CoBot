package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// AskUserInput is the ask_user request from a bot.
type AskUserInput struct {
	Question string
	Options  []string
	TaskID   string
}

// AskUser records a question awaiting the user. It never blocks the tool
// caller: the bot receives a waiting item and is expected to end its turn.
func (a *App) AskUser(ctx context.Context, principal auth.Principal, in AskUserInput) (domain.Question, error) {
	if principal.Kind != auth.KindBot {
		return domain.Question{}, domain.Forbidden("error.forbidden.bot_only", nil)
	}
	question := strings.TrimSpace(in.Question)
	if question == "" {
		return domain.Question{}, domain.InvalidRequest("error.question.text_required", nil)
	}
	bot, err := sqlite.GetBot(ctx, a.DB, principal.BotID)
	if err != nil {
		return domain.Question{}, sqlite.NormalizeError(err)
	}
	// A question may only reference a task of the asking member's own team
	// (review B6).
	if in.TaskID != "" {
		task, terr := sqlite.GetTask(ctx, a.DB, in.TaskID)
		if terr != nil {
			return domain.Question{}, sqlite.NormalizeError(terr)
		}
		if task.TeamID != bot.TeamID {
			return domain.Question{}, domain.Forbidden("error.message.other_team", map[string]any{"taskId": in.TaskID})
		}
	}
	item := domain.Question{
		ID: domain.NewID(), TeamID: bot.TeamID, RequesterBotID: principal.BotID,
		TaskID: in.TaskID, Question: question, Options: in.Options,
		Status: domain.QuestionOpen, Revision: 1,
	}
	if item.Options == nil {
		item.Options = []string{}
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return domain.Question{}, err
	}
	defer tx.Rollback()
	if err := sqlite.InsertQuestion(ctx, tx, item); err != nil {
		return domain.Question{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Question{}, err
	}
	_ = a.recordEvent(ctx, domain.Event{Type: domain.EventQuestionCreated, TeamID: item.TeamID, BotID: item.RequesterBotID, ResourceID: item.ID})
	a.Hub.Publish()
	return item, nil
}

// AnswerQuestion delivers the user's answer as a user message to the
// requesting bot in the same transaction, so the question and its message can
// never diverge. An answer is input, never a security authorization.
func (a *App) AnswerQuestion(ctx context.Context, principal auth.Principal, questionID, answerText string, expectedRevision int64) (domain.Question, error) {
	if principal.Kind != auth.KindUser {
		return domain.Question{}, domain.Forbidden("error.forbidden.user_only", nil)
	}
	question, err := sqlite.GetQuestion(ctx, a.DB, questionID)
	if err != nil {
		return domain.Question{}, sqlite.NormalizeError(err)
	}
	if question.Status != domain.QuestionOpen {
		return domain.Question{}, domain.Conflict("error.question.already_answered", map[string]any{"status": question.Status})
	}
	if strings.TrimSpace(answerText) == "" {
		return domain.Question{}, domain.InvalidRequest("error.question.answer_required", nil)
	}

	blocks, err := json.Marshal([]domain.TextBlock{{Type: "text", Text: answerText}})
	if err != nil {
		return domain.Question{}, err
	}
	message := domain.Message{
		ID: domain.NewID(), TeamID: question.TeamID, BotID: question.RequesterBotID,
		Kind: domain.MessageKindUser, Blocks: blocks,
		DeliveryStatus: domain.DeliveryQueued, RunID: question.ID,
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return domain.Question{}, err
	}
	defer tx.Rollback()
	answered, err := sqlite.AnswerQuestion(ctx, tx, questionID, answerText, message.ID, expectedRevision)
	if err != nil {
		return domain.Question{}, sqlite.NormalizeError(err)
	}
	if err := sqlite.InsertMessage(ctx, tx, message); err != nil {
		return domain.Question{}, err
	}
	// The user's answer is fresh input, so it opens a new causal chain (G02)
	// instead of spending the chain that asked the question.
	chain, chainErr := a.openChainLocked(ctx, tx, question.TeamID, message.ID, "")
	if chainErr != nil {
		return domain.Question{}, chainErr
	}
	if err := sqlite.InsertDeliveryRow(ctx, tx, sqlite.DeliveryRecord{
		ID: domain.NewID(), MessageID: message.ID, TeamID: question.TeamID,
		TargetBotID: question.RequesterBotID, Status: domain.DeliveryQueued, ChainID: chain.ID,
	}); err != nil {
		return domain.Question{}, err
	}
	// The asking member keeps its working projection if it is mid-run
	// (review B13): answering enqueues, it never clears a live run.
	if _, ferr := sqlite.FindActiveRun(ctx, tx, question.RequesterBotID); ferr != nil && errors.Is(ferr, sqlite.ErrNotFound) {
		if err := sqlite.SetBotRunState(ctx, tx, question.RequesterBotID, domain.BotStatusQueued, ""); err != nil {
			return domain.Question{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return domain.Question{}, err
	}
	_ = a.recordEvent(ctx, domain.Event{Type: domain.EventQuestionAnswered, TeamID: question.TeamID, BotID: question.RequesterBotID, ResourceID: question.ID})
	_ = a.recordEvent(ctx, domain.Event{Type: domain.EventMessageCreated, TeamID: question.TeamID, BotID: question.RequesterBotID, ResourceID: message.ID})
	a.Hub.Publish()
	if a.Scheduler != nil {
		a.Scheduler.Wake(question.RequesterBotID)
	}
	return answered, nil
}

// ListQuestions pages a team's questions for the user surface.
func (a *App) ListQuestions(ctx context.Context, principal auth.Principal, teamID, status string, limit int) ([]domain.Question, error) {
	if principal.Kind != auth.KindUser {
		return nil, domain.Forbidden("error.forbidden.user_only", nil)
	}
	return sqlite.ListQuestions(ctx, a.DB, teamID, status, limit)
}

// CancelQuestion withdraws a still-open question.
func (a *App) CancelQuestion(ctx context.Context, principal auth.Principal, questionID string, expectedRevision int64) (domain.Question, error) {
	question, err := sqlite.GetQuestion(ctx, a.DB, questionID)
	if err != nil {
		return domain.Question{}, sqlite.NormalizeError(err)
	}
	switch principal.Kind {
	case auth.KindUser:
	case auth.KindBot:
		if principal.BotID != question.RequesterBotID {
			return domain.Question{}, domain.Forbidden("error.question.requester_only", nil)
		}
	default:
		return domain.Question{}, domain.Unauthorized("error.unauthorized.principal_required")
	}
	if question.Status != domain.QuestionOpen {
		return domain.Question{}, domain.Conflict("error.question.already_answered", map[string]any{"status": question.Status})
	}
	if expectedRevision > 0 && question.Revision != expectedRevision {
		return domain.Question{}, domain.RevisionConflict("error.question.revision_conflict", map[string]any{"expected": expectedRevision, "actual": question.Revision})
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return domain.Question{}, err
	}
	defer tx.Rollback()
	if _, err := sqlite.CancelQuestionRow(ctx, tx, questionID, expectedRevision); err != nil {
		return domain.Question{}, sqlite.NormalizeError(err)
	}
	if err := tx.Commit(); err != nil {
		return domain.Question{}, err
	}
	_ = a.recordEvent(ctx, domain.Event{Type: domain.EventQuestionAnswered, TeamID: question.TeamID, BotID: question.RequesterBotID, ResourceID: question.ID})
	a.Hub.Publish()
	return sqlite.GetQuestion(ctx, a.DB, questionID)
}
