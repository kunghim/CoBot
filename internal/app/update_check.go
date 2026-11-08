package app

import (
	"context"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/updates"
)

// CheckForUpdate asks whether a newer release exists; see updates.CheckAs.
func (a *App) CheckForUpdate(ctx context.Context, principal auth.Principal) (updates.Result, error) {
	return updates.CheckAs(ctx, principal, a.Updates, a.Version)
}
