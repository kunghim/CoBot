// Package core is what every module of the application needs to change data:
// the guard around a write (retry lock, stored response) and the record of what
// happened (event table, ledger file). The modules moved out of internal/app
// build on it; it knows none of them.
package core

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/events"
	"github.com/kunghim/CoBot/internal/eventsink"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// Core is a view of the application's shared parts, cheap to build per call.
// The two checks a bot's write must pass live with the turns, so they are
// handed in.
type Core struct {
	DB         *sql.DB
	InstanceID string
	Hub        *events.Hub
	Events     *eventsink.Sink
	Logger     *slog.Logger
	Locks      *WriteLocks
	GapNoted   *atomic.Bool

	ValidateToolRun   func(context.Context, auth.Principal) error
	ValidateToolRunOn func(context.Context, sqlite.Queryer, auth.Principal) error
}

// WriteLocks serializes retries of one logical write; the zero value is ready.
type WriteLocks struct {
	mu    sync.Mutex
	locks map[string]*writeLock
}

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

// WritePrincipal names who a write belongs to in its idempotency record.
func WritePrincipal(p auth.Principal) string {
	if p.Kind == auth.KindBot {
		return "bot:" + p.BotID
	}
	return "user:" + p.UserID
}

// Serialize only retries of the same logical write. The durable record is
// saved in the business transaction, so this lock is not crash protection.
// Different operations and engine waits never share a global write lock.
func (l *WriteLocks) lock(ctx context.Context, key string) (func(), error) {
	l.mu.Lock()
	if l.locks == nil {
		l.locks = map[string]*writeLock{}
	}
	entry := l.locks[key]
	if entry == nil {
		entry = &writeLock{token: make(chan struct{}, 1)}
		entry.token <- struct{}{}
		l.locks[key] = entry
	}
	entry.users++
	l.mu.Unlock()
	releaseRef := func() {
		l.mu.Lock()
		entry.users--
		if entry.users == 0 {
			delete(l.locks, key)
		}
		l.mu.Unlock()
	}
	select {
	case <-ctx.Done():
		releaseRef()
		return nil, ctx.Err()
	case <-entry.token:
		return func() { entry.token <- struct{}{}; releaseRef() }, nil
	}
}

// Begin starts a write: it takes the retry lock and returns the stored response
// when this write already happened.
func Begin[T any](ctx context.Context, c Core, p auth.Principal) (*T, func(), error) {
	op := p.Write
	if op == nil {
		return nil, func() {}, nil
	}
	if strings.TrimSpace(op.Key) == "" || len(op.Key) > 128 || op.Scope == "" || op.ArgsHash == "" {
		return nil, func() {}, domain.InvalidRequest("error.idempotency.required", nil)
	}
	unlock, err := c.Locks.lock(ctx, WritePrincipal(p)+"/"+op.Scope+"/"+op.Key)
	if err != nil {
		return nil, func() {}, err
	}
	if p.Kind == auth.KindBot {
		if err := c.ValidateToolRun(ctx, p); err != nil {
			return nil, unlock, err
		}
	}
	raw, hit, err := sqlite.LookupIdempotency(ctx, c.DB, "resource."+op.Scope, WritePrincipal(p), op.Key, op.ArgsHash)
	if err != nil || !hit {
		return nil, unlock, err
	}
	var value T
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return nil, unlock, domain.Internal(errors.New("invalid persisted write response"))
	}
	return &value, unlock, nil
}

// SaveResult MUST be called before committing the transaction that
// creates/changes the resource. No post-commit dispatcher cache is involved.
func (c Core) SaveResult(ctx context.Context, tx *sql.Tx, p auth.Principal, value any) error {
	if p.Write == nil {
		return nil
	}
	if p.Kind == auth.KindBot {
		if err := c.ValidateToolRunOn(ctx, tx, p); err != nil {
			return err
		}
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if err := sqlite.SaveIdempotency(ctx, tx, "resource."+p.Write.Scope, WritePrincipal(p), p.Write.Key, p.Write.ArgsHash, string(raw)); err != nil {
		return err
	}
	return sqlite.PruneIdempotency(ctx, tx, time.Now().UTC().Add(-30*24*time.Hour))
}

// Only an already committed external-effect intent may update its outcome.
// A retry reads that outcome; it never repeats an uncertain RPC/file operation.
func (c Core) FinishIntent(ctx context.Context, p auth.Principal, value any) error {
	if p.Write == nil {
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	n, err := sqlite.UpdateIdempotencyResponse(ctx, c.DB, "resource."+p.Write.Scope, WritePrincipal(p), p.Write.Key, p.Write.ArgsHash, string(raw))
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("external write intent is missing")
	}
	return nil
}

// Record appends a durable event in its own short transaction, then
// mirrors it into the local ledger file. Subscribers never observe
// uncommitted state; a saturated ledger records one gap event instead of
// deleting un-imported evidence.
func (c Core) Record(ctx context.Context, ev domain.Event) error {
	stored, err := c.Insert(ctx, ev)
	if err != nil {
		return err
	}
	if c.Events == nil {
		return nil
	}
	if appendErr := c.Events.Append(stored); appendErr != nil {
		if errors.Is(appendErr, eventsink.ErrOverCapacity) && c.GapNoted.CompareAndSwap(false, true) {
			// The file budget is spent on un-imported evidence: the gap is
			// recorded in the database (the durable store), never by
			// deleting the evidence that is still only on disk.
			if _, gapErr := c.Insert(ctx, domain.Event{
				ID: domain.NewID(), Type: domain.EventLogGap, ResourceID: "events",
				Payload: MustJSON(map[string]any{"reason": "over_capacity"}),
			}); gapErr == nil {
				c.Hub.Publish()
			}
		}
		if c.Logger != nil {
			c.Logger.Warn("append event to local ledger", "error", appendErr)
		}
	}
	return nil
}

// Insert is Record without the ledger mirror; it exists so the gap
// event itself never recurses into appending.
func (c Core) Insert(ctx context.Context, ev domain.Event) (domain.Event, error) {
	tx, err := c.DB.BeginTx(ctx, nil)
	if err != nil {
		return domain.Event{}, fmt.Errorf("begin event tx: %w", err)
	}
	defer tx.Rollback()
	stored, err := sqlite.InsertEvent(ctx, tx, c.InstanceID, ev)
	if err != nil {
		return domain.Event{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Event{}, fmt.Errorf("commit event: %w", err)
	}
	return stored, nil
}

// MustJSON encodes a payload of known shape.
func MustJSON(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return data
}
