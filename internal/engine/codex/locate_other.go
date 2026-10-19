//go:build !darwin

package codex

import "os/exec"

func locatePlatformBinary() (string, error) { return "", exec.ErrNotFound }
