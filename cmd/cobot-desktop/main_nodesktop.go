//go:build !desktop

// Headless builds never compile the Wails shell. `go build ./...` and CI use
// this stub so the shared core stays testable without GUI toolchains.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "cobot-desktop requires the desktop build tag: wails build -tags desktop (see scripts/build.*)")
	os.Exit(1)
}
