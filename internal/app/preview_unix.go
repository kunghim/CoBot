//go:build !windows

package app

import (
	"os/exec"
	"syscall"
)

// previewCommandProcess wraps a start command for this platform: a team's
// preview command is a shell line, not an argv slice. The process gets its
// own group so a stop can end the whole tree.
func previewCommandProcess(command string) *exec.Cmd {
	cmd := exec.Command("sh", "-c", command)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd
}

// killProcessTree ends the process group of the start shell.
func killProcessTree(cmd *exec.Cmd) error {
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
