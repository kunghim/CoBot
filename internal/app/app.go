// Package app coordinates user and bot actions: transactions, permissions
// and event publication. Handlers and MCP dispatch both call these methods;
// neither executes SQL or engine RPC on its own.
package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"errors"
	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/avatars"
	"github.com/kunghim/CoBot/internal/browser"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/engine"
	"github.com/kunghim/CoBot/internal/engine/codex"
	"github.com/kunghim/CoBot/internal/events"
	"github.com/kunghim/CoBot/internal/eventsink"
	"github.com/kunghim/CoBot/internal/platform"
	"github.com/kunghim/CoBot/internal/store/sqlite"
	"github.com/kunghim/CoBot/internal/wiretrace"
	"sync/atomic"
)

// App is the application service shared by HTTP handlers, the MCP dispatcher
// and the desktop host. It is created once by bootstrap.
type App struct {
	DB    *sql.DB
	Paths platform.Paths
	Hub   *events.Hub
	// Live carries transient turn progress (steps, streamed text) to the
	// event stream; nil disables it.
	Live *events.LiveHub
	// OpenLocal launches a team file for the user; nil uses the system
	// opener (tests replace it).
	OpenLocal    func(path string, reveal bool) error
	InstanceID   string
	ServiceEpoch string
	Version      string
	Logger       *slog.Logger
	writeLocksMu sync.Mutex
	writeLocks   map[string]*writeLock

	// Scheduler dispatches queued deliveries to team app-servers; it is
	// assigned during composition so tests can inject a fake factory.
	Scheduler *Scheduler

	// CollaborationTokens is the bridge credential authority. Tokens are
	// minted when a thread starts and are only valid for this core lifetime.
	CollaborationTokens *auth.TokenStore
	// MCPBinary is the cobot executable the engine spawns as the bridge. It
	// is the same binary the core runs from, resolved at startup.
	MCPBinary string
	// MCPEndpoint is the loopback address the bridge calls back on.
	MCPEndpoint string

	// Trace is the developer recording (`cobot trace on`); the app adds who a
	// thread belongs to, so a recording can name both ends. Nil records nothing.
	Trace *wiretrace.Recorder

	// Events is the local durable event ledger (H08). Nil disables file
	// appending; the database stays the primary store either way.
	Events        *eventsink.Sink
	eventGapNoted atomic.Bool

	engineStatusMu    sync.Mutex
	engineStatusCache *EngineStatusInfo
	hookFailures      sync.Map // team ID -> observed failure of this profile's native guard

	engineControlMu sync.Mutex
	engineControl   *controlServer
	engineLoginMu   sync.Mutex
	engineLogin     *engine.LoginInfo
	// ControlFactory starts the profile-wide account/model app-server; it is
	// injected during composition so tests can substitute a fake.
	ControlFactory func(ctx context.Context, cwd string) (*codex.Server, error)

	// Avatars is this profile's downloaded avatar catalog; nil serves an
	// empty catalog and never downloads.
	Avatars *avatars.Catalog

	// Browser runs the teams' browsers for the members' browser tool; nil
	// answers the tool as unavailable.
	Browser *browser.Manager
}

// EngineStatusInfo is the cached non-secret engine projection.
type EngineStatusInfo struct {
	Status       string    `json:"status"`
	BinaryPath   string    `json:"binaryPath,omitempty"`
	Version      string    `json:"version,omitempty"`
	Protocol     string    `json:"protocol,omitempty"`
	LoggedIn     bool      `json:"loggedIn"`
	AuthMode     string    `json:"authMode,omitempty"`
	Capabilities []string  `json:"capabilities,omitempty"`
	ErrorKey     string    `json:"errorKey,omitempty"`
	CheckedAt    time.Time `json:"checkedAt"`
}

// Snapshot is the consistent read clients take before applying events.
type Snapshot struct {
	Seq      int64            `json:"seq"`
	Instance string           `json:"instanceId"`
	Settings domain.Settings  `json:"settings"`
	Teams    []TeamWithBots   `json:"teams"`
	Engine   EngineStatusInfo `json:"engine"`
}

// GetSettings returns the current business settings.
func (a *App) GetSettings(ctx context.Context) (domain.Settings, error) {
	s, err := sqlite.GetSettings(ctx, a.DB)
	return s, sqlite.NormalizeError(err)
}

// UpdateSettings validates, stores and records a settings change. Only the
// local user may change settings; bot principals are rejected server-side.
func (a *App) UpdateSettings(ctx context.Context, principal auth.Principal, patch domain.SettingsPatch, expectedRevision int64) (domain.Settings, error) {
	cached, unlock, writeErr := beginWrite[domain.Settings](ctx, a, principal)
	defer unlock()
	if writeErr != nil {
		return domain.Settings{}, writeErr
	}
	if cached != nil {
		return *cached, nil
	}

	if principal.Kind != auth.KindUser {
		return domain.Settings{}, domain.Forbidden("error.forbidden.user_only", nil)
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return domain.Settings{}, err
	}
	defer tx.Rollback()
	saved, err := sqlite.UpdateSettingsTx(ctx, tx, patch, expectedRevision)
	if err != nil {
		return domain.Settings{}, sqlite.NormalizeError(err)
	}
	if err := a.saveWriteResult(ctx, tx, principal, saved); err != nil {
		return domain.Settings{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Settings{}, err
	}
	if err := a.recordEvent(ctx, domain.Event{
		Type:       domain.EventSettingsUpdated,
		ResourceID: "settings",
		Revision:   saved.Revision,
		Payload:    mustJSON(map[string]any{"settings": saved}),
	}); err != nil {
		return domain.Settings{}, err
	}
	a.Hub.Publish()
	// A budget change can un-hold deliveries the old limit paused, so the
	// affected bots are re-woken immediately instead of waiting for a message
	// that may never come.
	if patch.BudgetScope != nil || patch.BudgetTokens != nil {
		a.ResumeBudgetPausedBots(ctx)
	}
	return saved, nil
}

// Snapshot returns entities plus the global cursor in one read. The engine
// field is the cached projection; the dedicated /engine endpoint probes.
func (a *App) Snapshot(ctx context.Context, principal auth.Principal) (Snapshot, error) {
	if principal.Kind != auth.KindUser {
		return Snapshot{}, domain.Forbidden("error.forbidden.user_only", nil)
	}
	tx, err := a.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Snapshot{}, err
	}
	defer tx.Rollback()
	settings, err := sqlite.GetSettings(ctx, tx)
	if err != nil {
		return Snapshot{}, err
	}
	seq, err := sqlite.GlobalSeq(ctx, tx)
	if err != nil {
		return Snapshot{}, err
	}
	teams, err := listTeamsOn(ctx, tx)
	if err != nil {
		return Snapshot{}, err
	}
	if err := tx.Commit(); err != nil {
		return Snapshot{}, err
	}
	return Snapshot{
		Seq:      seq,
		Instance: a.InstanceID,
		Settings: settings,
		Teams:    teams,
		Engine:   a.EngineStatusCached(),
	}, nil
}

// ListEvents pages durable events after a cursor for the caller's scope.
// The server validates the scope against the caller's identity; a client
// filter alone never widens visibility.
func (a *App) ListEvents(ctx context.Context, principal auth.Principal, scope domain.Scope, after int64, limit int) ([]domain.Event, error) {
	if principal.Kind != auth.KindUser {
		return nil, domain.Forbidden("error.forbidden.user_only", nil)
	}
	evs, err := sqlite.ListEvents(ctx, a.DB, scope, after, limit)
	return evs, sqlite.NormalizeError(err)
}

// recordEvent appends a durable event in its own short transaction, then
// mirrors it into the local ledger file (H08). Subscribers never observe
// uncommitted state; a saturated ledger records one gap event instead of
// deleting un-imported evidence.
func (a *App) recordEvent(ctx context.Context, ev domain.Event) error {
	stored, err := a.insertEvent(ctx, ev)
	if err != nil {
		return err
	}
	if a.Events == nil {
		return nil
	}
	if appendErr := a.Events.Append(stored); appendErr != nil {
		if errors.Is(appendErr, eventsink.ErrOverCapacity) && a.eventGapNoted.CompareAndSwap(false, true) {
			// The file budget is spent on un-imported evidence: the gap is
			// recorded in the database (the durable store), never by
			// deleting the evidence that is still only on disk (H08).
			if _, gapErr := a.insertEvent(ctx, domain.Event{
				ID: domain.NewID(), Type: domain.EventLogGap, ResourceID: "events",
				Payload: mustJSON(map[string]any{"reason": "over_capacity"}),
			}); gapErr == nil {
				a.Hub.Publish()
			}
		}
		if a.Logger != nil {
			a.Logger.Warn("append event to local ledger", "error", appendErr)
		}
	}
	return nil
}

// insertEvent is recordEvent without the ledger mirror; it exists so the gap
// event itself never recurses into appending.
func (a *App) insertEvent(ctx context.Context, ev domain.Event) (domain.Event, error) {
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return domain.Event{}, fmt.Errorf("begin event tx: %w", err)
	}
	defer tx.Rollback()
	stored, err := sqlite.InsertEvent(ctx, tx, a.InstanceID, ev)
	if err != nil {
		return domain.Event{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Event{}, fmt.Errorf("commit event: %w", err)
	}
	return stored, nil
}

func mustJSON(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return data
}
