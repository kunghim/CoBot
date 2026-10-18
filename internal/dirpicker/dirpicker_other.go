//go:build !windows && !darwin

package dirpicker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// pick uses zenity or kdialog when the desktop provides one. Both exit with
// status 1 when the user cancels.
func pick(ctx context.Context, title string) (string, error) {
	var cmd *exec.Cmd
	if path, err := exec.LookPath("zenity"); err == nil {
		cmd = exec.CommandContext(ctx, path, "--file-selection", "--directory", "--title="+title)
	} else if path, err := exec.LookPath("kdialog"); err == nil {
		cmd = exec.CommandContext(ctx, path, "--getexistingdirectory", ".", "--title", title)
	} else {
		return "", ErrUnavailable
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 {
			return "", ErrCancelled
		}
		return "", fmt.Errorf("folder chooser: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}
