package integration

import (
	"context"
	"testing"

	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// handCard hands a member a card the way the relay does: the project
// manager starts a requirement whose route is confirm → implement → accept,
// and the user confirms it. It returns the card and its requirement.
func (h *m2Harness) handCard(t *testing.T, team domain.Team, pm, owner domain.Bot, title, criteria string) (domain.Task, domain.Requirement) {
	t.Helper()
	ctx := context.Background()
	req, err := h.app.StartRequirement(ctx, auth.NewPrincipalBot(pm.ID, team.ID), app.StartRequirementInput{
		Title: title, Document: "需求：" + title,
		Stages: []domain.Stage{
			{Kind: domain.StageConfirm},
			{Name: title, Kind: domain.StageImplement, OwnerID: owner.ID, Criteria: criteria},
			{Kind: domain.StageAccept},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	questions, err := sqlite.ListQuestions(ctx, h.app.DB, team.ID, domain.QuestionOpen, 20)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range questions {
		if q.RequirementID == req.ID && q.Kind == domain.QuestionKindConfirm {
			if _, err := h.app.AnswerDecision(ctx, h.user, q.ID, domain.AcceptanceAccept, "", q.Revision); err != nil {
				t.Fatal(err)
			}
		}
	}
	cards, err := sqlite.ListRequirementTasks(ctx, h.app.DB, req.ID)
	if err != nil || len(cards) != 1 {
		t.Fatalf("cards of %s = %+v, %v", title, cards, err)
	}
	return cards[0], req
}
