package app

import (
	"context"
	"errors"
	"time"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// RetryRun runs again what a failed turn was started for. A turn can fail
// for reasons that pass — the account's usage limit, a dropped connection —
// and the work it carried (a card, a message) is then stuck: the member is
// idle, the card still theirs, and nothing would wake them. Retrying puts the
// work back in the member's queue, exactly as choosing "rerun" on a recovery
// card does.
//
// Only the member's latest turn can be retried, and only while the member is
// idle: a turn that came later has already moved the work on.
func (a *App) RetryRun(ctx context.Context, principal auth.Principal, runID string) (map[string]any, error) {
	if principal.Kind != auth.KindUser {
		return nil, domain.Forbidden("error.forbidden.user_only", nil)
	}
	cached, unlock, err := beginWrite[map[string]any](ctx, a, principal)
	defer unlock()
	if err != nil {
		return nil, err
	}
	if cached != nil {
		return *cached, nil
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	run, err := sqlite.GetRun(ctx, tx, runID)
	if err != nil {
		return nil, sqlite.NormalizeError(err)
	}
	if run.Status != domain.RunFailed {
		return nil, domain.Conflict("error.run.not_retryable", map[string]any{"status": run.Status})
	}
	if _, err := sqlite.FindActiveRun(ctx, tx, run.BotID); err == nil {
		return nil, domain.Conflict("error.run.bot_busy", nil)
	} else if !errors.Is(err, sqlite.ErrNotFound) {
		return nil, err
	}
	var newer int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM runs WHERE bot_id = ? AND started_at > ?`,
		run.BotID, run.StartedAt.UTC().Format(time.RFC3339Nano)).Scan(&newer); err != nil {
		return nil, err
	}
	if newer > 0 {
		return nil, domain.Conflict("error.run.not_latest", nil)
	}

	rows, err := tx.QueryContext(ctx, `SELECT id, message_id, task_id, status FROM deliveries WHERE run_id = ?`, run.ID)
	if err != nil {
		return nil, err
	}
	type carried struct{ id, messageID, taskID, status string }
	var work []carried
	for rows.Next() {
		var c carried
		if err := rows.Scan(&c.id, &c.messageID, &c.taskID, &c.status); err != nil {
			rows.Close()
			return nil, err
		}
		work = append(work, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	requeued := 0
	for _, c := range work {
		switch c.status {
		case domain.DeliveryAccepted, domain.DeliveryDispatching, domain.DeliveryUnknown:
		default:
			continue
		}
		// A card that was handed off or cancelled meanwhile has nothing left
		// to run again.
		if c.taskID != "" {
			task, err := sqlite.GetTask(ctx, tx, c.taskID)
			if err != nil && !errors.Is(err, sqlite.ErrNotFound) {
				return nil, err
			}
			if errors.Is(err, sqlite.ErrNotFound) || task.IsTerminal() {
				continue
			}
		}
		if err := sqlite.SetDeliveryStatus(ctx, tx, c.id, domain.DeliveryQueued); err != nil {
			return nil, err
		}
		if err := sqlite.SetMessageDeliveryStatus(ctx, tx, c.messageID, domain.DeliveryQueued); err != nil {
			return nil, err
		}
		requeued++
	}
	if requeued == 0 {
		return nil, domain.Conflict("error.run.nothing_to_retry", nil)
	}
	if err := sqlite.SetBotRunState(ctx, tx, run.BotID, domain.BotStatusQueued, ""); err != nil {
		return nil, err
	}
	result := map[string]any{"runId": run.ID, "requeued": requeued}
	if err := a.saveWriteResult(ctx, tx, principal, result); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	_ = a.recordEvent(ctx, domain.Event{Type: domain.EventRunUpdated, TeamID: run.TeamID, BotID: run.BotID, ResourceID: run.ID})
	a.Hub.Publish()
	if a.Scheduler != nil {
		a.Scheduler.Wake(run.BotID)
	}
	return result, nil
}
