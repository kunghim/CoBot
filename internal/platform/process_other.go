//go:build !windows

package platform

import "os/exec"

func HideChildWindow(cmd *exec.Cmd) {}
