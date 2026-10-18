//go:build windows

package platform

import (
	"os/exec"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"
)

func TestBackgroundCommandKeepsOptionsAndOutput(t *testing.T) {
	cmd := exec.Command("cmd.exe", "/c", "echo", "background-pipe")
	options := &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
	cmd.SysProcAttr = options
	HideChildWindow(cmd)
	if cmd.SysProcAttr != options || !options.HideWindow || options.CreationFlags&windows.CREATE_NEW_PROCESS_GROUP == 0 || options.CreationFlags&windows.CREATE_NO_WINDOW == 0 {
		t.Fatalf("background process options lost: %+v", options)
	}
	out, err := cmd.Output()
	if err != nil || strings.TrimSpace(string(out)) != "background-pipe" {
		t.Fatalf("background stdout = %q, error = %v", out, err)
	}
}
