package integration

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/engine"
	"github.com/kunghim/CoBot/internal/engine/codex"
	"github.com/kunghim/CoBot/internal/platform"
)

// TestRealCodexHandshake is an explicit R-category smoke: it spawns the real
// codex app-server with a private per-profile CODEX_HOME (no daily state),
// performs the handshake, reads the account projection and model list, and
// exits without running any model turn. It only runs when COBOT_R_SMOKE=1 so
// default CI never touches an installed account.
func TestRealCodexHandshake(t *testing.T) {
	if os.Getenv("COBOT_R_SMOKE") != "1" {
		t.Skip("set COBOT_R_SMOKE=1 to run the real codex handshake smoke")
	}
	binary, err := codex.Locate("")
	if err != nil {
		t.Fatalf("locate codex: %v", err)
	}
	version, err := codex.ProbeVersion(context.Background(), binary)
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	t.Logf("codex binary=%s version=%s", binary, version)

	root := t.TempDir()
	paths, err := platform.Resolve(platform.EnvDevelopment, filepath.Join(root, "profile"))
	if err != nil {
		t.Fatal(err)
	}
	if err := paths.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	workdir := filepath.Join(root, "work")
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatal(err)
	}

	exited := make(chan error, 1)
	server, err := codex.Start(context.Background(), codex.ServerOptions{
		Binary: binary,
		Cwd:    workdir,
		Env:    codex.EngineEnv(paths),
		Client: codex.ClientInfo{Name: "cobot-smoke", Title: "CoBot R smoke", Version: "test"},
		Callbacks: engine.Callbacks{
			OnExit: func(err error) { exited <- err },
		},
	})
	if err != nil {
		t.Fatalf("start app-server: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()

	accountRaw, err := server.AccountRead(context.Background())
	if err != nil {
		t.Fatalf("account/read: %v", err)
	}
	t.Logf("account projection: %s", redactAccount(accountRaw))

	modelsRaw, err := server.ModelList(context.Background())
	if err != nil {
		t.Fatalf("model/list: %v", err)
	}
	var models struct {
		Models []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(modelsRaw, &models); err != nil {
		t.Fatalf("decode models: %v (%s)", err, string(modelsRaw))
	}
	if len(models.Models) == 0 {
		// Not logged in: an empty list is an honest outcome, not a failure.
		t.Logf("model/list returned no models (account likely not logged in yet)")
		return
	}
	for _, m := range models.Models {
		t.Logf("model: %v", m["model"])
	}
}

func redactAccount(raw json.RawMessage) string {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return "<unparseable>"
	}
	safe := map[string]any{}
	safe["requiresOpenaiAuth"] = m["requiresOpenaiAuth"]
	if account, ok := m["account"].(map[string]any); ok {
		safe["accountType"] = account["type"]
	}
	out, _ := json.Marshal(safe)
	return string(out)
}
