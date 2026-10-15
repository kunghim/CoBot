package integration

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kunghim/CoBot/internal/bootstrap"
	"github.com/kunghim/CoBot/internal/platform"
)

func TestBootstrapUsesExplicitCompanionForMCPAndNativeHook(t *testing.T) {
	dir := t.TempDir()
	helper := filepath.Join(dir, "helper-cobot.exe")
	if err := os.WriteFile(helper, []byte("test companion; not executed"), 0o700); err != nil {
		t.Fatal(err)
	}
	core, err := bootstrap.Start(context.Background(), bootstrap.Options{Environment: platform.EnvDevelopment, DataRoot: filepath.Join(dir, "profile"), Listen: "127.0.0.1:0", Version: "test", HelperBinary: helper})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = core.Shutdown(context.Background()) })
	if core.App.MCPBinary != helper {
		t.Fatal(core.App.MCPBinary)
	}
	data, err := os.ReadFile(filepath.Join(core.Paths.CodexHome(), "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "helper-cobot.exe") || strings.Contains(string(data), "trusted_hash") {
		t.Fatal(string(data))
	}
}

func TestMissingCompanionDoesNotLeaveLockedProfile(t *testing.T) {
	root := filepath.Join(t.TempDir(), "profile")
	opts := bootstrap.Options{Environment: platform.EnvDevelopment, DataRoot: root, Listen: "127.0.0.1:0", HelperBinary: filepath.Join(t.TempDir(), "missing-helper")}
	if core, err := bootstrap.Start(context.Background(), opts); err == nil {
		core.Shutdown(context.Background())
		t.Fatal("accepted missing companion")
	}
	if _, err := os.Stat(filepath.Join(root, "runtime", "instance.lock")); !os.IsNotExist(err) {
		t.Fatal("left lock", err)
	}
}
