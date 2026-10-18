//go:build desktop && !bindings

// Command cobot-desktop is the Wails v2 shell around the same core the Web
// build uses. It owns the window, native dialogs and the process lifecycle;
// every business action is loopback HTTP against the shared core.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/kunghim/CoBot/internal/desktop"
	"github.com/kunghim/CoBot/internal/platform"
	"github.com/kunghim/CoBot/internal/webui"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
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

	err = wails.Run(&options.App{
		Title:  "CoBot",
		Width:  1280,
		Height: 800,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 250, G: 250, B: 250, A: 1},
		OnStartup: func(ctx context.Context) {
			bridge.SetContext(ctx)
			if err := bridge.StartCore(ctx, ""); err != nil {
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
