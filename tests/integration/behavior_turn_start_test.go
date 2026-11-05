package integration

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/engine"
	"github.com/kunghim/CoBot/internal/store/sqlite"
	"github.com/kunghim/CoBot/tests/integration/fakeengine"
)

// A turn goes through fixed phases before the engine takes it. These tests hold
// still what happens when one of them cannot go on.

// useFactory replaces the engine the team's members get.
func useFactory(t *testing.T, h *m2Harness, factory app.ServerFactory) {
	t.Helper()
	sched := app.NewScheduler(h.app, factory)
	h.app.Scheduler = sched
	sched.Start()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = sched.Stop(ctx)
	})
}

func lastRun(t *testing.T, h *m2Harness, botID string) (status, errorKey string) {
	t.Helper()
	err := h.app.DB.QueryRow(`SELECT status, error_key FROM runs WHERE bot_id = ? ORDER BY rowid DESC LIMIT 1`, botID).Scan(&status, &errorKey)
	if err != nil {
		t.Fatal(err)
	}
	return status, errorKey
}

// startFails sends a message and checks the turn ended failed, with the member
// free again and the message not treated as heard.
func startFails(t *testing.T, h *m2Harness, dev domain.Bot, wantKey string) {
	t.Helper()
	sent, err := h.app.SendMessage(context.Background(), h.user, app.SendMessageInput{BotID: dev.ID, Text: "做点事"})
	if err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 5*time.Second, "run failed", func() bool {
		var n int
		_ = h.app.DB.QueryRow(`SELECT count(*) FROM runs WHERE bot_id = ? AND status = ?`, dev.ID, domain.RunFailed).Scan(&n)
		return n == 1
	})
	if _, key := lastRun(t, h, dev.ID); key != wantKey {
		t.Fatalf("errorKey = %q, want %q", key, wantKey)
	}
	settlesAt(t, h, dev.ID, domain.BotStatusIdle)
	var delivery string
	if err := h.app.DB.QueryRow(`SELECT status FROM deliveries WHERE message_id = ?`, sent.Message.ID).Scan(&delivery); err != nil {
		t.Fatal(err)
	}
	if delivery != domain.DeliveryUnknown {
		t.Fatalf("delivery = %s, want %s", delivery, domain.DeliveryUnknown)
	}
}

func TestATurnWhoseEngineCannotStartFails(t *testing.T) {
	h := newM2Harness(t)
	_, bots := h.createTeam(t, []string{"fullstack-engineer"})
	useFactory(t, h, func(app.ServerSpec) (engine.Server, error) {
		return nil, errors.New("no engine here")
	})
	startFails(t, h, bots[1], "error.engine.unavailable")
}

// noThreads is an engine that is up but cannot start a thread.
type noThreads struct{ *fakeengine.Server }

func (noThreads) StartThread(context.Context, engine.ThreadSpec) (engine.ThreadInfo, error) {
	return engine.ThreadInfo{}, errors.New("no thread here")
}

func TestATurnWhoseThreadCannotStartFails(t *testing.T) {
	h := newM2Harness(t)
	_, bots := h.createTeam(t, []string{"fullstack-engineer"})
	useFactory(t, h, func(spec app.ServerSpec) (engine.Server, error) {
		return noThreads{fakeengine.New(spec.Callbacks, "epoch-no-threads")}, nil
	})
	startFails(t, h, bots[1], "error.engine.thread_failed")
}

// A team whose folder is gone starts nothing, and its queue waits.
func TestAFolderGoneHoldsTheQueue(t *testing.T) {
	h := newM2Harness(t)
	team, bots := h.createTeam(t, []string{"fullstack-engineer"})
	fake := h.ensureFake(t, team.ID)
	pm, dev := bots[0], bots[1]
	turn := startTurn(t, h, fake, dev, "第一件")
	h.handCard(t, team, pm, dev, "做页面", "能打开")
	// Deleted, not moved: a dispatch still running from the card can find the
	// folder half deleted, and must not write the marker back into it.
	if err := os.RemoveAll(h.teamDir); err != nil {
		t.Fatal(err)
	}
	fake.EmitTurnCompleted(turn.ThreadID, turn.TurnID, domain.RunCompleted, "好")
	waitUntil(t, 5*time.Second, "team paused", func() bool {
		current, _, err := sqlite.GetTeam(context.Background(), h.app.DB, team.ID)
		return err == nil && current.Status == domain.TeamStatusDirectoryMissing
	})
	settlesAt(t, h, dev.ID, domain.BotStatusQueued)
	if got := len(fake.Turns()); got != 1 {
		t.Fatalf("turns = %d, want 1", got)
	}
}

// A folder that has just lost its .cobot may be in the middle of being deleted.
// Nothing is written into it and no turn starts until it has been quiet for a
// while; a folder that stays is then repaired and its queue runs by itself.
func TestAFolderLosingItsMarkerWaitsBeforeRepair(t *testing.T) {
	h := newM2Harness(t)
	team, bots := h.createTeam(t, []string{"fullstack-engineer"})
	fake := h.ensureFake(t, team.ID)
	dev := bots[1]
	cobot := filepath.Join(h.teamDir, ".cobot")
	if err := os.RemoveAll(cobot); err != nil {
		t.Fatal(err)
	}

	h.app.CheckTeamDirectories(context.Background())
	if _, err := h.app.SendMessage(context.Background(), h.user, app.SendMessageInput{BotID: dev.ID, Text: "做点事"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if _, err := os.Stat(cobot); !os.IsNotExist(err) {
		t.Fatalf(".cobot came back while the folder was still changing: %v", err)
	}
	if got := len(fake.Turns()); got != 0 {
		t.Fatalf("turns = %d, want 0", got)
	}

	longAgo := time.Now().Add(-time.Hour)
	if err := os.Chtimes(h.teamDir, longAgo, longAgo); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, 10*time.Second, "the held turn", func() bool { return len(fake.Turns()) == 1 })
	if _, err := os.Stat(filepath.Join(cobot, "team.json")); err != nil {
		t.Fatalf("marker not repaired: %v", err)
	}
}

// The end of a turn may be reported twice: only the first counts.
func TestATurnEndReportedTwiceSettlesOnce(t *testing.T) {
	h := newM2Harness(t)
	team, bots := h.createTeam(t, []string{"fullstack-engineer"})
	fake := h.ensureFake(t, team.ID)
	dev := bots[1]
	turn := startTurn(t, h, fake, dev, "做点事")
	fake.EmitTurnCompleted(turn.ThreadID, turn.TurnID, domain.RunCompleted, "结果")
	settlesAt(t, h, dev.ID, domain.BotStatusIdle)
	fake.EmitTurnCompleted(turn.ThreadID, turn.TurnID, domain.RunCompleted, "结果")
	settlesAt(t, h, dev.ID, domain.BotStatusIdle)
	var replies int
	if err := h.app.DB.QueryRow(`SELECT count(*) FROM messages WHERE bot_id = ? AND kind = ?`, dev.ID, domain.MessageKindAssistant).Scan(&replies); err != nil {
		t.Fatal(err)
	}
	if replies != 1 {
		t.Fatalf("replies = %d, want 1", replies)
	}
}
