package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

// Control is the profile-wide engine: accounts, model lists, the guard's trust,
// threads of teams without a running engine, and filesystem watches. Turns never
// run on it.
type Control interface {
	AuthProvider
	AccountLogout(ctx context.Context) error
	ModelList(ctx context.Context) (json.RawMessage, error)
	Ping(ctx context.Context) error
	Shutdown(ctx context.Context) error
	DeleteThread(ctx context.Context, threadID string) error
	NativeHookStatus(ctx context.Context, cwd, sourcePath, command string) string
	TrustNativeHook(ctx context.Context, cwd, sourcePath, command string) (string, error)
	WatchDirectory(ctx context.Context, path string) (<-chan struct{}, error)
}

// Installation finds and describes the engine on this machine.
type Installation interface {
	Locate(configuredPath string) (string, error)
	ProbeVersion(ctx context.Context, binary string) (string, error)
	Probe(ctx context.Context, binary, version string) Status
	// BuildCatalog makes the model catalog an engine is started with so it
	// knows models of other vendors. The empty catalog means none is needed.
	BuildCatalog(ctx context.Context, binary string, models []ClientModel) (Catalog, error)
}

// ClientModel is a model of another vendor an engine's catalog describes.
type ClientModel struct {
	Slug          string
	Name          string
	ContextWindow int
	// Input lists what the model takes besides text; the engine knows image only.
	Input []string
	// Toggle says thinking can be switched off; Efforts are its levels.
	Toggle  bool
	Efforts []string
	// DefaultEffort is the level used when none is chosen.
	DefaultEffort string
}

// Catalog is a generated model catalog.
type Catalog struct {
	// Path is the file the engine is started with.
	Path string
	// Fingerprint identifies its content; two engines started with the same
	// fingerprint know the same models.
	Fingerprint string
}

// ToolBridge is the process an engine spawns to offer CoBot's collaboration
// tools to one thread.
type ToolBridge struct {
	Name    string
	Command string
	Args    []string
	Env     map[string]string
	// Required makes the thread fail rather than run without the tools.
	Required bool
	// AutoApprove runs the tools without asking the user: they are CoBot's own
	// surface, and the core enforces who may do what.
	AutoApprove bool
}

// ProviderEnvKey is the environment variable that carries a provider's API
// key into the engine. The name is stable per provider so a key can be
// rotated without renaming the configuration, and the secret itself never
// reaches a configuration file; only this variable name does.
func ProviderEnvKey(providerID string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(providerID) {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return "COBOT_PROVIDER_" + b.String() + "_KEY"
}

// ConfigRejection is an engine error that may mean the engine refused a
// configuration value because it does not understand it.
type ConfigRejection interface {
	error
	// RejectedConfig gives the engine's own explanation, and whether this
	// is such a refusal rather than some other failure.
	RejectedConfig() (reason string, rejected bool)
}

// RejectedConfig reports whether err is the engine refusing a configuration.
func RejectedConfig(err error) (reason string, rejected bool) {
	var refusal ConfigRejection
	if errors.As(err, &refusal) {
		return refusal.RejectedConfig()
	}
	return "", false
}
