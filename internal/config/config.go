// Package config persists only the startup parameters needed before the
// database can be opened. Business settings live in SQLite.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// File is the on-disk shape of config.json.
type File struct {
	DataRoot string `json:"dataRoot,omitempty"`
	Listen   string `json:"listen,omitempty"`
	LogLevel string `json:"logLevel,omitempty"`
	// AssetsBaseURL is where downloadable assets (the avatar catalog) are
	// published; empty means the CoBot repository.
	AssetsBaseURL string `json:"assetsBaseUrl,omitempty"`
}

// Load reads config.json; a missing file yields zero values (all defaults).
func Load(path string) (File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return File{}, nil
		}
		return File{}, fmt.Errorf("read config: %w", err)
	}
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return File{}, fmt.Errorf("decode config: %w", err)
	}
	f.DataRoot = strings.TrimSpace(f.DataRoot)
	f.Listen = strings.TrimSpace(f.Listen)
	f.LogLevel = strings.TrimSpace(f.LogLevel)
	f.AssetsBaseURL = strings.TrimSpace(f.AssetsBaseURL)
	return f, nil
}

// Save writes config.json atomically.
func Save(path string, f File) error {
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}
