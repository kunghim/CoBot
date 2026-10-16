package hookrun

import (
	"crypto/sha256"
	"encoding/base64"
	encodingbinary "encoding/binary"
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
	"unicode/utf16"

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
// writes trust state itself; the app records trust through the engine after
// VerifyNativeConfig. A foreign or user-modified hooks file is preserved,
// never merged.
func EnsureNativeConfig(paths platform.Paths, binary string) error {
	command, err := NativeCommand(binary, paths.Root)
	if err != nil {
		return err
	}
	path := filepath.Join(paths.CodexHome(), "hooks.json")
	ownerPath := filepath.Join(paths.CodexHome(), ".cobot-hook-definition.sha256")
	if previous, err := os.ReadFile(path); err == nil {
		owner, ownerErr := os.ReadFile(ownerPath)
		previousHash := sha256.Sum256(previous)
		if ownerErr == nil && strings.TrimSpace(string(owner)) != hex.EncodeToString(previousHash[:]) {
			return errors.New("private Codex hooks.json was edited; preserved")
		}
		if ownerErr != nil && !os.IsNotExist(ownerErr) {
			return ownerErr
		}
		var owned struct {
			Description string `json:"description"`
			Hooks       map[string][]struct {
				Hooks []struct {
					Type    string `json:"type"`
					Command string `json:"command"`
				} `json:"hooks"`
			} `json:"hooks"`
		}
		if json.Unmarshal(previous, &owned) != nil || owned.Description != nativeDescription || len(owned.Hooks) != 1 || len(owned.Hooks["PreToolUse"]) != 1 || len(owned.Hooks["PreToolUse"][0].Hooks) != 1 || owned.Hooks["PreToolUse"][0].Hooks[0].Type != "command" {
			return errors.New("private Codex hooks.json contains user configuration; preserved")
		}
		// Upgrade only the exact pre-manifest generated command for this
		// binary/root. Never infer ownership from an arbitrary command substring.
		if ownerErr != nil && owned.Hooks["PreToolUse"][0].Hooks[0].Command != legacyNativeCommand(binary, paths.Root) {
			return errors.New("native hook ownership is unknown; preserved")
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
		hash := sha256.Sum256(data)
		return writeAtomic(ownerPath, []byte(hex.EncodeToString(hash[:])))
	}
	if err := writeAtomic(path, data); err != nil {
		return err
	}
	hash := sha256.Sum256(data)
	return writeAtomic(ownerPath, []byte(hex.EncodeToString(hash[:])))
}

// VerifyNativeConfig confirms the private hooks file is exactly the guard
// CoBot generated for this binary and data root, matching its ownership
// record. Only then may CoBot record native trust on the user's behalf; an
// edited or foreign file is left for the user to review.
func VerifyNativeConfig(paths platform.Paths, binary string) error {
	command, err := NativeCommand(binary, paths.Root)
	if err != nil {
		return err
	}
	actual, err := os.ReadFile(filepath.Join(paths.CodexHome(), "hooks.json"))
	if err != nil {
		return err
	}
	owner, err := os.ReadFile(filepath.Join(paths.CodexHome(), ".cobot-hook-definition.sha256"))
	if err != nil {
		return err
	}
	hash := sha256.Sum256(actual)
	if strings.TrimSpace(string(owner)) != hex.EncodeToString(hash[:]) {
		return errors.New("private Codex hooks.json was edited")
	}
	expected, err := json.MarshalIndent(nativeDefinition(command), "", "  ")
	if err != nil {
		return err
	}
	if string(actual) != string(expected) {
		return errors.New("private Codex hooks.json is not the current CoBot guard")
	}
	return nil
}

func nativeDefinition(command string) map[string]any {
	return map[string]any{"description": nativeDescription, "hooks": map[string]any{
		"PreToolUse": []any{map[string]any{"hooks": []any{map[string]any{
			"type": "command", "command": command, "timeout": 5,
			"statusMessage": "CoBot lease guard",
		}}}},
	}}
}

func NativeCommand(binary, root string) (string, error) {
	if !filepath.IsAbs(binary) || !filepath.IsAbs(root) {
		return "", errors.New("native hook paths must be absolute")
	}
	quote := func(value string) (string, error) {
		if strings.ContainsAny(value, "\r\n\x00") {
			return "", errors.New("invalid native hook path")
		}
		return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'", nil
	}
	if runtime.GOOS == "windows" {
		if strings.ContainsAny(binary+root, "\r\n\x00") {
			return "", fmt.Errorf("invalid native hook path")
		}
		psQuote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }
		// Explicit PowerShell works when Codex launches command hooks through
		// either cmd or PowerShell. Encoded argv avoids nested shell expansion.
		script := "& " + psQuote(binary) + " hook --native-codex --data-root " + psQuote(root) + "; exit $LASTEXITCODE"
		units := utf16.Encode([]rune(script))
		raw := make([]byte, len(units)*2)
		for i, unit := range units {
			encodingbinary.LittleEndian.PutUint16(raw[i*2:], unit)
		}
		return "powershell.exe -NoLogo -NoProfile -NonInteractive -EncodedCommand " + base64.StdEncoding.EncodeToString(raw), nil
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

func legacyNativeCommand(binary, root string) string {
	if runtime.GOOS == "windows" {
		return "\"" + binary + "\" hook --native-codex --data-root \"" + root + "\""
	}
	return "'" + strings.ReplaceAll(binary, "'", "'\"'\"'") + "' hook --native-codex --data-root '" + strings.ReplaceAll(root, "'", "'\"'\"'") + "'"
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
