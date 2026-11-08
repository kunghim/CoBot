package collaboration

import (
	"context"
	"encoding/json"

	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/roles"
)

var startRequirementTool = toolDef{
	Name:       "start_requirement",
	Roles:      []string{RoleLeader},
	SubAgentOK: false,
	SideEffect: true,
	Description: "提交需求文档和链路（项目经理用）。stages 按顺序列出每一环；第一环是用户确认（前面最多一环需求评审），最后一环是你的验收汇报。" +
		"提交后系统把需求文档和链路交给用户确认，之后按链路把卡片送到各环负责人手上。提交后结束这一轮。",
	Schema: startRequirementSchema,
	Call:   callStartRequirement,
}

// stageKinds are the stage kinds a route may use.
var stageKinds = []string{
	domain.StageConfirm, domain.StageDesign, domain.StageReview, domain.StageTestCases,
	domain.StageImplement, domain.StageTest, domain.StageAccept,
}

func startRequirementSchema() json.RawMessage {
	s := newToolShape()
	s.add("title", textProp("Short title of the requirement."), true)
	s.add("document", textProp("The requirement document (Markdown): goal, scope and what is left out, verifiable completion criteria, assumptions and open questions."), true)
	stage := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": textProp("Stage name shown on its card, e.g. 实现登录页."),
			"kind": map[string]any{"type": "string", "enum": stageKinds,
				"description": "confirm (the user confirms), design, review, test_cases, implement, test, accept (your acceptance report, always last)."},
			"ownerId":    textProp("The member doing this stage, from the team roster. Omit for confirm and accept."),
			"reviewType": map[string]any{"type": "string", "enum": roles.ReviewTypes, "description": "Required for review: requirements, design, test_plan, code or general."},
			"criteria":   map[string]any{"type": "string", "description": "What this stage must deliver; for confirm, what the user confirms."},
		},
		"required":             []string{"kind"},
		"additionalProperties": false,
	}
	s.add("stages", map[string]any{"type": "array", "minItems": 2, "items": stage,
		"description": "The route in order. The first stage is confirm (optionally after one requirements review); the last is accept."}, true)
	return s.json()
}

func callStartRequirement(ctx context.Context, d *Dispatcher, principal auth.Principal, raw json.RawMessage) ToolResult {
	var args struct {
		Title     string         `json:"title"`
		Document  string         `json:"document"`
		Stages    []domain.Stage `json:"stages"`
		RequestID string         `json:"idempotencyKey"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return rejected("invalid_request", "error.invalid_json", nil)
	}
	if args.RequestID == "" {
		return rejected("invalid_request", "error.idempotency.required", nil)
	}
	req, err := d.app.StartRequirement(ctx, principal, app.StartRequirementInput{Title: args.Title, Document: args.Document, Stages: args.Stages})
	if err != nil {
		return errorResult(err)
	}
	return ToolResult{Status: StatusAccepted, Data: map[string]any{"requirementId": req.ID,
		"note": "已提交，系统会把需求文档和链路交给用户确认。现在结束这一轮。"}}
}
