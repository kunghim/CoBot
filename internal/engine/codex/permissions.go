package codex

import "github.com/kunghim/CoBot/internal/engine"

// permissionPolicy is how one CoBot permission mode reads in Codex terms.
type permissionPolicy struct {
	approval    string // AskForApproval
	sandbox     string // thread-level SandboxMode
	sandboxType string // turn-level SandboxPolicy type
}

// permissionPolicies maps CoBot's three modes onto Codex:
//
//	read_only: may read and analyse; asks before anything else
//	default:   free to read, write and run inside the team folder; asks to go
//	           beyond it (other paths, network)
//	full:      never asks, no sandbox
var permissionPolicies = map[string]permissionPolicy{
	"read_only": {approval: "on-request", sandbox: "read-only", sandboxType: "readOnly"},
	"default":   {approval: "on-request", sandbox: "workspace-write", sandboxType: "workspaceWrite"},
	"full":      {approval: "never", sandbox: "danger-full-access", sandboxType: "dangerFullAccess"},
}

func (p permissionPolicy) sandboxPolicy() map[string]any {
	return map[string]any{"type": p.sandboxType}
}

// applyPermission fills a thread's sandbox and approval policy from its
// permission mode; a spec without one keeps its explicit settings.
func applyPermission(spec engine.ThreadSpec) engine.ThreadSpec {
	if policy, ok := permissionPolicies[spec.Permission]; ok {
		spec.Sandbox, spec.ApprovalPolicy = policy.sandbox, policy.approval
	}
	return spec
}
