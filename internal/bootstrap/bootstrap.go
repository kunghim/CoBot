// Package bootstrap is the single composition root: it owns lock, storage,
// auth stores, app services and HTTP server, and tears them down in a fixed
// order. Only this package wires the real Codex adapter and SQLite.
package bootstrap

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	apihttp "github.com/kunghim/CoBot/internal/api/http"
	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/config"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/events"
	"github.com/kunghim/CoBot/internal/logging"
	"github.com/kunghim/CoBot/internal/platform"
	"github.com/kunghim/CoBot/internal/store/sqlite"
	"github.com/kunghim/CoBot/internal/webui"
)

// Options configure one core start.
type Options struct {
	Environment     platform.Environment
	DataRoot        string
	Listen          string // e.g. 127.0.0.1:0
	Version         string
	ConsoleLog      bool
	LogLevel        slog.Level
	DevServerOrigin string   // extra allowed origin for the Vite dev server
	ExtraOrigins    []string // e.g. the desktop shell asset origin
}

// Core is a running product instance.
type Core struct {
	Paths       platform.Paths
	Config      config.File
	DB          *sql.DB
	App         *app.App
	Sessions    *auth.SessionStore
	Bearer      *auth.BearerStore
	Issuer      *auth.BootstrapIssuer
	Logger      *slog.Logger
	Endpoint    string
	AdminSecret string
	StartedAt   time.Time

	// StartupBootstrap is a fresh one-time page secret minted at start; the
	// serve command prints it as a fragment link. It expires in 60 seconds.
	StartupBootstrap string

	listener net.Listener
	server   *http.Server
	api      *apihttp.Server
	lock     *platform.Lock
	closeLog func() error
}

// Start brings up the core: lock, storage, migrations, HTTP listener.
func Start(ctx context.Context, opts Options) (*Core, error) {
	paths, err := platform.Resolve(opts.Environment, opts.DataRoot)
	if err != nil {
		return nil, err
	}
	if err := paths.EnsureDirs(); err != nil {
		return nil, err
	}
	cfg, err := config.Load(paths.ConfigFile())
	if err != nil {
		return nil, err
	}
	listen := opts.Listen
	if listen == "" {
		listen = cfg.Listen
	}
	if listen == "" {
		listen = "127.0.0.1:0"
	}

	lock, err := platform.AcquireLock(paths, 2*time.Second)
	if err != nil {
		return nil, err
	}

	fail := func(err error) (*Core, error) {
		lock.Release()
		return nil, err
	}

	db, err := sqlite.Open(paths.DatabaseFile())
	if err != nil {
		return fail(err)
	}
	migrator, err := sqlite.NewMigrator(db)
	if err != nil {
		db.Close()
		return fail(err)
	}
	if err := migrator.MigrateToLatest(ctx, paths.BackupsDir()); err != nil {
		db.Close()
		return fail(err)
	}

	instanceID, err := ensureInstanceID(ctx, db)
	if err != nil {
		db.Close()
		return fail(err)
	}

	level := opts.LogLevel
	logger, closeLog, err := logging.New(logging.Options{Dir: paths.LogsDir(), Level: level, Console: opts.ConsoleLog})
	if err != nil {
		db.Close()
		return fail(err)
	}

	listener, err := net.Listen("tcp", listen)
	if err != nil {
		closeLog()
		db.Close()
		return fail(fmt.Errorf("listen on %s: %w", listen, err))
	}
	endpoint := "http://" + listener.Addr().String()

	adminSecret, err := platform.WriteAdminSecret(paths)
	if err != nil {
		listener.Close()
		closeLog()
		db.Close()
		return fail(err)
	}

	serviceEpoch := domain.NewEpochID()
	info := platform.InstanceInfo{
		InstanceID:   instanceID,
		ServiceEpoch: serviceEpoch,
		Endpoint:     endpoint,
		Profile:      string(paths.Environment),
		PID:          os.Getpid(),
		StartedAt:    time.Now().UTC(),
	}
	if err := lock.WriteInstance(info); err != nil {
		listener.Close()
		closeLog()
		db.Close()
		return fail(err)
	}
	if err := lock.WriteLockRecord(info); err != nil {
		listener.Close()
		closeLog()
		db.Close()
		return fail(err)
	}

	hub := events.NewHub()
	application := &app.App{
		DB:           db,
		Paths:        paths,
		Hub:          hub,
		InstanceID:   instanceID,
		ServiceEpoch: serviceEpoch,
		Version:      opts.Version,
		Logger:       logger,
	}

	issuer := auth.NewBootstrapIssuer(auth.DefaultBootstrapTTL)
	sessions := auth.NewSessionStore(auth.DefaultSessionTTL)
	bearer := auth.NewBearerStore(auth.DefaultBearerTTL)

	allowed := apihttp.AllowedOriginsFor(paths.Environment, endpoint, opts.DevServerOrigin, opts.ExtraOrigins...)
	assets, err := webui.Assets()
	if err != nil {
		logger.Warn("frontend assets unavailable; API-only mode", "error", err)
	}
	apiServer := &apihttp.Server{
		App:            application,
		Paths:          paths,
		Sessions:       sessions,
		Bearer:         bearer,
		Issuer:         issuer,
		Hub:            hub,
		AdminSecret:    adminSecret,
		AllowedOrigins: allowed,
		Endpoint:       endpoint,
		Version:        opts.Version,
		StartedAt:      info.StartedAt,
		Logger:         logger,
		Assets:         assets,
	}

	startupSecret, err := issuer.Issue()
	if err != nil {
		listener.Close()
		closeLog()
		db.Close()
		return fail(err)
	}

	core := &Core{
		Paths:            paths,
		Config:           cfg,
		DB:               db,
		App:              application,
		Sessions:         sessions,
		Bearer:           bearer,
		Issuer:           issuer,
		Logger:           logger,
		Endpoint:         endpoint,
		AdminSecret:      adminSecret,
		StartedAt:        info.StartedAt,
		StartupBootstrap: startupSecret,
		listener:         listener,
		server:           &http.Server{Handler: apiServer.Handler()},
		api:              apiServer,
		lock:             lock,
		closeLog:         closeLog,
	}

	go func() {
		if err := core.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server stopped", "error", err)
		}
	}()
	logger.Info("core started",
		"profile", string(paths.Environment),
		"endpoint", endpoint,
		"instanceId", instanceID,
		"schema", sqlite.MaxSupportedVersion,
	)
	return core, nil
}

// SetEngineStatus plugs the engine status projection after the adapter is
// constructed (M2+), keeping composition in this package.
func (c *Core) SetEngineStatus(p apihttp.EngineStatusProvider) {
	if c.api != nil {
		c.api.EngineStatus = p
	}
}

// Shutdown stops accepting requests, waits for in-flight handlers within the
// caller's context, then releases storage and the instance lock. It never
// touches resources of another instance.
func (c *Core) Shutdown(ctx context.Context) error {
	var errs []error
	if c.server != nil {
		if err := c.server.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("http shutdown: %w", err))
		}
	}
	if c.DB != nil {
		if err := c.DB.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close database: %w", err))
		}
	}
	if c.closeLog != nil {
		if err := c.closeLog(); err != nil {
			errs = append(errs, fmt.Errorf("close logs: %w", err))
		}
	}
	if c.lock != nil {
		c.lock.Release()
	}
	return errors.Join(errs...)
}

// ensureInstanceID creates the stable profile identity on first run and
// reuses it forever after; team markers reference this value.
func ensureInstanceID(ctx context.Context, db *sql.DB) (string, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("begin meta tx: %w", err)
	}
	defer tx.Rollback()
	id, err := sqlite.GetMeta(ctx, tx, "profileInstanceId")
	if err == nil && id != "" {
		if err := tx.Commit(); err != nil {
			return "", err
		}
		return id, nil
	}
	if err != nil && !errors.Is(err, sqlite.ErrNotFound) {
		return "", err
	}
	id = domain.NewID()
	if err := sqlite.SetMeta(ctx, tx, "profileInstanceId", id); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit meta: %w", err)
	}
	return id, nil
}
