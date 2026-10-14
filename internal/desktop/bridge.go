//go:build desktop

// Package desktop implements the native bridge of the Wails shell: window and
// directory picking plus the startup hand-off. Every business action stays on
// loopback HTTP; nothing here touches tasks, approvals or the database.
package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/kunghim/CoBot/internal/bootstrap"
	"github.com/kunghim/CoBot/internal/domain"
	"github.com/kunghim/CoBot/internal/platform"
)

// BootstrapResult is the hand-off the page exchanges for API credentials.
type BootstrapResult struct {
	Secret   string `json:"secret"`
	Endpoint string `json:"endpoint"`
}

// Bridge is exposed to the frontend as window.go.desktop.Bridge. Its
// constructor is side-effect free; the core starts in OnStartup so Wails
// binding generation never creates profiles, locks or logins.
type Bridge struct {
	mu           sync.Mutex
	wailsCtx     context.Context
	core         *bootstrap.Core
	adopted      *adoptedInstance
	environment  platform.Environment
	version      string
	extraOrigins []string
}

// adoptedInstance points at a core that another process already owns; the
// desktop then reuses it through the same-user runtime credential instead of
// starting a second scheduler.
type adoptedInstance struct {
	endpoint    string
	adminSecret string
	dataRoot    string
}

// NewBridge creates a side-effect-free bridge.
func NewBridge(environment platform.Environment, version string, extraOrigins []string) *Bridge {
	return &Bridge{environment: environment, version: version, extraOrigins: extraOrigins}
}

// StartCore boots (or adopts) the product core. Called from OnStartup.
func (b *Bridge) StartCore(ctx context.Context, dataRoot string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	core, err := bootstrap.Start(ctx, bootstrap.Options{
		Environment:  b.environment,
		DataRoot:     dataRoot,
		Version:      b.version,
		ConsoleLog:   true,
		ExtraOrigins: b.extraOrigins,
	})
	if err == nil {
		b.core = core
		return nil
	}
	var already *platform.ErrAlreadyRunning
	if !errors.As(err, &already) {
		return err
	}
	// Reuse the running core: locate it through instance.json and hold the
	// runtime credential for bootstrap minting.
	paths, rerr := platform.Resolve(b.environment, dataRoot)
	if rerr != nil {
		return fmt.Errorf("resolve profile for adoption: %w", rerr)
	}
	adminSecret, serr := platform.ReadAdminSecret(paths)
	if serr != nil {
		return fmt.Errorf("read runtime credential of running core: %w", serr)
	}
	b.adopted = &adoptedInstance{endpoint: already.Endpoint, adminSecret: adminSecret, dataRoot: paths.Root}
	return nil
}

// Shutdown stops a core this process owns; an adopted core is left alone.
func (b *Bridge) Shutdown(ctx context.Context) error {
	b.mu.Lock()
	core := b.core
	b.core = nil
	b.mu.Unlock()
	if core != nil {
		return core.Shutdown(ctx)
	}
	return nil
}

// Bootstrap hands the page a fresh one-time secret plus the loopback
// endpoint. With an owned core the secret comes from the in-memory issuer;
// with an adopted core it is minted through the runtime credential.
func (b *Bridge) Bootstrap() (*BootstrapResult, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.core != nil {
		secret, err := b.core.Issuer.Issue()
		if err != nil {
			return nil, err
		}
		return &BootstrapResult{Secret: secret, Endpoint: b.core.Endpoint}, nil
	}
	if b.adopted != nil {
		secret, err := mintAdoptedBootstrap(b.adopted)
		if err != nil {
			return nil, err
		}
		return &BootstrapResult{Secret: secret, Endpoint: b.adopted.endpoint}, nil
	}
	return nil, fmt.Errorf("core is not started yet")
}

// DataRoot exposes the profile root for the settings surface.
func (b *Bridge) DataRoot() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.core != nil {
		return b.core.Paths.Root
	}
	if b.adopted != nil {
		return b.adopted.dataRoot
	}
	return ""
}

func mintAdoptedBootstrap(adopted *adoptedInstance) (string, error) {
	body := `{"requestId":"` + domain.NewID() + `"}`
	req, err := http.NewRequest(http.MethodPost, adopted.endpoint+"/internal/v1/session/link", strings.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Instance-Secret", adopted.adminSecret)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("contact running core: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("running core refused bootstrap minting (status %d)", resp.StatusCode)
	}
	var envelope struct {
		Data struct {
			BootstrapSecret string `json:"bootstrapSecret"`
		} `json:"data"`
	}
	if err := decodeJSON(resp, &envelope); err != nil {
		return "", err
	}
	if envelope.Data.BootstrapSecret == "" {
		return "", fmt.Errorf("running core returned an empty bootstrap secret")
	}
	return envelope.Data.BootstrapSecret, nil
}

// LogError reports a shell-level failure to the core logger when available.
func (b *Bridge) LogError(msg string, err error) {
	b.mu.Lock()
	core := b.core
	b.mu.Unlock()
	logger := slog.Default()
	if core != nil && core.Logger != nil {
		logger = core.Logger
	}
	logger.Error(msg, "error", err)
}

func decodeJSON(resp *http.Response, dst any) error {
	return json.NewDecoder(resp.Body).Decode(dst)
}

// SetContext stores the Wails application context used by dialogs and quit.
func (b *Bridge) SetContext(ctx context.Context) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.wailsCtx = ctx
}

// OpenDirectory shows the native directory picker and returns the chosen
// path, or an empty string when the user cancels. Directory selection is a
// host capability only: it does not widen any engine permission.
func (b *Bridge) OpenDirectory() (string, error) {
	b.mu.Lock()
	ctx := b.wailsCtx
	b.mu.Unlock()
	if ctx == nil {
		return "", fmt.Errorf("application context is not ready")
	}
	return wailsruntime.OpenDirectoryDialog(ctx, wailsruntime.OpenDialogOptions{
		Title: "Select team working directory",
	})
}

// Quit asks the host to shut down; OnShutdown then stops the owned core with
// the product's bounded exit rules.
func (b *Bridge) Quit() error {
	b.mu.Lock()
	ctx := b.wailsCtx
	b.mu.Unlock()
	if ctx == nil {
		return fmt.Errorf("application context is not ready")
	}
	wailsruntime.Quit(ctx)
	return nil
}
