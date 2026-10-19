//go:build windows

package codex

import (
	"os/exec"
	"runtime"
	"testing"
)

// The desktop shell is a GUI program, but Windows still allocates a console
// for each child process. Without CREATE_NO_WINDOW the Codex app-server leaves
// a terminal window next to the app for the whole session.
func TestHideChildWindowSuppressesConsoleOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("console allocation only applies to Windows")
	}
	cmd := exec.Command("cmd.exe", "/c", "exit")
	hideChildWindow(cmd)
	if cmd.SysProcAttr == nil {
		t.Fatal("SysProcAttr not set: a console window would be created")
	}
	const createNoWindow = 0x08000000
	if cmd.SysProcAttr.CreationFlags&createNoWindow == 0 {
		t.Errorf("CreationFlags = %#x, want CREATE_NO_WINDOW set", cmd.SysProcAttr.CreationFlags)
	}
}

// Existing SysProcAttr set by a caller must be preserved, not replaced.
func TestHideChildWindowPreservesExistingSysProcAttr(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("SysProcAttr only applies to Windows")
	}
	cmd := exec.Command("cmd.exe", "/c", "exit")
	hideChildWindow(cmd)
	first := cmd.SysProcAttr
	hideChildWindow(cmd)
	if cmd.SysProcAttr != first {
		t.Error("SysProcAttr pointer replaced; flags set before the call would be lost")
	}
}
