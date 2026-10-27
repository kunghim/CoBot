//go:build darwin

package codex

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func locatePlatformBinary() (string, error) {
	roots := []string{"/Applications/ChatGPT.app"}
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, filepath.Join(home, "Applications", "ChatGPT.app"))
	}
	// Registered apps may have been renamed or installed outside Applications.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "/usr/bin/mdfind", `kMDItemCFBundleIdentifier == "com.openai.codex" || kMDItemCFBundleIdentifier == "com.openai.chat"`).Output(); err == nil {
		roots = append(roots, strings.Split(strings.TrimSpace(string(out)), "\n")...)
	}
	return findDesktopEngine(roots)
}
