package domain

import "time"

// Chain lifecycle. A chain is the causal unit a user started: bot-caused
// wakes inside it share one budget, and stopping the chain cancels only this
// chain's undelivered work.
const (
	ChainActive           = "active"
	ChainAwaitingApproval = "awaiting_approval"
	ChainStopped          = "stopped"
	ChainEvaluating       = "evaluating"
)

// Chain is one causal chain with its shared wake budget.
type Chain struct {
	ID              string    `json:"id"`
	TeamID          string    `json:"teamId"`
	OriginMessageID string    `json:"originMessageId"`
	OriginBotID     string    `json:"originBotId,omitempty"`
	Epoch           int64     `json:"epoch"`
	Status          string    `json:"status"`
	WakeCount       int64     `json:"wakeCount"`
	LimitCount      int64     `json:"limitCount"`
	RiskStart       int64     `json:"riskStart"`
	StopReason      string    `json:"stopReason,omitempty"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

// RiskZoneStart computes the first wake index that needs evaluation. The
// danger zone is the last ceil(limit*0.2) wakes: for the default 6 that is
// wakes 5 and 6, evaluated before they start, and a 7th never starts.
func RiskZoneStart(limit int64) int64 {
	if limit <= 0 {
		return 1
	}
	risk := (limit + 4) / 5 // ceil(limit*0.2)
	start := limit - risk + 1
	if start < 1 {
		start = 1
	}
	return start
}

// NeedsEvaluation reports whether the next wake index falls in the risk zone
// or beyond the hard limit. Neither can be overridden by a model.
func (c Chain) NeedsEvaluation(nextWake int64) bool {
	return nextWake >= c.RiskStart
}

// OverLimit reports whether the next wake exceeds what the user allowed.
func (c Chain) OverLimit(nextWake int64) bool {
	return nextWake > c.LimitCount
}

// Chain option ids on the confirmation card; the user either lets exactly
// this chain continue or stops exactly this chain.
const (
	ChainOptionContinue = "continue"
	ChainOptionStop     = "stop"
)

// Chain approval type; its decision has a product side effect (grant one more
// wake, or stop the chain) and never talks to an engine.
const ApprovalChain = "chain"

// EventChainUpdated is published whenever a chain's state or count changes.
const EventChainUpdated = "chain.updated"
