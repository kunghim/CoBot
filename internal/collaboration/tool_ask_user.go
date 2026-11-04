package collaboration

import (
	"context"
	"encoding/json"

	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/auth"
)

var askUserTool = toolDef{
	Name:        "ask_user",
	Roles:       []string{RoleLeader, RoleMember},
	SubAgentOK:  false,
	SideEffect:  true,
	Description: "向用户提问。只用于只有用户能决定的问题；需求范围的变化请用 handoff 上报项目经理。调用后结束这一轮，用户回答后你会被唤醒。",
	Schema:      askUserSchema,
	Call:        callAskUser,
}

func askUserSchema() json.RawMessage {
	s := newToolShape()
	s.add("question", textProp("Specific input needed from the user. End this turn after asking."), true)
	s.add("options", stringsProp("Optional suggested answers, not security permissions."), false)
	return s.json()
}

func callAskUser(ctx context.Context, d *Dispatcher, principal auth.Principal, raw json.RawMessage) ToolResult {
	var args struct {
		Question  string   `json:"question"`
		Options   []string `json:"options"`
		RequestID string   `json:"idempotencyKey"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return rejected("invalid_request", "error.invalid_json", nil)
	}
	if args.RequestID == "" {
		return rejected("invalid_request", "error.idempotency.required", nil)
	}
	question, err := d.app.AskUser(ctx, principal, app.AskUserInput{Question: args.Question, Options: args.Options})
	if err != nil {
		return errorResult(err)
	}
	return ToolResult{Status: StatusWaiting, Data: map[string]any{"questionId": question.ID}}
}
