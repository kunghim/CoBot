// Package hookrun implements CoBot's hook adapter. A hook process is
// given one JSON request on stdin and answers with one JSON result on
// stdout. Only commands the user configured in the team's .cobot/hooks.json
// ever run — the adapter never invents hook names and never trusts an
// unconfigured one. Side-effect hooks additionally require a fresh local
// lease for the member they act for. Execution is honest about its limits:
// a command that already started cannot be rolled back, and the adapter says
// so rather than claiming isolation.
package hookrun

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/kunghim/CoBot/internal/platform"
)

// Request is one hook invocation read from stdin.
type Request struct {
	Name    string          `json:"name"`
	Payload json.RawMessage `json:"payload"`
}

// HookSpec is one user-configured hook: an exact argv (never a shell
// string), whether it performs side effects, and its timeout.
type HookSpec struct {
	Command        []string `json:"command"`
	SideEffect     bool     `json:"sideEffect"`
	TimeoutSeconds int      `json:"timeoutSeconds"`
}

// HookConfig is the team's .cobot/hooks.json.
type HookConfig struct {
	Hooks map[string]HookSpec `json:"hooks"`
}

// Result is the honest outcome written to stdout.
type Result struct {
	Status   string `json:"status"` // ok|skipped|refused|error
	Covered  bool   `json:"covered"`
	Executed bool   `json:"executed"`
	Reason   string `json:"reason,omitempty"`
	ExitCode *int   `json:"exitCode,omitempty"`
	TimedOut bool   `json:"timedOut,omitempty"`
	Output   string `json:"output,omitempty"`
}

// Exit codes: the engine (or a human) can branch on them without parsing.
const (
	ExitOK         = 0 // ok or honestly skipped
	ExitInvalid    = 1 // malformed request
	ExitRefused    = 3 // side-effect hook without a fresh lease
	ExitFailed     = 4 // configured command failed or timed out
	maxOutputBytes = 4096
)

// Run executes one hook request and writes the result. teamDir locates the
// user's trust configuration; leaseDir/botID/instanceID describe the member
// the hook acts for (side-effect gates only; empty values skip that gate and
// the result says why through behavior, not claims).
func Run(ctx context.Context, stdin io.Reader, stdout io.Writer, teamDir, leaseDir, botID, instanceID string) int {
	var req Request
	decoder := json.NewDecoder(stdin)
	if err := decoder.Decode(&req); err != nil || req.Name == "" {
		return writeResult(stdout, Result{Status: "error", Reason: "invalid_json"}, ExitInvalid)
	}

	config := HookConfig{Hooks: map[string]HookSpec{}}
	data, err := os.ReadFile(filepath.Join(teamDir, ".cobot", "hooks.json"))
	if err == nil {
		if json.Unmarshal(data, &config) != nil {
			// A broken config runs nothing; the name is reported uncovered.
			config = HookConfig{Hooks: map[string]HookSpec{}}
		}
	}
	spec, covered := config.Hooks[req.Name]
	if !covered {
		// Skipping is not protection; the result says the hook is simply not
		// covered by this installation, so the reported coverage stays real.
		return writeResult(stdout, Result{Status: "skipped", Covered: false, Reason: "hook_not_configured"}, ExitOK)
	}
	if len(spec.Command) == 0 {
		return writeResult(stdout, Result{Status: "error", Covered: true, Reason: "empty_command"}, ExitFailed)
	}

	if spec.SideEffect {
		if leaseDir == "" || botID == "" || instanceID == "" {
			return writeResult(stdout, Result{Status: "refused", Covered: true, Reason: "lease_identity_missing"}, ExitRefused)
		}
		if _, reason, _ := platform.ReadLeaseDir(leaseDir, botID, instanceID, time.Now()); reason != platform.LeaseReasonOK {
			return writeResult(stdout, Result{Status: "refused", Covered: true, Reason: "lease_" + reason}, ExitRefused)
		}
	}

	timeout := time.Duration(spec.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, spec.Command[0], spec.Command[1:]...)
	cmd.Stdin = bytes.NewReader(req.Payload)
	cmd.Env = append(os.Environ(), "COBOT_HOOK_NAME="+req.Name)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	runErr := cmd.Run()
	exitCode := 0
	if runErr != nil {
		exitCode = 1
		if exitError, ok := runErr.(*exec.ExitError); ok {
			exitCode = exitError.ExitCode()
		}
	}
	output := out.String()
	if len(output) > maxOutputBytes {
		output = output[:maxOutputBytes] + "…(truncated)"
	}
	if cmdCtx.Err() == context.DeadlineExceeded {
		// The command started; nothing claims it can be rolled back.
		return writeResult(stdout, Result{
			Status: "error", Covered: true, Executed: true,
			Reason: "timeout", TimedOut: true, Output: output,
		}, ExitFailed)
	}
	if runErr != nil {
		return writeResult(stdout, Result{
			Status: "error", Covered: true, Executed: true,
			Reason: "command_failed", ExitCode: &exitCode, Output: output,
		}, ExitFailed)
	}
	return writeResult(stdout, Result{Status: "ok", Covered: true, Executed: true, ExitCode: &exitCode, Output: output}, ExitOK)
}

func writeResult(stdout io.Writer, result Result, exit int) int {
	data, err := json.Marshal(result)
	if err != nil {
		fmt.Fprintln(stdout, `{"status":"error","reason":"marshal_failed"}`)
		return ExitInvalid
	}
	fmt.Fprintln(stdout, string(data))
	return exit
}
