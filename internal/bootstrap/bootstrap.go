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
	"path/filepath"
	"strconv"
	"sync"
	"time"

	apihttp "github.com/kunghim/CoBot/internal/api/http"
	"github.com/kunghim/CoBot/internal/app"
	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/avatars"
	"github.com/kunghim/CoBot/internal/browser"
	"github.com/kunghim/CoBot/internal/collaboration"
	"github.com/kunghim/CoBot/internal/config"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/engine"
	"github.com/kunghim/CoBot/internal/engine/codex"
	"github.com/kunghim/CoBot/internal/events"
	"github.com/kunghim/CoBot/internal/eventsink"
	"github.com/kunghim/CoBot/internal/hookrun"
	"github.com/kunghim/CoBot/internal/logging"
	"github.com/kunghim/CoBot/internal/platform"
	"github.com/kunghim/CoBot/internal/roles"
	"github.com/kunghim/CoBot/internal/secrets"
	"github.com/kunghim/CoBot/internal/store/sqlite"
	"github.com/kunghim/CoBot/internal/webui"
	"github.com/kunghim/CoBot/internal/wiretrace"
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
	HelperBinary    string   // desktop's same-version cobot companion; empty for the CLI itself
	// EngineSelfCheck probes Codex right after startup and records native
	// hook trust, so users never configure either by hand. Product entry
	// points enable it; tests leave it off and drive the engine themselves.
	EngineSelfCheck bool
	// AvatarSync checks for a newer avatar catalog right after startup.
	// Product entry points enable it; tests stay offline.
	AvatarSync bool
}

// Core is a running product instance.
type Core struct {
	Paths         platform.Paths
	Config        config.File
	DB            *sql.DB
	App           *app.App
	Sessions      *auth.SessionStore
	Bearer        *auth.BearerStore
	Issuer        *auth.BootstrapIssuer
	Logger        *slog.Logger
	Endpoint      string
	AdminSecret   string
	StartedAt     time.Time
	StopRequested <-chan struct{}

	// StartupBootstrap is a fresh one-time page secret minted at start; the
	// serve command prints it as a fragment link. It expires in 60 seconds.
	StartupBootstrap string

	listener    net.Listener
	server      *http.Server
	api         *apihttp.Server
	lock        *platform.Lock
	closeLog    func() error
	scheduler   *app.Scheduler
	stopStreams context.CancelFunc
}

// Start brings up the core: lock, storage, migrations, HTTP listener.
func Start(ctx context.Context, opts Options) (*Core, error) {
	helper := opts.HelperBinary
	if helper == "" {
		var err error
		helper, err = os.Executable()
		if err != nil {
			return nil, fmt.Errorf("resolve CoBot helper: %w", err)
		}
	}
	helper, err := filepath.Abs(helper)
	if err != nil {
		return nil, err
	}
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
	stablePort := listen == ""
	if stablePort {
		// Without an explicit address the profile keeps one port across
		// restarts, so a bookmarked page and its session keep working.
		listen = net.JoinHostPort("127.0.0.1", strconv.Itoa(preferredPort(paths)))
	}
	if err := loopbackOnly(listen); err != nil {
		return nil, err
	}

	lock, err := platform.AcquireLock(paths, 2*time.Second)
	if err != nil {
		return nil, err
	}

	fail := func(err error) (*Core, error) {
		lock.Release()
		return nil, err
	}
	if info, err := os.Stat(helper); err != nil || info.IsDir() {
		return fail(fmt.Errorf("CoBot helper is missing or not a file: %s", helper))
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
	if err != nil && stablePort {
		// The usual port is taken: use any free one and keep it from now on.
		listener, err = net.Listen("tcp", "127.0.0.1:0")
	}
	if err == nil && stablePort {
		rememberPort(paths, listener.Addr())
	}
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
		Live:         events.NewLiveHub(),
		InstanceID:   instanceID,
		ServiceEpoch: serviceEpoch,
		Version:      opts.Version,
		Logger:       logger,
	}
	// The avatar catalog is downloaded, not bundled. A bad address in
	// config.json falls back to the published one instead of failing startup.
	catalog, catalogErr := avatars.New(paths.AvatarsDir(), cfg.AssetsBaseURL)
	if catalogErr != nil {
		logger.Warn("assetsBaseUrl ignored", "error", catalogErr)
		catalog, _ = avatars.New(paths.AvatarsDir(), "")
	}
	application.Avatars = catalog
	application.Browser = browser.NewManager(paths.BrowserDir(), logger)

	// The credential store holds the model providers' API keys, encrypted
	// with the platform's key protection. A store that cannot be opened is
	// not fatal: the product still works, only the provider secrets do not.
	store, credErr := secrets.New(paths.CredentialsDir())
	if credErr != nil {
		logger.Warn("credential store unavailable", "error", credErr)
	} else {
		application.Credentials = store
	}

	// Engine composition: production uses the real Codex adapter; tests
	// replace the factories through the App fields before Start.
	if err := hookrun.EnsureNativeConfig(paths, helper); err != nil {
		// Never overwrite user hooks or bypass native trust to force coverage.
		logger.Warn("native hook registration unavailable", "error", err)
	}
	// The developer recording (`cobot trace on`) of this core: engine stdio
	// and every HTTP exchange. It records nothing until switched on.
	trace := wiretrace.New(paths.Root, "core")
	application.Trace = trace
	newServer := func(startCtx context.Context, cwd string, callbacks engine.Callbacks) (*codex.Server, error) {
		settings, err := application.GetSettings(startCtx)
		if err != nil {
			return nil, err
		}
		binary, err := codex.Locate(settings.EnginePath)
		if err != nil {
			return nil, err
		}
		// The provider secrets ride in the child's environment; the
		// configuration only names the variables, so the key itself never
		// reaches a file the user might open.
		secrets, err := application.ProviderEnv(startCtx)
		if err != nil {
			return nil, err
		}
		return codex.Start(startCtx, codex.ServerOptions{
			Binary:    binary,
			Cwd:       cwd,
			Env:       codex.EngineEnv(paths, secrets),
			Client:    codex.ClientInfo{Name: "cobot", Title: "CoBot", Version: opts.Version},
			Callbacks: callbacks,
			Logger:    logger,
			Trace:     trace,
		})
	}
	scheduler := app.NewScheduler(application, func(spec app.ServerSpec) (engine.Server, error) {
		return newServer(spec.Context, spec.Cwd, spec.Callbacks)
	})
	// A developer can shorten the idle engine shutdown to watch it happen
	// (for example COBOT_IDLE_ENGINE_TIMEOUT=30s); it is not a user setting.
	if raw := os.Getenv("COBOT_IDLE_ENGINE_TIMEOUT"); raw != "" {
		if timeout, err := time.ParseDuration(raw); err == nil {
			scheduler.IdleEngineTimeout = timeout
			logger.Info("idle engine timeout overridden", "timeout", timeout.String())
		} else {
			logger.Warn("ignoring COBOT_IDLE_ENGINE_TIMEOUT", "value", raw, "error", err)
		}
	}
	application.Scheduler = scheduler
	application.ControlFactory = func(startCtx context.Context, cwd string) (*codex.Server, error) {
		return newServer(startCtx, cwd, engine.Callbacks{
			OnNotification: func(n engine.Notification) {
				if n.Method == "account/login/completed" {
					go application.EngineLoginCompleted(n.Raw)
				}
				if n.Method == "account/updated" || n.Method == "account/login/completed" {
					application.EngineStatusInvalidated()
				}
			},
		})
	}
	startupComplete := false
	defer func() {
		if !startupComplete {
			_ = listener.Close()
			_ = closeLog()
			_ = db.Close()
			lock.Release()
		}
	}()
	// The local event ledger mirrors every recorded event to disk (H08).
	// Startup drains whatever previous runs left into the database — by event
	// id, so nothing duplicates — before reconciliation classifies state.
	sink, sinkErr := eventsink.Open(paths.Root, instanceID)
	if sinkErr != nil {
		return nil, fmt.Errorf("open event ledger: %w", sinkErr)
	}
	application.Events = sink
	if imported, ierr := application.ImportEventFiles(ctx); ierr != nil {
		return nil, fmt.Errorf("import event files: %w", ierr)
	} else if imported > 0 {
		logger.Info("imported events from local ledger", "count", imported)
	}
	// Classify whatever the previous process left behind before the loop can
	// claim anything: unknown runs, unknown deliveries, dead-connection
	// approvals, and one recovery card per unresolved run (H03/H05).
	if err := application.ReconcileAfterRestart(ctx); err != nil {
		return nil, fmt.Errorf("reconcile after restart: %w", err)
	}

	// Collaboration composition: the token store is the bridge credential
	// authority, and the dispatcher is the single permission path for MCP
	// tools. Both live in the core; the bridge holds no database handle.
	collabTokens := auth.NewTokenStore(0)
	application.CollaborationTokens = collabTokens
	dispatcher := collaboration.NewDispatcher(application, collabTokens)
	// The bridge is this same executable: the engine spawns `cobot mcp`, so
	// the binary must be the running one, not a PATH lookup that could pick a
	// different version.
	application.MCPBinary = helper
	application.MCPEndpoint = endpoint
	issuer := auth.NewBootstrapIssuer(auth.DefaultBootstrapTTL)
	sessions := auth.NewPersistentSessionStore(auth.DefaultSessionTTL, paths.SessionsFile())
	bearer := auth.NewBearerStore(auth.DefaultBearerTTL)

	allowed := apihttp.AllowedOriginsFor(paths.Environment, endpoint, opts.DevServerOrigin, opts.ExtraOrigins...)
	assets, err := webui.Assets()
	if err != nil {
		logger.Warn("frontend assets unavailable; API-only mode", "error", err)
	}
	stopRequested := make(chan struct{})
	var requestStopOnce sync.Once
	apiServer := &apihttp.Server{
		RequestShutdown: func() { requestStopOnce.Do(func() { close(stopRequested) }) },
		App:             application,
		Paths:           paths,
		Sessions:        sessions,
		Bearer:          bearer,
		Issuer:          issuer,
		Hub:             hub,
		AdminSecret:     adminSecret,
		AllowedOrigins:  allowed,
		Endpoint:        endpoint,
		Version:         opts.Version,
		StartedAt:       info.StartedAt,
		Logger:          logger,
		Assets:          assets,

		CollaborationTokens: collabTokens,
		Dispatcher:          dispatcher,
		EngineEpochOfTeam:   application.EngineEpochOfTeam,
	}

	startupSecret, err := issuer.Issue()
	if err != nil {
		return nil, err
	}

	streamContext, stopStreams := context.WithCancel(context.Background())
	apiServer.StreamStop = streamContext.Done()
	core := &Core{
		stopStreams:      stopStreams,
		StopRequested:    stopRequested,
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
		server:           &http.Server{Handler: wiretrace.Middleware(trace, apiServer.Handler())},
		api:              apiServer,
		lock:             lock,
		closeLog:         closeLog,
		scheduler:        scheduler,
	}

	// Ownership transfers only after all fallible startup work has finished;
	// no dispatch worker may outlive a failed Start call.
	startupComplete = true
	scheduler.Start()
	if opts.AvatarSync {
		application.StartAvatarSync()
	}
	go application.CleanAvatarUploads(context.Background())
	if opts.EngineSelfCheck {
		go func() {
			info := application.EngineStatus(ctx)
			logger.Info("engine self-check", "status", info.Status, "version", info.Version, "binary", info.BinaryPath, "loggedIn", info.LoggedIn, "errorKey", info.ErrorKey, "capabilities", info.Capabilities)
		}()
	}
	go func() {
		if err := core.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server stopped", "error", err)
		}
	}()
	// Bring the editable role library up to date at every start, not only
	// when a team is created: unedited copies of retired defaults upgrade,
	// user edits stay.
	if err := roles.EnsureProfileCopy(paths.RolesDir()); err != nil {
		logger.Warn("role library not refreshed", "error", err)
	}
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
	// Order: stop dispatch and engines first, then HTTP, then storage.
	if c.scheduler != nil {
		if err := c.scheduler.Stop(ctx); err != nil {
			errs = append(errs, fmt.Errorf("scheduler shutdown: %w", err))
		}
	}
	if c.App != nil && c.App.Browser != nil {
		c.App.Browser.Shutdown()
	}
	if c.App != nil {
		if err := c.App.ShutdownControl(ctx); err != nil {
			errs = append(errs, fmt.Errorf("control server shutdown: %w", err))
		}
	}
	if c.server != nil {
		// Event streams are intentionally long-lived. Finish them before
		// http.Shutdown waits for handlers, while storage is still available.
		if c.stopStreams != nil {
			c.stopStreams()
		}
		if err := c.server.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("http shutdown: %w", err))
			_ = c.server.Close()
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
