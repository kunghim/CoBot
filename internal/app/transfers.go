package app

import (
	"context"
	"strings"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// OfferTransfer starts a two-phase hand-over. The owner is unchanged until
// the target accepts, so a task never has zero or two owners.
func (a *App) OfferTransfer(ctx context.Context, principal auth.Principal, taskID, toBotID, note string) (domain.Transfer, error) {
	task, err := sqlite.GetTask(ctx, a.DB, taskID)
	if err != nil {
		return domain.Transfer{}, sqlite.NormalizeError(err)
	}
	switch principal.Kind {
	case auth.KindUser:
		// the user may arrange any hand-over
	case auth.KindBot:
		if principal.BotID != task.OwnerID {
			return domain.Transfer{}, domain.Forbidden("error.transfer.owner_only", nil)
		}
	default:
		return domain.Transfer{}, domain.Unauthorized("error.unauthorized.principal_required")
	}
	if toBotID == task.OwnerID {
		return domain.Transfer{}, domain.InvalidRequest("error.transfer.to_self", nil)
	}
	if err := a.assertSameTeamOwner(ctx, a.DB, task.TeamID, toBotID); err != nil {
		return domain.Transfer{}, err
	}
	// Only one live offer per task, so an old offer can never be accepted
	// after the owner has moved on.
	if open, err := sqlite.ListTransfersForTask(ctx, a.DB, task.ID); err != nil {
		return domain.Transfer{}, err
	} else {
		for _, t := range open {
			if t.Status == domain.TransferOffered {
				return domain.Transfer{}, domain.Conflict("error.transfer.already_offered", map[string]any{"transferId": t.ID})
			}
		}
	}
	transfer := domain.Transfer{
		ID: domain.NewID(), TaskID: task.ID, FromBotID: task.OwnerID,
		ToBotID: toBotID, TaskRevision: task.Revision, Note: strings.TrimSpace(note),
		Status: domain.TransferOffered,
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return domain.Transfer{}, err
	}
	defer tx.Rollback()
	if err := sqlite.InsertTransfer(ctx, tx, transfer); err != nil {
		return domain.Transfer{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Transfer{}, err
	}
	_ = a.recordEvent(ctx, domain.Event{Type: domain.EventTransferOffered, TeamID: task.TeamID, ResourceID: transfer.ID})
	a.Hub.Publish()
	return transfer, nil
}

// AcceptTransfer completes the hand-over in one transaction: the task moves
// to the target, the old owner's undelivered assignment is cancelled and the
// new owner receives exactly one.
func (a *App) AcceptTransfer(ctx context.Context, principal auth.Principal, transferID string) (domain.Task, error) {
	transfer, err := sqlite.GetTransfer(ctx, a.DB, transferID)
	if err != nil {
		return domain.Task{}, sqlite.NormalizeError(err)
	}
	switch principal.Kind {
	case auth.KindUser:
	case auth.KindBot:
		if principal.BotID != transfer.ToBotID {
			return domain.Task{}, domain.Forbidden("error.transfer.target_only", nil)
		}
	default:
		return domain.Task{}, domain.Unauthorized("error.unauthorized.principal_required")
	}
	if transfer.Status != domain.TransferOffered {
		return domain.Task{}, domain.Conflict("error.transfer.not_offered", map[string]any{"status": transfer.Status})
	}
	task, err := sqlite.GetTask(ctx, a.DB, transfer.TaskID)
	if err != nil {
		return domain.Task{}, sqlite.NormalizeError(err)
	}
	if task.OwnerID != transfer.FromBotID {
		// The owner moved on; the offer is stale and can never steal a task.
		_ = a.staleTransfer(ctx, transfer)
		return domain.Task{}, domain.Conflict("error.transfer.owner_changed", map[string]any{"ownerId": task.OwnerID})
	}
	if task.Revision != transfer.TaskRevision {
		_ = a.staleTransfer(ctx, transfer)
		return domain.Task{}, domain.Conflict("error.transfer.task_changed", map[string]any{"taskRevision": task.Revision})
	}
	// The old owner may not still be writing files for the task.
	if err := a.assertOwnerRunEnded(ctx, transfer.FromBotID, task.ID); err != nil {
		return domain.Task{}, err
	}

	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return domain.Task{}, err
	}
	defer tx.Rollback()
	if err := sqlite.SetTransferStatus(ctx, tx, transfer.ID, domain.TransferAccepted); err != nil {
		return domain.Task{}, err
	}
	if err := a.cancelUndeliveriesForTaskLocked(ctx, tx, task.ID, transfer.FromBotID); err != nil {
		return domain.Task{}, err
	}
	if err := sqlite.ChangeTaskOwner(ctx, tx, task.ID, transfer.ToBotID); err != nil {
		return domain.Task{}, err
	}
	task.OwnerID = transfer.ToBotID
	task.Revision++
	ready, err := a.dependenciesSatisfied(ctx, tx, task.ID)
	if err != nil {
		return domain.Task{}, err
	}
	// The hand-over wake belongs to the chain that produced the work, so a
	// long task cannot reset its budget simply by changing hands.
	chainID, err := sqlite.ChainOfTask(ctx, tx, task.ID)
	if err != nil {
		return domain.Task{}, err
	}
	if err := a.enqueueAssignmentLocked(ctx, tx, task, ready, chainID); err != nil {
		return domain.Task{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Task{}, err
	}
	_ = a.recordEvent(ctx, domain.Event{Type: domain.EventTransferAccepted, TeamID: task.TeamID, ResourceID: transfer.ID})
	a.recordTaskUpdated(ctx, task)
	if ready && a.Scheduler != nil {
		a.Scheduler.Wake(task.OwnerID)
	}
	return sqlite.GetTask(ctx, a.DB, task.ID)
}

// staleTransfer marks an offer that can no longer be honoured.
func (a *App) staleTransfer(ctx context.Context, transfer domain.Transfer) error {
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := sqlite.SetTransferStatus(ctx, tx, transfer.ID, domain.TransferStale); err != nil {
		return err
	}
	return tx.Commit()
}

// CancelTransfer withdraws a live offer.
func (a *App) CancelTransfer(ctx context.Context, principal auth.Principal, transferID string) (domain.Transfer, error) {
	transfer, err := sqlite.GetTransfer(ctx, a.DB, transferID)
	if err != nil {
		return domain.Transfer{}, sqlite.NormalizeError(err)
	}
	switch principal.Kind {
	case auth.KindUser:
	case auth.KindBot:
		if principal.BotID != transfer.FromBotID {
			return domain.Transfer{}, domain.Forbidden("error.transfer.offerer_only", nil)
		}
	default:
		return domain.Transfer{}, domain.Unauthorized("error.unauthorized.principal_required")
	}
	if transfer.Status != domain.TransferOffered {
		return domain.Transfer{}, domain.Conflict("error.transfer.not_offered", map[string]any{"status": transfer.Status})
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return domain.Transfer{}, err
	}
	defer tx.Rollback()
	if err := sqlite.SetTransferStatus(ctx, tx, transfer.ID, domain.TransferCancelled); err != nil {
		return domain.Transfer{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Transfer{}, err
	}
	_ = a.recordEvent(ctx, domain.Event{Type: domain.EventTransferOffered, TeamID: "", ResourceID: transfer.ID})
	a.Hub.Publish()
	return transfer, nil
}
