package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	apihttp "github.com/kunghim/CoBot/internal/api/http"
	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/collaboration"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/engine"
	"github.com/kunghim/CoBot/internal/events"
	"github.com/kunghim/CoBot/internal/platform"
	"github.com/kunghim/CoBot/internal/secrets"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// The provider surface over HTTP.
//
// The client adds a request id to every write, and a handler that does not
// declare it refuses its own product's request. These tests therefore speak
// the way the product speaks: a request id in every body.

type fakeBinder struct {
	bound    []domain.Provider
	selected string
}

func (b *fakeBinder) Dialects() []string { return []string{domain.DialectOpenAIResponses} }
func (b *fakeBinder) BindProvider(ctx context.Context, p domain.Provider) error {
	b.bound = append(b.bound, p)
	return nil
}
func (b *fakeBinder) UnbindProvider(ctx context.Context, id string) error { return nil }
func (b *fakeBinder) SelectProvider(ctx context.Context, id string) error {
	b.selected = id
	return nil
}

// providerGateway answers like a vendor: a model list, a minimal Responses
// call, and a 402 for the model that cannot be paid for.
func providerGateway(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"good-model"},{"id":"unpaid-model"}]}`))
		case "/v1/responses":
			body, _ := io.ReadAll(r.Body)
			var payload struct {
				Model string `json:"model"`
			}
			_ = json.Unmarshal(body, &payload)
			if payload.Model == "unpaid-model" {
				w.WriteHeader(http.StatusPaymentRequired)
				_, _ = w.Write([]byte(`{"error":{"code":"insufficient_quota","message":"余额不足"}}`))
				return
			}
			_, _ = w.Write([]byte(`{"id":"resp-1","status":"completed","output":[]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestProviderSurfaceSpeaksTheClientsRequest(t *testing.T) {
	root := t.TempDir()
	paths, err := platform.Resolve(platform.EnvDevelopment, filepath.Join(root, "profile"))
	if err != nil {
		t.Fatal(err)
	}
	if err := paths.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	db, err := sqlite.Open(paths.DatabaseFile())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	migrator, err := sqlite.NewMigrator(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrator.MigrateToLatest(context.Background(), paths.BackupsDir()); err != nil {
		t.Fatal(err)
	}
	store, err := secrets.New(paths.CredentialsDir())
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	a := &app.App{
		DB: db, Paths: paths, Hub: events.NewHub(), Live: events.NewLiveHub(),
		InstanceID: "test-instance", ServiceEpoch: "epoch-0", Version: "test", Logger: logger,
		Credentials: store,
	}
	binder := &fakeBinder{}
	a.ProviderBinderFactory = func(context.Context) (engine.ProviderBinder, error) { return binder, nil }
	tokens := auth.NewTokenStore(time.Hour)
	a.CollaborationTokens = tokens
	bearer := auth.NewBearerStore(time.Hour)
	apiServer := &apihttp.Server{
		App: a, Paths: paths, Sessions: auth.NewSessionStore(time.Hour), Bearer: bearer,
		Issuer: auth.NewBootstrapIssuer(time.Minute), Hub: a.Hub, Logger: logger,
		CollaborationTokens: tokens,
		Dispatcher:          collaboration.NewDispatcher(a, tokens),
	}
	server := httptest.NewServer(apiServer.Handler())
	t.Cleanup(server.Close)

	tok, err := bearer.Issue(auth.LocalUserID)
	if err != nil {
		t.Fatal(err)
	}
	call := func(method, path string, body map[string]any) (int, map[string]any) {
		t.Helper()
		var reader io.Reader
		if body != nil {
			data, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			reader = bytes.NewReader(data)
		}
		req, err := http.NewRequest(method, server.URL+path, reader)
		if err != nil {
			t.Fatal(err)
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("Authorization", "Bearer "+tok.Token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		var out map[string]any
		_ = json.Unmarshal(raw, &out)
		return resp.StatusCode, out
	}
	envelope := func(out map[string]any) map[string]any {
		if d, ok := out["data"].(map[string]any); ok {
			return d
		}
		return out
	}

	gateway := providerGateway(t)
	base := gateway.URL + "/v1"

	// The vendor guess is a local pattern match and never calls out.
	status, out := call("POST", "/api/v1/providers/detect", map[string]any{
		"requestId": "p-detect", "apiKey": "sk-ant-unknown-vendor-key",
	})
	if status != http.StatusOK {
		t.Fatalf("detect = %d, want 200: %v", status, out)
	}

	// The model list, as the settings page asks for it.
	status, out = call("POST", "/api/v1/providers/discover", map[string]any{
		"requestId": "p-discover", "baseUrl": base, "apiKey": "sk-test",
	})
	if status != http.StatusOK {
		t.Fatalf("discover = %d, want 200: %v", status, out)
	}
	models, _ := envelope(out)["models"].([]any)
	if len(models) != 2 {
		t.Fatalf("discovered %d models, want 2: %v", len(models), out)
	}

	// The per-model check.
	status, out = call("POST", "/api/v1/providers/probe-models", map[string]any{
		"requestId": "p-probe", "baseUrl": base, "modelIds": []string{"good-model", "unpaid-model"}, "apiKey": "sk-test",
	})
	if status != http.StatusOK {
		t.Fatalf("probe-models = %d, want 200: %v", status, out)
	}
	results, _ := envelope(out)["results"].(map[string]any)
	good, _ := results["good-model"].(map[string]any)
	if good == nil || good["ok"] != true {
		t.Fatalf("the working model was reported as broken: %v", results)
	}
	unpaid, _ := results["unpaid-model"].(map[string]any)
	if unpaid == nil || unpaid["ok"] == true {
		t.Fatalf("the unpaid model was reported as working: %v", results)
	}

	// The write itself.
	status, out = call("POST", "/api/v1/providers", map[string]any{
		"requestId": "p-create", "id": "gateway", "name": "测试网关", "kind": "custom", "authMode": "api_key",
		"endpoints": []map[string]any{{"dialect": domain.DialectOpenAIResponses, "baseUrl": base}},
		"models":    []map[string]any{{"id": "good-model", "status": "ok"}},
		"apiKey":    "sk-test",
	})
	if status != http.StatusCreated {
		t.Fatalf("create provider = %d, want 201: %v", status, out)
	}
	if len(binder.bound) != 1 {
		t.Fatalf("the engine was asked to take %d providers, want 1", len(binder.bound))
	}
	if binder.bound[0].ID != "gateway" {
		t.Fatalf("the engine was given %q", binder.bound[0].ID)
	}

	// The key went to the credential store, not into the record.
	status, out = call("GET", "/api/v1/providers", nil)
	if status != http.StatusOK {
		t.Fatalf("list providers = %d, want 200: %v", status, out)
	}
	items, _ := envelope(out)["providers"].([]any)
	if len(items) != 1 {
		t.Fatalf("the profile holds %d providers, want 1", len(items))
	}
	first, _ := items[0].(map[string]any)
	if first["hasCredential"] != true {
		t.Fatalf("the provider reports no stored key: %v", first)
	}
	if first["apiKey"] != nil || first["secret"] != nil {
		t.Fatalf("the projection carries the secret: %v", first)
	}
}
