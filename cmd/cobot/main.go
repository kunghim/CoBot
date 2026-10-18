// Command cobot is the standalone product executable: `cobot serve` runs the
// loopback core used by Web and the desktop shell; `cobot mcp`/`cobot hook`
// are helper subcommands of this same binary.
//
// It links with the console subsystem so `cobot serve` can print its one-time
// startup link. The desktop package ships a separate windowless helper
// (cmd/cobot-helper) for the pipe-driven subcommands Codex launches.
package main

import (
	"os"

	"github.com/kunghim/CoBot/internal/cli"
)

// version is stamped by the build scripts via -ldflags.
var version = "dev"

func main() {
	os.Exit(cli.Run(os.Args[1:], version))
}
