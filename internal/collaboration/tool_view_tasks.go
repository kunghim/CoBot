package collaboration

import (
	"context"
	"encoding/json"

	"github.com/kunghim/CoBot/internal/auth"
)

var viewTasksTool = toolDef{
	Name:        "view_tasks",
	Roles:       []string{RoleLeader, RoleMember},
	SubAgentOK:  true,
	SideEffect:  false,
	Description: "查看需求和卡片。不传参数列出进行中的需求和它们的卡片；传 requirementId 返回这个需求的需求文档、链路和卡片；传 cardId 返回这张卡片和它的交接记录。",
	Schema:      viewTasksSchema,
	Call:        callViewTasks,
}

func viewTasksSchema() json.RawMessage {
	s := newToolShape()
	s.add("requirementId", textProp("Optional: one requirement to read with its document, route and cards."), false)
	s.add("cardId", textProp("Optional: one card to read with its handoff records."), false)
	return s.json()
}

func callViewTasks(ctx context.Context, d *Dispatcher, principal auth.Principal, raw json.RawMessage) ToolResult {
	var args struct {
		RequirementID string `json:"requirementId"`
		CardID        string `json:"cardId"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return rejected("invalid_request", "error.invalid_json", nil)
	}
	data, err := d.app.ViewTasks(ctx, principal, args.RequirementID, args.CardID)
	if err != nil {
		return errorResult(err)
	}
	return ToolResult{Status: StatusOK, Data: data}
}
