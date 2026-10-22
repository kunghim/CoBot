package app

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/engine"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// Startup reconciliation and the recovery card.
//
// After a restart nothing is taken over: a live PID or a resumable thread is
// not proof that CoBot still controls an execution. Instead the previous
// instance's non-terminal work is classified honestly — runs become unknown
// and lock their bots, claimed deliveries become unknown, approvals bound to
// the dead connection are invalidated (decided history stays) — and every
// unresolved run gets exactly one recovery card offering the capabilities
// this build really has: continue, rerun, abandon.

// recoveryBinding stores which run a recovery card decides, inside the
// approval's engine request id slot (same pattern as recruitment and chains).
type recoveryBinding struct {
	RunID      string `json:"runId"`
	BotID      string `json:"botId"`
	DeliveryID string `json:"deliveryId,omitempty"`
}

// Only projection changes: original choices remain an immutable snapshot.
func (a *App) projectRecoveryOptions(ctx context.Context, approval *domain.Approval) {
	a.projectRecoveryOptionsOn(ctx, a.DB, approval)
}

func (a *App) projectRecoveryOptionsOn(ctx context.Context, q sqlite.Queryer, approval *domain.Approval) {
	var binding recoveryBinding
	_ = json.Unmarshal(approval.EngineRequestID, &binding)
	run, rerr := sqlite.GetRun(ctx, q, binding.RunID)
	session, serr := sqlite.GetProviderSession(ctx, q, run.BotID)
	if rerr == nil && serr == nil && run.ProviderTurnID != "" && session.ProviderThreadID != "" && session.EngineEpoch == run.EngineEpoch {
		return
	}
	for i := range approval.Options {
		approval.Options[i].Disabled = true
		approval.Options[i].DisabledReasonKey = "error.recovery.end_unverified"
	}
}

// ReconcileAfterRestart classifies the work a previous process left behind.
// It runs once at startup, before the dispatch loop starts, and is
// idempotent: re-running it never stacks second cards and never re-marks a
// run the user already resolved.
func (a *App) ReconcileAfterRestart(ctx context.Context) error {
	runs, err := sqlite.ListUnresolvedRuns(ctx, a.DB)
	if err != nil {
		return err
	}
	native, err := sqlite.ListPendingApprovalsOfType(ctx, a.DB, domain.ApprovalNativeTool)
	if err != nil {
		return err
	}

	var runIDs, approvalIDs []string
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, run := range runs {
		// Keep the dead connection's turn id and epoch: they document which
		// engine the run belonged to, which is exactly what "unknown after
		// restart" must not lose: both the known and the unknown facts are kept.
		if err := sqlite.UpdateRunStatus(ctx, tx, run.ID, domain.RunUnknown,
			run.ProviderTurnID, run.EngineEpoch, "error.run.unknown_after_restart", false); err != nil {
			return err
		}
		if err := sqlite.SetBotRunState(ctx, tx, run.BotID, domain.BotStatusUnknown, run.ID); err != nil {
			return err
		}
		deliveryID, err := sqlite.MarkDeliveryUnknownByRun(ctx, tx, run.ID)
		if err != nil {
			return err
		}
		cardID, err := ensureRecoveryCardLocked(ctx, tx, run, deliveryID)
		if err != nil {
			return err
		}
		runIDs = append(runIDs, run.ID)
		if cardID != "" {
			approvalIDs = append(approvalIDs, cardID)
		}
	}
	// Approvals bound to the previous connection can never be answered (a new
	// connection refuses old request ids); invalidating them at
	// startup keeps dead cards out of the pager instead of letting them fail
	// decision by decision.
	for _, approval := range native {
		if err := sqlite.SetApprovalRequestState(ctx, tx, approval.ID, domain.ApprovalStateInvalidated); err != nil {
			return err
		}
		approvalIDs = append(approvalIDs, approval.ID)
	}
	// Decisions survive restart; an unsent native reply cannot be delivered
	// on a different connection. Product feedback keeps its durable outbox.
	if _, err := tx.ExecContext(ctx, `UPDATE approval_responses SET state = ?, detail = 'engine_epoch_changed' WHERE state = ? AND delivery_id = '' AND approval_id IN (SELECT id FROM approvals WHERE type = ?)`, domain.ResponseUnknown, domain.ResponseQueued, domain.ApprovalNativeTool); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE approval_responses SET state = ?, detail = 'execution_outcome_unknown' WHERE state = ? AND delivery_id IN (SELECT id FROM deliveries WHERE status = ?)`, domain.ResponseUnknown, domain.ResponseQueued, domain.DeliveryUnknown); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	// A chain decision whose side effect never landed (the process died
	// between the approval CAS and the effect) would strand the chain in
	// awaiting_approval with only a decided card — nobody could decide it
	// again. Replay the recorded decision now: keep the known fact and
	// complete the effect.
	if err := a.replayStuckChainDecisions(ctx); err != nil {
		return err
	}

	// A risk evaluation interrupted by the restart must not strand its chain
	// in evaluating: the chains return to active and held deliveries are
	// re-woken, so the gate can evaluate again.
	if evaluating, rerr := sqlite.ResetEvaluatingChains(ctx, a.DB); rerr == nil {
		for _, chainID := range evaluating {
			_ = a.recordEvent(ctx, domain.Event{Type: domain.EventChainUpdated, ResourceID: chainID})
		}
		if len(evaluating) > 0 {
			a.ResumeBudgetPausedBots(ctx)
		}
	}
	if len(runs) == 0 && len(native) == 0 {
		return nil // a clean first start stays quiet
	}

	// Events and per-approval projections happen after the transaction: the
	// pool is a single connection.
	for _, runID := range runIDs {
		_ = a.recordEvent(ctx, domain.Event{Type: domain.EventRunUpdated, ResourceID: runID})
	}
	for _, id := range approvalIDs {
		if approval, err := sqlite.GetApproval(ctx, a.DB, id); err == nil {
			if approval.Type == domain.ApprovalNativeTool && approval.RequestState == domain.ApprovalStateInvalidated {
				// No decision ever seeded a response row for this card, so
				// the expired outcome is written directly.
				_ = sqlite.EnsureApprovalResponseState(ctx, a.DB, id, domain.ResponseExpired, "engine_epoch_changed")
			}
			a.publishApprovalUpdated(ctx, approval)
		}
	}
	a.Hub.Publish()
	return nil
}

// Unknown work needs an entry point even when only the engine connection
// died. Creating the card grants no execution authority; decisions still
// require verifyRecoveryTurn's exact terminal evidence.
func ensureRecoveryCardLocked(ctx context.Context, tx *sql.Tx, run domain.Run, deliveryID string) (string, error) {
	exists, err := sqlite.HasPendingRecoveryForRun(ctx, tx, run.ID)
	if err != nil || exists {
		return "", err
	}
	binding, err := json.Marshal(recoveryBinding{RunID: run.ID, BotID: run.BotID, DeliveryID: deliveryID})
	if err != nil {
		return "", err
	}
	card := domain.Approval{ID: domain.NewID(), TeamID: run.TeamID, RequesterBotID: run.BotID, EngineRequestID: binding,
		Type: domain.ApprovalRecovery, Summary: "approval.card.recovery", Reason: "unresolved_run", RequestState: domain.ApprovalStatePending,
		Options: []domain.ApprovalOption{
			{ID: domain.RecoveryOptionContinue, MessageKey: "approval.option.recovery_continue", Text: "只释放成员"},
			{ID: domain.RecoveryOptionRerun, MessageKey: "approval.option.recovery_rerun", Text: "重跑"},
			{ID: domain.RecoveryOptionAbandon, MessageKey: "approval.option.recovery_abandon", Text: "放弃"},
		},
	}
	if err := sqlite.InsertApproval(ctx, tx, card); err != nil {
		return "", err
	}
	return card.ID, nil
}

// replayStuckChainDecisions re-applies the decided option of every chain
// card whose chain still waits. ApplyChainDecision is idempotent for the
// continue path (the wake count uses the unique key) and safe to re-run for
// stop (sweep and interrupt are no-ops when already applied).
func (a *App) replayStuckChainDecisions(ctx context.Context) error {
	chains, err := sqlite.ListChains(ctx, a.DB, "", domain.ChainAwaitingApproval, 200)
	if err != nil {
		return err
	}
	for _, chain := range chains {
		pending, err := pendingChainDecisionExists(ctx, a.DB, chain.TeamID, chain.ID)
		if err != nil {
			return err
		}
		if pending {
			continue
		}
		cards, err := sqlite.ListApprovals(ctx, a.DB, chain.TeamID, domain.ApprovalStateDecided, 100)
		if err != nil {
			return err
		}
		for _, card := range cards {
			if card.Type != domain.ApprovalChain || card.Decision == nil {
				continue
			}
			var binding chainApprovalBinding
			if json.Unmarshal(card.EngineRequestID, &binding) != nil || binding.ChainID != chain.ID || (binding.ChainEpoch != 0 && binding.ChainEpoch != chain.Epoch) {
				continue
			}
			a.Logger.Warn("replaying a chain decision whose side effect was interrupted", "chainId", chain.ID, "option", card.Decision.OptionID)
			if err := a.ApplyChainDecision(ctx, card, card.Decision.OptionID); err != nil {
				// One bad card must not brick startup: report it, leave the
				// chain in its current state for a human, keep reconciling.
				a.Logger.Error("chain decision replay failed", "chainId", chain.ID, "error", err)
			}
			break
		}
	}
	return nil
}

// ApplyRecoveryDecision executes the user's choice for an unresolved run.
// The exact provider execution must be proven terminal before any choice
// releases the bot; what happens to the triggering delivery depends on the
// choice — and "continue" deliberately changes nothing else, preserving the
// unknown delivery as a fact rather than pretending it succeeded.
func (a *App) ApplyRecoveryDecision(ctx context.Context, approval domain.Approval, optionID string) error {
	status, err := a.verifyRecoveryTurn(ctx, approval)
	if err != nil {
		return err
	}
	return a.applyVerifiedRecoveryDecision(ctx, approval, optionID, status)
}

func (a *App) verifyRecoveryTurn(ctx context.Context, approval domain.Approval) (string, error) {
	var binding recoveryBinding
	if json.Unmarshal(approval.EngineRequestID, &binding) != nil || binding.RunID == "" {
		return "", domain.InvalidRequest("error.recovery.binding_missing", nil)
	}
	run, err := sqlite.GetRun(ctx, a.DB, binding.RunID)
	if err != nil {
		return "", sqlite.NormalizeError(err)
	}
	if run.Status != domain.RunUnknown {
		return "", domain.Conflict("error.recovery.not_unknown", map[string]any{"status": run.Status})
	}
	session, err := sqlite.GetProviderSession(ctx, a.DB, run.BotID)
	if err != nil || session.ProviderThreadID == "" || session.EngineEpoch != run.EngineEpoch || run.ProviderTurnID == "" || a.Scheduler == nil {
		return "", domain.Conflict("error.recovery.end_unverified", nil)
	}
	_, cwd, err := sqlite.GetTeam(ctx, a.DB, run.TeamID)
	if err != nil {
		return "", err
	}
	server, err := a.Scheduler.EnsureTeamServer(ctx, run.TeamID, cwd)
	if err != nil {
		return "", domain.EngineUnavailable("error.engine.unavailable", nil, err)
	}
	inspector, ok := server.(engine.TurnInspector)
	if !ok {
		return "", domain.Conflict("error.recovery.end_unverified", nil)
	}
	status, err := inspector.InspectTurn(ctx, session.ProviderThreadID, run.ProviderTurnID)
	if err != nil {
		return "", domain.Conflict("error.recovery.end_unverified", nil)
	}
	switch status {
	case domain.RunCompleted, domain.RunFailed, domain.RunInterrupted:
		return status, nil
	default:
		return "", domain.Conflict("error.recovery.end_unverified", map[string]any{"providerStatus": status})
	}
}

func (a *App) applyVerifiedRecoveryDecision(ctx context.Context, approval domain.Approval, optionID, terminalStatus string) error {
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	run, err := a.applyVerifiedRecoveryDecisionLocked(ctx, tx, approval, optionID, terminalStatus)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	a.finishRecoveryDecision(ctx, run)
	return nil
}

func (a *App) applyVerifiedRecoveryDecisionLocked(ctx context.Context, tx *sql.Tx, approval domain.Approval, optionID, terminalStatus string) (domain.Run, error) {
	switch terminalStatus {
	case domain.RunCompleted, domain.RunFailed, domain.RunInterrupted:
	default:
		return domain.Run{}, domain.Conflict("error.recovery.end_unverified", nil)
	}
	var binding recoveryBinding
	if err := json.Unmarshal(approval.EngineRequestID, &binding); err != nil || binding.RunID == "" {
		return domain.Run{}, domain.InvalidRequest("error.recovery.binding_missing", nil)
	}
	if optionID != domain.RecoveryOptionContinue && optionID != domain.RecoveryOptionRerun && optionID != domain.RecoveryOptionAbandon {
		return domain.Run{}, domain.InvalidRequest("error.approval.unknown_option", map[string]any{"optionId": optionID})
	}
	run, err := sqlite.GetRun(ctx, tx, binding.RunID)
	if err != nil {
		return domain.Run{}, sqlite.NormalizeError(err)
	}
	if run.TeamID != approval.TeamID || run.BotID != approval.RequesterBotID || run.Status != domain.RunUnknown {
		return domain.Run{}, domain.Conflict("error.recovery.not_unknown", map[string]any{"status": run.Status})
	}
	// Confirming the old execution ended is the precondition for any of the
	// three choices; without it nothing may rerun.
	if err := sqlite.UpdateRunStatus(ctx, tx, run.ID, terminalStatus,
		run.ProviderTurnID, run.EngineEpoch, "error.run.unknown_after_restart", true); err != nil {
		return domain.Run{}, err
	}
	if binding.DeliveryID != "" {
		delivery, err := sqlite.GetDelivery(ctx, tx, binding.DeliveryID)
		if err != nil {
			return domain.Run{}, sqlite.NormalizeError(err)
		}
		if delivery.RunID != run.ID || delivery.TargetBotID != run.BotID {
			return domain.Run{}, domain.Conflict("error.recovery.binding_missing", nil)
		}
		switch optionID {
		case domain.RecoveryOptionRerun:
			// The run is already confirmed ended, so an accepted delivery —
			// the common case: the engine took the turn and the process died
			// mid-execution — is rerunnable too. Cancelled or live-queued
			// work must not silently rerun.
			if delivery.Status != domain.DeliveryUnknown &&
				delivery.Status != domain.DeliveryDispatching &&
				delivery.Status != domain.DeliveryAccepted {
				return domain.Run{}, domain.Conflict("error.recovery.delivery_not_rerunnable", map[string]any{"status": delivery.Status})
			}
			if err := sqlite.SetDeliveryStatus(ctx, tx, delivery.ID, domain.DeliveryQueued); err != nil {
				return domain.Run{}, err
			}
			if err := sqlite.SetMessageDeliveryStatus(ctx, tx, delivery.MessageID, domain.DeliveryQueued); err != nil {
				return domain.Run{}, err
			}
		case domain.RecoveryOptionAbandon:
			if delivery.Status == domain.DeliveryUnknown || delivery.Status == domain.DeliveryDispatching || delivery.Status == domain.DeliveryQueued || delivery.Status == domain.DeliveryAccepted {
				if err := sqlite.SetDeliveryStatus(ctx, tx, delivery.ID, domain.DeliveryCancelled); err != nil {
					return domain.Run{}, err
				}
				if err := sqlite.SetMessageDeliveryStatus(ctx, tx, delivery.MessageID, domain.DeliveryCancelled); err != nil {
					return domain.Run{}, err
				}
			}
		case domain.RecoveryOptionContinue:
			// Deliberately untouched: the delivery stays unknown, keeping the
			// uncertainty as a fact instead of resolving it in either
			// direction.
		}
	}
	nextStatus := domain.BotStatusIdle
	if queued, err := sqlite.CountQueuedDeliveries(ctx, tx, run.BotID); err == nil && queued > 0 {
		nextStatus = domain.BotStatusQueued
	}
	if err := sqlite.SetBotRunState(ctx, tx, run.BotID, nextStatus, ""); err != nil {
		return domain.Run{}, err
	}
	return run, nil
}

func (a *App) finishRecoveryDecision(ctx context.Context, run domain.Run) {
	_ = a.recordEvent(ctx, domain.Event{Type: domain.EventRunUpdated, TeamID: run.TeamID, BotID: run.BotID, ResourceID: run.ID})
	a.Hub.Publish()
	if a.Scheduler != nil {
		a.Scheduler.Wake(run.BotID)
	}
}
