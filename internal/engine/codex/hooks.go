package codex

import (
	"context"
	"encoding/json"
	"path/filepath"
)

// NativeHookStatus uses the actual native registry, including native trust.
// It does not turn configured/untrusted hooks into a protection claim.
func (s *Server) NativeHookStatus(ctx context.Context, cwd, sourcePath, command string) string {
	var response struct {
		Data []struct {
			Errors []json.RawMessage `json:"errors"`
			Hooks  []struct {
				Command     string `json:"command"`
				Enabled     bool   `json:"enabled"`
				TrustStatus string `json:"trustStatus"`
				SourcePath  string `json:"sourcePath"`
				EventName   string `json:"eventName"`
			} `json:"hooks"`
		} `json:"data"`
	}
	if err := s.client.Call(ctx, "hooks/list", map[string]any{"cwds": []string{cwd}}, &response); err != nil {
		return "unavailable"
	}
	for _, entry := range response.Data {
		if len(entry.Errors) > 0 {
			return "unavailable"
		}
		for _, hook := range entry.Hooks {
			if hook.EventName != "preToolUse" || filepath.Clean(hook.SourcePath) != filepath.Clean(sourcePath) || hook.Command != command {
				continue
			}
			if !hook.Enabled {
				return "disabled"
			}
			if hook.TrustStatus != "trusted" {
				return "review_required"
			}
			return "trusted"
		}
	}
	return "unconfigured"
}
