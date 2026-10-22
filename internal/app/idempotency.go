package app

import (
	"context"

	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// WriteReplay returns the stored response of an earlier write with the same
// scope, principal and key. A hash mismatch surfaces as an idempotency
// conflict: the same key may never carry different arguments.
func (a *App) WriteReplay(ctx context.Context, scope, principal, key, argsHash string) (string, bool, error) {
	return sqlite.LookupIdempotency(ctx, a.DB, scope, principal, key, argsHash)
}
