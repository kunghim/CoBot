package collaboration

import (
	"fmt"

	"github.com/kunghim/CoBot/internal/auth"
)

// Tool statuses mirror the wire contract: accepted means the fact is
// persisted, never that a turn completed.
const (
	StatusOK          = "ok"
	StatusAccepted    = "accepted"
	StatusDuplicate   = "duplicate"
	StatusWaiting     = "waiting"
	StatusRejected    = "rejected"
	StatusUnavailable = "unavailable"
)

// ToolError is a structured tool failure carrying the product error code and
// its i18n message key so the bridge can render an honest message.
type ToolError struct {
	Status     string
	Code       string
	MessageKey string
	Params     map[string]any
}

func (e *ToolError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.MessageKey)
}

// Reject builds a rejected tool error (permission or lifecycle refusal).
func Reject(code, messageKey string, params map[string]any) *ToolError {
	return &ToolError{Status: StatusRejected, Code: code, MessageKey: messageKey, Params: params}
}

// Unavailable builds an unavailable tool error (engine or core cannot serve).
func Unavailable(code, messageKey string, params map[string]any) *ToolError {
	return &ToolError{Status: StatusUnavailable, Code: code, MessageKey: messageKey, Params: params}
}

// ToolResult is what the dispatcher returns to the bridge.
type ToolResult struct {
	Status string         `json:"status"`
	Data   map[string]any `json:"data,omitempty"`
	Error  *ToolError     `json:"error,omitempty"`
}

// WaitResult is returned by ask_user: the tool never blocks on the user.
type WaitResult struct {
	QuestionID string `json:"questionId"`
	Status     string `json:"status"`
}

// IsSideEffectTool reports whether a tool changes product state. From
// outside the core — the spawned bridge, a hook process — an executor whose
// local lease is stale must not run these; read tools always pass (H06).
func IsSideEffectTool(name string) bool {
	switch name {
	case "deliver_task", "review_task",
		"send_message", "create_task", "propose_task", "assign_task", "update_task",
		"submit_evidence", "report_completion", "confirm_task", "offer_transfer",
		"accept_transfer", "ask_user", "request_member", "request_merge", "remember":
		return true
	default:
		return false
	}
}

// Bridge env aliases for the lease gate (the bridge reads them from its own
// environment, like the endpoint and token).
const (
	BridgeEnvBotID    = auth.BridgeEnvBotID
	BridgeEnvLeaseDir = auth.BridgeEnvLeaseDir
)
