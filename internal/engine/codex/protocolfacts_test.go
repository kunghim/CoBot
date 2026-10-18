package codex

import (
	"slices"
	"testing"
)

func TestProtocolFactsShowNewMessagesAndFields(t *testing.T) {
	old := []byte(`{"definitions":{
	  "ServerNotification":{"oneOf":[
	    {"properties":{"method":{"enum":["turn/started"]},"params":{"$ref":"#/definitions/TurnStarted"}},"required":["method"]}]},
	  "TurnStarted":{"type":"object","properties":{"turnId":{"type":"string"}},"required":["turnId"]}}}`)
	current := []byte(`{"definitions":{
	  "ServerNotification":{"oneOf":[
	    {"properties":{"method":{"enum":["turn/started"]},"params":{"$ref":"#/definitions/TurnStarted"}},"required":["method"]},
	    {"properties":{"method":{"enum":["turn/paused"]}},"required":["method"]}]},
	  "TurnStarted":{"type":"object","properties":{"turnId":{"type":"string"},"model":{"type":["string","null"]}},"required":["turnId"]}}}`)
	before, err := SchemaFacts("codex_app_server_protocol.v2.schemas", old)
	if err != nil {
		t.Fatal(err)
	}
	after, err := SchemaFacts("codex_app_server_protocol.v2.schemas", current)
	if err != nil {
		t.Fatal(err)
	}
	diff := DiffProtocol(ProtocolFacts{Facts: before}, ProtocolFacts{Facts: after})
	if len(diff.Removed) != 0 {
		t.Fatalf("removed = %v", diff.Removed)
	}
	var methods []string
	for _, fact := range diff.Added {
		if union, method, ok := IsMethodFact(fact); ok {
			methods = append(methods, union+" "+method)
		}
	}
	if !slices.Equal(methods, []string{"ServerNotification turn/paused"}) {
		t.Fatalf("new messages = %v (added %v)", methods, diff.Added)
	}
	if !slices.Contains(diff.Added, "codex_app_server_protocol.v2.schemas/TurnStarted.model : null|string") {
		t.Fatalf("new field missing: %v", diff.Added)
	}
}

func TestBaselineIsReadable(t *testing.T) {
	baseline, err := Baseline()
	if err != nil || baseline.CodexVersion == "" || len(baseline.Facts) == 0 {
		t.Fatalf("baseline: %v %q %d", err, baseline.CodexVersion, len(baseline.Facts))
	}
}
