package codex

import (
	"context"
	"slices"
	"testing"

	"github.com/kunghim/CoBot/internal/engine"
)

// An HTTP-only engine runs the official account on httpProvider: a new
// thread starts there, its turns switch the default to it and back, and
// CoBot is told the thread is on the official account, so a model change
// within the account keeps the thread (#93).
func TestAnHTTPOnlyEngineRunsTheOfficialAccountWithoutWebSockets(t *testing.T) {
	server, fake := startStrictEngine(t)
	server.httpOnly = true
	ctx := context.Background()
	for _, provider := range []string{builtinProvider, ""} {
		info, err := server.StartThread(ctx, engine.ThreadSpec{Model: "m", ModelProvider: provider})
		if err != nil {
			t.Fatal(err)
		}
		if info.ModelProvider != builtinProvider {
			t.Fatalf("thread for %q reported on %q, want the official account", provider, info.ModelProvider)
		}
		fake.mu.Lock()
		onEngine := fake.threads[info.ThreadID]
		fake.mu.Unlock()
		if onEngine != httpProvider {
			t.Fatalf("thread for %q started on %q, want %q", provider, onEngine, httpProvider)
		}
	}
	info, err := server.StartThread(ctx, engine.ThreadSpec{Model: "m", ModelProvider: builtinProvider})
	if err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	fake.log = nil
	fake.mu.Unlock()
	if _, err := server.StartTurn(ctx, info.ThreadID, "hello", engine.TurnOptions{Model: "m"}); err != nil {
		t.Fatalf("turn: %v", err)
	}
	want := []string{"default=" + httpProvider, "turn/start on " + httpProvider, "default=openai"}
	if !slices.Equal(fake.log, want) {
		t.Fatalf("log = %q\nwant  %q", fake.log, want)
	}
}

// A thread started on the official account before the switch resumes on
// httpProvider in an HTTP-only engine, and is still reported as the
// official account's.
func TestAnHTTPOnlyEngineResumesAnOfficialThread(t *testing.T) {
	server, fake := startStrictEngine(t)
	server.httpOnly = true
	resumed, err := server.ResumeThread(context.Background(), "thread-openai", engine.ThreadSpec{Model: "m", ModelProvider: builtinProvider})
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if resumed.ModelProvider != builtinProvider {
		t.Fatalf("resumed on %q, want the official account", resumed.ModelProvider)
	}
	if want := []string{"default=" + httpProvider, "resume on " + httpProvider, "default=openai"}; !slices.Equal(fake.log, want) {
		t.Fatalf("log = %q", fake.log)
	}
}

// Another provider is untouched by the transport, and an engine started the
// default way keeps the official account on the built-in provider.
func TestTheTransportOnlyConcernsTheOfficialAccount(t *testing.T) {
	server, fake := startStrictEngine(t)
	server.httpOnly = true
	info, err := server.StartThread(context.Background(), engine.ThreadSpec{Model: "m", ModelProvider: "gw"})
	if err != nil || info.ModelProvider != "gw" {
		t.Fatalf("gateway thread = %+v, %v", info, err)
	}
	plain, _ := startStrictEngine(t)
	info, err = plain.StartThread(context.Background(), engine.ThreadSpec{Model: "m", ModelProvider: builtinProvider})
	if err != nil || info.ModelProvider != builtinProvider {
		t.Fatalf("official thread = %+v, %v", info, err)
	}
	if fake.threads["thread-gw"] != "gw" {
		t.Fatalf("gateway thread on %q", fake.threads["thread-gw"])
	}
}
