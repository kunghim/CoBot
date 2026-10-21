package hookrun

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kunghim/CoBot/internal/platform"
)

// TestHookChildHelper is the configured command in these tests: it reports
// that it ran by writing a marker file, then answers with a fixed line. The
// helper-process pattern keeps the tests free of platform-specific binaries.
func TestHookChildHelper(t *testing.T) {
	if os.Getenv("COBOT_HOOK_CHILD") != "1" {
		return
	}
	var payload bytes.Buffer
	_, _ = payload.ReadFrom(os.Stdin)
	if marker := os.Getenv("COBOT_HOOK_MARKER"); marker != "" {
		_ = os.WriteFile(marker, payload.Bytes(), 0o644)
	}
	_, _ = os.Stdout.WriteString("child-ran\n")
	os.Exit(0)
}

func writeConfig(t *testing.T, teamDir string, config HookConfig) {
	t.Helper()
	dir := filepath.Join(teamDir, ".cobot")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(config)
	if err := os.WriteFile(filepath.Join(dir, "hooks.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func runHookForTest(t *testing.T, teamDir, leaseDir, botID, instance string, request Request) (int, Result) {
	t.Helper()
	in, _ := json.Marshal(request)
	var out bytes.Buffer
	code := Run(context.Background(), bytes.NewReader(in), &out, teamDir, leaseDir, botID, instance)
	var result Result
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("parse result %q: %v", out.String(), err)
	}
	return code, result
}

func childSpec(sideEffect bool) HookSpec {
	return HookSpec{
		Command:    []string{os.Args[0], "-test.run=TestHookChildHelper"},
		SideEffect: sideEffect,
	}
}

// TestHookAdapterReportsRealCoverage: only user-configured commands
// run, an unknown name is honestly uncovered (not "blocked"), and a side
// effect without a fresh lease is refused before anything executes.
func TestHookAdapterReportsRealCoverage(t *testing.T) {
	teamDir := t.TempDir()
	leaseRoot := t.TempDir()
	leaseDir := platform.LeaseDir(leaseRoot)
	marker := filepath.Join(t.TempDir(), "marker")
	t.Setenv("COBOT_HOOK_CHILD", "1")
	t.Setenv("COBOT_HOOK_MARKER", marker)

	// Malformed input is an error, never a silent skip.
	var out bytes.Buffer
	code := Run(context.Background(), bytes.NewReader([]byte("{")), &out, teamDir, leaseDir, "bot-1", "instance-a")
	if code != ExitInvalid {
		t.Fatalf("malformed stdin exit = %d, want %d", code, ExitInvalid)
	}

	// An unconfigured name runs nothing and says it is not covered.
	code, result := runHookForTest(t, teamDir, leaseDir, "bot-1", "instance-a", Request{Name: "unknown-hook"})
	if code != ExitOK || result.Covered || result.Reason != "hook_not_configured" {
		t.Fatalf("unconfigured = code %d result %+v, want honest skip", code, result)
	}

	// A configured side-effect hook without a lease is refused, and the
	// command never starts.
	writeConfig(t, teamDir, HookConfig{Hooks: map[string]HookSpec{"before_save": childSpec(true)}})
	code, result = runHookForTest(t, teamDir, leaseDir, "bot-1", "instance-a", Request{Name: "before_save"})
	if code != ExitRefused || result.Status != "refused" || result.Reason != "lease_missing" {
		t.Fatalf("lease-less side effect = code %d result %+v, want refused/lease_missing", code, result)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a refused side-effect hook must never start its command")
	}

	// A stale lease is refused for the same reason, locally.
	stale := platform.Lease{BotID: "bot-1", TeamID: "team-1", InstanceID: "instance-a", UpdatedAt: time.Now().Add(-platform.LeaseTTL - time.Second)}
	if err := platform.WriteLease(leaseRoot, stale); err != nil {
		t.Fatal(err)
	}
	code, result = runHookForTest(t, teamDir, leaseDir, "bot-1", "instance-a", Request{Name: "before_save"})
	if code != ExitRefused || result.Reason != "lease_expired" {
		t.Fatalf("stale lease = code %d reason %q, want refused/lease_expired", code, result.Reason)
	}

	// Fresh lease: the configured command runs and its output is reported.
	fresh := stale
	fresh.UpdatedAt = time.Now()
	if err := platform.WriteLease(leaseRoot, fresh); err != nil {
		t.Fatal(err)
	}
	code, result = runHookForTest(t, teamDir, leaseDir, "bot-1", "instance-a", Request{Name: "before_save", Payload: json.RawMessage(`{"k":"v"}`)})
	if code != ExitOK || !result.Executed || strings.TrimSpace(result.Output) != "child-ran" {
		t.Fatalf("fresh lease = code %d result %+v, want executed child", code, result)
	}
	markerBytes, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("command did not run: %v", err)
	}
	if string(markerBytes) != `{"k":"v"}` {
		t.Fatalf("child stdin payload = %q, want the hook payload", markerBytes)
	}

	// A non-side-effect hook needs no lease: reads (and read-like hooks) keep
	// working without one.
	writeConfig(t, teamDir, HookConfig{Hooks: map[string]HookSpec{"on_note": childSpec(false)}})
	emptyLeaseDir := t.TempDir()
	code, result = runHookForTest(t, teamDir, emptyLeaseDir, "bot-1", "instance-a", Request{Name: "on_note"})
	if code != ExitOK || !result.Executed {
		t.Fatalf("read-like hook without lease = code %d result %+v, want executed", code, result)
	}
}
