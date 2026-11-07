package app

import (
	"context"
	"testing"

	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// What a member does when its turn ends, by the questions it has open. The next
// dispatch would correct a wrong answer here, so only this level can see one.
func TestAMemberWaitsOnTheCardsTheUserDecides(t *testing.T) {
	cases := []struct {
		kind, want string
	}{
		{"", domain.BotStatusWaitingInput},
		{domain.QuestionKindRecruitment, domain.BotStatusWaitingInput},
		{domain.QuestionKindConfirm, domain.BotStatusIdle},
		{domain.QuestionKindAcceptance, domain.BotStatusIdle},
	}
	for _, c := range cases {
		t.Run("kind "+c.kind, func(t *testing.T) {
			a := providerTestApp(t, &fakeBinder{})
			ctx := context.Background()
			if _, err := a.DB.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
				t.Fatal(err)
			}
			tx, err := a.DB.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if err := sqlite.InsertQuestion(ctx, tx, domain.Question{
				ID: "q", TeamID: "team", RequesterBotID: "bot", Kind: c.kind, Status: domain.QuestionOpen, Question: "?",
			}); err != nil {
				t.Fatal(err)
			}
			if got := settledMemberStatusTx(ctx, tx, "bot"); got != c.want {
				t.Fatalf("status = %s, want %s", got, c.want)
			}
		})
	}
}
