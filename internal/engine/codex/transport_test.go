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

// A thread keeps the provider it started on. One started on the official
// account before the switch still resumes in an HTTP-only engine (on its own
// provider, after the engine names it), and one started HTTP-only resumes in
// an engine started the default way; both are the official account's.
func TestAnOfficialThreadResumesOnTheTransportItStartedOn(t *testing.T) {
	for _, tc := range []struct {
		name     string
		httpOnly bool
		on       string
		want     []string
	}{
		{"old thread, HTTP-only engine", true, builtinProvider, []string{
			"default=" + httpProvider, "resume on " + httpProvider, "default=openai", "resume on openai"}},
		{"HTTP-only thread, default engine", false, httpProvider, []string{
			"resume on openai", "default=" + httpProvider, "resume on " + httpProvider, "default=openai"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, fake := startStrictEngine(t)
			server.httpOnly = tc.httpOnly
			fake.pinned = true
			fake.threads["old"] = tc.on
			ctx := context.Background()
			resumed, err := server.ResumeThread(ctx, "old", engine.ThreadSpec{Model: "m", ModelProvider: builtinProvider})
			if err != nil {
				t.Fatalf("resume: %v", err)
			}
			if resumed.ModelProvider != builtinProvider {
				t.Fatalf("resumed on %q, want the official account", resumed.ModelProvider)
			}
			if !slices.Equal(fake.log, tc.want) {
				t.Fatalf("log = %q, want %q", fake.log, tc.want)
			}
			fake.log = nil
			if _, err := server.StartTurn(ctx, "old", "hello", engine.TurnOptions{Model: "m"}); err != nil {
				t.Fatalf("turn on the resumed thread: %v", err)
			}
		})
	}
}

// A refusal for another vendor's provider is not retried.
func TestAResumeRefusedForAnotherProviderIsNotRetried(t *testing.T) {
	server, fake := startStrictEngine(t)
	server.httpOnly = true
	fake.pinned = true
	fake.threads["gw-thread"] = "gw"
	if _, err := server.ResumeThread(context.Background(), "gw-thread", engine.ThreadSpec{Model: "m", ModelProvider: builtinProvider}); err == nil {
		t.Fatal("a thread of another provider was resumed as the official account's")
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
