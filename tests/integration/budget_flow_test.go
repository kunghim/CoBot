package integration

import (
	"context"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// pendingBudgetCards counts the open questions about a budget.
func pendingBudgetCards(t *testing.T, h *m2Harness) (count int, summary string) {
	t.Helper()
	err := h.app.DB.QueryRowContext(context.Background(), `SELECT count(*), COALESCE(max(summary), '') FROM approvals WHERE type = ? AND request_state = ?`,
		domain.ApprovalBudget, domain.ApprovalStatePending).Scan(&count, &summary)
	if err != nil {
		t.Fatal(err)
	}
	return count, summary
}

func lastBudgetCard(t *testing.T, h *m2Harness) domain.Approval {
	t.Helper()
	var id string
	if err := h.app.DB.QueryRowContext(context.Background(), `SELECT id FROM approvals WHERE type = ? ORDER BY created_at DESC LIMIT 1`, domain.ApprovalBudget).Scan(&id); err != nil {
		t.Fatal(err)
	}
	card, err := sqlite.GetApproval(context.Background(), h.app.DB, id)
	if err != nil {
		t.Fatal(err)
	}
	return card
}

// A team spends its daily budget; the next message is held and the user is
// asked once. Resetting counts again from now and the held message runs.
func TestATeamOverItsDailyBudgetAsksAndResetLetsTheHeldMessageRun(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, nil)
	captain := bots[0]
	fake := h.ensureFake(t, team.ID)
	limit := int64(100)
	if _, err := h.app.SetTeamBudget(ctx, h.user, team.ID, &limit); err != nil {
		t.Fatal(err)
	}

	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: captain.ID, Text: "第一条"}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 5*time.Second, "turn 1", func() bool { return len(fake.Turns()) == 1 })
	turn := fake.Turns()[0]
	fake.EmitTokenUsage(turn.ThreadID, turn.TurnID, 90, 10, 0)
	fake.EmitTurnCompleted(turn.ThreadID, turn.TurnID, "completed", "好")
	waitUntil(t, 5*time.Second, "turn 1 done", func() bool {
		_, err := sqlite.FindActiveRun(ctx, h.app.DB, captain.ID)
		return err != nil
	})

	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: captain.ID, Text: "第二条"}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 5*time.Second, "a question about the budget", func() bool {
		n, _ := pendingBudgetCards(t, h)
		return n == 1
	})
	if _, summary := pendingBudgetCards(t, h); summary != "approval.card.budget_team" {
		t.Fatalf("card is about %q, want the team's limit", summary)
	}
	if len(fake.Turns()) != 1 {
		t.Fatal("the message ran although the team had spent its budget")
	}
	// Asking again changes nothing: still one question.
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: captain.ID, Text: "第三条"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if n, _ := pendingBudgetCards(t, h); n != 1 {
		t.Fatalf("%d open questions, want the one", n)
	}

	card := lastBudgetCard(t, h)
	if _, err := h.app.DecideApproval(ctx, h.user, card.ID, domain.BudgetOptionReset, "", 0); err != nil {
		t.Fatalf("reset from the card: %v", err)
	}
	waitUntil(t, 5*time.Second, "the held message to run", func() bool { return len(fake.Turns()) == 2 })
	if n, _ := pendingBudgetCards(t, h); n != 0 {
		t.Fatalf("%d open questions after the answer", n)
	}
}

// Choosing to wait keeps the team held and is not asked again the same day.
func TestWaitingOnTheBudgetCardKeepsTheTeamHeldWithoutAskingAgain(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, nil)
	captain := bots[0]
	fake := h.ensureFake(t, team.ID)
	limit := int64(100)
	if _, err := h.app.SetTeamBudget(ctx, h.user, team.ID, &limit); err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: captain.ID, Text: "第一条"}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 5*time.Second, "turn 1", func() bool { return len(fake.Turns()) == 1 })
	turn := fake.Turns()[0]
	fake.EmitTokenUsage(turn.ThreadID, turn.TurnID, 90, 10, 0)
	fake.EmitTurnCompleted(turn.ThreadID, turn.TurnID, "completed", "好")
	waitUntil(t, 5*time.Second, "turn 1 done", func() bool {
		_, err := sqlite.FindActiveRun(ctx, h.app.DB, captain.ID)
		return err != nil
	})
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: captain.ID, Text: "第二条"}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 5*time.Second, "a question about the budget", func() bool {
		n, _ := pendingBudgetCards(t, h)
		return n == 1
	})

	if _, err := h.app.DecideApproval(ctx, h.user, lastBudgetCard(t, h).ID, domain.BudgetOptionWait, "", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: captain.ID, Text: "第三条"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(400 * time.Millisecond)
	if n, _ := pendingBudgetCards(t, h); n != 0 {
		t.Fatal("asked again the same day after the user chose to wait")
	}
	if len(fake.Turns()) != 1 {
		t.Fatal("waiting must not let a held message run")
	}
}

// A turn that fails without ever reporting tokens leaves today's spend unknown,
// which holds the team again; the user is asked again, and each answer lets the
// next held message run. Nothing ever stays queued with no question open.
func TestAHeldMessageIsNeverLeftQueuedWithoutAQuestionAfterAFailedTurn(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, nil)
	captain := bots[0]
	fake := h.ensureFake(t, team.ID)
	limit := int64(100)
	if _, err := h.app.SetTeamBudget(ctx, h.user, team.ID, &limit); err != nil {
		t.Fatal(err)
	}
	send := func(text string) {
		t.Helper()
		if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: captain.ID, Text: text}); err != nil {
			t.Fatal(err)
		}
	}
	idle := func(what string) {
		t.Helper()
		waitUntil(t, 5*time.Second, what, func() bool {
			_, err := sqlite.FindActiveRun(ctx, h.app.DB, captain.ID)
			return err != nil
		})
	}
	reset := func() {
		t.Helper()
		waitUntil(t, 5*time.Second, "a question about the budget", func() bool {
			n, _ := pendingBudgetCards(t, h)
			return n == 1
		})
		if _, err := h.app.DecideApproval(ctx, h.user, lastBudgetCard(t, h).ID, domain.BudgetOptionReset, "", 0); err != nil {
			t.Fatalf("reset from the card: %v", err)
		}
	}

	send("一")
	waitUntil(t, 5*time.Second, "turn 1", func() bool { return len(fake.Turns()) == 1 })
	first := fake.Turns()[0]
	fake.EmitTokenUsage(first.ThreadID, first.TurnID, 90, 10, 0)
	fake.EmitTurnCompleted(first.ThreadID, first.TurnID, "completed", "好")
	idle("turn 1 done")

	send("二")
	reset()
	waitUntil(t, 5*time.Second, "turn 2", func() bool { return len(fake.Turns()) == 2 })
	second := fake.Turns()[1]
	fake.EmitTurnCompleted(second.ThreadID, second.TurnID, "failed", "")
	idle("turn 2 failed")

	send("三")
	reset()
	waitUntil(t, 5*time.Second, "turn 3 to run after the second answer", func() bool { return len(fake.Turns()) == 3 })
}
