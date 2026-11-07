package collaboration

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/kunghim/CoBot/internal/auth"
)

var sendMessageTool = toolDef{
	Name:       "send_message",
	Roles:      []string{RoleLeader, RoleMember},
	SubAgentOK: true,
	SideEffect: true,
	Description: "给同事留言，只用于反馈和告知：对方不会被唤醒，下一次开始工作时才会看到。不要用它派活、催人或代替交接。" +
		"同一内容要告诉几个人时用 targetBotIds 一次发完。",
	Schema: sendMessageSchema,
	Call:   callSendMessage,
}

// uniqueTargets is the send_message recipient list: targetBotIds, or the
// single legacy targetBotId, without repeats.
func uniqueTargets(single string, many []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, id := range append([]string{single}, many...) {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// recipientKey derives one recipient's idempotency key from the broadcast's.
func recipientKey(key, target string) string {
	if len(key) > 100 {
		key = key[:100]
	}
	suffix := target
	if len(suffix) > 12 {
		suffix = suffix[:12]
	}
	return key + "/" + suffix
}

// perRecipient gives each recipient's write its own idempotency identity.
func perRecipient(p auth.Principal, target string) auth.Principal {
	if p.Write == nil {
		return p
	}
	write := *p.Write
	write.Key = recipientKey(write.Key, target)
	p.Write = &write
	return p
}

func sendMessageSchema() json.RawMessage {
	s := newToolShape()
	s.add("targetBotIds", stringsProp("Recipient member IDs (from the team roster in your instructions); one message reaches each."), false)
	s.add("targetBotId", textProp("A single recipient member ID; use targetBotIds for several."), false)
	s.add("text", textProp("The note for the recipients. They are not woken; they read it when they next work."), true)
	return s.json()
}

func callSendMessage(ctx context.Context, d *Dispatcher, principal auth.Principal, raw json.RawMessage) ToolResult {
	var args struct {
		TargetBotID  string   `json:"targetBotId"`
		TargetBotIDs []string `json:"targetBotIds"`
		Text         string   `json:"text"`
		RequestID    string   `json:"idempotencyKey"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return rejected("invalid_request", "error.invalid_json", nil)
	}
	if args.RequestID == "" {
		return rejected("invalid_request", "error.idempotency.required", nil)
	}
	targets := uniqueTargets(args.TargetBotID, args.TargetBotIDs)
	if len(targets) == 0 {
		return rejected("invalid_request", "error.message.target_required", nil)
	}
	if len(targets) == 1 {
		result, err := d.app.NotifyFromBot(ctx, principal, targets[0], args.Text, "", args.RequestID)
		if err != nil {
			return errorResult(err)
		}
		return ToolResult{Status: StatusAccepted, Data: result}
	}
	// One message per recipient, each its own idempotent write so a
	// retried broadcast neither duplicates nor drops a recipient.
	results := make([]map[string]any, 0, len(targets))
	for _, target := range targets {
		result, err := d.app.NotifyFromBot(ctx, perRecipient(principal, target), target, args.Text, "", recipientKey(args.RequestID, target))
		if err != nil {
			return errorResult(err)
		}
		result["targetBotId"] = target
		results = append(results, result)
	}
	return ToolResult{Status: StatusAccepted, Data: map[string]any{"status": "notified", "recipients": results}}
}
