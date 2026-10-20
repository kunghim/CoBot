package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// configWriteParams mirrors the app-server's config/value/write request.
// Writing through the engine instead of editing config.toml by hand keeps
// Codex the only writer of its own file format — and, more importantly, lets
// it reject a bad value instead of silently falling back to defaults.
type configWriteParams struct {
	KeyPath       string `json:"keyPath"`
	Value         any    `json:"value"`
	MergeStrategy string `json:"mergeStrategy"`
	FilePath      string `json:"filePath,omitempty"`
	// ExpectedVersion guards against a concurrent writer; empty skips the
	// check.
	ExpectedVersion string `json:"expectedVersion,omitempty"`
}

type configWriteResult struct {
	Status   string `json:"status"`
	Version  string `json:"version"`
	FilePath string `json:"filePath"`
}

type configReadResult struct {
	Config map[string]any `json:"config"`
}

// writeConfigValue upserts one configuration key through the engine and
// returns the new file version. A rejected value comes back as an error that
// already explains the fix — Codex refuses a bad wire_api instead of dropping
// the whole configuration and quietly using its own default endpoint.
//
// The value is whatever the key holds: a table for a provider, a string for
// the provider the engine runs with.
func (s *Server) writeConfigValue(ctx context.Context, keyPath string, value any, mergeStrategy string) (configWriteResult, error) {
	var out configWriteResult
	err := s.client.Call(ctx, "config/value/write", configWriteParams{
		KeyPath:       keyPath,
		Value:         value,
		MergeStrategy: mergeStrategy,
	}, &out)
	if err != nil {
		return configWriteResult{}, fmt.Errorf("codex config write %s: %w", keyPath, err)
	}
	return out, nil
}

// readConfig reads the engine's effective configuration.
func (s *Server) readConfig(ctx context.Context) (map[string]any, error) {
	var out configReadResult
	if err := s.client.Call(ctx, "config/read", map[string]any{}, &out); err != nil {
		return nil, fmt.Errorf("codex config read: %w", err)
	}
	return out.Config, nil
}

// configModelProviders returns the engine's model provider table.
func configModelProviders(cfg map[string]any) map[string]any {
	raw, ok := cfg["model_providers"].(map[string]any)
	if !ok {
		return nil
	}
	return raw
}

// configErrorMessage extracts the engine's own explanation from a rejected
// config write, so the user sees what Codex would have said rather than a
// generic failure.
func configErrorMessage(err error) string {
	var rpcErr *RPCError
	if errors.As(err, &rpcErr) && rpcErr.Message != "" {
		return rpcErr.Message
	}
	return err.Error()
}

// configValidationFailed reports whether the engine rejected the value
// because it does not understand it, as opposed to a transport problem. The
// caller turns this into a request error instead of a server error: the
// user's input was wrong, not ours.
func configValidationFailed(err error) bool {
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) {
		return false
	}
	var data struct {
		ConfigWriteErrorCode string `json:"config_write_error_code"`
	}
	if err := json.Unmarshal(rpcErr.Data, &data); err != nil {
		return false
	}
	return data.ConfigWriteErrorCode == "configValidationError"
}

// IsConfigValidationError reports whether the engine refused a configuration
// value because it does not understand it.
func IsConfigValidationError(err error) bool { return configValidationFailed(err) }

// ConfigErrorMessage returns the engine's own explanation of a rejected
// configuration, so the user reads what Codex would have said.
func ConfigErrorMessage(err error) string { return configErrorMessage(err) }
