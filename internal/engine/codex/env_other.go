//go:build !darwin

package codex

func platformEngineEnv(base []string) []string { return base }
