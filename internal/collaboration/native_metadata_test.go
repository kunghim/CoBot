package collaboration

import "testing"

func TestNativeTurnIdentityDoesNotUseModelArguments(t *testing.T) {
	for _, value := range []any{`{"thread_id":"thread","turn_id":"turn","session_id":"root"}`, map[string]any{"thread_id": "thread", "turn_id": "turn"}} {
		thread, turn := nativeTurnIdentity(map[string]any{"x-codex-turn-metadata": value})
		if thread != "thread" || turn != "turn" {
			t.Fatalf("identity lost: %q/%q", thread, turn)
		}
	}
	if thread, turn := nativeTurnIdentity(map[string]any{"arguments": map[string]any{"thread_id": "forged", "turn_id": "forged"}}); thread != "" || turn != "" {
		t.Fatal("model supplied identity accepted")
	}
}
