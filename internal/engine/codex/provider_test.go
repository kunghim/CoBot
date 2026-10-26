package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/domain"
)

// configServer is a stand-in for the engine's configuration surface. It
// answers config/value/write and config/read the way the real app-server
// does, including refusing a value it does not understand.
type configServer struct {
	writes []map[string]any
	read   map[string]any
	// rejectWith, when set, makes every write fail with this error.
	rejectWith *RPCError
	// silent makes the engine accept requests without answering them.
	silent bool
}

func (c *configServer) start(t *testing.T) *Client {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	client := NewClient(nopCloser{inW}, outR, ClientHooks{})
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
			result := map[string]any{}
			if c.silent {
				continue
			}
			switch req.Method {
			case "config/value/write":
				params := req.Params
				if c.rejectWith != nil {
					outW.Write([]byte(`{"id":` + string(req.ID) + `,"error":` +
						mustJSON(c.rejectWith) + `}` + "\n"))
					continue
				}
				c.writes = append(c.writes, params)
				result = map[string]any{"status": "ok", "version": "sha256:test", "filePath": "/tmp/config.toml"}
			case "config/read":
				result = map[string]any{"config": c.read}
			}
			outW.Write([]byte(`{"id":` + string(req.ID) + `,"result":` +
				mustJSON(result) + `}` + "\n"))
		}
	}()
	return client
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "null"
	}
	return string(b)
}

func testProvider() domain.Provider {
	return domain.Provider{
		ID:       "cobot_deepseek",
		Name:     "DeepSeek",
		Kind:     domain.ProviderKindCustom,
		AuthMode: domain.ProviderAuthAPIKey,
		Endpoints: []domain.ProviderEndpoint{
			{Dialect: domain.DialectOpenAIResponses, BaseURL: "https://api.deepseek.com/v1"},
		},
		Models:  []domain.ProviderModel{{ID: "deepseek-v4-pro"}},
		Retries: domain.ProviderRetries{Request: 1, Stream: 2},
	}
}

func TestBindProviderWritesTheEngineTable(t *testing.T) {
	ctx := context.Background()
	backend := &configServer{read: map[string]any{
		"model_providers": map[string]any{
			"cobot_deepseek": map[string]any{"base_url": "https://api.deepseek.com/v1"},
		},
	}}
	server := &Server{client: backend.start(t)}

	if err := server.BindProvider(ctx, testProvider()); err != nil {
		t.Fatal(err)
	}
	if len(backend.writes) != 1 {
		t.Fatalf("wrote %d times, want 1", len(backend.writes))
	}
	write := backend.writes[0]
	if write["keyPath"] != "model_providers.cobot_deepseek" {
		t.Fatalf("wrong key path: %v", write["keyPath"])
	}
	if write["mergeStrategy"] != "upsert" {
		t.Fatalf("wrong merge strategy: %v", write["mergeStrategy"])
	}
	value, _ := write["value"].(map[string]any)
	if value["base_url"] != "https://api.deepseek.com/v1" {
		t.Fatalf("wrong base url: %v", value["base_url"])
	}
	if value["wire_api"] != "responses" {
		t.Fatalf("wrong wire api: %v", value["wire_api"])
	}
	// The secret must not travel in the configuration: only the name of the
	// variable that holds it.
	if value["env_key"] != ProviderEnvKey("cobot_deepseek") {
		t.Fatalf("wrong env key: %v", value["env_key"])
	}
	if _, has := value["experimental_bearer_token"]; has {
		t.Fatal("the configuration carries a secret")
	}
	// The values come back from JSON as numbers, not ints.
	if value["request_max_retries"] != float64(1) || value["stream_max_retries"] != float64(2) {
		t.Fatalf("retries did not reach the engine: %v", value)
	}
}

func TestBindProviderRefusesAProviderTheEngineCannotSpeak(t *testing.T) {
	ctx := context.Background()
	backend := &configServer{rejectWith: &RPCError{
		Code:    -32600,
		Message: "Invalid configuration: `wire_api = \"chat\"` is no longer supported.",
		Data:    json.RawMessage(`{"config_write_error_code":"configValidationError"}`),
	}}
	server := &Server{client: backend.start(t)}

	err := server.BindProvider(ctx, testProvider())
	if err == nil {
		t.Fatal("a rejected configuration was accepted")
	}
	if !strings.Contains(err.Error(), "no longer supported") {
		t.Fatalf("the engine's own explanation was lost: %v", err)
	}
	if !IsConfigValidationError(err) {
		t.Fatal("a rejected configuration was not recognised as one")
	}
}

func TestBindProviderRefusesAProviderWithoutAResponsesEndpoint(t *testing.T) {
	ctx := context.Background()
	backend := &configServer{}
	server := &Server{client: backend.start(t)}

	p := testProvider()
	p.Endpoints = []domain.ProviderEndpoint{}
	if err := server.BindProvider(ctx, p); err == nil {
		t.Fatal("a provider the engine cannot speak was bound")
	}
	if len(backend.writes) != 0 {
		t.Fatal("a provider the engine cannot speak reached the engine")
	}
}

func TestBindProviderRefusesTheOfficialProvider(t *testing.T) {
	ctx := context.Background()
	backend := &configServer{}
	server := &Server{client: backend.start(t)}

	p := testProvider()
	p.AuthMode = domain.ProviderAuthChatGPT
	if err := server.BindProvider(ctx, p); err == nil {
		t.Fatal("the official provider was bound as if it had an endpoint")
	}
}

func TestUnbindProviderRewritesTheTableWithoutIt(t *testing.T) {
	ctx := context.Background()
	backend := &configServer{read: map[string]any{
		"model_providers": map[string]any{
			"cobot_deepseek": map[string]any{"base_url": "https://api.deepseek.com/v1"},
			"cobot_kimi":     map[string]any{"base_url": "https://api.moonshot.cn/v1"},
		},
	}}
	server := &Server{client: backend.start(t)}

	if err := server.UnbindProvider(ctx, "cobot_deepseek"); err != nil {
		t.Fatal(err)
	}
	if len(backend.writes) != 1 {
		t.Fatalf("wrote %d times, want 1", len(backend.writes))
	}
	write := backend.writes[0]
	if write["keyPath"] != "model_providers" || write["mergeStrategy"] != "replace" {
		t.Fatalf("unbind did not rewrite the table: %v", write)
	}
	value, _ := write["value"].(map[string]any)
	if _, still := value["cobot_deepseek"]; still {
		t.Fatal("the unbound provider is still in the table")
	}
	if _, left := value["cobot_kimi"]; !left {
		t.Fatal("unbind removed another provider")
	}
}

func TestProviderEnvKeyIsStableAndSafe(t *testing.T) {
	if got := ProviderEnvKey("cobot_deepseek"); got != "COBOT_PROVIDER_COBOT_DEEPSEEK_KEY" {
		t.Fatalf("unexpected env key: %q", got)
	}
	// A provider id is a table name in the engine's configuration; the
	// variable name derived from it must stay a plain identifier.
	for _, id := range []string{"a-b", "a.b", "a b", "中文"} {
		key := ProviderEnvKey(id)
		for _, r := range key {
			if !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') && r != '_' {
				t.Fatalf("env key %q holds an unsafe character %q", key, r)
			}
		}
	}
}

func TestBindProviderHonoursTheCallersDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	// An engine that never answers must not outlive the caller's deadline.
	backend := &configServer{silent: true}
	server := &Server{client: backend.start(t)}

	start := time.Now()
	if err := server.BindProvider(ctx, testProvider()); err == nil {
		t.Fatal("a call without an answer reported success")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("the call took %s, the deadline was not honoured", elapsed)
	}
}
