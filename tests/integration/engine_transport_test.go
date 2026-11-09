package integration

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/tests/integration/fakeengine"
)

// Choosing HTTP only lets the chat's engine go between turns: the next turn
// runs on an engine started HTTP-only and resumes the chat's thread there,
// so the conversation keeps its history (#93).
func TestAChatMovesToTheNewEngineTransportBetweenTurns(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	var mu sync.Mutex
	var threads []string
	made := attachChats(t, h, func(f *chatFake) {
		mu.Lock()
		f.Know(threads...)
		mu.Unlock()
		settings, err := h.app.GetSettings(ctx)
		if err != nil {
			t.Error(err)
			return
		}
		f.HTTPOnlyMode = settings.HTTPOnly()
	})
	first, err := h.app.SendChat(ctx, h.user, "", app.ChatSendInput{RequestID: "transport-1", Text: "first"})
	if err != nil {
		t.Fatal(err)
	}
	before := chatCreated(t, made)
	if before.HTTPOnly() {
		t.Fatal("a fresh profile started the engine HTTP-only")
	}
	run := chatWait(t, h, first.Chat.ID, domain.RunRunning).Runs[0]
	mu.Lock()
	threads = append(threads, run.ThreadID)
	mu.Unlock()
	before.EmitTurnCompleted(run.ThreadID, run.TurnID, "completed", "one")
	chatWait(t, h, first.Chat.ID, domain.RunCompleted)

	settings, err := h.app.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	http := domain.EngineTransportHTTP
	if _, err := h.app.UpdateSettings(ctx, h.user, domain.SettingsPatch{EngineTransport: &http}, settings.Revision); err != nil {
		t.Fatal(err)
	}

	if _, err := h.app.SendChat(ctx, h.user, first.Chat.ID, app.ChatSendInput{RequestID: "transport-2", Text: "second"}); err != nil {
		t.Fatal(err)
	}
	after := chatCreated(t, made)
	if !after.HTTPOnly() {
		t.Fatal("the next turn ran on an engine started the old way")
	}
	page := chatWait(t, h, first.Chat.ID, domain.RunRunning)
	second := page.Runs[len(page.Runs)-1]
	if second.ThreadID != run.ThreadID {
		t.Fatalf("the chat moved to thread %q, want its own thread %q resumed", second.ThreadID, run.ThreadID)
	}
	if after.Resumes() != 1 {
		t.Fatalf("resumes on the new engine = %d, want 1", after.Resumes())
	}
	waitUntil(t, 5*time.Second, "the old engine shut down", func() bool { return before.ShutdownCalls() > 0 })
}

// An unknown transport is refused, so the profile never names one no
// engine understands.
func TestAnUnknownEngineTransportIsRefused(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	settings, err := h.app.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if settings.EngineTransport != domain.EngineTransportAuto {
		t.Fatalf("fresh profile transport = %q, want auto", settings.EngineTransport)
	}
	wrong := "websocket"
	if _, err := h.app.UpdateSettings(ctx, h.user, domain.SettingsPatch{EngineTransport: &wrong}, settings.Revision); err == nil {
		t.Fatal("an unknown transport was stored")
	}
}

// A team's idle engine is replaced the next time the team needs it, and the
// one that replaces it is started the new way; a team whose engine already
// runs the chosen transport keeps it.
func TestATeamEngineMovesToTheNewEngineTransportWhenIdle(t *testing.T) {
	h := newM2Harness(t)
	ctx := context.Background()
	h.onFake = func(f *fakeengine.Server) {
		settings, err := h.app.GetSettings(ctx)
		if err != nil {
			t.Error(err)
			return
		}
		f.HTTPOnlyMode = settings.HTTPOnly()
	}
	team, _ := h.createTeam(t, nil)
	first, err := h.app.Scheduler.EnsureTeamServer(ctx, team.ID, team.Directory)
	if err != nil {
		t.Fatal(err)
	}
	again, err := h.app.Scheduler.EnsureTeamServer(ctx, team.ID, team.Directory)
	if err != nil || again != first {
		t.Fatalf("an unchanged transport replaced the engine: %v", err)
	}

	settings, err := h.app.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	http := domain.EngineTransportHTTP
	if _, err := h.app.UpdateSettings(ctx, h.user, domain.SettingsPatch{EngineTransport: &http}, settings.Revision); err != nil {
		t.Fatal(err)
	}
	next, err := h.app.Scheduler.EnsureTeamServer(ctx, team.ID, team.Directory)
	if err != nil {
		t.Fatal(err)
	}
	if next == first {
		t.Fatal("the idle engine was kept after the transport changed")
	}
	if !next.(*fakeengine.Server).HTTPOnly() {
		t.Fatal("the replacement engine was not started HTTP-only")
	}
	old := first.(*fakeengine.Server)
	waitUntil(t, 5*time.Second, "the old team engine shut down", func() bool { return old.ShutdownCalls() > 0 })
}
