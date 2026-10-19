package collaboration

import (
	"encoding/json"

	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/roles"
)

// stageKinds are the stage kinds a route may use.
var stageKinds = []string{
	domain.StageConfirm, domain.StageDesign, domain.StageReview, domain.StageTestCases,
	domain.StageImplement, domain.StageTest, domain.StageAccept,
}

// These are wire schemas, not domain permissions. The application still
// validates identity, ownership, the route and execution state.
func toolSchema(name string) json.RawMessage {
	text := func(description string) any {
		return map[string]any{"type": "string", "description": description, "minLength": 1}
	}
	strings := func(description string) any {
		return map[string]any{"type": "array", "description": description, "items": map[string]any{"type": "string", "minLength": 1}}
	}
	props := map[string]any{}
	required := []string{}
	add := func(key string, shape any, mandatory bool) {
		props[key] = shape
		if mandatory {
			required = append(required, key)
		}
	}
	switch name {
	case "view_tasks":
		add("requirementId", text("Optional: one requirement to read with its document, route and cards."), false)
		add("cardId", text("Optional: one card to read with its handoff records."), false)
	case "start_requirement":
		add("title", text("Short title of the requirement."), true)
		add("document", text("The requirement document (Markdown): goal, scope and what is left out, verifiable completion criteria, assumptions and open questions."), true)
		stage := map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": text("Stage name shown on its card, e.g. 实现登录页."),
				"kind": map[string]any{"type": "string", "enum": stageKinds,
					"description": "confirm (the user confirms), design, review, test_cases, implement, test, accept (your acceptance report, always last)."},
				"ownerId":    text("The member doing this stage, from the team roster. Omit for confirm and accept."),
				"reviewType": map[string]any{"type": "string", "enum": roles.ReviewTypes, "description": "Required for review: requirements, design, test_plan, code or general."},
				"criteria":   map[string]any{"type": "string", "description": "What this stage must deliver; for confirm, what the user confirms."},
			},
			"required":             []string{"kind"},
			"additionalProperties": false,
		}
		add("stages", map[string]any{"type": "array", "minItems": 2, "items": stage,
			"description": "The route in order. The first stage is confirm (optionally after one requirements review); the last is accept."}, true)
	case "handoff":
		add("cardId", text("The card you hold and are handing off."), true)
		add("outcome", map[string]any{"type": "string", "enum": []string{domain.HandoffDone, domain.HandoffBack, domain.HandoffEscalate, domain.HandoffResume},
			"description": "done, back (review and test stages only), escalate (to the project manager), resume (project manager, on an escalation card)."}, true)
		add("summary", text("What you did, where the output is, how to run or open it, and the checks you actually ran with their results; for back, what is wrong and what fixed looks like; for the acceptance stage, the report to the user."), true)
		add("artifactRefs", strings("Optional paths of the produced files."), false)
		add("mode", map[string]any{"type": "string", "enum": []string{domain.ReworkRerun, domain.ReworkReturnToMe},
			"description": "Required for back: rerun (after the fix the route runs on again) or return_to_me (after the fix the work comes straight back to you)."}, false)
		add("suggestions", map[string]any{"type": "string", "description": "Optional, with done: improvements that do not block; kept as open items for the final report."}, false)
		add("document", map[string]any{"type": "string", "description": "Required on a requirement-document card: the full revised requirement document."}, false)
	case "send_message":
		add("targetBotIds", strings("Recipient member IDs (from the team roster in your instructions); one message reaches each."), false)
		add("targetBotId", text("A single recipient member ID; use targetBotIds for several."), false)
		add("text", text("The note for the recipients. They are not woken; they read it when they next work."), true)
	case "ask_user":
		add("question", text("Specific input needed from the user. End this turn after asking."), true)
		add("options", strings("Optional suggested answers, not security permissions."), false)
	default:
		panic("missing MCP schema: " + name)
	}
	// Writes take their idempotency key from the engine's call id, so the
	// model never has to invent one.
	shape := map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}
	data, err := json.Marshal(shape)
	if err != nil {
		panic(err)
	}
	return data
}
