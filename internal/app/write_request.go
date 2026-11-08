package app

import (
	"context"
	"database/sql"

	"github.com/kunghim/CoBot/internal/app/core"
	"github.com/kunghim/CoBot/internal/auth"
)

// WithWriteRequest records normalized request identity; see core.WithWriteRequest.
func WithWriteRequest(p auth.Principal, scope, key string, args any) (auth.Principal, error) {
	return core.WithWriteRequest(p, scope, key, args)
}

func writePrincipal(p auth.Principal) string { return core.WritePrincipal(p) }

func beginWrite[T any](ctx context.Context, a *App, p auth.Principal) (*T, func(), error) {
	return core.Begin[T](ctx, a.core(), p)
}

func (a *App) saveWriteResult(ctx context.Context, tx *sql.Tx, p auth.Principal, value any) error {
	return a.core().SaveResult(ctx, tx, p, value)
}

func (a *App) finishWriteIntent(ctx context.Context, p auth.Principal, value any) error {
	return a.core().FinishIntent(ctx, p, value)
}
