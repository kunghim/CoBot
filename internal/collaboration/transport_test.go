package collaboration

import (
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
)

func TestMCPRequestIdentityIsStableAndTurnScoped(t *testing.T) {
	key := func(id any, turn, modern string) string {
		t.Helper()
		meta := map[string]any{"x-codex-turn-metadata": map[string]any{"thread_id": "thread", "turn_id": turn}}
		if modern != "" {
			meta["callId"] = modern
		}
		params, _ := json.Marshal(map[string]any{"name": "create_task", "arguments": map[string]any{}, "_meta": meta})
		requestID, err := jsonrpc.MakeID(id)
		if err != nil {
			t.Fatal(err)
		}
		request := &jsonrpc.Request{ID: requestID, Method: "tools/call", Params: params}
		preserveCallID(request)
		var result struct {
			Meta map[string]any `json:"_meta"`
		}
		if err := json.Unmarshal(request.Params, &result); err != nil {
			t.Fatal(err)
		}
		value, _ := result.Meta["callId"].(string)
		return value
	}
	first := key(float64(1), "turn-1", "")
	if first == "" || first != key(float64(1), "turn-1", "") {
		t.Fatal("retry lost its transport identity")
	}
	for _, other := range []string{key(float64(2), "turn-1", ""), key("1", "turn-1", ""), key(float64(1), "turn-2", "")} {
		if other == first {
			t.Fatal("distinct calls shared an identity")
		}
	}
	if key(float64(1), "turn-1", "native-call") != "native-call" {
		t.Fatal("overwrote native call identity")
	}
	if key(float64(1), "", "") != "" {
		t.Fatal("derived identity without a native turn")
	}
}
