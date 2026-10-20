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

// The real engine, a private home and a fake gateway.
//
// The engine accepted a provider on a thread from the start; what has to
// keep holding is that the thread really reaches that provider, with that
// provider's key. A fake engine cannot show that, so this check drives the
// real app-server.
//
// It needs the real codex binary, so it stays out of the default run: set
// COBOT_R_SMOKE=1 to execute it.
func TestRealEngineThreadReachesItsProvider(t *testing.T) {
	if os.Getenv("COBOT_R_SMOKE") != "1" {
		t.Skip("set COBOT_R_SMOKE=1 to run the real engine provider check")
	}
	binary, err := Locate("")
	if err != nil {
		t.Fatalf("locate codex: %v", err)
	}

	// The gateway the provider points at. It records the request that
	// reaches it, with the key the engine used.
	var mu sync.Mutex
	var gotPath, gotAuth, gotModel string
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload struct {
			Model  string `json:"model"`
			Stream bool   `json:"stream"`
		}
		_ = json.Unmarshal(body, &payload)
		mu.Lock()
		gotPath, gotAuth, gotModel = r.URL.Path, r.Header.Get("Authorization"), payload.Model
		mu.Unlock()
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
			fmt.Fprintf(w, "event: response.completed\ndata: %s\n\n", mustJSON(response))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(mustJSON(response)))
	}))
	defer gateway.Close()

	root := t.TempDir()
	paths, err := platform.Resolve(platform.EnvDevelopment, filepath.Join(root, "profile"))
	if err != nil {
		t.Fatal(err)
	}
	if err := paths.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	// The provider the thread will name: a table in the private home, with
	// the key in the environment, exactly as CoBot writes it.
	config := "model = \"gpt-5-codex\"\n" +
		"model_provider = \"gateway\"\n" +
		"approval_policy = \"never\"\n" +
		"sandbox_mode = \"danger-full-access\"\n" +
		"\n[model_providers.gateway]\n" +
		"name = \"Gateway\"\n" +
		"base_url = \"" + gateway.URL + "/v1\"\n" +
		"wire_api = \"responses\"\n" +
		"requires_openai_auth = false\n" +
		"env_key = \"GATEWAY_KEY\"\n"
	if err := os.WriteFile(filepath.Join(paths.CodexHome(), "config.toml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(root, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}

	exited := make(chan error, 1)
	server, err := Start(context.Background(), ServerOptions{
		Binary: binary,
		Cwd:    work,
		Env:    EngineEnv(paths, map[string]string{"GATEWAY_KEY": "sk-gateway-secret"}),
		Client: ClientInfo{Name: "cobot-check", Title: "CoBot provider check", Version: "test"},
		Callbacks: engine.Callbacks{
			OnExit: func(err error) { exited <- err },
		},
	})
	if err != nil {
		t.Fatalf("start app-server: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	info, err := server.StartThread(ctx, engine.ThreadSpec{
		Cwd: work, Permission: "full", Model: "gpt-5-codex", ModelProvider: "gateway",
	})
	if err != nil {
		t.Fatalf("start a thread that names its provider: %v", err)
	}
	if info.ModelProvider != "gateway" {
		t.Fatalf("the engine reported the provider %q, want gateway", info.ModelProvider)
	}
	if _, err := server.StartTurn(ctx, info.ThreadID, "ping", engine.TurnOptions{Model: "gpt-5-codex"}); err != nil {
		t.Fatalf("start a turn: %v", err)
	}

	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		path, auth, model := gotPath, gotAuth, gotModel
		mu.Unlock()
		if path != "" {
			if !strings.HasSuffix(path, "/responses") {
				t.Fatalf("the provider was asked for %q, want the Responses path", path)
			}
			if auth != "Bearer sk-gateway-secret" {
				t.Fatalf("the provider was asked with %q, want the provider's own key", auth)
			}
			if model != "gpt-5-codex" {
				t.Fatalf("the provider was asked for the model %q", model)
			}
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("the provider was never reached")
}
