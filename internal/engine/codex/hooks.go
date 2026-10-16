package codex

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
)

// nativeHook is the native registry entry for CoBot's own guard.
type nativeHook struct {
	Key         string
	CurrentHash string
	Enabled     bool
	Trusted     bool
}

// findNativeHook reads the actual native registry, including native trust,
// and returns only the entry whose source file, event and command are exactly
// CoBot's. Project hooks in team directories never match.
func (s *Server) findNativeHook(ctx context.Context, cwd, sourcePath, command string) (nativeHook, bool, error) {
	var response struct {
		Data []struct {
			Errors []json.RawMessage `json:"errors"`
			Hooks  []struct {
				Key         string `json:"key"`
				Command     string `json:"command"`
				CurrentHash string `json:"currentHash"`
				Enabled     bool   `json:"enabled"`
				IsManaged   bool   `json:"isManaged"`
				TrustStatus string `json:"trustStatus"`
				SourcePath  string `json:"sourcePath"`
				EventName   string `json:"eventName"`
			} `json:"hooks"`
		} `json:"data"`
	}
	if err := s.client.Call(ctx, "hooks/list", map[string]any{"cwds": []string{cwd}}, &response); err != nil {
		return nativeHook{}, false, err
	}
	for _, entry := range response.Data {
		if len(entry.Errors) > 0 {
			return nativeHook{}, false, errors.New("native hook registry reported errors")
		}
		for _, hook := range entry.Hooks {
			if hook.EventName != "preToolUse" || hook.IsManaged || filepath.Clean(hook.SourcePath) != filepath.Clean(sourcePath) || hook.Command != command {
				continue
			}
			return nativeHook{Key: hook.Key, CurrentHash: hook.CurrentHash, Enabled: hook.Enabled, Trusted: hook.TrustStatus == "trusted"}, true, nil
		}
	}
	return nativeHook{}, false, nil
}

// NativeHookStatus reports CoBot's guard from the native registry. It does
// not turn configured/untrusted hooks into a protection claim.
func (s *Server) NativeHookStatus(ctx context.Context, cwd, sourcePath, command string) string {
	hook, found, err := s.findNativeHook(ctx, cwd, sourcePath, command)
	switch {
	case err != nil:
		return "unavailable"
	case !found:
		return "unconfigured"
	case !hook.Enabled:
		return "disabled"
	case !hook.Trusted:
		return "review_required"
	}
	return "trusted"
}

// TrustNativeHook records native trust for CoBot's own guard through the
// official config API, the same record /hooks review writes, then re-reads
// the registry. It trusts only the exact entry findNativeHook matches, at the
// hash Codex computed, and only in the private CODEX_HOME this server runs
// with. The caller must have verified that the hooks file is CoBot's own.
func (s *Server) TrustNativeHook(ctx context.Context, cwd, sourcePath, command string) (string, error) {
	hook, found, err := s.findNativeHook(ctx, cwd, sourcePath, command)
	if err != nil {
		return "unavailable", err
	}
	if !found {
		return "unconfigured", nil
	}
	if !hook.Enabled {
		return "disabled", nil
	}
	if hook.Trusted {
		return "trusted", nil
	}
	if hook.Key == "" || hook.CurrentHash == "" {
		return "review_required", errors.New("native hook entry has no key or hash")
	}
	// The key contains the hooks file path, whose dots a dotted keyPath would
	// split; writing it as one object member keeps it intact.
	edit := map[string]any{
		"keyPath":       "hooks.state",
		"mergeStrategy": "upsert",
		"value":         map[string]any{hook.Key: map[string]any{"trusted_hash": hook.CurrentHash}},
	}
	if err := s.client.Call(ctx, "config/batchWrite", map[string]any{"edits": []any{edit}, "reloadUserConfig": true}, nil); err != nil {
		return "review_required", err
	}
	return s.NativeHookStatus(ctx, cwd, sourcePath, command), nil
}
