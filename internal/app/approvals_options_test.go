package app

import (
	"encoding/json"
	"testing"

	"github.com/kunghim/CoBot/internal/domain"
)

func TestNativeOptionsReplyInTheEngineShape(t *testing.T) {
	command := domain.Approval{Type: domain.ApprovalNativeTool, Method: "item/commandExecution/requestApproval"}
	for _, option := range nativeToolOptions(command.Method) {
		response, err := engineResponseFor(command, option.ID)
		if err != nil || response["decision"] != option.ID {
			t.Fatalf("%s -> %v %v", option.ID, response, err)
		}
	}
	if len(nativeToolOptions(command.Method)) != 4 {
		t.Fatal("commands offer allow, allow for session, decline, decline and stop")
	}
	if _, err := engineResponseFor(command, "acceptWithExecpolicyAmendment"); err == nil {
		t.Fatal("an option CoBot never offers must be refused")
	}

	permissions := domain.Approval{Type: domain.ApprovalNativeTool, Method: "item/permissions/requestApproval", Summary: `{"cwd":"E:\\w","permissions":{"network":{"enabled":true}}}`}
	grant, err := engineResponseFor(permissions, domain.DecisionGrantSession)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(grant)
	if string(data) != `{"permissions":{"network":{"enabled":true}},"scope":"session"}` {
		t.Fatalf("grant = %s", data)
	}
	turn, _ := engineResponseFor(permissions, domain.DecisionGrantTurn)
	if turn["scope"] != "turn" {
		t.Fatalf("turn grant = %v", turn)
	}
	deny, _ := engineResponseFor(permissions, "deny")
	if data, _ := json.Marshal(deny); string(data) != `{"permissions":{}}` {
		t.Fatalf("deny = %s", data)
	}
	// Without the asked access on record there is nothing exact to grant.
	if _, err := engineResponseFor(domain.Approval{Type: domain.ApprovalNativeTool, Method: permissions.Method, Summary: "item/permissions/requestApproval"}, domain.DecisionGrantTurn); err == nil {
		t.Fatal("a grant without the asked access must be refused")
	}
}
