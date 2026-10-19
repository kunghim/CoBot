//go:build !darwin

package codex

// MinSupportedVersion retains the Windows-verified app-server baseline on
// platforms without a separate native compatibility result.
const MinSupportedVersion = "0.161.0"
