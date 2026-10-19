package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// A turn that failed for a reason that passes (the usage limit) leaves its
// card with an idle member; retrying runs the card's turn again.
func TestRetryRunPutsAFailedCardTurnBack(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	team, bots := h.createTeam(t, []string{"fullstack-engineer"})
	leader, owner := bots[0], bots[1]
	fake := h.ensureFake(t, team.ID)

	_, req := h.handCard(t, team, leader, owner, "额度用尽的卡", "做完")
	waitUntil(t, 5*time.Second, "card turn", func() bool { return len(fake.Turns()) == 1 })
	failedRun, err := sqlite.FindActiveRun(ctx, h.app.DB, owner.ID)
	if err != nil {
		t.Fatal(err)
	}

	// A turn that is still running cannot be retried.
	var de *domain.Error
	if _, err := h.app.RetryRun(ctx, h.user, failedRun.ID); !errors.As(err, &de) || de.Code != domain.CodeConflict {
		t.Fatalf("retry of a running turn = %v, want conflict", err)
	}

	fake.EmitTurnCompleted(fake.Turns()[0].ThreadID, fake.Turns()[0].TurnID, "failed", "usageLimitExceeded")
	waitUntil(t, 5*time.Second, "run failed", func() bool {
		r, err := sqlite.GetRun(ctx, h.app.DB, failedRun.ID)
		return err == nil && r.Status == domain.RunFailed
	})
	// Nothing wakes the member by itself.
	time.Sleep(300 * time.Millisecond)
	if len(fake.Turns()) != 1 {
		t.Fatalf("a failed turn was rerun without being asked: %d turns", len(fake.Turns()))
	}

	result, err := h.app.RetryRun(ctx, h.user, failedRun.ID)
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if result["requeued"] != 1 {
		t.Fatalf("retry result = %v, want one delivery requeued", result)
	}
	waitUntil(t, 5*time.Second, "card turn again", func() bool { return len(fake.Turns()) == 2 })

	// Once a later turn exists the failed one is history.
	fake.EmitTurnCompleted(fake.Turns()[1].ThreadID, fake.Turns()[1].TurnID, "completed", "做完了")
	waitUntil(t, 5*time.Second, "second turn ended", func() bool { return h.runStatus(t, owner.ID) != "running" })
	if _, err := h.app.RetryRun(ctx, h.user, failedRun.ID); !errors.As(err, &de) || de.Code != domain.CodeConflict {
		t.Fatalf("retry of an old failed turn = %v, want conflict", err)
	}

	// A cancelled card has nothing left to run.
	_, other := h.handCard(t, team, leader, owner, "取消前失败的卡", "做完")
	waitUntil(t, 5*time.Second, "third turn", func() bool { return len(fake.Turns()) == 3 })
	third, err := sqlite.FindActiveRun(ctx, h.app.DB, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	fake.EmitTurnCompleted(fake.Turns()[2].ThreadID, fake.Turns()[2].TurnID, "failed", "usageLimitExceeded")
	waitUntil(t, 5*time.Second, "third run failed", func() bool {
		r, err := sqlite.GetRun(ctx, h.app.DB, third.ID)
		return err == nil && r.Status == domain.RunFailed
	})
	if _, err := h.app.CancelRequirement(ctx, h.user, other.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.RetryRun(ctx, h.user, third.ID); !errors.As(err, &de) || de.Code != domain.CodeConflict {
		t.Fatalf("retry of a cancelled card's turn = %v, want conflict", err)
	}
	_ = req
}
