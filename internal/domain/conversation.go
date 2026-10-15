package domain

import (
	"encoding/json"
	"time"
)

// Message kinds; user text is preserved verbatim and never rewritten.
const (
	MessageKindUser          = "user"
	MessageKindAssistant     = "assistant"
	MessageKindCollaboration = "collaboration"
	MessageKindSystem        = "system"
)

// Delivery statuses. accepted only means the engine accepted the turn, not
// that the task is done.
const (
	DeliveryWaitingDependency = "waiting_dependency"
	DeliveryQueued            = "queued"
	DeliveryDispatching       = "dispatching"
	DeliveryAccepted          = "accepted"
	DeliveryCancelled         = "cancelled"
	DeliveryUnknown           = "unknown"
)

// Message is one persisted chat entry of a bot conversation.
type Message struct {
	ID             string          `json:"id"`
	TeamID         string          `json:"teamId"`
	BotID          string          `json:"botId"`
	Kind           string          `json:"kind"`
	Blocks         json.RawMessage `json:"blocks"`
	RequestID      string          `json:"requestId,omitempty"`
	DeliveryStatus string          `json:"deliveryStatus,omitempty"`
	RunID          string          `json:"runId,omitempty"`
	SourceBotID    string          `json:"sourceBotId,omitempty"`
	CreatedAt      time.Time       `json:"createdAt"`
}

// TextBlocks is the helper shape for the first version's text-only blocks.
type TextBlocks struct {
	Blocks []TextBlock `json:"blocks"`
}

// TextBlock is one markdown-capable text block (raw HTML is never rendered).
type TextBlock struct {
	Type string `json:"type"` // text
	Text string `json:"text"`
}

// RenderText joins blocks for engine input (user content passed verbatim).
func (m Message) RenderText() string {
	var blocks []TextBlock
	if err := json.Unmarshal(m.Blocks, &blocks); err != nil {
		return ""
	}
	out := ""
	for i, b := range blocks {
		if i > 0 {
			out += "\n"
		}
		out += b.Text
	}
	return out
}

// Run statuses follow the engine lifecycle; unknown is never auto-retried.
const (
	RunStarting        = "starting"
	RunRunning         = "running"
	RunWaitingApproval = "waiting_approval"
	RunWaitingInput    = "waiting_input"
	RunCompleted       = "completed"
	RunFailed          = "failed"
	RunInterrupted     = "interrupted"
	RunUnknown         = "unknown"
)

// Run is one dispatched turn.
type Run struct {
	ID                string    `json:"id"`
	TeamID            string    `json:"teamId"`
	BotID             string    `json:"botId"`
	ProviderSessionID string    `json:"providerSessionId,omitempty"`
	ProviderTurnID    string    `json:"providerTurnId,omitempty"`
	EngineEpoch       string    `json:"engineEpoch,omitempty"`
	Status            string    `json:"status"`
	ErrorKey          string    `json:"errorKey,omitempty"`
	StartedAt         time.Time `json:"startedAt"`
	EndedAt           time.Time `json:"endedAt,omitempty"`
}

// Approval types.
const (
	ApprovalNativeTool    = "native_tool"
	ApprovalCollaboration = "collaboration"
	ApprovalRecruitment   = "recruitment"
	ApprovalRecovery      = "recovery"
	ApprovalMerge         = "merge"
)

// Recovery option ids on the unresolved-execution card. This build cannot
// reattach to a previous process's execution — a live PID or a resumable
// thread is never a takeover (H04) — so the card offers exactly the
// capabilities that exist: continue, rerun, abandon (H05).
const (
	RecoveryOptionContinue = "continue"
	RecoveryOptionRerun    = "rerun"
	RecoveryOptionAbandon  = "abandon"
)

// Approval request states; decided history survives later invalidation.
const (
	ApprovalStatePending     = "pending"
	ApprovalStateDecided     = "decided"
	ApprovalStateInvalidated = "invalidated"
)

// Approval response delivery states; a decision is not a confirmed reply.
const (
	ResponseQueued    = "queued"
	ResponseSent      = "sent"
	ResponseConfirmed = "confirmed"
	ResponseUnknown   = "unknown"
	ResponseExpired   = "expired"
)

// ApprovalOption is one immutable choice snapshot.
type ApprovalOption struct {
	ID                string `json:"id"`
	MessageKey        string `json:"messageKey,omitempty"`
	Text              string `json:"text,omitempty"`
	Disabled          bool   `json:"disabled,omitempty"`
	DisabledReasonKey string `json:"disabledReasonKey,omitempty"`
}

// Approval is one user decision request.
type Approval struct {
	ID              string                `json:"id"`
	TeamID          string                `json:"teamId"`
	RequesterBotID  string                `json:"requesterBotId"`
	RunID           string                `json:"runId,omitempty"`
	EngineRequestID json.RawMessage       `json:"-"`
	EngineEpoch     string                `json:"-"`
	Type            string                `json:"type"`
	Method          string                `json:"method,omitempty"`
	Summary         string                `json:"summary"`
	Reason          string                `json:"reason,omitempty"`
	Options         []ApprovalOption      `json:"options"`
	Details         *MergeApprovalDetails `json:"details,omitempty"`
	RequestState    string                `json:"requestState"`
	Decision        *ApprovalDecision     `json:"decision,omitempty"`
	ResponseState   string                `json:"responseState,omitempty"`
	CreatedAt       time.Time             `json:"createdAt"`
	Revision        int64                 `json:"revision"`
}

// MergeApprovalDetails is a safe, immutable snapshot of the facts the user
// is authorizing. Reports remain attributed Agent evidence, not host tests.
type MergeApprovalDetails struct {
	TaskID             string     `json:"taskId"`
	TaskRevision       int64      `json:"taskRevision"`
	CandidateRef       string     `json:"candidateRef"`
	TargetRef          string     `json:"targetRef"`
	ExecutionDirectory string     `json:"executionDirectory"`
	EvidenceIDs        []string   `json:"evidenceIds"`
	Evidence           []Evidence `json:"evidence"`
}

// ApprovalDecision is the user's recorded choice.
type ApprovalDecision struct {
	OptionID  string    `json:"optionId"`
	Comment   string    `json:"comment,omitempty"`
	Actor     string    `json:"actor"`
	DecidedAt time.Time `json:"decidedAt"`
}

// IsPending reports whether the card can still be decided.
func (a Approval) IsPending() bool { return a.RequestState == ApprovalStatePending }

// Engine approval decision values preserved from the native protocol.
const (
	DecisionAcceptEngine  = "accept"
	DecisionDeclineEngine = "decline"
)
