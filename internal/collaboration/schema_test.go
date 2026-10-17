package collaboration

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestEveryToolAdvertisesItsArguments(t *testing.T) {
	for _, name := range ToolsForRole("") {
		var shape struct {
			Properties           map[string]any `json:"properties"`
			Required             []string       `json:"required"`
			AdditionalProperties bool           `json:"additionalProperties"`
		}
		if err := json.Unmarshal(toolSchema(name), &shape); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if shape.Properties == nil || shape.AdditionalProperties {
			t.Fatalf("%s missing parameter contract", name)
		}
		if toolDescriptions[name] == "" {
			t.Fatalf("%s has no description", name)
		}
		// Writes take their idempotency key from the engine's call id.
		if _, advertised := shape.Properties["idempotencyKey"]; advertised {
			t.Fatalf("%s makes the model invent an idempotency key", name)
		}
		if shape.Properties["from"] != nil || shape.Properties["teamId"] != nil || shape.Properties["sourceRun"] != nil {
			t.Fatalf("%s exposes host identity", name)
		}
	}
}

func TestEachRoleIsOfferedOnlyItsTools(t *testing.T) {
	if got := ToolsForRole(RoleLeader); !slices.Equal(got, []string{"send_message", "ask_user", "create_task", "review_task", "view_tasks"}) {
		t.Fatalf("leader tools = %v", got)
	}
	if got := ToolsForRole(RoleMember); !slices.Equal(got, []string{"send_message", "ask_user", "deliver_task", "view_tasks"}) {
		t.Fatalf("member tools = %v", got)
	}
}

func TestWritesDeriveTheirKeyFromTheCallID(t *testing.T) {
	call := withDerivedKey(ToolCall{Name: "deliver_task", Arguments: json.RawMessage(`{"taskId":"t1","summary":"done"}`), CallID: "exec-1"})
	var args map[string]any
	_ = json.Unmarshal(call.Arguments, &args)
	key, _ := args["idempotencyKey"].(string)
	if key == "" {
		t.Fatal("no key derived")
	}
	// A retry of the same call gets the same key; other arguments in the same
	// code cell (same call id) get another.
	again := withDerivedKey(ToolCall{Name: "deliver_task", Arguments: json.RawMessage(`{"summary":"done","taskId":"t1"}`), CallID: "exec-1"})
	other := withDerivedKey(ToolCall{Name: "deliver_task", Arguments: json.RawMessage(`{"taskId":"t2","summary":"done"}`), CallID: "exec-1"})
	keyOf := func(c ToolCall) string {
		var m map[string]any
		_ = json.Unmarshal(c.Arguments, &m)
		s, _ := m["idempotencyKey"].(string)
		return s
	}
	if keyOf(again) != key || keyOf(other) == key {
		t.Fatalf("keys: %q %q %q", key, keyOf(again), keyOf(other))
	}
	// A key the caller gave is kept; reads and calls without a call id are untouched.
	if keyOf(withDerivedKey(ToolCall{Name: "send_message", Arguments: json.RawMessage(`{"idempotencyKey":"mine"}`), CallID: "x"})) != "mine" {
		t.Fatal("caller key replaced")
	}
	if keyOf(withDerivedKey(ToolCall{Name: "view_tasks", Arguments: json.RawMessage(`{}`), CallID: "x"})) != "" {
		t.Fatal("read tool got a key")
	}
	if keyOf(withDerivedKey(ToolCall{Name: "send_message", Arguments: json.RawMessage(`{}`)})) != "" {
		t.Fatal("key derived without a call id")
	}
}
