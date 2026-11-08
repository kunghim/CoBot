package collaboration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/browser"
	"github.com/kunghim/CoBot/internal/domain"
)

// App is the slice of application services the collaboration tools need. It
// is an interface so the dispatcher stays testable and never reaches into the
// database itself.
type App interface {
	ValidateToolRun(ctx context.Context, principal auth.Principal) error
	ViewTasks(ctx context.Context, principal auth.Principal, requirementID, cardID string) (map[string]any, error)
	StartRequirement(ctx context.Context, principal auth.Principal, in app.StartRequirementInput) (domain.Requirement, error)
	Handoff(ctx context.Context, principal auth.Principal, in app.HandoffInput) (domain.Task, error)
	NotifyFromBot(ctx context.Context, principal auth.Principal, targetBotID, text, taskID, requestID string) (map[string]any, error)
	AskUser(ctx context.Context, principal auth.Principal, in app.AskUserInput) (domain.Question, error)
	RequestMember(ctx context.Context, principal auth.Principal, in app.RequestMemberInput) (domain.Question, error)
	UseBrowser(ctx context.Context, principal auth.Principal, req browser.Request) (browser.Result, error)
	// Write idempotency: a retried side-effect call replays its stored
	// result; the same key with different arguments is a conflict.
	WriteReplay(ctx context.Context, scope, principal, key, argsHash string) (string, bool, error)
}

// Dispatcher validates a bot's identity and routes one tool call onto the
// application layer. Both the user API and the MCP API end up in the same app
// methods, so the permission rules live in exactly one place.
type Dispatcher struct {
	app    App
	tokens *auth.TokenStore
}

// NewDispatcher wires the dispatcher to the application services and the
// in-memory token store.
func NewDispatcher(application App, tokens *auth.TokenStore) *Dispatcher {
	return &Dispatcher{app: application, tokens: tokens}
}

// ResolveToken maps a bridge credential onto a bot principal.
func (d *Dispatcher) ResolveToken(token string) (auth.Principal, auth.BotBinding, error) {
	binding, err := d.tokens.Resolve(token)
	if err != nil {
		return auth.Principal{}, auth.BotBinding{}, err
	}
	principal := auth.NewPrincipalBot(binding.BotID, binding.TeamID)
	principal.RunID = binding.RunID
	return principal, binding, nil
}

// dispatchTimeout bounds every bridge-driven call so a stuck application call
// can never hang the engine's tool loop.
const dispatchTimeout = 30 * time.Second

// ToolCall is one MCP tool invocation arriving from the bridge.
type ToolCall struct {
	Name      string
	Arguments json.RawMessage
	// CallID is the engine's id for this call (bridge transport metadata).
	CallID string
}

// withDerivedKey gives a write without an idempotencyKey one derived from the
// engine's call id and the arguments: a retry of the same call replays, two
// different calls inside one code cell (which share the call id) stay apart,
// and the model never has to invent a key.
func withDerivedKey(call ToolCall) ToolCall {
	if call.CallID == "" || !IsSideEffectTool(call.Name) {
		return call
	}
	args := map[string]any{}
	if len(call.Arguments) > 0 && json.Unmarshal(call.Arguments, &args) != nil {
		return call
	}
	if key, _ := args["idempotencyKey"].(string); key != "" {
		return call
	}
	hash := canonicalArgsHash(call.Arguments)
	args["idempotencyKey"] = "call:" + call.CallID + ":" + hash[:16]
	if len(args["idempotencyKey"].(string)) > 128 {
		args["idempotencyKey"] = "call:" + hash
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return call
	}
	call.Arguments = raw
	return call
}

func decodeArgs(raw json.RawMessage, dst any) error {
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, dst)
}

// toolError maps a product error onto the wire contract's rejected status.
func toolError(err error) *ToolError {
	if err == nil {
		return nil
	}
	var te *ToolError
	if ok := asToolError(err, &te); ok {
		return te
	}
	de := domain.AsError(err)
	status := StatusRejected
	switch de.Code {
	case domain.CodeEngineUnavailable, domain.CodeCapabilityMissing:
		status = StatusUnavailable
	case domain.CodeConflict, domain.CodeRevisionConflict, domain.CodeIdempotencyConflict:
		status = StatusRejected
	}
	return &ToolError{Status: status, Code: string(de.Code), MessageKey: de.MessageKey, Params: de.Params}
}

func asToolError(err error, target **ToolError) bool {
	te, ok := err.(*ToolError)
	if ok {
		*target = te
	}
	return ok
}

// CallTool routes one tool by name with write idempotency: a retried
// side-effect call carrying the same key and the same arguments replays the
// stored result instead of creating a second resource, and the same key with
// different arguments is rejected. Read tools and keyless calls pass through
// to the router, where each write tool still enforces key presence.
func (d *Dispatcher) CallTool(ctx context.Context, principal auth.Principal, call ToolCall) ToolResult {
	call = withDerivedKey(call)
	if IsSideEffectTool(call.Name) {
		if err := d.app.ValidateToolRun(ctx, principal); err != nil {
			return errorResult(err)
		}
	}
	var replayKey, replayHash string
	if IsSideEffectTool(call.Name) && principal.Kind == auth.KindBot && principal.BotID != "" {
		var probe struct {
			RequestID string `json:"idempotencyKey"`
		}
		if err := json.Unmarshal(call.Arguments, &probe); err == nil && probe.RequestID != "" {
			replayKey = probe.RequestID
			replayHash = canonicalArgsHash(call.Arguments)
			cached, hit, rerr := d.app.WriteReplay(ctx, "mcp."+call.Name, principal.BotID, replayKey, replayHash)
			if rerr != nil {
				return errorResult(rerr)
			}
			if hit {
				var stored ToolResult
				if json.Unmarshal([]byte(cached), &stored) == nil {
					return stored
				}
			}
			principal.Write = &auth.WriteRequest{Scope: "mcp." + call.Name, Key: replayKey, ArgsHash: replayHash}
		}
	}
	return d.callToolOnce(ctx, principal, call)
}

// canonicalArgsHash fingerprints arguments with stable key order, so a retry
// that reserializes the same object still matches the stored request.
func canonicalArgsHash(raw json.RawMessage) string {
	var canon any
	if err := json.Unmarshal(raw, &canon); err != nil {
		canon = string(raw)
	}
	stable, _ := json.Marshal(canon)
	sum := sha256.Sum256(stable)
	return hex.EncodeToString(sum[:])
}

// callToolOnce routes one call through the table (pre-idempotency behavior).
func (d *Dispatcher) callToolOnce(ctx context.Context, principal auth.Principal, call ToolCall) ToolResult {
	tool := toolByName[call.Name]
	timeout := dispatchTimeout
	if tool != nil && tool.Timeout > 0 {
		timeout = tool.Timeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if tool == nil {
		return rejected("invalid_request", "error.tool.unknown", map[string]any{"tool": call.Name})
	}
	if principal.SubAgent && !tool.SubAgentOK {
		return rejected("forbidden", "error.tool.subagent_forbidden", map[string]any{"tool": call.Name})
	}
	return tool.Call(ctx, d, principal, call.Arguments)
}

func rejected(code, messageKey string, params map[string]any) ToolResult {
	return ToolResult{Status: StatusRejected, Error: withHint(&ToolError{Status: StatusRejected, Code: code, MessageKey: messageKey, Params: params})}
}

func errorResult(err error) ToolResult {
	te := withHint(toolError(err))
	return ToolResult{Status: te.Status, Error: te}
}
