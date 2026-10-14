// Package engine defines the narrow execution interface CoBot needs from a
// complete external agent (first version: Codex app-server). It is shaped by
// the first real adapter; recovery/model/permission extras stay optional. CoBot
// never re-implements the model/tool loop, shell or Git.
package engine

import (
	"context"
	"encoding/json"
)

// ThreadSpec describes one bot conversation thread.
type ThreadSpec struct {
	Cwd                   string         // must be the team directory
	Sandbox               string         // read-only | workspace-write
	ApprovalPolicy        string         // untrusted | on-request | never
	ApprovalsReviewer     string         // user
	Model                 string         // optional override
	DeveloperInstructions string         // role + collaboration instructions
	Config                map[string]any // thread-scoped config overrides
}

// ThreadInfo is the provider-side identity of a started/resumed thread.
type ThreadInfo struct {
	ThreadID      string
	Cwd           string
	Model         string
	ModelProvider string
}

// Server is one team's app-server instance. It is not safe for arbitrary
// concurrent Close; calls themselves are safe to run in parallel.
type Server interface {
	StartThread(ctx context.Context, spec ThreadSpec) (ThreadInfo, error)
	ResumeThread(ctx context.Context, threadID string) error
	// StartTurn dispatches one user turn and returns the provider turn id;
	// completion arrives as notifications, not as this call's result.
	StartTurn(ctx context.Context, threadID, text string, model string) (turnID string, err error)
	InterruptTurn(ctx context.Context, threadID string) error
	// ReplyApproval answers a pending native approval request with the exact
	// response shape the engine expects (decision / permissions).
	ReplyApproval(ctx context.Context, requestID json.RawMessage, response map[string]any) error
	AccountRead(ctx context.Context) (json.RawMessage, error)
	AccountLoginStart(ctx context.Context) error
	AccountLoginCancel(ctx context.Context) error
	ModelList(ctx context.Context) (json.RawMessage, error)
	Shutdown(ctx context.Context) error
	Epoch() string
}

// ApprovalRequest is a normalized native tool approval arriving from the
// engine. The original request id and params are preserved verbatim so the
// reply can be routed exactly.
type ApprovalRequest struct {
	RequestID json.RawMessage
	Method    string // item/commandExecution/requestApproval, ...
	ThreadID  string
	TurnID    string
	ItemID    string
	Command   string
	Cwd       string
	Reason    string
	Paths     []string // file changes involved
	RawParams json.RawMessage
}

// Notification is a normalized engine event for the app layer.
type Notification struct {
	Method     string
	ThreadID   string
	TurnID     string
	ItemID     string
	ItemType   string
	Text       string
	TurnStatus string // completed | interrupted | failed | inProgress
	TokenUsage json.RawMessage
	Raw        json.RawMessage
}

// Callbacks is the handler set an app-server delivers events through.
type Callbacks struct {
	OnApproval     func(ApprovalRequest)
	OnNotification func(Notification)
	OnExit         func(err error)
}

// Status is the non-secret engine projection surfaced by GET /engine.
type Status struct {
	BinaryPath   string
	Version      string
	Protocol     string
	LoggedIn     bool
	AuthMode     string // chatgpt | apiKey | ...
	Capabilities []string
}
