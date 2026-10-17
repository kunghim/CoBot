// Command cobot is the standalone product executable: `cobot serve` runs the
// loopback core used by Web and the desktop shell; `cobot mcp`/`cobot hook`
// (added in later milestones) are helper subcommands of this same binary.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/bootstrap"
	"github.com/kunghim/CoBot/internal/collaboration"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/hookrun"
	"github.com/kunghim/CoBot/internal/platform"
	"github.com/kunghim/CoBot/internal/wiretrace"
)

// version is stamped by the build scripts via -ldflags.
var version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "serve":
		os.Exit(runServe(os.Args[2:]))
	case "mcp":
		os.Exit(runMCP(os.Args[2:]))
	case "hook":
		os.Exit(runHook(os.Args[2:]))
	case "trace":
		os.Exit(runTrace(os.Args[2:]))
	case "version", "--version", "-v":
		fmt.Println(version)
	case "help", "--help", "-h":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `CoBot — local multi-agent collaboration

Usage:
  cobot serve [--profile development|production] [--data-root <path>] [--listen host:port]
              [--log-level debug|info|warn|error] [--no-open]
  cobot version
  cobot mcp                      # MCP stdio bridge for one bot (spawned by the engine)
  cobot hook                     # run one configured hook from stdin JSON (H07)
  cobot trace on|off|status|view|protocol [--data-root <path>]
                                 # developer recording of raw exchanges (never commit it)

The core binds loopback only. `+"`serve`"+` opens the default browser with a
one-time startup link (also printed; --no-open only prints it); a second serve
on the same profile opens a fresh link to the running instance instead of
starting another scheduler.
`)
}

func runServe(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	profile := fs.String("profile", string(platform.EnvDevelopment), "data profile: development or production")
	dataRoot := fs.String("data-root", "", "override the profile data root (tests use explicit temp roots)")
	listen := fs.String("listen", "", "loopback listen address; default is a stable port per data root")
	logLevel := fs.String("log-level", "info", "log level: debug, info, warn or error")
	devOrigin := fs.String("dev-origin", "", "extra allowed browser origin (development Vite server)")
	noOpen := fs.Bool("no-open", false, "print the startup link without opening a browser")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	env, err := platform.ParseEnvironment(*profile)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	level := slog.LevelInfo
	switch *logLevel {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		fmt.Fprintf(os.Stderr, "unknown log level %q\n", *logLevel)
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	core, err := bootstrap.Start(ctx, bootstrap.Options{
		Environment:     env,
		DataRoot:        *dataRoot,
		Listen:          *listen,
		Version:         version,
		ConsoleLog:      true,
		LogLevel:        level,
		DevServerOrigin: *devOrigin,
		EngineSelfCheck: true,
	})
	if err != nil {
		var already *platform.ErrAlreadyRunning
		if errors.As(err, &already) {
			fmt.Fprintf(os.Stderr, "CoBot is already running for profile %s\n  endpoint: %s\n  (pid %d owns this profile; a second scheduler is never started)\n", env, already.Endpoint, already.PID)
			if link := mintLinkForRunningInstance(env, *dataRoot); link != "" {
				fmt.Printf("  fresh web link (one-time, 60s): %s\n", link)
				launchBrowser(link, *noOpen)
			}
			return 3
		}
		fmt.Fprintf(os.Stderr, "failed to start core: %v\n", err)
		return 1
	}

	fmt.Printf("CoBot core (%s) is running\n", version)
	fmt.Printf("  profile:   %s\n", env)
	fmt.Printf("  endpoint:  %s\n", core.Endpoint)
	fmt.Printf("  data root: %s\n", core.Paths.Root)
	link := core.Endpoint + "/#cobot-bootstrap=" + core.StartupBootstrap
	fmt.Printf("  web link:  %s\n", link)
	fmt.Println("  (the link is one-time and expires in 60 seconds; a second `cobot serve` mints a fresh one)")
	launchBrowser(link, *noOpen)

	select {
	case <-ctx.Done():
	case <-core.StopRequested:
	}
	fmt.Println("shutting down: stopping new work and waiting for in-flight requests (max 10s)...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := core.Shutdown(shutdownCtx); err != nil {
		fmt.Fprintf(os.Stderr, "shutdown reported: %v\n", err)
		return 1
	}
	fmt.Println("stopped")
	return 0
}

// launchBrowser opens the startup link unless the caller (scripts, tests)
// asked only for the printed link.
func launchBrowser(link string, noOpen bool) {
	if noOpen {
		return
	}
	if err := openBrowser(link); err != nil {
		fmt.Fprintf(os.Stderr, "  could not open a browser (%v); open the link above within 60 seconds\n", err)
		return
	}
	fmt.Println("  opened in your default browser")
}

// mintLinkForRunningInstance asks the live core for a fresh one-time startup
// secret using the same-user runtime credential, so a missed 60s link or a
// lost page session can be re-authorized without restarting the core.
func mintLinkForRunningInstance(env platform.Environment, dataRoot string) string {
	paths, err := platform.Resolve(env, dataRoot)
	if err != nil {
		return ""
	}
	info, err := platform.ReadInstance(paths)
	if err != nil {
		return ""
	}
	adminSecret, err := platform.ReadAdminSecret(paths)
	if err != nil {
		return ""
	}
	body, _ := json.Marshal(map[string]string{"requestId": domain.NewID()})
	req, err := http.NewRequest(http.MethodPost, info.Endpoint+"/internal/v1/session/link", bytes.NewReader(body))
	if err != nil {
		return ""
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Instance-Secret", adminSecret)
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var envelope struct {
		Data struct {
			BootstrapSecret string `json:"bootstrapSecret"`
			Endpoint        string `json:"endpoint"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return ""
	}
	if envelope.Data.BootstrapSecret == "" {
		return ""
	}
	return envelope.Data.Endpoint + "/#cobot-bootstrap=" + envelope.Data.BootstrapSecret
}

// runMCP serves the MCP stdio bridge for exactly one bot. The engine spawns
// it with the endpoint and the bot token in the environment; the token proves
// which bot is calling, so the bridge itself carries no identity. It never
// opens the database and holds no delivery queue: every call goes to the core,
// which owns the facts.
func runMCP(args []string) int {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	dataRoot := fs.String("data-root", "", "CoBot profile, for the developer recording")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	trace := wiretrace.New(*dataRoot, "mcp")
	defer trace.Close()
	endpoint := os.Getenv(collaboration.BridgeEnvEndpoint)
	token := os.Getenv(collaboration.BridgeEnvToken)
	if endpoint == "" || token == "" {
		fmt.Fprintln(os.Stderr, "cobot mcp: "+collaboration.BridgeEnvEndpoint+" and "+collaboration.BridgeEnvToken+
			" must be set by the engine; this subcommand is not meant to be run by hand")
		return 2
	}
	// A dedicated logger on stderr keeps stdout clean for the MCP stream.
	stderr := wiretrace.NewLines(trace, "mcp.stderr", "bridge→codex", "")
	defer stderr.Flush()
	logger := slog.New(slog.NewTextHandler(wiretrace.TeeWriter(os.Stderr, stderr), &slog.HandlerOptions{Level: slog.LevelWarn}))
	bridge := collaboration.NewBridge(endpoint, token, logger)
	bridge.SetTrace(trace)
	started, _ := json.Marshal(map[string]any{"args": os.Args, "botId": os.Getenv(auth.BridgeEnvBotID)})
	trace.Record("mcp.process", "start", "", started)
	if err := bridge.Run(context.Background()); err != nil && !errors.Is(err, io.EOF) {
		fmt.Fprintln(wiretrace.TeeWriter(os.Stderr, stderr), "cobot mcp:", err)
		trace.Record("mcp.process", "exit", "", []byte("1 "+err.Error()))
		return 1
	}
	trace.Record("mcp.process", "exit", "", []byte("0"))
	return 0
}

// runHook executes one user-configured hook from a stdin JSON request (H07).
// It never invents hook names: an unconfigured name is reported uncovered,
// and only commands present in the team's .cobot/hooks.json run. Side-effect
// hooks additionally require a fresh local lease for the member they act
// for. The result is one JSON object on stdout; see internal/hookrun.
func runHook(args []string) int {
	fs := flag.NewFlagSet("hook", flag.ContinueOnError)
	native := fs.Bool("native-codex", false, "handle Codex PreToolUse with a host-bound lease")
	root := fs.String("data-root", "", "explicit CoBot profile for the native hook")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *native {
		// The recording keeps what Codex handed the guard and its answer.
		trace := wiretrace.New(*root, "hook")
		defer trace.Close()
		in := wiretrace.NewLines(trace, "hook.stdin", "codex→hook", "")
		out := wiretrace.NewLines(trace, "hook.stdout", "hook→codex", "")
		code := hookrun.RunNativeCodex(wiretrace.TeeReader(os.Stdin, in), wiretrace.TeeWriter(os.Stdout, out), *root)
		in.Flush()
		out.Flush()
		trace.Record("hook.process", "exit", "", []byte(strconv.Itoa(code)))
		return code
	}
	return hookrun.Run(
		context.Background(), os.Stdin, os.Stdout,
		os.Getenv("COBOT_TEAM_DIR"), os.Getenv(auth.BridgeEnvLeaseDir),
		os.Getenv(auth.BridgeEnvBotID), os.Getenv("COBOT_INSTANCE_ID"),
	)
}
