package app

import (
	"context"
	"time"

	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// WriteReplay returns the stored response of an earlier write with the same
// scope, principal and key. A hash mismatch surfaces as an idempotency
// conflict: the same key may never carry different arguments (E05).
func (a *App) WriteReplay(ctx context.Context, scope, principal, key, argsHash string) (string, bool, error) {
	return sqlite.LookupIdempotency(ctx, a.DB, scope, principal, key, argsHash)
}

// WriteReplaySave records the outcome of a successful write so its retry
// replays instead of duplicating. A concurrent identical save is harmless
// (same key, same hash); the winner's copy is served to both.
func (a *App) WriteReplaySave(ctx context.Context, scope, principal, key, argsHash, response string) error {
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := sqlite.SaveIdempotency(ctx, tx, scope, principal, key, argsHash, response); err != nil {
		if isUniqueViolation(err) {
			// A concurrent identical call saved first; its copy answers both.
			_ = tx.Rollback()
			return nil
		}
		return err
	}
	// Bound the table: replay rows older than the retry horizon are dead
	// weight (review B: the table must not grow forever).
	cutoff := time.Now().UTC().Add(-30 * 24 * time.Hour).Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `DELETE FROM idempotency WHERE created_at < ?`, cutoff); err != nil {
		return err
	}
	return tx.Commit()
}
