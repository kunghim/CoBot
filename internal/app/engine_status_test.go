package app

import (
	"encoding/json"
	"testing"
)

func TestObservedHookFailureOverridesTrustWithoutMutatingCache(t *testing.T) {
	a := &App{}
	trusted := EngineStatusInfo{Capabilities: []string{"app-server", "codexHook:trusted"}}
	a.hookFailures.Store("team", true)
	if got := a.withHookFailures(trusted); got.Capabilities[1] != "codexHook:failed" {
		t.Fatal(got)
	}
	if trusted.Capabilities[1] != "codexHook:trusted" {
		t.Fatal("mutated cached capabilities")
	}
	a.hookFailures.Delete("team")
	if got := a.withHookFailures(trusted); got.Capabilities[1] != "codexHook:trusted" {
		t.Fatal(got)
	}
}

func TestOfficialAccountProjection(t *testing.T) {
	for _, tc := range []struct {
		raw, mode string
		logged    bool
	}{
		{`{"account":null,"requiresOpenaiAuth":true}`, "", false},
		{`{"account":{"type":"chatgpt","email":"private@example.invalid","planType":"plus"},"requiresOpenaiAuth":true}`, "chatgpt", true},
		{`{"account":{"type":"apiKey"},"requiresOpenaiAuth":true}`, "apiKey", true},
		{`{"account":{"type":"futureUnknown"}}`, "", false},
	} {
		got := parseAccount(json.RawMessage(tc.raw))
		if got.LoggedIn != tc.logged || got.AuthMode != tc.mode {
			t.Errorf("account projection: got %+v want logged=%v mode=%s", got, tc.logged, tc.mode)
		}
	}
}

func TestOfficialModelProjection(t *testing.T) {
	raw := json.RawMessage(`{"data":[{"id":"m1","model":"model-one","displayName":"One","internalSecret":"must-not-leak"},{"id":"m2","model":"model-two"}],"nextCursor":null}`)
	var got struct {
		Models []map[string]any `json:"models"`
	}
	if err := json.Unmarshal(sanitizeModels(raw), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Models) != 2 {
		t.Fatalf("model count=%d, want 2", len(got.Models))
	}
	if got.Models[0]["slug"] != "model-one" || got.Models[0]["internalSecret"] != nil {
		t.Fatal("incorrect public model projection", got.Models)
	}
}
