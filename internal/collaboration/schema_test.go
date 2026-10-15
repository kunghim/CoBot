package collaboration

import (
	"encoding/json"
	"testing"
)

func TestEveryToolAdvertisesItsArguments(t *testing.T) {
	for _, name := range []string{"get_context", "list_tasks", "get_task", "list_roles", "send_message", "create_task", "propose_task", "assign_task", "update_task", "submit_evidence", "report_completion", "confirm_task", "offer_transfer", "accept_transfer", "ask_user", "request_member", "request_merge", "remember", "search_memory"} {
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
		if IsSideEffectTool(name) {
			if shape.Properties["idempotencyKey"] == nil {
				t.Fatalf("%s missing idempotency key", name)
			}
			found := false
			for _, key := range shape.Required {
				found = found || key == "idempotencyKey"
			}
			if !found {
				t.Fatalf("%s key not required", name)
			}
		}
		if shape.Properties["from"] != nil || shape.Properties["teamId"] != nil || shape.Properties["sourceRun"] != nil {
			t.Fatalf("%s exposes host identity", name)
		}
	}
}
