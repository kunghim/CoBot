package app

import (
	"context"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// RequestInterrupt persists intent before the RPC. Replaying an uncertain
// response returns that fact instead of interrupting an execution again.
func (a *App) RequestInterrupt(ctx context.Context, p auth.Principal, id string) (map[string]any, error) {
	if p.Kind != auth.KindUser {
		return nil, domain.Forbidden("error.forbidden.user_only", nil)
	}
	cached, unlock, err := beginWrite[map[string]any](ctx, a, p)
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
	run, err := sqlite.GetRun(ctx, tx, id)
	if err != nil {
		return nil, sqlite.NormalizeError(err)
	}
	switch run.Status {
	case domain.RunStarting, domain.RunRunning, domain.RunWaitingApproval, domain.RunWaitingInput:
	default:
		return nil, domain.Conflict("error.run.not_active", nil)
	}
	result := map[string]any{"runId": id, "interruptRequested": true, "responseState": "unknown"}
	if err := a.saveWriteResult(ctx, tx, p, result); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	if _, err := a.InterruptRun(ctx, id); err != nil {
		// The native completion, not the outbound RPC, determines terminal state.
		result["messageKey"] = "error.engine.interrupt_unconfirmed"
	} else {
		result["responseState"] = "sent"
	}
	if err := a.finishWriteIntent(ctx, p, result); err != nil {
		return nil, err
	}
	return result, nil
}
