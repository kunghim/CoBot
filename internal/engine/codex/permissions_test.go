package codex

import (
	"encoding/json"
	"testing"

	"github.com/kunghim/CoBot/internal/engine"
)

func TestPermissionModesMapOntoCodex(t *testing.T) {
	cases := map[string][3]string{
		"read_only": {"on-request", "read-only", "readOnly"},
		"default":   {"on-request", "workspace-write", "workspaceWrite"},
		"full":      {"never", "danger-full-access", "dangerFullAccess"},
	}
	for mode, want := range cases {
		spec := applyPermission(engine.ThreadSpec{Permission: mode, Sandbox: "x", ApprovalPolicy: "untrusted"})
		if spec.ApprovalPolicy != want[0] || spec.Sandbox != want[1] {
			t.Errorf("%s thread = %s/%s", mode, spec.ApprovalPolicy, spec.Sandbox)
		}
		policy := permissionPolicies[mode]
		data, _ := json.Marshal(TurnStartParams{ThreadID: "t", ApprovalPolicy: policy.approval, SandboxPolicy: policy.sandboxPolicy()})
		var wire struct {
			ApprovalPolicy string `json:"approvalPolicy"`
			SandboxPolicy  struct {
				Type string `json:"type"`
			} `json:"sandboxPolicy"`
		}
		if err := json.Unmarshal(data, &wire); err != nil || wire.ApprovalPolicy != want[0] || wire.SandboxPolicy.Type != want[2] {
			t.Errorf("%s turn wire = %s", mode, data)
		}
	}
	// A spec without a mode (the risk evaluator) keeps its own settings.
	if spec := applyPermission(engine.ThreadSpec{Sandbox: "read-only", ApprovalPolicy: "never"}); spec.Sandbox != "read-only" || spec.ApprovalPolicy != "never" {
		t.Fatalf("explicit settings overridden: %+v", spec)
	}
	// A turn without a mode sends no override at all.
	data, _ := json.Marshal(TurnStartParams{ThreadID: "t"})
	if string(data) != `{"threadId":"t","input":null}` {
		t.Fatalf("empty override leaked: %s", data)
	}
}

func TestPermissionRequestCarriesTheAskedAccess(t *testing.T) {
	var got engine.ApprovalRequest
	server := &Server{pending: map[string]pendingApproval{}, callbacks: engine.Callbacks{OnApproval: func(req engine.ApprovalRequest) { got = req }}}
	server.handleRequest(InboundRequest{ID: json.RawMessage(`7`), Method: "item/permissions/requestApproval", Params: json.RawMessage(`{"threadId":"th","turnId":"tu","itemId":"i","cwd":"E:\\w","reason":"需要联网安装依赖","permissions":{"network":{"enabled":true}}}`)})
	if string(got.Permissions) != `{"network":{"enabled":true}}` || got.Reason != "需要联网安装依赖" {
		t.Fatalf("permissions request = %+v", got)
	}
	server.handleRequest(InboundRequest{ID: json.RawMessage(`8`), Method: "item/commandExecution/requestApproval", Params: json.RawMessage(`{"threadId":"th","turnId":"tu","itemId":"c","command":"npm install","networkApprovalContext":{"host":"registry.npmjs.org"}}`)})
	if !got.Network || got.Permissions != nil {
		t.Fatalf("network command = %+v", got)
	}
}
