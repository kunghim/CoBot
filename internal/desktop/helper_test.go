//go:build desktop

package desktop

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// The engine launches the MCP/hook bridge as a child process, and Windows
// gives every console-subsystem child a console window, which flashed a
// terminal next to the app on each team start. The desktop package therefore
// prefers the GUI-subsystem cobot-helper.exe.
func TestHelperResolutionPrefersWindowlessBinary(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windowless helper is a Windows artifact")
	}
	dir := t.TempDir()
	writeEmpty(t, filepath.Join(dir, "cobot.exe"))
	writeEmpty(t, filepath.Join(dir, "cobot-helper.exe"))
	got := resolveHelperBinary(dir)
	if filepath.Base(got) != "cobot-helper.exe" {
		t.Errorf("resolveHelperBinary = %q, want cobot-helper.exe", filepath.Base(got))
	}
}

// A development tree without the built helper must still start, falling back to
// the console binary rather than failing outright.
func TestHelperResolutionFallsBackToConsoleBinary(t *testing.T) {
	dir := t.TempDir()
	name := "cobot"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	writeEmpty(t, filepath.Join(dir, name))
	got := resolveHelperBinary(dir)
	if filepath.Base(got) != name {
		t.Errorf("resolveHelperBinary = %q, want %s", filepath.Base(got), name)
	}
}

func writeEmpty(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
}
