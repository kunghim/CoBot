package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"sync"
	"testing"

	"github.com/kunghim/CoBot/internal/engine"
)

// strictEngine behaves like the real one: it refuses a call on a thread when
// its default provider is not the thread's, and keeps a log of what it saw.
type strictEngine struct {
	mu        sync.Mutex
	current   string
	threads   map[string]string
	log       []string
	refuseKey bool
}

func (e *strictEngine) answer(method string, params map[string]any) map[string]any {
	e.mu.Lock()
	defer e.mu.Unlock()
	refusal := map[string]any{"error": map[string]any{"code": -32600, "message": "This chat uses another provider"}}
	switch method {
	case "config/value/write":
		if e.refuseKey {
			return map[string]any{"error": map[string]any{"code": -32600, "message": "cannot write"}}
		}
		e.current = params["value"].(string)
		e.log = append(e.log, "default="+e.current)
		return map[string]any{"result": map[string]any{"status": "ok"}}
	case "thread/start":
		id := "thread-" + params["modelProvider"].(string)
		e.threads[id] = params["modelProvider"].(string)
		return map[string]any{"result": map[string]any{"thread": map[string]any{"id": id}, "modelProvider": e.threads[id]}}
	case "thread/resume":
		e.log = append(e.log, "resume on "+e.current)
		if params["modelProvider"].(string) != e.current {
			return refusal
		}
		e.threads[params["threadId"].(string)] = e.current
		return map[string]any{"result": map[string]any{"thread": map[string]any{"id": params["threadId"]}, "modelProvider": e.current}}
	case "turn/start", "turn/steer":
		e.log = append(e.log, method+" on "+e.current)
		if e.threads[params["threadId"].(string)] != e.current {
			return refusal
		}
		return map[string]any{"result": map[string]any{"turn": map[string]any{"id": "turn"}}}
	}
	return map[string]any{"result": map[string]any{}}
}

func startStrictEngine(t *testing.T) (*Server, *strictEngine) {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	fake := &strictEngine{current: builtinProvider, threads: map[string]string{}}
	client := NewClient(nopCloser{inW}, outR, ClientHooks{})
	server := &Server{client: client, intents: map[string]string{}, loaded: map[string]bool{}}
	t.Cleanup(func() { inW.Close(); outW.Close(); client.Wait() })
	go func() {
		scanner := bufio.NewScanner(inR)
		for scanner.Scan() {
			var req struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
				Params map[string]any  `json:"params"`
			}
			if json.Unmarshal(scanner.Bytes(), &req) != nil {
				return
			}
			reply := fake.answer(req.Method, req.Params)
			reply["id"] = req.ID
			writeJSONLine(outW, reply)
		}
	}()
	return server, fake
}

func TestATurnRunsAsItsThreadsProviderAndTheDefaultGoesBack(t *testing.T) {
	server, fake := startStrictEngine(t)
	ctx := context.Background()
	info, err := server.StartThread(ctx, engine.ThreadSpec{Model: "m", ModelProvider: "gw"})
	if err != nil {
		t.Fatal(err)
	}
	turn, err := server.StartTurn(ctx, info.ThreadID, "hello", engine.TurnOptions{Model: "m"})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := server.SteerTurn(ctx, info.ThreadID, turn, "more"); err != nil {
		t.Fatalf("steer: %v", err)
	}
	if _, err := server.ResumeThread(ctx, info.ThreadID, engine.ThreadSpec{Model: "m", ModelProvider: "gw"}); err != nil {
		t.Fatalf("resume: %v", err)
	}
	want := []string{
		"default=gw", "turn/start on gw", "default=openai",
		"default=gw", "turn/steer on gw", "default=openai",
		"default=gw", "resume on gw", "default=openai",
	}
	if !slices.Equal(fake.log, want) {
		t.Fatalf("log = %q\nwant  %q", fake.log, want)
	}
}

func TestATurnOnTheEnginesOwnAccountLeavesTheDefaultAlone(t *testing.T) {
	server, fake := startStrictEngine(t)
	ctx := context.Background()
	info, err := server.StartThread(ctx, engine.ThreadSpec{Model: "m", ModelProvider: builtinProvider})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.StartTurn(ctx, info.ThreadID, "hello", engine.TurnOptions{Model: "m"}); err != nil {
		t.Fatal(err)
	}
	if want := []string{"turn/start on openai"}; !slices.Equal(fake.log, want) {
		t.Fatalf("log = %q", fake.log)
	}
}

// Members of one team can run on different providers at the same moment.
func TestThreadsOnDifferentProvidersDoNotGetInEachOthersWay(t *testing.T) {
	server, _ := startStrictEngine(t)
	ctx := context.Background()
	var threads []string
	for _, provider := range []string{"gw", "other", builtinProvider} {
		info, err := server.StartThread(ctx, engine.ThreadSpec{Model: "m", ModelProvider: provider})
		if err != nil {
			t.Fatal(err)
		}
		threads = append(threads, info.ThreadID)
	}
	var wg sync.WaitGroup
	for round := 0; round < 20; round++ {
		for _, thread := range threads {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, err := server.StartTurn(ctx, thread, "hello", engine.TurnOptions{Model: "m"}); err != nil {
					t.Errorf("%s: %v", thread, err)
				}
			}()
		}
	}
	wg.Wait()
}

// A turn that could not be set up did not start, and the caller has to know.
func TestAProviderThatCannotBeSetRefusesTheTurn(t *testing.T) {
	server, fake := startStrictEngine(t)
	ctx := context.Background()
	info, err := server.StartThread(ctx, engine.ThreadSpec{Model: "m", ModelProvider: "gw"})
	if err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	fake.refuseKey = true
	fake.mu.Unlock()
	_, err = server.StartTurn(ctx, info.ThreadID, "hello", engine.TurnOptions{Model: "m"})
	if !errors.Is(err, engine.ErrRejected) {
		t.Fatalf("err = %v, want it to say the turn was refused", err)
	}
	if slices.Contains(fake.log, "turn/start on openai") {
		t.Fatal("the turn was sent anyway")
	}
}
