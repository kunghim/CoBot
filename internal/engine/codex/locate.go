package codex

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/kunghim/CoBot/internal/engine"
	"github.com/kunghim/CoBot/internal/platform"
)

// ErrNotFound reports that the desktop app has no usable bundled engine.
var ErrNotFound = errors.New("ChatGPT desktop Codex engine not found")

// Locate only accepts the engine shipped inside the ChatGPT desktop app.
// An explicit path supports nonstandard app locations, not standalone CLIs.
func Locate(configuredPath string) (string, error) {
	configuredPath = strings.TrimSpace(configuredPath)
	if configuredPath != "" {
		abs, err := filepath.Abs(configuredPath)
		if err != nil {
			return "", fmt.Errorf("resolve configured codex path: %w", err)
		}
		info, err := os.Stat(abs)
		if err != nil || info.IsDir() {
			return "", fmt.Errorf("configured codex path is not a file: %s", abs)
		}
		resolved, err := filepath.EvalSymlinks(abs)
		if err != nil || !isDesktopEngine(resolved) {
			return "", fmt.Errorf("%w: configured path must be inside the ChatGPT desktop app", ErrNotFound)
		}
		return resolved, nil
	}
	found, err := locatePlatformBinary()
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrNotFound, err)
	}
	return found, nil
}

// ProbeVersion runs `codex --version` and returns the CLI version string.
func ProbeVersion(ctx context.Context, binary string) (string, error) {
	cmd := exec.CommandContext(ctx, binary, "--version")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	hideChildWindow(cmd)
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("codex --version: %w", err)
	}
	fields := strings.Fields(strings.TrimSpace(out.String()))
	if len(fields) < 2 {
		return "", fmt.Errorf("unexpected codex --version output: %q", out.String())
	}
	return fields[len(fields)-1], nil
}

// Probe builds the non-secret engine status for this profile.
func Probe(ctx context.Context, binary, version string) engine.Status {
	capabilities := []string{"app-server", "thread/start", "turn/start", "native-approvals", "account/login"}
	return engine.Status{
		BinaryPath:   binary,
		Version:      version,
		Protocol:     "app-server/stdio",
		Capabilities: capabilities,
	}
}

// EngineEnv builds the child process environment: the private per-profile
// CODEX_HOME and SQLite state root, never the user's daily Codex state. The
// returned slice replaces the child's environment entirely.
//
// secrets carries the API keys of the configured model providers, keyed by
// the environment variable name the provider's configuration refers to (see
// ProviderEnvKey). The key itself never reaches config.toml — only the
// variable name does.
func EngineEnv(paths platform.Paths, secrets map[string]string) []string {
	overrides := map[string]string{
		"CODEX_HOME":        paths.CodexHome(),
		"CODEX_SQLITE_HOME": paths.CodexSQLiteDir(),
	}
	for name, value := range secrets {
		if name == "" || value == "" {
			continue
		}
		overrides[name] = value
	}
	base := platformEngineEnv(os.Environ())
	out := make([]string, 0, len(base)+len(overrides))
	for _, kv := range base {
		key, _, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if _, isOverride := overrides[strings.ToUpper(key)]; isOverride {
			continue // never leak a pre-existing CODEX_HOME or provider key
		}
		out = append(out, kv)
	}
	for key, value := range overrides {
		out = append(out, key+"="+value)
	}
	return out
}

// ThreadConfig merges global engine config overrides (for example the
// Windows MXC sandbox selection) with per-thread settings.
func ThreadConfig(extra map[string]any) map[string]any {
	merged := map[string]any{}
	for k, v := range WindowsSandboxConfig() {
		merged[k] = v
	}
	for k, v := range extra {
		merged[k] = v
	}
	if len(merged) == 0 {
		return nil
	}
	return merged
}
