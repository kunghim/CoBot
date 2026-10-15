package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// Chains implement G01/G02/G03's accounting half: every bot-caused wake
// (a bot-to-bot message or a hand-over) spends from one shared budget per
// causal chain. A user message opens a new chain and never clears a running
// one. The hard limit and the risk zone cannot be overridden by a model; past
// them, exactly one more wake starts only after the user approves it.

// chainApprovalBinding stores which delivery asked for a decision inside the
// approval's engine request id slot (the same trick recruitment uses for a
// role id).
type chainApprovalBinding struct {
	ChainID    string `json:"chainId"`
	DeliveryID string `json:"deliveryId"`
	BotID      string `json:"botId"`
	OverLimit  bool   `json:"overLimit"`
	ChainEpoch int64  `json:"chainEpoch,omitempty"`
}

// openChainLocked starts a new causal chain with the current settings
// snapshot. The limit is fixed here so later settings edits never silently
// resize a budget that work is already running against.
func (a *App) openChainLocked(ctx context.Context, tx *sql.Tx, teamID, originMessageID, originBotID string) (domain.Chain, error) {
	settings, err := sqlite.GetSettings(ctx, tx)
	if err != nil {
		return domain.Chain{}, err
	}
	limit := int64(settings.ChainLimit)
	chain := domain.Chain{
		ID: domain.NewID(), TeamID: teamID, OriginMessageID: originMessageID, OriginBotID: originBotID,
		Epoch: 1, Status: domain.ChainActive, LimitCount: limit, RiskStart: domain.RiskZoneStart(limit),
		CreatedAt: time.Now().UTC(),
	}
	if err := sqlite.InsertChain(ctx, tx, chain); err != nil {
		return domain.Chain{}, err
	}
	return chain, nil
}

// ChainGateResult is what the dispatcher must do with a claimed delivery.
type ChainGateResult int

const (
	// ChainGateAllowed: dispatch may proceed (no chain, or the wake was
	// counted in this transaction).
	ChainGateAllowed ChainGateResult = iota
	// ChainGateSkip: the delivery stays queued; a user decision or a later
	// wake will move it.
	ChainGateSkip
	// ChainGateDrop: the stopped chain's work must be swept away.
	ChainGateDrop
)

// ChainGateAction is persistence the caller must do AFTER rolling the claim
// transaction back. The gate's skip/drop paths never commit the claim, so
// anything that must survive (the confirmation card, the cancellation sweep)
// has to be written in its own transaction — otherwise it would be created
// and immediately undone, and the gate would ask again forever.
type ChainGateAction int

const (
	// ChainActionNone: nothing extra to persist.
	ChainActionNone ChainGateAction = iota
	// ChainActionRaiseCard: write the chain confirmation card and set the
	// chain to awaiting_approval.
	ChainActionRaiseCard
	// ChainActionSweepStopped: cancel the stopped chain's undelivered work.
	ChainActionSweepStopped
	// ChainActionRunRiskEval: judge the wake in a separate read-only
	// session; nothing is persisted by the gate itself (G03).
	ChainActionRunRiskEval
)

// EvaluateChainGate applies the chain budget inside the caller's claim
// transaction: counting and dispatch eligibility are decided together, so a
// crash between them can never lose or double-count a wake (G01). Events are
// returned instead of written: the pool is a single connection, and writing
// an event from inside an open transaction would deadlock. The caller emits
// them after committing.
func (a *App) EvaluateChainGate(ctx context.Context, tx *sql.Tx, delivery sqlite.DeliveryRecord) (ChainGateResult, ChainGateAction, []domain.Event, error) {
	if delivery.ChainID == "" {
		return ChainGateAllowed, ChainActionNone, nil, nil // user work: no bot wake to count
	}
	chain, err := sqlite.GetChain(ctx, tx, delivery.ChainID)
	if err != nil {
		// A delivery pointing at a missing chain must not dispatch uncounted.
		return ChainGateSkip, ChainActionNone, nil, err
	}
	switch chain.Status {
	case domain.ChainStopped:
		// Stopping already cancelled the chain's undelivered work; sweep
		// defensively in case this delivery slipped past that decision. The
		// write happens after the claim rolls back, so it survives.
		return ChainGateDrop, ChainActionSweepStopped, nil, nil
	case domain.ChainAwaitingApproval:
		return ChainGateSkip, ChainActionNone, nil, nil // the pending card decides
	case domain.ChainEvaluating:
		return ChainGateSkip, ChainActionNone, nil, nil // a risk eval is in flight (G03)
	}
	if delivery.MessageID == chain.OriginMessageID {
		// The chain's root is user-caused input; only bot-caused wakes spend
		// the budget (G01).
		return ChainGateAllowed, ChainActionNone, nil, nil
	}
	key := delivery.DispatchKey
	if key == "" {
		key = delivery.ID
	}
	// A delivery whose wake is already counted was authorized earlier — by
	// being inside the budget or by the user's continue — so it dispatches
	// without spending anything more, even if the chain has since moved on.
	counted, err := sqlite.ChainWakeCounted(ctx, tx, chain.ID, key)
	if err != nil {
		return ChainGateSkip, ChainActionNone, nil, err
	}
	if counted {
		return ChainGateAllowed, ChainActionNone, nil, nil
	}
	next := chain.WakeCount + 1
	if chain.OverLimit(next) {
		// The hard limit is never shown to a model (G03): past it only the
		// user can grant one more wake. The card is written by the caller
		// after the claim rolls back, so the ask survives.
		exists, err := pendingChainDecisionExists(ctx, tx, chain.TeamID, chain.ID)
		if err != nil {
			return ChainGateSkip, ChainActionNone, nil, err
		}
		if exists {
			return ChainGateSkip, ChainActionNone, nil, nil
		}
		return ChainGateSkip, ChainActionRaiseCard, nil, nil
	}
	if chain.NeedsEvaluation(next) {
		// Inside the risk zone the model judges in its read-only session;
		// ask/unknown/failure falls back to the same user card (G03).
		return ChainGateSkip, ChainActionRunRiskEval, nil, nil
	}
	if _, err := sqlite.CountChainWake(ctx, tx, chain.ID, key, delivery.ID, delivery.TargetBotID); err != nil {
		return ChainGateSkip, ChainActionNone, nil, err
	}
	if err := sqlite.IncrementChainWake(ctx, tx, chain.ID); err != nil {
		return ChainGateSkip, ChainActionNone, nil, err
	}
	return ChainGateAllowed, ChainActionNone, []domain.Event{{Type: domain.EventChainUpdated, TeamID: chain.TeamID, BotID: delivery.TargetBotID, ResourceID: chain.ID}}, nil
}

// PersistChainAction writes the post-rollback work inside the caller's fresh
// transaction: the confirmation card, or the stopped-chain sweep.
func (a *App) PersistChainAction(ctx context.Context, tx *sql.Tx, action ChainGateAction, delivery sqlite.DeliveryRecord) ([]domain.Event, error) {
	switch action {
	case ChainActionNone:
		return nil, nil
	case ChainActionSweepStopped:
		if _, err := sqlite.CancelChainDeliveries(ctx, tx, delivery.ChainID); err != nil {
			return nil, err
		}
		return nil, nil
	case ChainActionRaiseCard:
		chain, err := sqlite.GetChain(ctx, tx, delivery.ChainID)
		if err != nil {
			return nil, sqlite.NormalizeError(err)
		}
		return a.requestChainDecision(ctx, tx, chain, delivery, chain.OverLimit(chain.WakeCount+1))
	default:
		return nil, nil
	}
}

// requestChainDecision raises exactly one pending confirmation card per
// chain. The summary uses the product's own wording; the reason keeps the
// true internal trigger so the record never invents a security risk.
// pendingChainDecisionExists reports whether the chain already has an open
// confirmation card, so repeated wake attempts never stack duplicates.
func pendingChainDecisionExists(ctx context.Context, q sqlite.Queryer, teamID, chainID string) (bool, error) {
	var count int
	err := q.QueryRowContext(ctx, `SELECT count(*) FROM approvals WHERE team_id = ? AND type = ? AND request_state = ? AND json_valid(engine_request_id) AND json_extract(engine_request_id,'$.chainId') = ?`, teamID, domain.ApprovalChain, domain.ApprovalStatePending, chainID).Scan(&count)
	return count > 0, err
}

func (a *App) requestChainDecision(ctx context.Context, tx *sql.Tx, chain domain.Chain, delivery sqlite.DeliveryRecord, overLimit bool) ([]domain.Event, error) {
	exists, err := pendingChainDecisionExists(ctx, tx, chain.TeamID, chain.ID)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, nil // this chain already waits on a card
	}
	summary := "approval.card.chain_risk"
	reason := "risk_zone"
	if overLimit {
		summary = "approval.card.chain_over_limit"
		reason = "over_limit"
	}
	binding, err := json.Marshal(chainApprovalBinding{
		ChainID: chain.ID, DeliveryID: delivery.ID, BotID: delivery.TargetBotID, OverLimit: overLimit, ChainEpoch: chain.Epoch,
	})
	if err != nil {
		return nil, err
	}
	approval := domain.Approval{
		ID: domain.NewID(), TeamID: chain.TeamID, RequesterBotID: delivery.TargetBotID,
		EngineRequestID: binding, Type: domain.ApprovalChain,
		Summary: summary, Reason: reason,
		Options: []domain.ApprovalOption{
			{ID: domain.ChainOptionContinue, MessageKey: "approval.option.chain_continue", Text: "继续推进"},
			{ID: domain.ChainOptionStop, MessageKey: "approval.option.chain_stop", Text: "停止该链"},
		},
		RequestState: domain.ApprovalStatePending, CreatedAt: time.Now().UTC(),
	}
	if err := sqlite.InsertApproval(ctx, tx, approval); err != nil {
		return nil, err
	}
	if err := sqlite.SetChainStatus(ctx, tx, chain.ID, domain.ChainAwaitingApproval, ""); err != nil {
		return nil, err
	}
	return []domain.Event{{Type: domain.EventApprovalCreated, TeamID: chain.TeamID, BotID: delivery.TargetBotID, ResourceID: approval.ID}}, nil
}

// ApplyChainDecision executes the user's continue/stop choice for a chain
// card. Continue grants exactly one more wake when the chain was over limit,
// counts the bound delivery's wake and returns the chain to active; stop
// cancels the chain's undelivered work and interrupts its executions.
func (a *App) ApplyChainDecision(ctx context.Context, approval domain.Approval, optionID string) error {
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	effect, err := a.applyChainDecisionLocked(ctx, tx, approval, optionID)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	a.finishChainDecision(ctx, effect)
	return nil
}

type chainDecisionEffect struct {
	chain     domain.Chain
	wakeBotID string
	accepted  []sqlite.DeliveryRecord
}

func (a *App) applyChainDecisionLocked(ctx context.Context, tx *sql.Tx, approval domain.Approval, optionID string) (effect chainDecisionEffect, err error) {
	var binding chainApprovalBinding
	if json.Unmarshal(approval.EngineRequestID, &binding) != nil || binding.ChainID == "" {
		return effect, domain.InvalidRequest("error.chain.binding_missing", nil)
	}
	chain, err := sqlite.GetChain(ctx, tx, binding.ChainID)
	if err != nil {
		return effect, sqlite.NormalizeError(err)
	}
	effect.chain = chain
	if chain.TeamID != approval.TeamID || chain.Status != domain.ChainAwaitingApproval || (binding.ChainEpoch != 0 && binding.ChainEpoch != chain.Epoch) {
		return effect, domain.Conflict("error.chain.decision_stale", nil)
	}
	delivery, err := sqlite.GetDelivery(ctx, tx, binding.DeliveryID)
	if err != nil {
		return effect, sqlite.NormalizeError(err)
	}
	if delivery.ChainID != chain.ID || delivery.TargetBotID != binding.BotID || delivery.Status != domain.DeliveryQueued {
		return effect, domain.Conflict("error.chain.decision_stale", nil)
	}
	counted, err := sqlite.ChainWakeCounted(ctx, tx, chain.ID, binding.DeliveryID)
	if err != nil {
		return effect, err
	}
	if counted {
		return effect, domain.Conflict("error.chain.decision_stale", nil)
	}
	switch optionID {
	case domain.ChainOptionContinue:
		if err := sqlite.BumpChainEpoch(ctx, tx, chain.ID); err != nil {
			return effect, err
		}
		if binding.OverLimit {
			// The user granted exactly one more wake; the model never
			// extends its own budget.
			if err := sqlite.GrantChainWake(ctx, tx, binding.ChainID); err != nil {
				return effect, err
			}
		}
		counted, err := sqlite.CountChainWake(ctx, tx, chain.ID, binding.DeliveryID, binding.DeliveryID, binding.BotID)
		if err != nil {
			return effect, err
		}
		if counted {
			if err := sqlite.IncrementChainWake(ctx, tx, chain.ID); err != nil {
				return effect, err
			}
		}
		if err := sqlite.SetChainStatus(ctx, tx, chain.ID, domain.ChainActive, ""); err != nil {
			return effect, err
		}
		effect.wakeBotID = binding.BotID
	case domain.ChainOptionStop:
		if err := sqlite.SetChainStatus(ctx, tx, chain.ID, domain.ChainStopped, "user_stopped"); err != nil {
			return effect, err
		}
		if _, err := sqlite.CancelChainDeliveries(ctx, tx, chain.ID); err != nil {
			return effect, err
		}
		var err error
		effect.accepted, err = sqlite.ActiveChainDeliveries(ctx, tx, chain.ID)
		if err != nil {
			return effect, err
		}
	default:
		return effect, domain.InvalidRequest("error.approval.unknown_option", nil)
	}
	return effect, nil
}

func (a *App) finishChainDecision(ctx context.Context, effect chainDecisionEffect) {
	chain := effect.chain
	a.recordEvent(ctx, domain.Event{Type: domain.EventChainUpdated, TeamID: chain.TeamID, ResourceID: chain.ID})
	a.Hub.Publish()
	// Interruption happens outside the transaction: it talks to engines.
	for _, delivery := range effect.accepted {
		if delivery.RunID == "" {
			continue
		}
		if _, err := a.InterruptRun(ctx, delivery.RunID); err != nil {
			a.Logger.Warn("stop chain: interrupt failed", "chainId", chain.ID, "runId", delivery.RunID, "error", err)
		}
	}
	if a.Scheduler != nil && effect.wakeBotID != "" {
		a.Scheduler.Wake(effect.wakeBotID)
	}
}

// chainOfRunLocked reports the causal chain of the run currently executing a
// bot, or "" when the bot is not inside a chained run. Bot-to-bot sends
// attribute their wake to this chain. It must run on the caller's
// transaction: the product pool is a single connection, so reading a.DB from
// inside an open transaction would deadlock.
func chainOfRunLocked(ctx context.Context, q sqlite.Queryer, botID string) string {
	run, err := sqlite.FindActiveRun(ctx, q, botID)
	if err != nil {
		return ""
	}
	chainID, err := sqlite.ChainOfRun(ctx, q, run.ID)
	if err != nil {
		return ""
	}
	return chainID
}
