package dirpicker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// pick asks the frontmost application (normally the browser) to show the
// standard folder chooser, so it opens in front of the page the user
// clicked from. AppleScript error -128 is the user cancelling.
func pick(ctx context.Context, title string) (string, error) {
	prompt := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(title)
	script := `tell application (path to frontmost application as text) to POSIX path of (choose folder with prompt "` + prompt + `")`
	cmd := exec.CommandContext(ctx, "osascript", "-e", script)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if strings.Contains(stderr.String(), "-128") {
			return "", ErrCancelled
		}
		var notFound *exec.Error
		if errors.As(err, &notFound) {
			return "", ErrUnavailable
		}
		return "", fmt.Errorf("osascript: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	path := strings.TrimSpace(stdout.String())
	if len(path) > 1 {
		path = strings.TrimSuffix(path, "/")
	}
	return path, nil
}
