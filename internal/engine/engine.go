// Package engine defines the narrow execution interface CoBot needs from a
// complete external agent (first version: Codex app-server). It is shaped by
// the first real adapter; recovery/model/permission extras stay optional. CoBot
// never re-implements the model/tool loop, shell or Git.
package engine

import (
	"context"
	"encoding/json"
	"errors"
)

// ErrRejected marks a request the engine answered with an error: it was
// received and refused, so nothing it asked for started. Any other failure
// (a timeout, a lost connection) leaves the outcome unknown.
var ErrRejected = errors.New("engine rejected the request")

// ThreadSpec describes one bot conversation thread.
type ThreadSpec struct {
	Cwd string // must be the team directory
	// Permission is the team's mode (read_only | default | full). When set,
	// the adapter derives the engine's sandbox and approval settings from it
	// and Sandbox/ApprovalPolicy are ignored.
	Permission        string
	Sandbox           string // read-only | workspace-write
	ApprovalPolicy    string // untrusted | on-request | never
	ApprovalsReviewer string // user
	Model             string // optional override
	// ModelProvider names the provider this thread runs with. Empty leaves
	// the engine on its own account. The engine refuses an id it does not
	// know, so an id is only sent for a provider that is configured.
	ModelProvider         string         // optional provider id
	DeveloperInstructions string         // role + collaboration instructions
	Config                map[string]any // thread-scoped config overrides
}

// TurnOptions are per-turn overrides. Permission (read_only | default |
// full) applies from this turn on, so a mode change never needs a new thread.
type TurnOptions struct {
	// Effort is the reasoning effort for this turn; empty is the model default.
	Effort     string
	Model      string
	Permission string
}

// ThreadInfo is the provider-side identity of a started/resumed thread.
type ThreadInfo struct {
	ThreadID      string
	Cwd           string
	Model         string
	ModelProvider string
	// Path is the file the engine keeps the thread's full record in, when
	// it says.
	Path string
}

// Server is one team's app-server instance. It is not safe for arbitrary
// concurrent Close; calls themselves are safe to run in parallel.
type Server interface {
	StartThread(ctx context.Context, spec ThreadSpec) (ThreadInfo, error)
	// ResumeThread reattaches a thread and reports the model and the
	// provider it runs with, as the engine answered it.
	ResumeThread(ctx context.Context, threadID string, spec ThreadSpec) (ThreadInfo, error)
	// StartTurn dispatches one user turn and returns the provider turn id;
	// completion arrives as notifications, not as this call's result.
	StartTurn(ctx context.Context, threadID, text string, opts TurnOptions) (turnID string, err error)
	InterruptTurn(ctx context.Context, threadID, turnID string) error
	// SteerTurn adds input to the thread's running turn, which must still be
	// turnID; it fails once that turn has ended.
	SteerTurn(ctx context.Context, threadID, turnID, text string) error
	// ReplyApproval answers a pending native approval request with the exact
	// response shape the engine expects (decision / permissions).
	ReplyApproval(ctx context.Context, requestID json.RawMessage, response map[string]any) error
	AccountRead(ctx context.Context) (json.RawMessage, error)
	AccountLoginStart(ctx context.Context) (LoginInfo, error)
	AccountLoginCancel(ctx context.Context, loginID string) error
	ModelList(ctx context.Context) (json.RawMessage, error)
	Shutdown(ctx context.Context) error
	Epoch() string
}

// TurnInspector reports only the exact persisted provider turn. Resuming a
// thread or observing a missing process is not evidence that a turn ended.
type TurnInspector interface {
	InspectTurn(ctx context.Context, threadID, turnID string) (string, error)
}

// LoginInfo is the pending official browser authorization, never a credential.
type LoginInfo struct {
	Type    string `json:"type"`
	LoginID string `json:"loginId"`
	AuthURL string `json:"authUrl"`
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
	Changes   []FileChange
	// Permissions is the extra access a permissions request asks for, in
	// the engine's own shape; granting echoes it back.
	Permissions json.RawMessage
	// Network marks a command that needs network access.
	Network   bool
	RawParams json.RawMessage
}

type FileChange struct {
	Path string          `json:"path"`
	Kind json.RawMessage `json:"kind,omitempty"`
	Diff string          `json:"diff"`
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

	// Step is the engine-neutral form of a started/completed turn step the
	// user should see (thinking, a command, a file change, a tool call...).
	// Nil for steps that are not shown or are the reply itself.
	Step *Step
	// DeltaKind/Delta carry streamed text: message (the turn's answer) |
	// commentary (what the model says while it works) | reasoning | output.
	DeltaKind string
	Delta     string
	// Phase of an agent message: "commentary" for what the model says while
	// it works, "final_answer" for the turn's answer; empty when the engine
	// does not say, which counts as the answer.
	Phase string
	// Problem is a turn-scoped error or warning reported by the engine.
	Problem *Problem
	// RequestID names the server request a serverRequest/resolved settled.
	RequestID json.RawMessage
}

// Step kinds shown in the conversation's process view.
const (
	StepReasoning  = "reasoning"
	StepCommand    = "command"
	StepFileChange = "file_change"
	StepTool       = "tool"
	StepWebSearch  = "web_search"
	StepPlan       = "plan"
	// StepMessage is what the model says while it works (Codex commentary);
	// it sits among the steps, not in the reply.
	StepMessage = "message"
	// StepAgent is a member working with its own Codex sub-agents: spawning,
	// instructing, waiting for or closing one, and their activity.
	StepAgent = "agent"
	// StepDiff is the turn's code changes so far, kept as one step that
	// always holds the newest diff.
	StepDiff = "diff"
	// StepCompaction is the engine folding the conversation so far into a
	// summary because it neared the model's context window.
	StepCompaction = "compaction"
)

// Step statuses.
const (
	StepInProgress = "in_progress"
	StepCompleted  = "completed"
	StepFailed     = "failed"
	StepDeclined   = "declined"
	// StepInterrupted settles a step its turn ended without completing.
	StepInterrupted = "interrupted"
)

// Step is one visible step of a turn. Detail is kind-specific JSON:
//
//	reasoning:   {text}
//	command:     {command, cwd, exitCode?, durationMs?, output}
//	file_change: {changes: [{path, kind: add|delete|update, movePath?, diff}]}
//	tool:        {server, tool, arguments, result?, error?, durationMs?}
//	web_search:  {query}
//	plan:        {text}
//	message:     {text}  (what the model says while working)
//	agent:       {tool, agents, prompt?} | {activity, agent}  (Codex sub-agents)
//	diff:        {diff, files}  (the turn's changes so far)
//	compaction:  {}
type Step struct {
	ID     string
	Kind   string
	Status string
	Detail json.RawMessage
}

// Problem is an engine error or warning inside a turn. WillRetry marks a
// transient failure the engine is already retrying (e.g. reconnecting).
//
// Code is the engine's own name for the failure (serverOverloaded,
// httpConnectionFailed...). FailureCode is the root cause behind it, taken
// from the vendor's HTTP status: an empty balance and a rejected key both
// arrive as "the request failed", and only the status tells them apart.
// It is empty when the engine named a failure without saying why.
type Problem struct {
	Level       string // error | warning
	Message     string
	Code        string
	FailureCode string
	WillRetry   bool
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
