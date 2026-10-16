package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kunghim/CoBot/internal/engine"
	"github.com/kunghim/CoBot/internal/engine/codex"
	"github.com/kunghim/CoBot/internal/hookrun"
	"github.com/kunghim/CoBot/internal/roles"
)

// controlServer is the profile-wide app-server used only for account and
// model operations (login coordinator). Business turns never run on it; it
// shares the environment's private CODEX_HOME so every team reuses one login.
type controlServer struct {
	mu     sync.Mutex
	server *codex.Server
	cwd    string
}

// EngineStatusInfo caching window; login changes arrive through the
// account/updated notification which invalidates immediately.
const engineStatusTTL = 30 * time.Second

// EngineStatus returns the current non-secret engine projection, probing on
// demand and caching briefly. It never fabricates a logged-in state.
func (a *App) EngineStatus(ctx context.Context) EngineStatusInfo {
	a.engineStatusMu.Lock()
	if cache := a.engineStatusCache; cache != nil && time.Since(cache.CheckedAt) < engineStatusTTL {
		a.engineStatusMu.Unlock()
		return a.withHookFailures(*cache)
	}
	a.engineStatusMu.Unlock()

	info := EngineStatusInfo{Status: "not_configured", CheckedAt: time.Now().UTC()}
	settings, err := a.GetSettings(ctx)
	if err != nil {
		info.Status = "error"
		info.ErrorKey = "error.engine.settings_unreadable"
		return info
	}
	binary, err := codex.Locate(settings.EnginePath)
	if err != nil {
		info.Status = "not_configured"
		info.ErrorKey = "error.engine.not_found"
		a.cacheEngineStatus(info)
		return info
	}
	info.BinaryPath = binary
	versionCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	version, err := codex.ProbeVersion(versionCtx, binary)
	cancel()
	if err != nil {
		info.Status = "error"
		info.ErrorKey = "error.engine.version_probe_failed"
		a.cacheEngineStatus(info)
		return info
	}
	info.Version = version
	if !codex.VersionSupported(version) {
		info.Status = "error"
		info.ErrorKey = "error.engine.version_unsupported"
		a.cacheEngineStatus(info)
		return info
	}
	probe := codex.Probe(ctx, binary, version)
	info.Protocol = probe.Protocol
	info.Capabilities = probe.Capabilities

	account, err := a.readAccount(ctx)
	if err != nil {
		// A control server that cannot start is a real capability problem,
		// reported as such; login state stays false, never optimistic.
		info.Status = "error"
		info.ErrorKey = "error.engine.control_unavailable"
		a.cacheEngineStatus(info)
		return info
	}
	info.Status = "ready"
	if server, err := a.control(ctx); err == nil {
		info.Capabilities = append(info.Capabilities, "codexHook:"+a.nativeHookState(ctx, server))
	}
	info.LoggedIn = account.LoggedIn
	info.AuthMode = account.AuthMode
	if info.LoggedIn {
		a.engineLoginMu.Lock()
		a.engineLogin = nil
		a.engineLoginMu.Unlock()
	}
	a.cacheEngineStatus(info)
	return a.withHookFailures(info)
}

// nativeHookState reports CoBot's guard and, when Codex only waits for
// review, records native trust itself: the guard is CoBot's own definition in
// CoBot's private CODEX_HOME, so asking the user to review it in a terminal
// adds no safety. Trust is recorded only after the hooks file is verified as
// exactly CoBot's; an edited file or a project hook is never trusted here.
func (a *App) nativeHookState(ctx context.Context, server *codex.Server) string {
	hookCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	command, err := hookrun.NativeCommand(a.MCPBinary, a.Paths.Root)
	if err != nil {
		return "unavailable"
	}
	cwd, source := a.Paths.WorkspacesDir(), filepath.Join(a.Paths.CodexHome(), "hooks.json")
	logger := a.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	state := server.NativeHookStatus(hookCtx, cwd, source, command)
	if state != "review_required" {
		return state
	}
	if err := hookrun.VerifyNativeConfig(a.Paths, a.MCPBinary); err != nil {
		logger.Warn("native hook left for review: hooks file is not CoBot's own", "error", err)
		return state
	}
	trusted, err := server.TrustNativeHook(hookCtx, cwd, source, command)
	if err != nil {
		logger.Warn("native hook trust failed", "error", err)
		return state
	}
	logger.Info("native hook trusted", "state", trusted)
	return trusted
}

// RetryEngineCheck drops the cached projection and re-runs the self-check,
// including native hook trust; the settings page offers it when a check
// failed.
func (a *App) RetryEngineCheck(ctx context.Context) EngineStatusInfo {
	a.invalidateEngineStatus()
	return a.EngineStatus(ctx)
}

// EngineStatusCached returns the last known projection without probing or
// spawning processes; snapshots use it to stay cheap.
func (a *App) EngineStatusCached() EngineStatusInfo {
	a.engineStatusMu.Lock()
	defer a.engineStatusMu.Unlock()
	if a.engineStatusCache != nil {
		return a.withHookFailures(*a.engineStatusCache)
	}
	return EngineStatusInfo{Status: "not_configured", CheckedAt: time.Now().UTC()}
}

func (a *App) withHookFailures(info EngineStatusInfo) EngineStatusInfo {
	failed := false
	a.hookFailures.Range(func(_, _ any) bool { failed = true; return false })
	if !failed {
		return info
	}
	info.Capabilities = append([]string(nil), info.Capabilities...)
	for i, value := range info.Capabilities {
		if strings.HasPrefix(value, "codexHook:") {
			info.Capabilities[i] = "codexHook:failed"
			return info
		}
	}
	info.Capabilities = append(info.Capabilities, "codexHook:failed")
	return info
}

func (a *App) cacheEngineStatus(info EngineStatusInfo) {
	a.engineStatusMu.Lock()
	a.engineStatusCache = &info
	a.engineStatusMu.Unlock()
}

// accountInfo is the safe projection of account/read.
type accountInfo struct {
	LoggedIn bool
	AuthMode string
}

func (a *App) readAccount(ctx context.Context) (accountInfo, error) {
	server, err := a.control(ctx)
	if err != nil {
		return accountInfo{}, err
	}
	rawCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	raw, err := server.AccountRead(rawCtx)
	if err != nil {
		return accountInfo{}, err
	}
	return parseAccount(raw), nil
}

// parseAccount extracts only non-secret discriminators from account/read.
func parseAccount(raw json.RawMessage) accountInfo {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return accountInfo{}
	}
	info := accountInfo{}
	if account, ok := m["account"].(map[string]any); ok {
		mode, _ := account["type"].(string)
		switch mode {
		case "chatgpt", "apiKey", "amazonBedrock":
			info.AuthMode = mode
			info.LoggedIn = true
		}
	}
	return info
}

// control lazily starts the account/model app-server for this profile.
func (a *App) control(ctx context.Context) (*codex.Server, error) {
	a.engineControlMu.Lock()
	defer a.engineControlMu.Unlock()
	if a.engineControl != nil && a.engineControl.server != nil {
		if err := a.engineControl.server.Ping(ctx); err == nil {
			return a.engineControl.server, nil
		}
		a.engineControl = nil
		a.engineLoginMu.Lock()
		a.engineLogin = nil
		a.engineLoginMu.Unlock()
	}
	if a.ControlFactory == nil {
		return nil, errors.New("engine factory is not configured")
	}
	cwd := a.Paths.WorkspacesDir()
	server, err := a.ControlFactory(ctx, cwd)
	if err != nil {
		return nil, fmt.Errorf("start control app-server: %w", err)
	}
	a.engineControl = &controlServer{server: server, cwd: cwd}
	return server, nil
}

// ShutdownControl stops the profile-wide control server during core exit.
func (a *App) ShutdownControl(ctx context.Context) error {
	a.engineControlMu.Lock()
	control := a.engineControl
	a.engineControl = nil
	a.engineControlMu.Unlock()
	if control != nil && control.server != nil {
		return control.server.Shutdown(ctx)
	}
	return nil
}

// StartEngineLogin triggers the official browser login on the control
// server; completion arrives as account/login/completed.
func (a *App) StartEngineLogin(ctx context.Context) (engine.LoginInfo, error) {
	server, err := a.control(ctx)
	if err != nil {
		return engine.LoginInfo{}, err
	}
	a.engineLoginMu.Lock()
	defer a.engineLoginMu.Unlock()
	if a.engineLogin != nil {
		return *a.engineLogin, nil
	}
	loginCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	info, err := server.AccountLoginStart(loginCtx)
	if err != nil {
		return engine.LoginInfo{}, err
	}
	a.engineLogin = &info
	a.invalidateEngineStatus()
	return info, nil
}

// CancelEngineLogin aborts a pending login.
func (a *App) CancelEngineLogin(ctx context.Context) error {
	server, err := a.control(ctx)
	if err != nil {
		return err
	}
	a.engineLoginMu.Lock()
	defer a.engineLoginMu.Unlock()
	if a.engineLogin == nil {
		return nil
	}
	cancelCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := server.AccountLoginCancel(cancelCtx, a.engineLogin.LoginID); err != nil {
		return err
	}
	a.engineLogin = nil
	a.invalidateEngineStatus()
	return nil
}

// EngineLoginCompleted runs outside the protocol reader, so a completion
// arriving before the start response cannot deadlock that response.
func (a *App) EngineLoginCompleted(raw json.RawMessage) {
	var result struct {
		LoginID string `json:"loginId"`
	}
	if json.Unmarshal(raw, &result) != nil {
		return
	}
	a.engineLoginMu.Lock()
	if a.engineLogin != nil && a.engineLogin.LoginID == result.LoginID {
		a.engineLogin = nil
	}
	a.engineLoginMu.Unlock()
	a.invalidateEngineStatus()
}

// EngineLogout ends the private environment session through the engine.
func (a *App) EngineLogout(ctx context.Context) error {
	server, err := a.control(ctx)
	if err != nil {
		return err
	}
	logoutCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := server.Call(logoutCtx, "account/logout", map[string]any{}, nil); err != nil {
		return err
	}
	a.invalidateEngineStatus()
	a.engineLoginMu.Lock()
	a.engineLogin = nil
	a.engineLoginMu.Unlock()
	return nil
}

// ModelList returns the engine-provided model list (non-secret).
func (a *App) ModelList(ctx context.Context) (json.RawMessage, error) {
	server, err := a.control(ctx)
	if err != nil {
		return nil, err
	}
	listCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	raw, err := server.ModelList(listCtx)
	if err != nil {
		return nil, err
	}
	return sanitizeModels(raw), nil
}

// sanitizeModels keeps only identifier-ish fields so no provider internals
// leak into the page.
func sanitizeModels(raw json.RawMessage) json.RawMessage {
	var payload struct {
		Models []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil || len(payload.Models) == 0 {
		return json.RawMessage(`{"models":[]}`)
	}
	clean := make([]map[string]any, 0, len(payload.Models))
	for _, m := range payload.Models {
		entry := map[string]any{}
		if model, ok := m["model"].(string); ok {
			entry["slug"] = model
		}
		for _, key := range []string{"slug", "id", "displayName", "description", "family"} {
			if v, ok := m[key]; ok {
				switch v.(type) {
				case string, float64, bool:
					entry[key] = v
				}
			}
		}
		if len(entry) > 0 {
			clean = append(clean, entry)
		}
	}
	out, err := json.Marshal(map[string]any{"models": clean})
	if err != nil {
		return json.RawMessage(`{"models":[]}`)
	}
	return out
}

// RoleLibrary returns the embedded role catalog; the user may pick any of
// the five professional roles when creating or extending a team.
func (a *App) RoleLibrary(ctx context.Context) ([]map[string]any, error) {
	all, err := roles.Defaults()
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(all))
	for _, role := range all {
		out = append(out, map[string]any{
			"id":             role.ID,
			"nameKey":        role.NameKey,
			"name":           role.Name,
			"contentVersion": role.ContentVersion,
		})
	}
	return out, nil
}
