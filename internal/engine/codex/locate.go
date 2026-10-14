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

// MinSupportedVersion is the lowest app-server protocol version CoBot has
// verified. Older builds are reported, never silently substituted.
const MinSupportedVersion = "0.161.0"

// ErrNotFound reports that no codex binary could be located.
var ErrNotFound = errors.New("codex binary not found")

// Locate resolves the codex executable: an explicit configured path wins,
// otherwise PATH lookup. No health diagnostic that touches the legacy
// sandbox is ever run here.
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
		return abs, nil
	}
	found, err := exec.LookPath("codex")
	if err != nil {
		return "", fmt.Errorf("%w (not on PATH and no configured path)", ErrNotFound)
	}
	return found, nil
}

// ProbeVersion runs `codex --version` and returns the CLI version string.
func ProbeVersion(ctx context.Context, binary string) (string, error) {
	cmd := exec.CommandContext(ctx, binary, "--version")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
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
func EngineEnv(paths platform.Paths) []string {
	overrides := map[string]string{
		"CODEX_HOME":        paths.CodexHome(),
		"CODEX_SQLITE_HOME": paths.CodexSQLiteDir(),
	}
	base := os.Environ()
	out := make([]string, 0, len(base)+len(overrides))
	for _, kv := range base {
		key, _, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if _, isOverride := overrides[strings.ToUpper(key)]; isOverride {
			continue // never leak a pre-existing CODEX_HOME into the engine
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
