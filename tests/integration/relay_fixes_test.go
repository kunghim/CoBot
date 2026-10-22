package integration

import (
	"context"
	"errors"
	"testing"

	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// relayDriver hands cards off directly through the app, the way members'
// tool calls do, without waiting for engine turns.
type relayDriver struct {
	t    *testing.T
	h    *m2Harness
	team domain.Team
}

func (d relayDriver) start(pm domain.Bot, stages []domain.Stage) domain.Requirement {
	d.t.Helper()
	req, err := d.h.app.StartRequirement(context.Background(), auth.NewPrincipalBot(pm.ID, d.team.ID), app.StartRequirementInput{
		Title: "回归", Document: "回归需求", Stages: stages,
	})
	if err != nil {
		d.t.Fatal(err)
	}
	return req
}

func (d relayDriver) confirm(req domain.Requirement) {
	d.t.Helper()
	questions, err := sqlite.ListQuestions(context.Background(), d.h.app.DB, d.team.ID, domain.QuestionOpen, 20)
	if err != nil {
		d.t.Fatal(err)
	}
	for _, q := range questions {
		if q.RequirementID == req.ID && q.Kind == domain.QuestionKindConfirm {
			if _, err := d.h.app.AnswerDecision(context.Background(), d.h.user, q.ID, domain.AcceptanceAccept, "", "", q.Revision); err != nil {
				d.t.Fatal(err)
			}
			return
		}
	}
	d.t.Fatal("no open confirmation")
}

// open is the requirement's one open card.
func (d relayDriver) open(req domain.Requirement) domain.Task {
	d.t.Helper()
	cards, err := sqlite.ListRequirementTasks(context.Background(), d.h.app.DB, req.ID)
	if err != nil {
		d.t.Fatal(err)
	}
	var open []domain.Task
	for _, c := range cards {
		if !c.IsTerminal() {
			open = append(open, c)
		}
	}
	if len(open) != 1 {
		d.t.Fatalf("open cards = %+v", open)
	}
	return open[0]
}

func (d relayDriver) handoff(card domain.Task, outcome, mode string) {
	d.t.Helper()
	if _, err := d.h.app.Handoff(context.Background(), auth.NewPrincipalBot(card.OwnerID, d.team.ID), app.HandoffInput{
		CardID: card.ID, Outcome: outcome, Mode: mode, Summary: outcome + " " + card.Title,
	}); err != nil {
		d.t.Fatalf("%s %s: %v", outcome, card.Title, err)
	}
}

// A member named on a running requirement's route cannot be deleted, even
// before its card arrives: the route would later hand a card to nobody.
func TestMemberOnARunningRouteCannotBeDeleted(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"fullstack-engineer", "qa-engineer"})
	pm, dev, qa := bots[0], bots[1], bots[2]
	d := relayDriver{t: t, h: h, team: team}
	req := d.start(pm, []domain.Stage{
		{Kind: domain.StageConfirm},
		{Name: "实现", Kind: domain.StageImplement, OwnerID: dev.ID},
		{Name: "测试", Kind: domain.StageTest, OwnerID: qa.ID},
		{Kind: domain.StageAccept},
	})
	d.confirm(req)

	_, err := h.app.DeleteBot(ctx, h.user, qa.ID, 0)
	var de *domain.Error
	if !errors.As(err, &de) || de.Code != domain.CodeConflict || de.Params["routeStages"] != int64(1) {
		t.Fatalf("deleting a member the route still needs = %v", err)
	}
	if _, err := h.app.CancelRequirement(ctx, h.user, req.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.DeleteBot(ctx, h.user, qa.ID, 0); err != nil {
		t.Fatalf("delete after the requirement ended: %v", err)
	}
}

// A rework card that is to come straight back to its reviewer keeps that
// return through an escalation and the project manager's resume.
func TestEscalatedReworkKeepsItsReturn(t *testing.T) {
	h := newM2Harness(t)
	team, bots := h.createTeam(t, []string{"fullstack-engineer", "qa-engineer", "code-reviewer"})
	pm, dev, qa, reviewer := bots[0], bots[1], bots[2], bots[3]
	d := relayDriver{t: t, h: h, team: team}
	req := d.start(pm, []domain.Stage{
		{Kind: domain.StageConfirm},
		{Name: "实现", Kind: domain.StageImplement, OwnerID: dev.ID},
		{Name: "测试", Kind: domain.StageTest, OwnerID: qa.ID},
		{Name: "代码评审", Kind: domain.StageReview, OwnerID: reviewer.ID, ReviewType: "code"},
		{Kind: domain.StageAccept},
	})
	d.confirm(req)
	d.handoff(d.open(req), domain.HandoffDone, "")                      // implementation
	d.handoff(d.open(req), domain.HandoffDone, "")                      // testing
	d.handoff(d.open(req), domain.HandoffBack, domain.ReworkReturnToMe) // review sends it back

	rework := d.open(req)
	if rework.OwnerID != dev.ID || rework.ReturnTo != 3 {
		t.Fatalf("rework card = %+v", rework)
	}
	d.handoff(rework, domain.HandoffEscalate, "")
	escalation := d.open(req)
	if escalation.StageKind != domain.CardEscalation || escalation.ReturnTo != 3 {
		t.Fatalf("escalation card = %+v", escalation)
	}
	d.handoff(escalation, domain.HandoffResume, "")
	resumed := d.open(req)
	if resumed.OwnerID != dev.ID || resumed.StageIndex != 1 || resumed.ReturnTo != 3 {
		t.Fatalf("resumed card = %+v", resumed)
	}
	d.handoff(resumed, domain.HandoffDone, "")
	if next := d.open(req); next.OwnerID != reviewer.ID || next.StageIndex != 3 {
		t.Fatalf("after the resumed rework: %+v, want straight back to the review", next)
	}
}
