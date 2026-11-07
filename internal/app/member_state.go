package app

import (
	"context"
	"database/sql"
	"errors"

	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// setMemberStateTx is the only place a member's run state changes.
func setMemberStateTx(ctx context.Context, tx *sql.Tx, botID, status, runID string) error {
	return sqlite.SetBotRunState(ctx, tx, botID, status, runID)
}

// markQueuedIfIdleTx marks a member queued unless it has a live run: enqueueing
// must not clear current_run_id of a run that is still going. A real database
// error is reported, never taken to mean the member is idle.
func markQueuedIfIdleTx(ctx context.Context, tx *sql.Tx, botID string) error {
	if _, err := sqlite.FindActiveRun(ctx, tx, botID); err == nil {
		return nil
	} else if !errors.Is(err, sqlite.ErrNotFound) {
		return err
	}
	return setMemberStateTx(ctx, tx, botID, domain.BotStatusQueued, "")
}
