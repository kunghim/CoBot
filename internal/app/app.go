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

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/engine/codex"
	"github.com/kunghim/CoBot/internal/events"
	"github.com/kunghim/CoBot/internal/platform"
	"github.com/kunghim/CoBot/internal/store/sqlite"
)

// App is the application service shared by HTTP handlers, the MCP dispatcher
// and the desktop host. It is created once by bootstrap.
type App struct {
	DB           *sql.DB
	Paths        platform.Paths
	Hub          *events.Hub
	InstanceID   string
	ServiceEpoch string
	Version      string
	Logger       *slog.Logger

	// Scheduler dispatches queued deliveries to team app-servers; it is
	// assigned during composition so tests can inject a fake factory.
	Scheduler *Scheduler

	engineStatusMu    sync.Mutex
	engineStatusCache *EngineStatusInfo

	engineControlMu sync.Mutex
	engineControl   *controlServer
	// ControlFactory starts the profile-wide account/model app-server; it is
	// injected during composition so tests can substitute a fake.
	ControlFactory func(cwd string) (*codex.Server, error)
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
	if principal.Kind != auth.KindUser {
		return domain.Settings{}, domain.Forbidden("error.forbidden.user_only", nil)
	}
	saved, err := sqlite.UpdateSettings(ctx, a.DB, patch, expectedRevision)
	if err != nil {
		return domain.Settings{}, sqlite.NormalizeError(err)
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
	return saved, nil
}

// Snapshot returns entities plus the global cursor in one read. The engine
// field is the cached projection; the dedicated /engine endpoint probes.
func (a *App) Snapshot(ctx context.Context, principal auth.Principal) (Snapshot, error) {
	if principal.Kind != auth.KindUser {
		return Snapshot{}, domain.Forbidden("error.forbidden.user_only", nil)
	}
	settings, err := a.GetSettings(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	seq, err := sqlite.GlobalSeq(ctx, a.DB)
	if err != nil {
		return Snapshot{}, err
	}
	teams, err := a.ListTeams(ctx, principal)
	if err != nil {
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

// recordEvent appends a durable event in its own short transaction and
// returns only after commit, so subscribers never observe uncommitted state.
func (a *App) recordEvent(ctx context.Context, ev domain.Event) error {
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin event tx: %w", err)
	}
	defer tx.Rollback()
	if _, err := sqlite.InsertEvent(ctx, tx, a.InstanceID, ev); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit event: %w", err)
	}
	return nil
}

func mustJSON(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return data
}
