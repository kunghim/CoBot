//go:build !darwin

package latex

import (
	"context"
	"os/exec"
)

func isolatedCommand(context.Context, string, string, string, string, string) (*exec.Cmd, error) {
	return nil, ErrUnavailable
}
