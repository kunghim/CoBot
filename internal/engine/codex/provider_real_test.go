package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/engine"
	"github.com/kunghim/CoBot/internal/platform"
)

// The real engine, a private home and fake gateways.
//
// The engine accepted a provider on a thread from the start; what has to
// keep holding is that the thread really reaches that provider, with that
// provider's key, and that a thread can be moved to another one.
//
// These checks need the real codex binary, so they stay out of the default
// run: set COBOT_R_SMOKE=1 to execute them.

// gateway is a fake provider endpoint. It records the request that reaches
// it, with the key the engine used.
type gateway struct {
	name   string
	server *httptest.Server

	mu    sync.Mutex
	path  string
	auth  string
	model string
	hits  int
}

func newGateway(t *testing.T, name string) *gateway {
	t.Helper()
	g := &gateway{name: name}
	g.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload struct {
			Model  string `json:"model"`
			Stream bool   `json:"stream"`
		}
		_ = json.Unmarshal(body, &payload)
		g.mu.Lock()
		g.path, g.auth, g.model = r.URL.Path, r.Header.Get("Authorization"), payload.Model
		g.hits++
		g.mu.Unlock()
		response := map[string]any{
			"id": "resp-1", "object": "response", "created_at": 0, "status": "completed",
			"model": payload.Model,
			"output": []any{map[string]any{
				"type": "message", "id": "msg-1", "role": "assistant", "status": "completed",
				"content": []any{map[string]any{"type": "output_text", "text": "ok"}},
			}},
			"usage": map[string]any{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2},
		}
		if payload.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "event: response.completed\ndata: %s\n\n", mustJSON(map[string]any{"type": "response.completed", "response": response}))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(mustJSON(response)))
	}))
	t.Cleanup(g.server.Close)
	return g
}

// waitForRequest waits for the gateway to be reached.
func (g *gateway) waitForRequest(t *testing.T, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		g.mu.Lock()
		hits := g.hits
		g.mu.Unlock()
		if hits > 0 {
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatalf("the %s provider was never reached", g.name)
}

func (g *gateway) last(t *testing.T) (string, string, string) {
	t.Helper()
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.path, g.auth, g.model
}

// realProviderHome writes a private Codex home whose providers point at the
// given gateways, and returns the paths and the environment that carries
// their keys.
func realProviderHome(t *testing.T, gateways map[string]*gateway) platform.Paths {
	t.Helper()
	root := t.TempDir()
	paths, err := platform.Resolve(platform.EnvDevelopment, filepath.Join(root, "profile"))
	if err != nil {
		t.Fatal(err)
	}
	if err := paths.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(gateways))
	for name := range gateways {
		names = append(names, name)
	}
	config := "model = \"gpt-5-codex\"\napproval_policy = \"never\"\nsandbox_mode = \"danger-full-access\"\n"
	for _, name := range names {
		config += fmt.Sprintf(
			"\n[model_providers.%s]\nname = \"%s\"\nbase_url = \"%s/v1\"\nwire_api = \"responses\"\nrequires_openai_auth = false\nenv_key = \"%s_KEY\"\n",
			name, name, gateways[name].server.URL, strings.ToUpper(name))
	}
	if err := os.WriteFile(filepath.Join(paths.CodexHome(), "config.toml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	return paths
}

func realEngineEnv(paths platform.Paths, gateways map[string]*gateway) []string {
	secrets := map[string]string{}
	for name := range gateways {
		secrets[strings.ToUpper(name)+"_KEY"] = "sk-" + name + "-secret"
	}
	return EngineEnv(paths, secrets)
}

func startRealEngine(t *testing.T, paths platform.Paths, env []string) *Server {
	t.Helper()
	binary, err := Locate("")
	if err != nil {
		t.Fatalf("locate codex: %v", err)
	}
	work := filepath.Join(t.TempDir(), "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	server, err := Start(context.Background(), ServerOptions{
		Binary: binary,
		Cwd:    work,
		Env:    env,
		Client: ClientInfo{Name: "cobot-check", Title: "CoBot provider check", Version: "test"},
	})
	if err != nil {
		t.Fatalf("start app-server: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	})
	return server
}

// Resuming without the turn history requires a capability negotiated by the
// adapter's real initialize handshake, even when the thread is already loaded.
func TestRealEngineResumesWithoutTurnHistory(t *testing.T) {
	if os.Getenv("COBOT_R_SMOKE") != "1" {
		t.Skip("set COBOT_R_SMOKE=1 to run the real engine resume check")
	}
	alpha := newGateway(t, "alpha")
	gateways := map[string]*gateway{"alpha": alpha}
	paths := realProviderHome(t, gateways)
	server := startRealEngine(t, paths, realEngineEnv(paths, gateways))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	spec := engine.ThreadSpec{Permission: "read_only", Model: "gpt-5.6-sol", ModelProvider: "alpha"}
	info, err := server.StartThread(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	turnID, err := server.StartTurn(ctx, info.ThreadID, "ping", engine.TurnOptions{Model: spec.Model})
	if err != nil {
		t.Fatal(err)
	}
	for {
		status, err := server.InspectTurn(ctx, info.ThreadID, turnID)
		if err == nil && status == "completed" {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatalf("initial turn did not complete: %v", err)
		case <-time.After(20 * time.Millisecond):
		}
	}
	resumed, err := server.ResumeThread(ctx, info.ThreadID, spec)
	if err != nil {
		t.Fatalf("resume without turn history: %v", err)
	}
	if resumed.ThreadID != info.ThreadID || resumed.ModelProvider != "alpha" {
		t.Fatalf("resume changed the thread or provider: %+v", resumed)
	}
}

// A thread that names a provider has to reach that provider, with that
// provider's key.
func TestRealEngineThreadReachesItsProvider(t *testing.T) {
	if os.Getenv("COBOT_R_SMOKE") != "1" {
		t.Skip("set COBOT_R_SMOKE=1 to run the real engine provider check")
	}
	alpha := newGateway(t, "alpha")
	paths := realProviderHome(t, map[string]*gateway{"alpha": alpha})
	server := startRealEngine(t, paths, realEngineEnv(paths, map[string]*gateway{"alpha": alpha}))

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	info, err := server.StartThread(ctx, engine.ThreadSpec{
		Cwd: "", Permission: "full", Model: "gpt-5-codex", ModelProvider: "alpha",
	})
	if err != nil {
		t.Fatalf("start a thread that names its provider: %v", err)
	}
	if info.ModelProvider != "alpha" {
		t.Fatalf("the engine reported the provider %q, want alpha", info.ModelProvider)
	}
	if _, err := server.StartTurn(ctx, info.ThreadID, "ping", engine.TurnOptions{Model: "gpt-5-codex"}); err != nil {
		t.Fatalf("start a turn: %v", err)
	}
	alpha.waitForRequest(t, 45*time.Second)
	path, auth, _ := alpha.last(t)
	if !strings.HasSuffix(path, "/responses") {
		t.Fatalf("the provider was asked for %q, want the Responses path", path)
	}
	if auth != "Bearer sk-alpha-secret" {
		t.Fatalf("the provider was asked with %q, want the provider's own key", auth)
	}
}

// A thread that has to run with another provider lets go of the one it was
// started with: the engine ignores a provider for a thread it already holds.
func TestRealEngineMovesAThreadToAnotherProvider(t *testing.T) {
	if os.Getenv("COBOT_R_SMOKE") != "1" {
		t.Skip("set COBOT_R_SMOKE=1 to run the real engine provider check")
	}
	alpha := newGateway(t, "alpha")
	beta := newGateway(t, "beta")
	gateways := map[string]*gateway{"alpha": alpha, "beta": beta}
	paths := realProviderHome(t, gateways)
	server := startRealEngine(t, paths, realEngineEnv(paths, gateways))

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	info, err := server.StartThread(ctx, engine.ThreadSpec{
		Cwd: "", Permission: "full", Model: "gpt-5-codex", ModelProvider: "alpha",
	})
	if err != nil {
		t.Fatalf("start a thread that names its provider: %v", err)
	}
	if _, err := server.StartTurn(ctx, info.ThreadID, "ping", engine.TurnOptions{Model: "gpt-5-codex"}); err != nil {
		t.Fatalf("start a turn: %v", err)
	}
	alpha.waitForRequest(t, 45*time.Second)

	// The team now runs with the other provider. The thread has to let go
	// of the one it was started with before it can be loaded again.
	if err := server.ArchiveThread(ctx, info.ThreadID); err != nil {
		t.Fatalf("archive the thread: %v", err)
	}
	if err := server.UnarchiveThread(ctx, info.ThreadID); err != nil {
		t.Fatalf("unarchive the thread: %v", err)
	}
	resumed, err := server.ResumeThread(ctx, info.ThreadID, engine.ThreadSpec{
		Cwd: "", Permission: "full", Model: "gpt-5-codex", ModelProvider: "beta",
	})
	if err != nil {
		t.Fatalf("resume the thread with the other provider: %v", err)
	}
	if resumed.ModelProvider != "beta" {
		t.Fatalf("the engine reported the provider %q, want beta", resumed.ModelProvider)
	}
	before := beta.hits
	if _, err := server.StartTurn(ctx, info.ThreadID, "ping", engine.TurnOptions{Model: "gpt-5-codex"}); err != nil {
		t.Fatalf("start a turn on the moved thread: %v", err)
	}
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		beta.mu.Lock()
		hits := beta.hits
		beta.mu.Unlock()
		if hits > before {
			_, auth, _ := beta.last(t)
			if auth != "Bearer sk-beta-secret" {
				t.Fatalf("the provider was asked with %q, want its own key", auth)
			}
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatal("the thread never reached the provider it was moved to")
}
