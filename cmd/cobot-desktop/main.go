//go:build desktop && !bindings

// Command cobot-desktop is the Wails v2 shell around the same core the Web
// build uses. It owns the window, native dialogs and the process lifecycle;
// every business action is loopback HTTP against the shared core.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/kunghim/CoBot/internal/desktop"
	"github.com/kunghim/CoBot/internal/platform"
	"github.com/kunghim/CoBot/internal/webui"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// version and environment are stamped by the build scripts. Development
// builds use the development profile; release builds pass production.
var (
	version     = "dev"
	environment = "development"
)

// desktopOrigins are the Wails asset-server origins admitted by the core's
// precise CORS allowlist (v2 serves the shell from wails.localhost).
var desktopOrigins = []string{
	"http://wails.localhost",
	"wails://wails",
	"http://wails.localhost:34115",
	"https://wails.localhost:34115",
	"http://localhost:34115",
	"http://127.0.0.1:34115",
}

func main() {
	// Release builds keep their production default, while native acceptance
	// can use an explicit throwaway root without touching the user's profile.
	dataRoot := flag.String("data-root", "", "override the profile data directory")
	flag.Parse()
	env, err := platform.ParseEnvironment(environment)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	assets, err := webui.Assets()
	if err != nil {
		fmt.Fprintln(os.Stderr, "frontend assets missing; run scripts/build first:", err)
		os.Exit(1)
	}
	bridge := desktop.NewBridge(env, version, desktopOrigins)
	var windowCtx context.Context

	err = wails.Run(&options.App{
		Title:  "CoBot",
		Width:  1280,
		Height: 800,
		AssetServer: &assetserver.Options{
			Assets: assets,
			// WKWebView keeps scheme-handler responses across rebuilds and
			// app upgrades: a cached index.html keeps loading the previous
			// bundle's hashed assets (also cached, though no longer embedded),
			// so the window shows the old UI after every update. Nothing here
			// is worth caching — the assets are local.
			Middleware: func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Cache-Control", "no-store")
					next.ServeHTTP(w, r)
				})
			},
		},
		BackgroundColour: &options.RGBA{R: 250, G: 250, B: 250, A: 1},
		// The page draws its own column headers in CoBot's theme: Windows
		// drops the system frame, macOS keeps only the traffic lights over
		// the page. Linux keeps its system bar. The mac title stays enabled
		// (empty by default, set to the conversation name in fullscreen) so
		// the hover-revealed titlebar shows the title beside the traffic
		// lights instead of an empty strip.
		Frameless: runtime.GOOS == "windows",
		Mac: &mac.Options{TitleBar: &mac.TitleBar{
			TitlebarAppearsTransparent: true,
			HideTitleBar:               false,
			FullSizeContent:            true,
			// UseToolbar stays off: an attached toolbar turns the titlebar
			// into toolbar height, and the fullscreen-hover strip that slides
			// down under the menu bar reads as several times a normal
			// toolbar. The standard-height titlebar keeps the strip slim.
			UseToolbar:           false,
			HideToolbarSeparator: true,
		}},
		// One window per profile: a second launch hands over to the first
		// and exits. Without it the extra process adopts the running core
		// as a guest, and closing the first window stops the core under it.
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: instanceID(env, *dataRoot),
			OnSecondInstanceLaunch: func(options.SecondInstanceData) {
				if windowCtx == nil {
					return
				}
				wailsruntime.WindowUnminimise(windowCtx)
				wailsruntime.WindowShow(windowCtx)
			},
		},
		OnStartup: func(ctx context.Context) {
			windowCtx = ctx
			bridge.SetContext(ctx)
			// The window title is drawn next to the traffic lights whenever
			// the titlebar shows (mac fullscreen hover); keep it empty except
			// when the frontend names the conversation in fullscreen.
			wailsruntime.WindowSetTitle(ctx, "")
			if err := bridge.StartCore(ctx, *dataRoot); err != nil {
				wailsruntime.MessageDialog(ctx, messageDialogError(err))
				wailsruntime.Quit(ctx)
			}
		},
		OnShutdown: func(ctx context.Context) {
			// Bounded exit: stop accepting work, wait for in-flight handlers,
			// then release storage and the instance lock (max 10s).
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := bridge.Shutdown(shutdownCtx); err != nil {
				slog.Error("core shutdown reported", "error", err)
			}
		},
		Bind: []any{bridge},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "wails:", err)
		os.Exit(1)
	}
}

// instanceID names the single-instance lock. It follows the data root, the
// same thing the core's own instance lock follows, so a throwaway profile used
// for testing does not collide with the installed app.
func instanceID(env platform.Environment, dataRoot string) string {
	sum := sha256.Sum256([]byte(string(env) + "\x00" + dataRoot))
	return "cobot-desktop-" + hex.EncodeToString(sum[:8])
}

func messageDialogError(err error) wailsruntime.MessageDialogOptions {
	return wailsruntime.MessageDialogOptions{
		Type:  wailsruntime.ErrorDialog,
		Title: "CoBot",
		Message: fmt.Sprintf(
			"The CoBot core could not start for profile %s.\n\n%v",
			environment, err,
		),
	}
}
