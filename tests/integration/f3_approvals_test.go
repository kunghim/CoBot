package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

func TestNativeReplyCannotUndoNewerCompletionOrApproval(t *testing.T) {
	for _, later := range []string{"completed", "approval"} {
		t.Run(later, func(t *testing.T) {
			h := newM2Harness(t)
			ctx := context.Background()
			team, bots := h.createTeam(t, nil)
			bot := bots[0]
			if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: bot.ID, Text: "native approval"}); err != nil {
				t.Fatal(err)
			}
			fake := h.fakeFor(t, team.ID)
			waitUntil(t, 5*time.Second, "native turn", func() bool { return len(fake.Turns()) == 1 })
			turn := fake.Turns()[0]
			fake.EmitApproval("first", turn.ThreadID, turn.TurnID, "first request")
			waitUntil(t, 5*time.Second, "native card", func() bool { cards, _ := h.app.PendingApprovals(ctx, h.user, team.ID, ""); return len(cards) == 1 })
			cards, _ := h.app.PendingApprovals(ctx, h.user, team.ID, "")
			fake.BeforeReplyReturn = func() {
				if later == "completed" {
					fake.EmitTurnCompleted(turn.ThreadID, turn.TurnID, "completed", "done before reply ack")
					waitUntil(t, 5*time.Second, "early completion", func() bool {
						_, err := sqlite.FindActiveRun(ctx, h.app.DB, bot.ID)
						return errors.Is(err, sqlite.ErrNotFound)
					})
				} else {
					fake.EmitApproval("second", turn.ThreadID, turn.TurnID, "later request")
					waitUntil(t, 5*time.Second, "later approval", func() bool {
						pending, _ := h.app.PendingApprovals(ctx, h.user, team.ID, "")
						return len(pending) == 1 && pending[0].ID != cards[0].ID
					})
				}
			}
			p, err := app.WithWriteRequest(h.user, "test.native-decision", "one-reply", map[string]any{"id": cards[0].ID})
			if err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if _, err := h.app.DecideApproval(ctx, p, cards[0].ID, "accept", "", cards[0].Revision); err != nil {
					t.Fatal(err)
				}
			}
			if len(fake.Replies()) != 1 {
				t.Fatal("retry replied to native request twice")
			}
			if later == "completed" {
				if run, err := sqlite.GetRun(ctx, h.app.DB, cards[0].RunID); err != nil || run.Status != domain.RunCompleted {
					t.Fatalf("completion overwritten: %+v %v", run, err)
				}
			} else if run, err := sqlite.FindActiveRun(ctx, h.app.DB, bot.ID); err != nil || run.Status != domain.RunWaitingApproval {
				t.Fatalf("later approval overwritten: %+v %v", run, err)
			}
		})
	}
}
