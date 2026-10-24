package app

import (
	"context"
	"time"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/updates"
)

// CheckForUpdate asks whether a newer release exists. It is the user pressing
// a button, never a background task, and a failure comes back as the result's
// state so the page can say what happened.
func (a *App) CheckForUpdate(ctx context.Context, principal auth.Principal) (updates.Result, error) {
	if principal.Kind != auth.KindUser {
		return updates.Result{}, domain.Forbidden("error.forbidden.user_only", nil)
	}
	if a.Updates == nil {
		return updates.Result{State: updates.Unreachable, Current: a.Version, CheckedAt: time.Now().UTC()}, nil
	}
	return a.Updates.Check(ctx), nil
}
