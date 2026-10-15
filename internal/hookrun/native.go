package hookrun

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"time"

	"github.com/kunghim/CoBot/internal/platform"
)

// NativeBinding is written by the host after it binds an exact native turn.
// It contains no credential. Old turns cannot borrow a later turn's lease.
type NativeBinding struct {
	ThreadID     string `json:"threadId"`
	TurnID       string `json:"turnId"`
	BotID        string `json:"botId"`
	RunID        string `json:"runId"`
	EngineEpoch  string `json:"engineEpoch"`
	InstanceID   string `json:"instanceId"`
	ServiceEpoch string `json:"serviceEpoch"`
}

func bindingPath(root, threadID, turnID string) string {
	hash := sha256.Sum256([]byte(threadID + "\x00" + turnID))
	return filepath.Join(root, "runtime", "hook-turns", hex.EncodeToString(hash[:])+".json")
}

func WriteNativeBinding(root string, binding NativeBinding) error {
	if binding.ThreadID == "" || binding.TurnID == "" || binding.BotID == "" || binding.RunID == "" || binding.EngineEpoch == "" || binding.ServiceEpoch == "" || binding.InstanceID == "" {
		return errors.New("incomplete native hook binding")
	}
	data, err := json.Marshal(binding)
	if err != nil {
		return err
	}
	return writeAtomic(bindingPath(root, binding.ThreadID, binding.TurnID), data)
}

func RemoveNativeBinding(root, threadID, turnID string) {
	_ = os.Remove(bindingPath(root, threadID, turnID))
}

// RunNativeCodex speaks Codex 0.161's real PreToolUse contract. A normal
// deny is JSON on stdout with exit 0; exit 2 is the native blocking fallback.
// This never executes a user command, rewrites a tool, or grants approval.
func RunNativeCodex(stdin io.Reader, stdout io.Writer, root string) int {
	deny := func(reason string) int {
		result := map[string]any{"hookSpecificOutput": map[string]any{
			"hookEventName": "PreToolUse", "permissionDecision": "deny",
			"permissionDecisionReason": "CoBot lease guard: " + reason,
		}}
		if json.NewEncoder(stdout).Encode(result) != nil {
			return 2
		}
		return 0
	}
	var input struct {
		Event    string `json:"hook_event_name"`
		ThreadID string `json:"session_id"`
		TurnID   string `json:"turn_id"`
		ToolName string `json:"tool_name"`
	}
	if err := json.NewDecoder(io.LimitReader(stdin, 4<<20)).Decode(&input); err != nil || input.Event != "PreToolUse" || input.ThreadID == "" || input.TurnID == "" || input.ToolName == "" || root == "" {
		return deny("invalid_native_input")
	}
	data, err := os.ReadFile(bindingPath(root, input.ThreadID, input.TurnID))
	var binding NativeBinding
	if err != nil || json.Unmarshal(data, &binding) != nil || binding.ThreadID != input.ThreadID || binding.TurnID != input.TurnID || binding.BotID == "" || binding.RunID == "" || binding.EngineEpoch == "" || binding.ServiceEpoch == "" || binding.InstanceID == "" {
		return deny("turn_not_bound")
	}
	instance, err := platform.ReadInstance(platform.Paths{Root: root})
	if err != nil || instance.InstanceID != binding.InstanceID || instance.ServiceEpoch != binding.ServiceEpoch {
		return deny("service_changed_or_stopped")
	}
	lease, reason, err := platform.ReadLease(root, binding.BotID, binding.InstanceID, time.Now().UTC())
	if err != nil || reason != platform.LeaseReasonOK {
		return deny("lease_" + reason)
	}
	if lease.Epoch != binding.EngineEpoch {
		return deny("engine_changed")
	}
	if lease.RunID != binding.RunID {
		return deny("run_not_live")
	}
	if _, err := io.WriteString(stdout, "{}\n"); err != nil {
		return 2
	}
	return 0
}

const nativeDescription = "CoBot native lease guard (generated; review in Codex /hooks)"

// EnsureNativeConfig registers only in CoBot's private Codex home. It never
// writes trust state. Codex skips an untrusted definition until normal /hooks
// review. A foreign or user-modified hooks file is preserved, never merged.
func EnsureNativeConfig(paths platform.Paths, binary string) error {
	command, err := nativeCommand(binary, paths.Root)
	if err != nil {
		return err
	}
	path := filepath.Join(paths.CodexHome(), "hooks.json")
	if previous, err := os.ReadFile(path); err == nil {
		var owned struct {
			Description string `json:"description"`
			Hooks       map[string][]struct {
				Hooks []struct {
					Type    string `json:"type"`
					Command string `json:"command"`
				} `json:"hooks"`
			} `json:"hooks"`
		}
		if json.Unmarshal(previous, &owned) != nil || owned.Description != nativeDescription || len(owned.Hooks) != 1 || len(owned.Hooks["PreToolUse"]) != 1 || len(owned.Hooks["PreToolUse"][0].Hooks) != 1 || owned.Hooks["PreToolUse"][0].Hooks[0].Type != "command" || !strings.Contains(owned.Hooks["PreToolUse"][0].Hooks[0].Command, " hook --native-codex --data-root ") {
			return errors.New("private Codex hooks.json contains user configuration; preserved")
		}
		var actual, expected any
		_ = json.Unmarshal(previous, &actual)
		baseline, _ := json.Marshal(nativeDefinition(owned.Hooks["PreToolUse"][0].Hooks[0].Command))
		_ = json.Unmarshal(baseline, &expected)
		if !reflect.DeepEqual(actual, expected) {
			return errors.New("private Codex hooks.json was edited; preserved")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	data, err := json.MarshalIndent(nativeDefinition(command), "", "  ")
	if err != nil {
		return err
	}
	if previous, err := os.ReadFile(path); err == nil && string(previous) == string(data) {
		return nil
	}
	return writeAtomic(path, data)
}

func nativeDefinition(command string) map[string]any {
	return map[string]any{"description": nativeDescription, "hooks": map[string]any{
		"PreToolUse": []any{map[string]any{"hooks": []any{map[string]any{
			"type": "command", "command": command, "timeout": 5,
			"statusMessage": "CoBot lease guard",
		}}}},
	}}
}

func nativeCommand(binary, root string) (string, error) {
	if !filepath.IsAbs(binary) || !filepath.IsAbs(root) {
		return "", errors.New("native hook paths must be absolute")
	}
	quote := func(value string) (string, error) {
		if strings.ContainsAny(value, "\r\n\x00") {
			return "", errors.New("invalid native hook path")
		}
		if runtime.GOOS == "windows" {
			// Codex command hooks use the platform shell. Reject cmd expansion
			// characters rather than interpolating a path into executable code.
			if strings.ContainsAny(value, "\"%!^&|<>") {
				return "", fmt.Errorf("unsupported shell character in hook path")
			}
			return "\"" + value + "\"", nil
		}
		return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'", nil
	}
	bin, err := quote(binary)
	if err != nil {
		return "", err
	}
	dir, err := quote(root)
	if err != nil {
		return "", err
	}
	return bin + " hook --native-codex --data-root " + dir, nil
}

func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".hook-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
