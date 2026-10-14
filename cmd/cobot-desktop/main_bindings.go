//go:build bindings

// Binding-generation entrypoint: Wails compiles and runs the app with the
// `bindings` tag to introspect registered methods. It must not create
// profiles, take locks, start engines or log in — the Bridge constructor is
// side-effect free and OnStartup does nothing here.
package main

import (
	"context"

	"github.com/kunghim/CoBot/internal/desktop"
	"github.com/kunghim/CoBot/internal/platform"
	"github.com/kunghim/CoBot/internal/webui"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

var (
	version     = "dev"
	environment = "development"
)

func main() {
	assets, _ := webui.Assets()
	bridge := desktop.NewBridge(platform.EnvDevelopment, version, nil)
	_ = wails.Run(&options.App{
		Title:       "CoBot",
		Width:       1280,
		Height:      800,
		AssetServer: &assetserver.Options{Assets: assets},
		OnStartup:   func(ctx context.Context) { /* bindings only */ },
		Bind:        []any{bridge},
	})
}
