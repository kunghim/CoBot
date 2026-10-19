// Package platform resolves profile locations and owns the environment-level
// instance lock. Nothing here touches business data.
package platform

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Environment selects the profile a core instance runs under.
type Environment string

const (
	EnvDevelopment Environment = "development"
	EnvProduction  Environment = "production"
)

// ParseEnvironment validates a user supplied profile name.
func ParseEnvironment(s string) (Environment, error) {
	switch Environment(strings.TrimSpace(s)) {
	case EnvDevelopment:
		return EnvDevelopment, nil
	case EnvProduction:
		return EnvProduction, nil
	default:
		return "", fmt.Errorf("unknown profile %q (want development or production)", s)
	}
}

// Paths holds every location owned by one profile. The profile root is never
// a team working directory; team resources live under a user-chosen path.
type Paths struct {
	Environment Environment
	Root        string
}

// DefaultRoot returns the conventional data root for an environment.
func DefaultRoot(env Environment) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	switch env {
	case EnvDevelopment:
		return filepath.Join(home, ".cobot-dev"), nil
	case EnvProduction:
		return filepath.Join(home, ".cobot"), nil
	default:
		return "", fmt.Errorf("unknown profile %q", env)
	}
}

// Resolve builds Paths for an environment, applying an explicit data root
// override when provided. Overrides are made absolute but not rewritten.
func Resolve(env Environment, dataRoot string) (Paths, error) {
	root := strings.TrimSpace(dataRoot)
	if root == "" {
		var err error
		root, err = DefaultRoot(env)
		if err != nil {
			return Paths{}, err
		}
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return Paths{}, fmt.Errorf("resolve data root: %w", err)
	}
	return Paths{Environment: env, Root: abs}, nil
}

func (p Paths) join(elem ...string) string {
	return filepath.Join(append([]string{p.Root}, elem...)...)
}

func (p Paths) ConfigFile() string     { return p.join("config.json") }
func (p Paths) DataDir() string        { return p.join("data") }
func (p Paths) DatabaseFile() string   { return p.join("data", "cobot.sqlite") }
func (p Paths) BackupsDir() string     { return p.join("backups") }
func (p Paths) EnginesDir() string     { return p.join("engines") }
func (p Paths) CodexDir() string       { return p.join("engines", "codex") }
func (p Paths) CodexHome() string      { return p.join("engines", "codex", "home") }
func (p Paths) CodexSQLiteDir() string { return p.join("engines", "codex", "sqlite") }
func (p Paths) RolesDir() string       { return p.join("roles") }

// AvatarUploadsDir keeps the pictures the user chose as avatars.
func (p Paths) AvatarUploadsDir() string { return p.join("avatars", "uploads") }

// AvatarsDir caches the downloaded avatar catalog of this profile.
func (p Paths) AvatarsDir() string    { return p.join("assets", "avatars") }
func (p Paths) WorkspacesDir() string { return p.join("workspaces") }

// BrowserDir keeps the teams' browser profiles (their login state).
func (p Paths) BrowserDir() string     { return p.join("browser") }
func (p Paths) RuntimeDir() string     { return p.join("runtime") }
func (p Paths) LockFile() string       { return p.join("runtime", "instance.lock") }
func (p Paths) InstanceFile() string   { return p.join("runtime", "instance.json") }
func (p Paths) CredentialsDir() string { return p.join("runtime", "credentials") }

// SessionsFile keeps signed-in browser sessions across core restarts.
func (p Paths) SessionsFile() string {
	return p.join("runtime", "credentials", "page-sessions.json")
}

func (p Paths) AdminSecretFile() string {
	return p.join("runtime", "credentials", "instance-admin.secret")
}
func (p Paths) EventsDir() string           { return p.join("events") }
func (p Paths) EventsImportedDir() string   { return p.join("events", "imported") }
func (p Paths) EventsQuarantineDir() string { return p.join("events", "quarantine") }
func (p Paths) LogsDir() string             { return p.join("logs") }
func (p Paths) CacheDir() string            { return p.join("cache") }
func (p Paths) WebViewDir() string          { return p.join("webview") }

// Reserved returns paths that must never be bound as a team directory because
// they hold credentials or engine state.
func (p Paths) Reserved() []string {
	return []string{p.Root, p.CodexDir(), p.RuntimeDir(), p.DataDir(), p.LogsDir(), p.EventsDir()}
}

// EnsureDirs creates the profile skeleton with owner-only permissions where
// credentials live. Existing directories are left untouched.
func (p Paths) EnsureDirs() error {
	dirs := []string{
		p.Root, p.DataDir(), p.BackupsDir(), p.EnginesDir(), p.CodexHome(), p.CodexSQLiteDir(),
		p.RolesDir(), p.WorkspacesDir(), p.RuntimeDir(), p.CredentialsDir(),
		p.EventsDir(), p.EventsImportedDir(), p.EventsQuarantineDir(),
		p.LogsDir(), p.CacheDir(), p.WebViewDir(),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create directory %s: %w", dir, err)
		}
	}
	return nil
}
