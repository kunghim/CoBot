package collaboration

import "encoding/json"

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
	task := func() { add("taskId", text("Task ID returned by get_context/list_tasks/create_task."), true) }
	revision := func() {
		add("expectedRevision", map[string]any{"type": "integer", "minimum": 1, "description": "Current revision from get_task; stale writes are rejected."}, true)
	}
	limit := func() { add("limit", map[string]any{"type": "integer", "minimum": 1, "maximum": 100}, false) }
	switch name {
	case "get_context":
		add("taskId", text("Optional task whose current context is requested."), false)
	case "list_tasks":
		add("status", map[string]any{"type": "string", "enum": []string{"todo", "doing", "review", "done", "cancelled"}}, false)
		limit()
	case "get_task":
		task()
	case "list_roles":
	case "send_message":
		add("targetBotIds", strings("Recipient member IDs from get_context, all in this team; one message reaches each."), false)
		add("targetBotId", text("A single recipient member ID; use targetBotIds for several."), false)
		add("text", text("Message to the recipients."), true)
		add("taskId", text("Related task ID, when applicable."), false)
		add("notifyOnly", map[string]any{"type": "boolean", "description": "Information only: recipients see it in their next turn and are not woken. Use for progress notes that need no action."}, false)
	case "create_task", "propose_task":
		add("title", text("Task title."), true)
		add("description", map[string]any{"type": "string", "description": "Work to perform."}, false)
		add("acceptanceCriteria", text("Observable conditions required for completion."), true)
		if name == "create_task" {
			add("ownerId", text("Exactly one team member ID from get_context."), true)
			add("parentId", text("Optional organizational parent task."), false)
			add("dependencyIds", strings("Tasks that must be done before execution."), false)
		}
	case "assign_task":
		task()
		add("ownerId", text("New owner member ID."), true)
		revision()
	case "update_task":
		task()
		add("status", map[string]any{"type": "string", "enum": []string{"todo", "doing", "review", "cancelled"}, "description": "A valid lifecycle transition; done uses confirm_task."}, true)
		add("blockedReason", map[string]any{"type": "string", "description": "Optional reason work is blocked; empty clears it."}, false)
		revision()
	case "submit_evidence":
		task()
		add("summary", text("What was produced and how it was verified."), true)
		add("artifactRefs", strings("Paths or stable references to outputs."), false)
		add("candidateRef", text("Only for Git work: the commit this evidence is about. Leave empty otherwise; never invent a version by hashing files."), false)
		add("testResult", text("The tests actually run and their outcome."), false)
		add("reviewResult", text("The actual review outcome, when this evidence is a review."), false)
	case "report_completion", "confirm_task":
		task()
		add("evidenceIds", strings("Evidence IDs for this task and candidate."), true)
		revision()
	case "offer_transfer":
		task()
		add("toBotId", text("Proposed recipient member ID."), true)
		add("note", map[string]any{"type": "string", "description": "Handoff notes."}, false)
	case "accept_transfer":
		add("transferId", text("Offered transfer ID."), true)
		revision()
	case "ask_user":
		add("question", text("Specific input needed from the user. End this work after receiving waiting."), true)
		add("options", strings("Optional suggested answers, not security permissions."), false)
		add("taskId", text("Task blocked by this question."), false)
	case "request_member":
		add("roleId", text("Role ID from list_roles."), true)
		add("reason", text("Why the team needs this member."), true)
	case "request_merge":
		task()
		add("candidateRef", text("Exact immutable version to authorize."), true)
		add("targetRef", text("Target reference."), true)
		add("executionDirectory", text("Registered execution directory."), true)
		add("evidenceIds", strings("Matching test/review evidence IDs."), true)
	case "remember":
		add("body", text("Reusable working method or preference, not task evidence or secrets."), true)
		add("sourceTaskId", text("Optional task source."), false)
		add("content", text("Legacy alias for body."), false)
		add("taskId", text("Legacy alias for sourceTaskId."), false)
	case "search_memory":
		add("query", map[string]any{"type": "string", "description": "Search text; empty returns recent methods."}, false)
		add("limit", map[string]any{"type": "integer", "minimum": 1, "maximum": 10}, false)
	default:
		panic("missing MCP schema: " + name)
	}
	if IsSideEffectTool(name) {
		add("idempotencyKey", map[string]any{"type": "string", "minLength": 1, "maxLength": 128, "description": "Stable unique key for this operation. Reuse only to retry identical arguments."}, true)
	}
	shape := map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}
	if name == "remember" {
		// Preserve documented legacy spelling without advertising two required bodies.
		filtered := []string{}
		for _, key := range required {
			if key != "body" {
				filtered = append(filtered, key)
			}
		}
		shape["required"] = filtered
		shape["anyOf"] = []any{map[string]any{"required": []string{"body"}}, map[string]any{"required": []string{"content"}}}
	}
	data, err := json.Marshal(shape)
	if err != nil {
		panic(err)
	}
	return data
}
