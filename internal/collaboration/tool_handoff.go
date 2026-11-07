package collaboration

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
)

var handoffTool = toolDef{
	Name:       "handoff",
	Roles:      []string{RoleLeader, RoleMember},
	SubAgentOK: false,
	SideEffect: true,
	Description: "交出你手上的一张卡片。outcome=done：完成，系统按链路交给下一环；检查环节通过但有可选建议时写 suggestions。" +
		"outcome=back：退回（只有评审和测试环节可用），系统会交给产出这项工作的那一环，mode 选 rerun（改完重新往下走）或 return_to_me（改完直接回到你）。" +
		"outcome=escalate：上报项目经理。outcome=resume：项目经理处理完上报卡后交回原环节继续。交出后结束这一轮。",
	Schema: handoffSchema,
	Call:   callHandoff,
}

// handoffPreview is how to start a result the user can run.
type handoffPreview struct {
	Command string `json:"command"`
	URL     string `json:"url"`
}

// previewCommand keeps the run instruction the member gave, or empty when
// the handoff carried no preview at all.
func previewCommand(p *handoffPreview) string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(p.Command)
}

func previewURL(p *handoffPreview) string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(p.URL)
}

func handoffSchema() json.RawMessage {
	s := newToolShape()
	s.add("cardId", textProp("The card you hold and are handing off."), true)
	s.add("outcome", map[string]any{"type": "string", "enum": []string{domain.HandoffDone, domain.HandoffBack, domain.HandoffEscalate, domain.HandoffResume},
		"description": "done, back (review and test stages only), escalate (to the project manager), resume (project manager, on an escalation card)."}, true)
	s.add("summary", textProp("What you did, where the output is, how to run or open it, and the checks you actually ran with their results; for back, what is wrong and what fixed looks like; for the acceptance stage, the report to the user."), true)
	s.add("artifactRefs", stringsProp("Optional paths of the produced files."), false)
	s.add("mode", map[string]any{"type": "string", "enum": []string{domain.ReworkRerun, domain.ReworkReturnToMe},
		"description": "Required for back: rerun (after the fix the route runs on again) or return_to_me (after the fix the work comes straight back to you)."}, false)
	s.add("suggestions", map[string]any{"type": "string", "description": "Optional, with done: improvements that do not block; kept as open items for the final report."}, false)
	s.add("preview", map[string]any{"type": "object", "description": "Optional, with done and a result the user can run: how to start it and where to open it. command is the start command run in the team directory; url is the address to open once it answers.",
		"properties": map[string]any{
			"command": map[string]any{"type": "string"},
			"url":     map[string]any{"type": "string"},
		}, "required": []string{"command"}, "additionalProperties": false}, false)
	s.add("document", map[string]any{"type": "string", "description": "Required on a requirement-document card: the full revised requirement document."}, false)
	return s.json()
}

func callHandoff(ctx context.Context, d *Dispatcher, principal auth.Principal, raw json.RawMessage) ToolResult {
	var args struct {
		CardID       string          `json:"cardId"`
		Outcome      string          `json:"outcome"`
		Summary      string          `json:"summary"`
		ArtifactRefs []string        `json:"artifactRefs"`
		Preview      *handoffPreview `json:"preview"`
		Mode         string          `json:"mode"`
		Suggestions  string          `json:"suggestions"`
		Document     string          `json:"document"`
		RequestID    string          `json:"idempotencyKey"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return rejected("invalid_request", "error.invalid_json", nil)
	}
	if args.RequestID == "" {
		return rejected("invalid_request", "error.idempotency.required", nil)
	}
	card, err := d.app.Handoff(ctx, principal, app.HandoffInput{
		CardID: args.CardID, Outcome: args.Outcome, Summary: args.Summary, ArtifactRefs: args.ArtifactRefs,
		PreviewCommand: previewCommand(args.Preview), PreviewURL: previewURL(args.Preview),
		Mode: args.Mode, Suggestions: args.Suggestions, Document: args.Document,
	})
	if err != nil {
		return errorResult(err)
	}
	return ToolResult{Status: StatusAccepted, Data: map[string]any{"cardId": card.ID, "status": card.Status,
		"note": "已交接，系统会按链路继续。这张卡片处理完了；手上没有别的卡片就结束这一轮。"}}
}
