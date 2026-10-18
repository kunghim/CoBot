//go:build !windows

package codex

func resolveLauncher(path string) string { return path }
