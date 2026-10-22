package integration

import (
	"context"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/app"
)

// Cancelling a requirement stops the turn a member is spending on its card,
// but never a turn that is for something else.
func TestCancelRequirementInterruptsOnlyItsOwnWork(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"fullstack-engineer"})
	leader, owner := bots[0], bots[1]
	fake := h.ensureFake(t, team.ID)

	// The member is working on the card: cancelling interrupts that turn.
	_, req := h.handCard(t, team, leader, owner, "要取消的卡", "做完")
	waitUntil(t, 5*time.Second, "card turn", func() bool { return len(fake.Turns()) == 1 })
	if len(fake.Interrupts()) != 0 {
		t.Fatal("interrupted before the cancel")
	}
	if _, err := h.app.CancelRequirement(ctx, h.user, req.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if got := len(fake.Interrupts()); got != 1 {
		t.Fatalf("interrupts after cancelling the card's requirement = %d, want 1", got)
	}
	fake.EmitTurnCompleted(fake.Turns()[0].ThreadID, fake.Turns()[0].TurnID, "interrupted", "")
	waitUntil(t, 5*time.Second, "turn ended", func() bool { return h.runStatus(t, owner.ID) != "running" })

	// A turn the user started is not touched by a cancel of a card that is
	// still waiting behind it.
	if _, err := h.app.SendMessage(ctx, h.user, app.SendMessageInput{BotID: owner.ID, Text: "先帮我看个问题"}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 5*time.Second, "user turn", func() bool { return len(fake.Turns()) == 2 })
	_, other := h.handCard(t, team, leader, owner, "排在后面的卡", "做完")
	if _, err := h.app.CancelRequirement(ctx, h.user, other.ID); err != nil {
		t.Fatalf("cancel second: %v", err)
	}
	if got := len(fake.Interrupts()); got != 1 {
		t.Fatalf("a user's turn was interrupted by the cancel: interrupts = %d, want still 1", got)
	}
}

// A cancel that names the revision it saw is refused once the requirement
// has moved on, and goes through when the revision still holds. The write
// guard copies an If-Match header into this revision.
func TestCancelRequirementChecksARevisionWhenOneIsSent(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"fullstack-engineer"})
	_, req := h.handCard(t, team, bots[0], bots[1], "带版本取消", "做完")

	stale, err := app.WithWriteRequest(h.user, "http.POST /api/v1/requirements/{id}/cancel", "stale-cancel", map[string]any{"id": req.ID})
	if err != nil {
		t.Fatal(err)
	}
	stale.Write.ExpectedRevision = req.Revision + 7
	if _, err := h.app.CancelRequirement(ctx, stale, req.ID); err == nil {
		t.Fatal("a cancel naming a stale revision went through")
	}

	current, err := app.WithWriteRequest(h.user, "http.POST /api/v1/requirements/{id}/cancel", "current-cancel", map[string]any{"id": req.ID})
	if err != nil {
		t.Fatal(err)
	}
	current.Write.ExpectedRevision = req.Revision
	if _, err := h.app.CancelRequirement(ctx, current, req.ID); err != nil {
		t.Fatalf("a cancel naming the current revision: %v", err)
	}
}
