package domain

import "time"

// Usage row status. A turn that ended without ever reporting tokens is an
// unknown, not a zero: a budget may rely on it, so the product must say it
// cannot know (G08).
const (
	UsageStatusRecorded = "recorded"
	UsageStatusUnknown  = "unknown"
)

// Usage purpose separates business turns from system sessions such as risk
// evaluations. Both count toward their team (G06); only business turns count
// toward a per-bot budget.
const (
	UsagePurposeRun      = "run"
	UsagePurposeRiskEval = "risk_eval"
)

// UsageEntry is one turn's token snapshot in the ledger.
type UsageEntry struct {
	ID                string    `json:"id"`
	TeamID            string    `json:"teamId"`
	BotID             string    `json:"botId,omitempty"`
	Purpose           string    `json:"purpose"`
	EngineEpoch       string    `json:"engineEpoch"`
	ThreadID          string    `json:"threadId"`
	TurnID            string    `json:"turnId"`
	InputTokens       int64     `json:"inputTokens"`
	OutputTokens      int64     `json:"outputTokens"`
	CachedInputTokens int64     `json:"cachedInputTokens"`
	Status            string    `json:"status"`
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

// BillableTokens is what the budget measures: input plus output. The cached
// input portion is already inside the input figure and is never added a
// second time (G06).
func (u UsageEntry) BillableTokens() int64 {
	return u.InputTokens + u.OutputTokens
}

// EventUsageUpdated is published when the ledger records or changes an entry.
const EventUsageUpdated = "usage.updated"

// EventBudgetPaused is published when a budget or unknown usage stops new
// turns for a bot.
const EventBudgetPaused = "budget.paused"
