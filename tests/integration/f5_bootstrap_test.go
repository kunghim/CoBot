package integration

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/auth"
	"github.com/kunghim/CoBot/internal/bootstrap"
	"github.com/kunghim/CoBot/internal/hookrun"
	"github.com/kunghim/CoBot/internal/platform"
)

func TestCoreShutdownClosesConnectedEventStream(t *testing.T) {
	core, err := bootstrap.Start(context.Background(), bootstrap.Options{Environment: platform.EnvDevelopment, DataRoot: t.TempDir(), Listen: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = core.Shutdown(context.Background()) })
	token, err := core.Bearer.Issue(auth.LocalUserID)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodGet, core.Endpoint+"/api/v1/events?after=0", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token.Token)
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatal(response.Status)
	}
	reader := bufio.NewReader(response.Body)
	if line, err := reader.ReadString('\n'); err != nil || !strings.HasPrefix(line, ": connected") {
		t.Fatalf("stream did not open: %q %v", line, err)
	}
	drained := make(chan error, 1)
	go func() { _, err := io.Copy(io.Discard, reader); drained <- err }()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := core.Shutdown(ctx); err != nil {
		t.Fatalf("connected stream blocked graceful shutdown: %v", err)
	}
	select {
	case err := <-drained:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("event stream stayed open after shutdown")
	}
}

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
	expected, err := hookrun.NativeCommand(helper, core.Paths.Root)
	if err != nil {
		t.Fatal(err)
	}
	var definition map[string]any
	if err := json.Unmarshal(data, &definition); err != nil {
		t.Fatal(err)
	}
	command := definition["hooks"].(map[string]any)["PreToolUse"].([]any)[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)["command"]
	if command != expected || strings.Contains(string(data), "trusted_hash") {
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
