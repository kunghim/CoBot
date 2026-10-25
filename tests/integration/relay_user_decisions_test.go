package integration

import (
	"context"
	"testing"

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
}
