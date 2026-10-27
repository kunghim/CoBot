package latex

import (
	"context"
	"os/exec"
)

func isolatedCommand(ctx context.Context, compiler, cache, project, output, input string) (*exec.Cmd, error) {
	profile, err := sandboxProfile(compiler, cache, project, output)
	if err != nil {
		return nil, err
	}
	return exec.CommandContext(ctx, "/usr/bin/sandbox-exec", "-p", profile, compiler, "--untrusted", "--only-cached", "--outdir", output, input), nil
}
