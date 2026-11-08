package collaboration

import (
	"context"
	"encoding/json"
	"slices"
	"time"

	"github.com/kunghim/CoBot/internal/auth"
)

// The tools a member's model is offered. Every member is its own Codex
// session that knows nothing of the others, so these tools are its only way
// to reach the team; they are kept to the few that move the work, and each
// role gets only what it may use. Work moves only on cards: CoBot hands each
// stage to its owner, and a member hands its card off; a message only
// informs.
const (
	RoleLeader = "leader"
	RoleMember = "member"

	// BridgeEnvRole names the role a spawned bridge serves.
	BridgeEnvRole = auth.BridgeEnvRole
)

// toolDef is one tool: who is offered it, what the model reads about it, and
// how a call runs. Its business lives in the application layer, behind App.
type toolDef struct {
	Name        string
	Roles       []string
	SubAgentOK  bool          // a sub-agent spawned inside the member's turn may call it
	SideEffect  bool          // an executor with a stale lease must not run it
	Timeout     time.Duration // dispatchTimeout when zero
	Description string
	Schema      func() json.RawMessage
	Call        func(ctx context.Context, d *Dispatcher, principal auth.Principal, raw json.RawMessage) ToolResult
}

// toolTable lists every tool. Its order is what the model sees: ToolsForRole
// keeps it, so moving an entry changes a role's list.
var toolTable = []*toolDef{&startRequirementTool, &handoffTool, &viewTasksTool, &sendMessageTool, &askUserTool, &requestMemberTool, &browserTool}

var toolByName = func() map[string]*toolDef {
	byName := make(map[string]*toolDef, len(toolTable))
	for _, tool := range toolTable {
		byName[tool.Name] = tool
	}
	return byName
}()

// ToolsForRole lists the tools offered to a role. An unknown role (a bridge
// spawned without one) gets the leader's set; the core still checks every
// call.
func ToolsForRole(role string) []string {
	if role != RoleMember {
		role = RoleLeader
	}
	var names []string
	for _, tool := range toolTable {
		if slices.Contains(tool.Roles, role) {
			names = append(names, tool.Name)
		}
	}
	return names
}
