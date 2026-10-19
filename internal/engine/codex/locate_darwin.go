//go:build darwin

package codex

import (
	"os"
	"os/exec"
	"path/filepath"
)

// Finder provides a minimal PATH. Check normal CLI installation directories
// after PATH lookup fails; never install a CLI or alter the user's shell.
func locatePlatformBinary() (string, error) {
	var candidates []string
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates,
			filepath.Join(home, ".local", "bin", "codex"),
			filepath.Join(home, ".cargo", "bin", "codex"))
	}
	candidates = append(candidates, "/opt/homebrew/bin/codex", "/usr/local/bin/codex")
	for _, candidate := range candidates {
		if found, err := exec.LookPath(candidate); err == nil {
			return found, nil
		}
	}
	return "", exec.ErrNotFound
}
