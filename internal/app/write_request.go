package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

type writeLock struct {
	token chan struct{}
	users int
}

// WithWriteRequest records normalized request identity. Resource IDs must be
// included in args by the HTTP caller; MCP arguments already contain them.
func WithWriteRequest(p auth.Principal, scope, key string, args any) (auth.Principal, error) {
	if p.Write != nil || key == "" {
		return p, nil
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return p, domain.InvalidRequest("error.invalid_request", nil)
	}
	var canonical any
	if err := json.Unmarshal(raw, &canonical); err != nil {
		return p, err
	}
	raw, err = json.Marshal(canonical)
	if err != nil {
		return p, err
	}
	hash := sha256.Sum256(raw)
	p.Write = &auth.WriteRequest{Scope: scope, Key: key, ArgsHash: hex.EncodeToString(hash[:])}
	return p, nil
}

func writePrincipal(p auth.Principal) string {
	if p.Kind == auth.KindBot {
		return "bot:" + p.BotID
	}
	return "user:" + p.UserID
}

// Serialize only retries of the same logical write. The durable record is
// saved in the business transaction, so this lock is not crash protection.
// Different operations and engine waits never share a global write lock.
func (a *App) lockWrite(ctx context.Context, key string) (func(), error) {
	a.writeLocksMu.Lock()
	if a.writeLocks == nil {
		a.writeLocks = map[string]*writeLock{}
	}
	entry := a.writeLocks[key]
	if entry == nil {
		entry = &writeLock{token: make(chan struct{}, 1)}
		entry.token <- struct{}{}
		a.writeLocks[key] = entry
	}
	entry.users++
	a.writeLocksMu.Unlock()
	releaseRef := func() {
		a.writeLocksMu.Lock()
		entry.users--
		if entry.users == 0 {
			delete(a.writeLocks, key)
		}
		a.writeLocksMu.Unlock()
	}
	select {
	case <-ctx.Done():
		releaseRef()
		return nil, ctx.Err()
	case <-entry.token:
		return func() { entry.token <- struct{}{}; releaseRef() }, nil
	}
}

func beginWrite[T any](ctx context.Context, a *App, p auth.Principal) (*T, func(), error) {
	op := p.Write
	if op == nil {
		return nil, func() {}, nil
	}
	if strings.TrimSpace(op.Key) == "" || len(op.Key) > 128 || op.Scope == "" || op.ArgsHash == "" {
		return nil, func() {}, domain.InvalidRequest("error.idempotency.required", nil)
	}
	unlock, err := a.lockWrite(ctx, writePrincipal(p)+"/"+op.Scope+"/"+op.Key)
	if err != nil {
		return nil, func() {}, err
	}
	if p.Kind == auth.KindBot {
		if err := a.ValidateToolRun(ctx, p); err != nil {
			return nil, unlock, err
		}
	}
	raw, hit, err := sqlite.LookupIdempotency(ctx, a.DB, "resource."+op.Scope, writePrincipal(p), op.Key, op.ArgsHash)
	if err != nil || !hit {
		return nil, unlock, err
	}
	var value T
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return nil, unlock, domain.Internal(errors.New("invalid persisted write response"))
	}
	return &value, unlock, nil
}

// saveWriteResult MUST be called before committing the transaction that
// creates/changes the resource. No post-commit dispatcher cache is involved.
func (a *App) saveWriteResult(ctx context.Context, tx *sql.Tx, p auth.Principal, value any) error {
	if p.Write == nil {
		return nil
	}
	if p.Kind == auth.KindBot {
		if err := a.validateToolRunOn(ctx, tx, p); err != nil {
			return err
		}
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if err := sqlite.SaveIdempotency(ctx, tx, "resource."+p.Write.Scope, writePrincipal(p), p.Write.Key, p.Write.ArgsHash, string(raw)); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `DELETE FROM idempotency WHERE created_at < ?`, time.Now().UTC().Add(-30*24*time.Hour).Format(time.RFC3339Nano))
	return err
}

// Only an already committed external-effect intent may update its outcome.
// A retry reads that outcome; it never repeats an uncertain RPC/file operation.
func (a *App) finishWriteIntent(ctx context.Context, p auth.Principal, value any) error {
	if p.Write == nil {
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	result, err := a.DB.ExecContext(ctx, `UPDATE idempotency SET response = ? WHERE scope = ? AND principal = ? AND key = ? AND request_hash = ?`, string(raw), "resource."+p.Write.Scope, writePrincipal(p), p.Write.Key, p.Write.ArgsHash)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("external write intent is missing")
	}
	return nil
}
