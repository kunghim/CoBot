package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// newTestDiscoverer returns a discoverer wired to the given test server.
func newTestDiscoverer(client *http.Client, probeTimeout time.Duration) *Discoverer {
	return &Discoverer{
		client:       client,
		timeout:      5 * time.Second,
		probeTimeout: probeTimeout,
	}
}

func TestDiscoverReturnsUniqueIDsInOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"id":"a"},{"model":"b"},{"id":"a"}]}`)
	}))
	defer server.Close()

	ids, err := New().Discover(context.Background(), server.URL, "sk-test")
	if err != nil {
		t.Fatalf("Discover returned error: %v", err)
	}
	want := []string{"a", "b"}
	if len(ids) != len(want) {
		t.Fatalf("got ids %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("got ids %v, want %v", ids, want)
		}
	}
}

func TestDiscoverRejectsUnknownShape(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"not a model list", `{"foo":1}`},
		{"plain text", `this is not json`},
		{"data is not a list", `{"data":"nope"}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()

			ids, err := New().Discover(context.Background(), server.URL, "sk-test")
			if err == nil {
				t.Fatalf("expected an error for body %q, got ids %v", tc.body, ids)
			}
			// An endpoint we do not understand must not look like an
			// endpoint with no models.
			if ids != nil {
				t.Fatalf("expected no ids alongside the error, got %v", ids)
			}
		})
	}
}

func TestDiscoverUnauthorizedIsClassified(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"message":"invalid api key"}}`)
	}))
	defer server.Close()

	_, err := New().Discover(context.Background(), server.URL, "sk-test")
	if err == nil {
		t.Fatal("expected an error for HTTP 401")
	}
	if !strings.Contains(err.Error(), CodeAuthInvalid) {
		t.Fatalf("error %q does not carry the auth_invalid code", err.Error())
	}
	if code, _ := classify(http.StatusUnauthorized, ""); code != CodeAuthInvalid {
		t.Fatalf("classify(401) = %q, want %q", code, CodeAuthInvalid)
	}
	// The user reads the reason alone, not the operation and the code.
	if got := Reason(err); got != messageAuthInvalid {
		t.Fatalf("Reason = %q, want %q", got, messageAuthInvalid)
	}
}

func TestDiscoverErrorHidesSecret(t *testing.T) {
	const secret = "sk-SECRET"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":{"message":"boom `+secret+`"}}`)
	}))
	defer server.Close()

	_, err := New().Discover(context.Background(), server.URL, secret)
	if err == nil {
		t.Fatal("expected an error for HTTP 500")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error message leaks the key: %q", err.Error())
	}
	if !strings.Contains(err.Error(), CodeNetwork) {
		t.Fatalf("error %q does not carry the network code", err.Error())
	}
}

func TestSecretTravelsOnlyInTheHeader(t *testing.T) {
	const secret = "sk-header-only"

	var mu sync.Mutex
	var seenAuth string
	var seenBody string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		seenAuth = r.Header.Get("Authorization")
		seenBody = string(body)
		mu.Unlock()

		if strings.HasSuffix(r.URL.Path, "/models") {
			_, _ = io.WriteString(w, `{"data":[{"id":"a"}]}`)
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"pong"}}]}`)
	}))
	defer server.Close()

	discoverer := newTestDiscoverer(server.Client(), 2*time.Second)

	if _, err := discoverer.Discover(context.Background(), server.URL, secret); err != nil {
		t.Fatalf("Discover returned error: %v", err)
	}
	mu.Lock()
	auth, body := seenAuth, seenBody
	mu.Unlock()
	if auth != "Bearer "+secret {
		t.Fatalf("Authorization header = %q, want %q", auth, "Bearer "+secret)
	}
	if strings.Contains(body, secret) {
		t.Fatalf("discover body leaks the key: %q", body)
	}

	results := discoverer.Probe(context.Background(), server.URL, secret, []string{"a"})
	mu.Lock()
	auth, body = seenAuth, seenBody
	mu.Unlock()
	if auth != "Bearer "+secret {
		t.Fatalf("Authorization header = %q, want %q", auth, "Bearer "+secret)
	}
	if strings.Contains(body, secret) {
		t.Fatalf("probe body leaks the key: %q", body)
	}
	if !results["a"].OK {
		t.Fatalf("probe result = %+v, want OK", results["a"])
	}
}

func TestProbeAllOK(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"pong"}}]}`)
	}))
	defer server.Close()

	models := []string{"a", "b", "c"}
	results := New().Probe(context.Background(), server.URL, "sk-test", models)
	if len(results) != len(models) {
		t.Fatalf("got %d results, want %d", len(results), len(models))
	}
	for _, id := range models {
		result, ok := results[id]
		if !ok {
			t.Fatalf("missing result for model %q", id)
		}
		if !result.OK {
			t.Fatalf("model %q failed: %+v", id, result)
		}
		if result.Code != CodeOK {
			t.Fatalf("model %q code = %q, want %q", id, result.Code, CodeOK)
		}
	}
}

func TestProbeClassifiesEachFailure(t *testing.T) {
	cases := []struct {
		model  string
		status int
		want   string
	}{
		{"m402", http.StatusPaymentRequired, CodeQuota},
		{"m429", http.StatusTooManyRequests, CodeRateLimit},
		{"m401", http.StatusUnauthorized, CodeAuthInvalid},
		{"m404", http.StatusNotFound, CodeModelUnknown},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		text := string(body)
		for _, tc := range cases {
			if strings.Contains(text, tc.model) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, `{"error":{"message":"failed"}}`)
				return
			}
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"pong"}}]}`)
	}))
	defer server.Close()

	models := []string{"m402", "m429", "m401", "m404", "good"}
	results := New().Probe(context.Background(), server.URL, "sk-test", models)
	if len(results) != len(models) {
		t.Fatalf("got %d results, want %d", len(results), len(models))
	}
	for _, tc := range cases {
		result, ok := results[tc.model]
		if !ok {
			t.Fatalf("missing result for model %q", tc.model)
		}
		if result.OK {
			t.Fatalf("model %q reported OK, want a failure", tc.model)
		}
		if result.Code != tc.want {
			t.Fatalf("model %q code = %q, want %q", tc.model, result.Code, tc.want)
		}
		if result.Message == "" {
			t.Fatalf("model %q has no message", tc.model)
		}
	}
	// A failing model must not take the others down with it.
	if !results["good"].OK {
		t.Fatalf("healthy model failed: %+v", results["good"])
	}
}

func TestProbeBodyClueBeatsStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":{"code":"insufficient_quota"}}`)
	}))
	defer server.Close()

	results := New().Probe(context.Background(), server.URL, "sk-test", []string{"m"})
	if results["m"].Code != CodeQuota {
		t.Fatalf("code = %q, want %q", results["m"].Code, CodeQuota)
	}
}

func TestProbeConcurrencyStaysAtTheLimit(t *testing.T) {
	var mu sync.Mutex
	inFlight := 0
	peak := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		inFlight++
		if inFlight > peak {
			peak = inFlight
		}
		mu.Unlock()

		// Hold the request open so the probes really overlap.
		time.Sleep(100 * time.Millisecond)

		mu.Lock()
		inFlight--
		mu.Unlock()
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"pong"}}]}`)
	}))
	defer server.Close()

	models := make([]string, 0, 16)
	for i := 0; i < 16; i++ {
		models = append(models, string(rune('a'+i)))
	}

	results := New().Probe(context.Background(), server.URL, "sk-test", models)
	if len(results) != len(models) {
		t.Fatalf("got %d results, want %d", len(results), len(models))
	}

	mu.Lock()
	observed := peak
	mu.Unlock()
	if observed > maxProbeConcurrency {
		t.Fatalf("peak concurrency = %d, want at most %d", observed, maxProbeConcurrency)
	}
	if observed < 2 {
		t.Fatalf("peak concurrency = %d, the probes did not overlap at all", observed)
	}
}

func TestProbeTimeoutIsReportedAsAFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Answer later than the probe deadline.
		time.Sleep(300 * time.Millisecond)
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"pong"}}]}`)
	}))
	defer server.Close()

	discoverer := newTestDiscoverer(server.Client(), 50*time.Millisecond)
	results := discoverer.Probe(context.Background(), server.URL, "sk-test", []string{"slow"})
	result, ok := results["slow"]
	if !ok {
		t.Fatal("missing result for the slow model")
	}
	if result.OK {
		t.Fatal("the slow model reported OK")
	}
	// A model that never answered is a model we could not reach.
	if result.Code != CodeNetwork {
		t.Fatalf("code = %q, want %q", result.Code, CodeNetwork)
	}
}

func TestProbeEmptyModelList(t *testing.T) {
	results := New().Probe(context.Background(), "http://127.0.0.1:1", "sk-test", nil)
	if len(results) != 0 {
		t.Fatalf("got %d results, want none", len(results))
	}
}

func TestProbeReturnsEveryModelEvenWhenAllFail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":{"message":"boom"}}`)
	}))
	defer server.Close()

	models := []string{"a", "b", "c"}
	results := New().Probe(context.Background(), server.URL, "sk-test", models)
	if len(results) != len(models) {
		t.Fatalf("got %d results, want %d", len(results), len(models))
	}
	for _, id := range models {
		result, ok := results[id]
		if !ok {
			t.Fatalf("missing result for model %q", id)
		}
		if result.OK {
			t.Fatalf("model %q reported OK", id)
		}
	}
}

// The probe speaks the dialect the engine speaks. The engine only ever
// calls /responses, so a probe that asked /chat/completions would tell a
// Responses-only vendor that its model does not exist.
func TestProbeSpeaksTheEnginesDialect(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"resp-1","status":"completed","output":[]}`)
	}))
	defer server.Close()

	results := New().Probe(context.Background(), server.URL, "sk-test", []string{"a-model"})
	if !results["a-model"].OK {
		t.Fatalf("the model was reported as broken: %+v", results)
	}
	if gotPath != "/responses" {
		t.Fatalf("the probe asked %q, want /responses", gotPath)
	}
	if gotBody["model"] != "a-model" {
		t.Fatalf("the probe named %v, want a-model", gotBody["model"])
	}
	if gotBody["input"] == nil {
		t.Fatalf("the probe did not use the Responses input field: %v", gotBody)
	}
	if gotAuth != "Bearer sk-test" {
		t.Fatalf("the key did not travel in the header: %q", gotAuth)
	}
}

// A real vendor's catalogue is far bigger than the cap on a failure answer.
// A truncated list is not a small list: it is a body that no longer parses,
// and it used to be reported as an endpoint that speaks no known protocol.
func TestDiscoverReadsARealisticCatalogue(t *testing.T) {
	entries := make([]string, 0, 400)
	for i := 0; i < 400; i++ {
		entries = append(entries, fmt.Sprintf(`{"id":"vendor-model-%03d","object":"model","owned_by":"vendor"}`, i))
	}
	body := `{"object":"list","data":[` + strings.Join(entries, ",") + `]}`
	if len(body) <= maxBodyBytes {
		t.Fatalf("the fixture is %d bytes; it no longer covers the old cap", len(body))
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	defer server.Close()

	got, err := New().Discover(context.Background(), server.URL, "sk-test")
	if err != nil {
		t.Fatalf("Discover returned error: %v", err)
	}
	if len(got) != 400 {
		t.Fatalf("got %d ids, want 400", len(got))
	}
	if got[0] != "vendor-model-000" || got[399] != "vendor-model-399" {
		t.Fatalf("the catalogue did not survive the read: %v", got[:2])
	}
}

func TestClassifyTable(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		want    string
		message string
	}{
		{"payment required", http.StatusPaymentRequired, "", CodeQuota, messageQuota},
		{"quota in body", http.StatusInternalServerError, "insufficient_quota", CodeQuota, messageQuota},
		{"too many requests", http.StatusTooManyRequests, "", CodeRateLimit, messageRateLimit},
		{"rate limit in body", http.StatusBadRequest, "rate limit exceeded", CodeRateLimit, messageRateLimit},
		{"unauthorized", http.StatusUnauthorized, "", CodeAuthInvalid, messageAuthInvalid},
		{"forbidden", http.StatusForbidden, "", CodeAuthInvalid, messageAuthInvalid},
		{"invalid key in body", http.StatusBadRequest, "invalid_api_key", CodeAuthInvalid, messageAuthInvalid},
		{"not found", http.StatusNotFound, "", CodeModelUnknown, messageModelUnknown},
		{"model not found in body", http.StatusBadRequest, "model_not_found", CodeModelUnknown, messageModelUnknown},
		{"context length in body", http.StatusBadRequest, "context_length_exceeded", CodeContextExceeded, messageContextExceeded},
		{"method not allowed", http.StatusMethodNotAllowed, "", CodeProtocolUnsupported, messageProtocolUnsupported},
		{"not implemented", http.StatusNotImplemented, "", CodeProtocolUnsupported, messageProtocolUnsupported},
		{"unsupported in body", http.StatusBadRequest, "unsupported operation", CodeProtocolUnsupported, messageProtocolUnsupported},
		{"no status", 0, "", CodeNetwork, messageNetwork},
		{"server error", http.StatusBadGateway, "", CodeNetwork, messageNetwork},
		{"anything else", http.StatusBadRequest, "", CodeRequestRejected, messageRequestRejected},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, message := classify(tc.status, tc.body)
			if code != tc.want {
				t.Fatalf("code = %q, want %q", code, tc.want)
			}
			if message != tc.message {
				t.Fatalf("message = %q, want %q", message, tc.message)
			}
			if len([]rune(message)) > 20 {
				t.Fatalf("message %q is longer than 20 characters", message)
			}
		})
	}
}
