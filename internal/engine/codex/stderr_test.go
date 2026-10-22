package codex

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestCodexStderrKeepsItsOwnLevel(t *testing.T) {
	var out bytes.Buffer
	w := &stderrLog{logger: slog.New(slog.NewTextHandler(&out, &slog.HandlerOptions{Level: slog.LevelInfo}))}
	lines := strings.Join([]string{
		"2026-10-18T08:30:37.548133Z  INFO app_server.request: close",
		"2026-10-18T08:30:37.548133Z DEBUG reqwest::connect: starting new connection",
		"2026-10-18T08:30:38.355998Z ERROR codex_models_manager::manager: failed to refresh available models",
		"2026-10-18T08:30:38.355998Z  WARN codex_core: slow response",
		"thread 'main' panicked at src/main.rs",
	}, "\n") + "\n"
	if _, err := w.Write([]byte(lines)); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"failed to refresh available models", "slow response", "panicked"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q dropped from the core log:\n%s", want, got)
		}
	}
	for _, noise := range []string{"app_server.request", "starting new connection"} {
		if strings.Contains(got, noise) {
			t.Errorf("%q logged at info level:\n%s", noise, got)
		}
	}
	if !strings.Contains(got, "level=ERROR") {
		t.Errorf("a Codex error is not logged as an error:\n%s", got)
	}
}
