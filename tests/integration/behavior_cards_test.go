package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// What the user's answer to a card reaches depends on the decision it carries,
// and each answer checks it is for the right kind of card. These hold still what
// comes back when the two do not go together.

func (h *m2Harness) answerStatus(t *testing.T, questionID string, body map[string]any) (int, string) {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, h.api.URL+"/api/v1/questions/"+questionID+"/answers", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+h.bearer)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Error struct {
			MessageKey string `json:"messageKey"`
		} `json:"error"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out.Error.MessageKey
}

func TestAnswerRoutesByDecisionAndChecksTheCard(t *testing.T) {
	h := newM2Harness(t)
	team, bots := h.createTeam(t, []string{"fullstack-engineer"})
	ctx := context.Background()

	const (
		notAcceptance  = "error.question.not_acceptance"
		notRecruitment = "error.question.not_recruitment"
		useDecision    = "error.question.acceptance_decision"
	)
	cases := []struct {
		kind, decision string
		key            string
	}{
		{domain.QuestionKindAsk, domain.AcceptanceAccept, notAcceptance},
		{domain.QuestionKindAsk, domain.AcceptanceRevise, notAcceptance},
		{domain.QuestionKindAsk, domain.RecruitmentApprove, notRecruitment},
		{domain.QuestionKindAsk, domain.RecruitmentReject, notRecruitment},
		{domain.QuestionKindAsk, "bogus", useDecision},
		{domain.QuestionKindConfirm, "", useDecision},
		{domain.QuestionKindConfirm, domain.RecruitmentApprove, notRecruitment},
		{domain.QuestionKindConfirm, domain.RecruitmentReject, notRecruitment},
		{domain.QuestionKindConfirm, "bogus", useDecision},
		{domain.QuestionKindAcceptance, "", useDecision},
		{domain.QuestionKindAcceptance, domain.RecruitmentApprove, notRecruitment},
		{domain.QuestionKindAcceptance, domain.RecruitmentReject, notRecruitment},
		{domain.QuestionKindAcceptance, "bogus", useDecision},
		{domain.QuestionKindRecruitment, "", useDecision},
		{domain.QuestionKindRecruitment, domain.AcceptanceAccept, notAcceptance},
		{domain.QuestionKindRecruitment, domain.AcceptanceRevise, notAcceptance},
		{domain.QuestionKindRecruitment, "bogus", useDecision},
	}
	for i, c := range cases {
		t.Run(fmt.Sprintf("%q gets %q", c.kind, c.decision), func(t *testing.T) {
			id := fmt.Sprintf("q%d", i)
			tx, err := h.app.DB.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if err := sqlite.InsertQuestion(ctx, tx, domain.Question{ID: id, Kind: c.kind, TeamID: team.ID, RequesterBotID: bots[1].ID, Question: id, Options: []string{}, Status: domain.QuestionOpen, Revision: 1}); err != nil {
				t.Fatal(err)
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			status, key := h.answerStatus(t, id, map[string]any{"requestId": id, "answerText": "x", "decision": c.decision, "expectedRevision": 1})
			if status != http.StatusBadRequest || key != c.key {
				t.Fatalf("got %d %q, want 400 %q", status, key, c.key)
			}
			after, err := sqlite.GetQuestion(ctx, h.app.DB, id)
			if err != nil {
				t.Fatal(err)
			}
			if after.Status != domain.QuestionOpen {
				t.Fatalf("the card moved on to %q", after.Status)
			}
		})
	}
}

// A card of a type nothing decides is refused, not guessed at.
func TestDecidingACardOfUnknownTypeIsRefused(t *testing.T) {
	h := newM2Harness(t)
	team, bots := h.createTeam(t, []string{"fullstack-engineer"})
	ctx := context.Background()
	tx, err := h.app.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	card := domain.Approval{ID: "a1", TeamID: team.ID, RequesterBotID: bots[1].ID, Type: "invented", Summary: "x", RequestState: domain.ApprovalStatePending, Revision: 1, Options: []domain.ApprovalOption{{ID: "ok"}}}
	if err := sqlite.InsertApproval(ctx, tx, card); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	_, err = h.app.DecideApproval(ctx, h.user, card.ID, "ok", "", 1, nil)
	var de *domain.Error
	if !errors.As(err, &de) || de.Code != domain.CodeConflict || de.MessageKey != "error.approval.option_unavailable" {
		t.Fatalf("got %v", err)
	}
}
