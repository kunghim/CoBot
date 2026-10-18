//go:build windows

// Command cobot-helper is the windowless MCP/hook bridge of the CoBot desktop
// package. Codex launches it as a child process, and Windows allocates a
// console window for every console-subsystem child, which flashed a terminal
// next to the app every time a team or member started. The desktop build links
// this entry point with the GUI subsystem (-H=windowsgui) so it never asks for
// one.
//
// Only the pipe-driven subcommands belong here. `cobot serve` prints its
// one-time startup link on stdout and stays in the console binary
// (cmd/cobot).
package main

import (
	"fmt"
	"os"

	"github.com/kunghim/CoBot/internal/cli"
)

// Keep the same build-time version stamp as the console executable.
var version = "dev"

func main() {
	args := os.Args[1:]
	if len(args) > 0 {
		switch args[0] {
		case "mcp", "hook":
			os.Exit(cli.Run(args, version))
		}
	}
	fmt.Fprintln(os.Stderr, "cobot-helper: expected the mcp or hook subcommand")
	os.Exit(2)
}
