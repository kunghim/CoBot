package app

import (
	"context"
	"slices"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
)

// questionRoute is one way of answering a card: the kinds of card it is for and
// the decisions an answer to them carries. An answer goes by its decision, and
// the function it reaches checks the card is one of its kinds.
type questionRoute struct {
	kinds     []string
	decisions []string
	answer    func(a *App, ctx context.Context, p auth.Principal, questionID, decision, mode, text string, expected int64) (domain.Question, error)
}

// questionRoutes has every kind of card and every decision the interface lists,
// each once.
var questionRoutes = []questionRoute{
	{
		kinds:     []string{domain.QuestionKindAsk},
		decisions: []string{""},
		answer: func(a *App, ctx context.Context, p auth.Principal, questionID, _, _, text string, expected int64) (domain.Question, error) {
			return a.AnswerQuestion(ctx, p, questionID, text, expected)
		},
	},
	{
		kinds:     []string{domain.QuestionKindConfirm, domain.QuestionKindAcceptance},
		decisions: []string{domain.AcceptanceAccept, domain.AcceptanceRevise},
		answer: func(a *App, ctx context.Context, p auth.Principal, questionID, decision, mode, text string, expected int64) (domain.Question, error) {
			return a.AnswerDecision(ctx, p, questionID, decision, mode, text, expected)
		},
	},
	{
		kinds:     []string{domain.QuestionKindRecruitment},
		decisions: []string{domain.RecruitmentApprove, domain.RecruitmentReject},
		answer: func(a *App, ctx context.Context, p auth.Principal, questionID, decision, _, text string, expected int64) (domain.Question, error) {
			return a.AnswerRecruitment(ctx, p, questionID, decision, text, expected)
		},
	},
}

// AnswerCard takes the user's answer to any card that waits on them.
func (a *App) AnswerCard(ctx context.Context, principal auth.Principal, questionID, decision, mode, text string, expected int64) (domain.Question, error) {
	for _, route := range questionRoutes {
		if slices.Contains(route.decisions, decision) {
			return route.answer(a, ctx, principal, questionID, decision, mode, text, expected)
		}
	}
	return domain.Question{}, domain.InvalidRequest("error.question.acceptance_decision", nil)
}
