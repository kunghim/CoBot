//go:build !windows

package codex

import "os/exec"

// hideChildWindow is a no-op outside Windows, which has no per-child console
// allocation to suppress.
func hideChildWindow(cmd *exec.Cmd) {}
