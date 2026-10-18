//go:build windows

package platform

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// HideChildWindow keeps background CLI utilities from allocating a visible
// console when their parent is a GUI application. Existing options survive.
func HideChildWindow(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_NO_WINDOW
	cmd.SysProcAttr.HideWindow = true
}
