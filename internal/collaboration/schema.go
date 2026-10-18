package collaboration

import (
	"encoding/json"

	"github.com/kunghim/CoBot/internal/roles"
)

// These are wire schemas, not domain permissions. The application still
// validates identity, ownership, current revision and execution state.
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
		add("taskId", text("Optional: one task to read in full; omit to list every task of the team."), false)
	case "deliver_task":
		add("taskId", text("The task you own and are delivering."), true)
		add("summary", text("What you produced, how to run or open it, and the checks you actually ran with their results."), true)
		add("artifactRefs", strings("Optional paths of the produced files."), false)
	case "review_task":
		add("taskId", text("The delivered task to review."), true)
		add("decision", map[string]any{"type": "string", "enum": []string{"accept", "reject"}, "description": "accept completes the task; reject sends it back to its owner."}, true)
		add("comment", map[string]any{"type": "string", "description": "Required for reject: what is missing. Optional for accept."}, false)
	case "send_message":
		add("targetBotIds", strings("Recipient member IDs (from the team roster in your instructions); one message reaches each."), false)
		add("targetBotId", text("A single recipient member ID; use targetBotIds for several."), false)
		add("text", text("Message to the recipients."), true)
		add("taskId", text("Related task ID, when applicable."), false)
		add("notifyOnly", map[string]any{"type": "boolean", "description": "Information only: recipients see it in their next turn and are not woken. Only for notes that need no action; deliveries, completion reports and anything to verify or decide must wake the recipient."}, false)
	case "create_task":
		add("title", text("Task title."), true)
		add("description", map[string]any{"type": "string", "description": "Work to perform."}, false)
		add("acceptanceCriteria", text("Observable conditions required for completion."), true)
		add("ownerId", text("Exactly one team member ID from the team roster in your instructions."), true)
		add("parentId", text("Optional organizational parent task."), false)
		add("dependencyIds", strings("Task IDs that must be done first; this task reaches its owner only after they are."), false)
		add("reviewType", map[string]any{"type": "string", "enum": roles.ReviewTypes, "description": "Optional: the kind of review the delivery needs (requirements, design, test_plan, code, general). A reviewer member then decides it; omit to accept it yourself."}, false)
	case "ask_user":
		add("question", text("Specific input needed from the user. End this work after receiving waiting."), true)
		add("options", strings("Optional suggested answers, not security permissions."), false)
		add("taskId", text("Task blocked by this question."), false)
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
