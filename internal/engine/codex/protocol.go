package codex

import "encoding/json"

// ClientInfo identifies CoBot to the app-server.
type ClientInfo struct {
	Name    string `json:"name"`
	Title   string `json:"title,omitempty"`
	Version string `json:"version"`
}

// InitializeParams starts the protocol handshake.
type InitializeParams struct {
	ClientInfo ClientInfo `json:"clientInfo"`
}

// ThreadStartParams mirrors the app-server thread/start request.
type ThreadStartParams struct {
	Cwd                   string         `json:"cwd,omitempty"`
	Sandbox               string         `json:"sandbox,omitempty"`
	ApprovalPolicy        string         `json:"approvalPolicy,omitempty"`
	ApprovalsReviewer     string         `json:"approvalsReviewer,omitempty"`
	Model                 string         `json:"model,omitempty"`
	DeveloperInstructions string         `json:"developerInstructions,omitempty"`
	Config                map[string]any `json:"config,omitempty"`
}

// ThreadResumeParams mirrors thread/resume.
type ThreadResumeParams struct {
	Model                 string         `json:"model,omitempty"`
	DeveloperInstructions string         `json:"developerInstructions,omitempty"`
	ThreadID              string         `json:"threadId"`
	Cwd                   string         `json:"cwd,omitempty"`
	Sandbox               string         `json:"sandbox,omitempty"`
	ApprovalPolicy        string         `json:"approvalPolicy,omitempty"`
	ApprovalsReviewer     string         `json:"approvalsReviewer,omitempty"`
	Config                map[string]any `json:"config,omitempty"`
	// ExcludeTurns leaves the thread's history out of the reply: CoBot keeps
	// its own record, and full-history replies are deprecated.
	ExcludeTurns bool `json:"excludeTurns,omitempty"`
}

// ThreadRef is the provider thread identity.
type ThreadRef struct {
	ID string `json:"id"`
}

// ThreadStartResult is the thread/start response (experiment-verified
// fields; unknown extras are preserved by the raw result on the wire).
type ThreadStartResult struct {
	Thread        ThreadRef `json:"thread"`
	Cwd           string    `json:"cwd"`
	Model         string    `json:"model"`
	ModelProvider string    `json:"modelProvider"`
}

// UserInput is one input item of a turn.
type UserInput struct {
	Type string `json:"type"` // text
	Text string `json:"text"`
}

// TurnStartParams mirrors turn/start.
type TurnStartParams struct {
	Effort         string         `json:"effort,omitempty"`
	ThreadID       string         `json:"threadId"`
	Input          []UserInput    `json:"input"`
	Model          string         `json:"model,omitempty"`
	ApprovalPolicy string         `json:"approvalPolicy,omitempty"`
	SandboxPolicy  map[string]any `json:"sandboxPolicy,omitempty"`
}

// TurnRef identifies a turn and its lifecycle status.
type TurnRef struct {
	ID     string          `json:"id"`
	Status string          `json:"status"` // completed | interrupted | failed | inProgress
	Error  json.RawMessage `json:"error,omitempty"`
}

// TurnStartResult is the turn/start response.
type TurnStartResult struct {
	Turn TurnRef `json:"turn"`
}

// TurnCompletedParams is the turn/completed notification payload.
type TurnCompletedParams struct {
	ThreadID string  `json:"threadId"`
	Turn     TurnRef `json:"turn"`
}

// ItemCompletedParams is the item/completed notification payload.
type ItemCompletedParams struct {
	CompletedAtMs int64           `json:"completedAtMs"`
	Item          json.RawMessage `json:"item"`
	ThreadID      string          `json:"threadId"`
	TurnID        string          `json:"turnId"`
}

// ThreadItem is a provider thread item; only the fields CoBot persists are
// typed, the rest stays available through Raw.
type ThreadItem struct {
	Type string `json:"type"` // agentMessage | userMessage | commandExecution | fileChange | reasoning | webSearch | todoList | planUpdate | ...
	ID   string `json:"id"`
	Text string `json:"text,omitempty"`
	// Phase of an agentMessage: commentary | final_answer (null on older engines).
	Phase string `json:"phase,omitempty"`
}

// AgentMessageParams decodes an item whose type is agentMessage.
type AgentMessageParams struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Text string `json:"text"`
}

// ItemStartedParams is the item/started notification payload.
type ItemStartedParams struct {
	Item        json.RawMessage `json:"item"`
	ThreadID    string          `json:"threadId"`
	TurnID      string          `json:"turnId"`
	StartedAtMs int64           `json:"startedAtMs"`
}

// ItemAgentMessageDeltaParams is the streaming delta payload.
type ItemAgentMessageDeltaParams struct {
	Delta    string `json:"delta"`
	ItemID   string `json:"itemId,omitempty"`
	ThreadID string `json:"threadId,omitempty"`
	TurnID   string `json:"turnId,omitempty"`
}

// TokenUsageBreakdown matches the engine's cumulative usage semantics.
type TokenUsageBreakdown struct {
	CachedInputTokens     int64 `json:"cachedInputTokens"`
	InputTokens           int64 `json:"inputTokens"`
	OutputTokens          int64 `json:"outputTokens"`
	ReasoningOutputTokens int64 `json:"reasoningOutputTokens"`
	TotalTokens           int64 `json:"totalTokens"`
	CacheWriteInputTokens int64 `json:"cacheWriteInputTokens,omitempty"`
}

// ThreadTokenUsage carries last and cumulative usage of one turn.
type ThreadTokenUsage struct {
	Last               TokenUsageBreakdown `json:"last"`
	Total              TokenUsageBreakdown `json:"total"`
	ModelContextWindow *int64              `json:"modelContextWindow,omitempty"`
}

// TokenUsageParams is the thread/tokenUsage/updated payload.
type TokenUsageParams struct {
	ThreadID   string           `json:"threadId"`
	TurnID     string           `json:"turnId"`
	TokenUsage ThreadTokenUsage `json:"tokenUsage"`
}

// AccountUpdatedParams is a best-effort shape of account/* notifications;
// CoBot only persists the non-secret discriminators.
type AccountUpdatedParams struct {
	Account json.RawMessage `json:"account,omitempty"`
}

// Approval decision constants used by command/file approvals.
const (
	DecisionAccept  = "accept"
	DecisionDecline = "decline"
)
