package app

import (
	"context"
	"encoding/json"

	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// Startup reconciliation and the recovery card (H03/H04/H05/H09).
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
	if len(runs) == 0 && len(native) == 0 {
		return nil // a clean first start stays quiet
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
		// restart" must not lose (H09 keeps both known and unknown facts).
		if err := sqlite.UpdateRunStatus(ctx, tx, run.ID, domain.RunUnknown,
			run.ProviderTurnID, run.EngineEpoch, "error.run.unknown_after_restart", true); err != nil {
			return err
		}
		if err := sqlite.SetBotRunState(ctx, tx, run.BotID, domain.BotStatusUnknown, run.ID); err != nil {
			return err
		}
		deliveryID, err := sqlite.MarkDeliveryUnknownByRun(ctx, tx, run.ID)
		if err != nil {
			return err
		}
		exists, err := sqlite.HasPendingRecoveryForRun(ctx, tx, run.ID)
		if err != nil {
			return err
		}
		if exists {
			runIDs = append(runIDs, run.ID)
			continue // the user is already deciding this one
		}
		binding, err := json.Marshal(recoveryBinding{RunID: run.ID, BotID: run.BotID, DeliveryID: deliveryID})
		if err != nil {
			return err
		}
		card := domain.Approval{
			ID: domain.NewID(), TeamID: run.TeamID, RequesterBotID: run.BotID,
			EngineRequestID: binding, Type: domain.ApprovalRecovery,
			Summary: "上次执行在进程退出后状态未知，请选择处理方式",
			Reason:  "unresolved_run",
			Options: []domain.ApprovalOption{
				{ID: domain.RecoveryOptionContinue, MessageKey: "approval.option.recovery_continue", Text: "继续"},
				{ID: domain.RecoveryOptionRerun, MessageKey: "approval.option.recovery_rerun", Text: "重跑当前工作"},
				{ID: domain.RecoveryOptionAbandon, MessageKey: "approval.option.recovery_abandon", Text: "放弃"},
			},
			RequestState: domain.ApprovalStatePending,
		}
		if err := sqlite.InsertApproval(ctx, tx, card); err != nil {
			return err
		}
		runIDs = append(runIDs, run.ID)
		approvalIDs = append(approvalIDs, card.ID)
	}
	// Approvals bound to the previous connection can never be answered (F04
	// proves a new connection refuses old request ids); invalidating them at
	// startup keeps dead cards out of the pager instead of letting them fail
	// decision by decision.
	for _, approval := range native {
		if err := sqlite.SetApprovalRequestState(ctx, tx, approval.ID, domain.ApprovalStateInvalidated); err != nil {
			return err
		}
		approvalIDs = append(approvalIDs, approval.ID)
	}
	if err := tx.Commit(); err != nil {
		return err
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

// ApplyRecoveryDecision executes the user's choice for an unresolved run.
// The run's execution from the previous process is confirmed ended (it died
// with that process); what happens to the triggering delivery depends on the
// choice — and "continue" deliberately changes nothing else, preserving the
// unknown delivery as a fact rather than pretending it succeeded (H09).
func (a *App) ApplyRecoveryDecision(ctx context.Context, approval domain.Approval, optionID string) error {
	var binding recoveryBinding
	if err := json.Unmarshal(approval.EngineRequestID, &binding); err != nil || binding.RunID == "" {
		return domain.InvalidRequest("error.recovery.binding_missing", nil)
	}
	if optionID != domain.RecoveryOptionContinue && optionID != domain.RecoveryOptionRerun && optionID != domain.RecoveryOptionAbandon {
		return domain.InvalidRequest("error.approval.unknown_option", map[string]any{"optionId": optionID})
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	run, err := sqlite.GetRun(ctx, tx, binding.RunID)
	if err != nil {
		return sqlite.NormalizeError(err)
	}
	if run.Status != domain.RunUnknown {
		return domain.Conflict("error.recovery.not_unknown", map[string]any{"status": run.Status})
	}
	// Confirming the old execution ended is the precondition for any of the
	// three choices; without it nothing may rerun (H05).
	if err := sqlite.UpdateRunStatus(ctx, tx, run.ID, domain.RunInterrupted,
		run.ProviderTurnID, run.EngineEpoch, "error.run.unknown_after_restart", true); err != nil {
		return err
	}
	if binding.DeliveryID != "" {
		delivery, err := sqlite.GetDelivery(ctx, tx, binding.DeliveryID)
		if err != nil {
			return sqlite.NormalizeError(err)
		}
		switch optionID {
		case domain.RecoveryOptionRerun:
			// Only a delivery whose execution is unknown can be requeued; a
			// cancelled or already-queued one must not silently rerun.
			if delivery.Status != domain.DeliveryUnknown && delivery.Status != domain.DeliveryDispatching {
				return domain.Conflict("error.recovery.delivery_not_rerunnable", map[string]any{"status": delivery.Status})
			}
			if err := sqlite.SetDeliveryStatus(ctx, tx, delivery.ID, domain.DeliveryQueued); err != nil {
				return err
			}
			if err := sqlite.SetMessageDeliveryStatus(ctx, tx, delivery.MessageID, domain.DeliveryQueued); err != nil {
				return err
			}
		case domain.RecoveryOptionAbandon:
			if delivery.Status == domain.DeliveryUnknown || delivery.Status == domain.DeliveryDispatching || delivery.Status == domain.DeliveryQueued {
				if err := sqlite.SetDeliveryStatus(ctx, tx, delivery.ID, domain.DeliveryCancelled); err != nil {
					return err
				}
				if err := sqlite.SetMessageDeliveryStatus(ctx, tx, delivery.MessageID, domain.DeliveryCancelled); err != nil {
					return err
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
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	_ = a.recordEvent(ctx, domain.Event{Type: domain.EventRunUpdated, TeamID: run.TeamID, BotID: run.BotID, ResourceID: run.ID})
	a.Hub.Publish()
	if a.Scheduler != nil {
		// Rerun reclaims the requeued delivery; continue/abandon free the
		// queue for whatever waited behind the locked bot.
		a.Scheduler.Wake(run.BotID)
	}
	return nil
}
