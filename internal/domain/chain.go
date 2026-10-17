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
	ID              string `json:"id"`
	TeamID          string `json:"teamId"`
	OriginMessageID string `json:"originMessageId"`
	OriginBotID     string `json:"originBotId,omitempty"`
	Epoch           int64  `json:"epoch"`
	Status          string `json:"status"`
	WakeCount       int64  `json:"wakeCount"`
	LimitCount      int64  `json:"limitCount"`
	// RiskStart is kept for stored rows; model risk evaluation is gone.
	RiskStart  int64     `json:"riskStart"`
	StopReason string    `json:"stopReason,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
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
