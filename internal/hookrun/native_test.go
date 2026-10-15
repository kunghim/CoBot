package hookrun

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/platform"
)

func TestNativeGuardRequiresExactLiveTurn(t *testing.T) {
	root := t.TempDir()
	binding := NativeBinding{ThreadID: "native-thread", TurnID: "native-turn", BotID: "bot", RunID: "run", EngineEpoch: "engine", InstanceID: "instance", ServiceEpoch: "service"}
	input := `{"hook_event_name":"PreToolUse","session_id":"native-thread","turn_id":"native-turn","tool_name":"Bash"}`
	check := func(want string) {
		t.Helper()
		var out bytes.Buffer
		code := RunNativeCodex(strings.NewReader(input), &out, root)
		if code != 0 {
			t.Fatal(code)
		}
		if want == "" {
			if strings.TrimSpace(out.String()) != "{}" {
				t.Fatal(out.String())
			}
		} else if !strings.Contains(out.String(), `"permissionDecision":"deny"`) || !strings.Contains(out.String(), want) {
			t.Fatal(out.String())
		}
	}
	check("turn_not_bound")
	if err := WriteNativeBinding(root, binding); err != nil {
		t.Fatal(err)
	}
	check("service_changed_or_stopped")
	data, _ := json.Marshal(platform.InstanceInfo{InstanceID: "instance", ServiceEpoch: "service"})
	if err := writeAtomic(filepath.Join(root, "runtime", "instance.json"), data); err != nil {
		t.Fatal(err)
	}
	check("lease_missing")
	lease := platform.Lease{BotID: "bot", RunID: "run", InstanceID: "instance", Epoch: "engine", UpdatedAt: time.Now()}
	if err := platform.WriteLease(root, lease); err != nil {
		t.Fatal(err)
	}
	check("")
	lease.RunID = "next-run"
	if err := platform.WriteLease(root, lease); err != nil {
		t.Fatal(err)
	}
	check("run_not_live")
	lease.RunID = "run"
	lease.UpdatedAt = time.Now().Add(-platform.LeaseTTL - time.Second)
	if err := platform.WriteLease(root, lease); err != nil {
		t.Fatal(err)
	}
	check("lease_expired")
	lease.UpdatedAt = time.Now()
	lease.Epoch = "other-engine"
	if err := platform.WriteLease(root, lease); err != nil {
		t.Fatal(err)
	}
	check("engine_changed")
	lease.Epoch = "engine"
	if err := platform.WriteLease(root, lease); err != nil {
		t.Fatal(err)
	}
	binding.ServiceEpoch = "previous-service"
	if err := WriteNativeBinding(root, binding); err != nil {
		t.Fatal(err)
	}
	check("service_changed_or_stopped")
	binding.ServiceEpoch = "service"
	if err := WriteNativeBinding(root, binding); err != nil {
		t.Fatal(err)
	}
	RemoveNativeBinding(root, binding.ThreadID, binding.TurnID)
	check("turn_not_bound")
}

func TestNativeConfigNeverTrustsOrOverwritesUserHooks(t *testing.T) {
	paths := platform.Paths{Root: t.TempDir()}
	binary := filepath.Join(paths.Root, "CoBot app", "cobot.exe")
	if err := EnsureNativeConfig(paths, binary); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(paths.CodexHome(), "hooks.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "trusted_hash") || !strings.Contains(string(data), "--native-codex") {
		t.Fatal(string(data))
	}
	if err := EnsureNativeConfig(paths, binary); err != nil {
		t.Fatal(err)
	}
	var edited map[string]any
	if err := json.Unmarshal(data, &edited); err != nil {
		t.Fatal(err)
	}
	edited["customUserField"] = true
	changed, _ := json.Marshal(edited)
	if err := os.WriteFile(path, changed, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := EnsureNativeConfig(paths, binary); err == nil {
		t.Fatal("overwrote edited owned file")
	}
	preserved, _ := os.ReadFile(path)
	if !bytes.Equal(changed, preserved) {
		t.Fatal("lost user edit")
	}
	custom := []byte(`{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"custom"}]}]}}`)
	if err := os.WriteFile(path, custom, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := EnsureNativeConfig(paths, binary); err == nil {
		t.Fatal("overwrote user hooks")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(after, custom) {
		t.Fatal(string(after))
	}
}

func TestSideEffectHookRequiresCompleteLeaseIdentity(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, HookConfig{Hooks: map[string]HookSpec{"effect": {Command: []string{"must-not-run"}, SideEffect: true}}})
	for _, fields := range [][3]string{{"", "bot", "instance"}, {dir, "", "instance"}, {dir, "bot", ""}} {
		code, result := runHookForTest(t, dir, fields[0], fields[1], fields[2], Request{Name: "effect"})
		if code != ExitRefused || result.Executed || result.Reason != "lease_identity_missing" {
			t.Fatal(code, result)
		}
	}
}
