//go:build windows

package app

import (
	"os/exec"
)

// previewCommandProcess wraps a start command for this platform: a team's
// preview command is a shell line, not an argv slice.
func previewCommandProcess(command string) *exec.Cmd {
	return exec.Command("cmd", "/C", command)
}

// killProcessTree ends the process and its children.
func killProcessTree(cmd *exec.Cmd) error {
	kill := exec.Command("taskkill", "/T", "/F", "/PID", itoa(cmd.Process.Pid))
	return kill.Run()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
