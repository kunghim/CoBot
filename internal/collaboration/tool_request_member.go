package collaboration

import (
	"context"
	"encoding/json"

	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/auth"
)

var requestMemberTool = toolDef{
	Name:       "request_member",
	Roles:      []string{RoleLeader},
	SubAgentOK: false,
	SideEffect: true,
	Description: "向用户申请招募新成员（项目经理用）。roleIds 写角色库里团队还没有的角色 ID，一次可以申请多个；reason 写清当前工作为什么需要它们。" +
		"申请会变成一张等用户决定的卡片，同意后成员才会加入团队，你会收到结果。提交申请后结束这一轮，等结果；同意之前不要把这个角色安排进链路，也不要重复申请同一个角色。",
	Schema: requestMemberSchema,
	Call:   callRequestMember,
}

func requestMemberSchema() json.RawMessage {
	s := newToolShape()
	s.add("roleIds", stringsProp("Role IDs from the role library that the team does not have yet, e.g. [\"fullstack-engineer\"]."), true)
	s.add("reason", textProp("Why the current work needs these members. End this turn after asking; the user decides on a card."), true)
	return s.json()
}

func callRequestMember(ctx context.Context, d *Dispatcher, principal auth.Principal, raw json.RawMessage) ToolResult {
	var args struct {
		RoleIDs   []string `json:"roleIds"`
		Reason    string   `json:"reason"`
		RequestID string   `json:"idempotencyKey"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return rejected("invalid_request", "error.invalid_json", nil)
	}
	if args.RequestID == "" {
		return rejected("invalid_request", "error.idempotency.required", nil)
	}
	question, err := d.app.RequestMember(ctx, principal, app.RequestMemberInput{RoleIDs: args.RoleIDs, Reason: args.Reason})
	if err != nil {
		return errorResult(err)
	}
	return ToolResult{Status: StatusWaiting, Data: map[string]any{"questionId": question.ID, "roleIds": question.RoleIDs}}
}
