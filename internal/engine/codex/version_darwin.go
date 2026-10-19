//go:build darwin

package codex

// MinSupportedVersion is the macOS CLI version verified in an ARM64 native
// smoke test: sign-in, model turns, task handoff, file tools and approvals.
// Windows has a separate baseline; other native platforms need acceptance.
const MinSupportedVersion = "0.146.0"
