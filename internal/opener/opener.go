// Package opener hands a local file to the operating system: open it with
// its default program, or show it in the file manager. Callers decide which
// paths are allowed; this package only launches.
package opener

import (
	"os/exec"
	"path/filepath"
	"runtime"
)

// Open opens path with the system's default program for it.
func Open(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", path)
	case "darwin":
		cmd = exec.Command("open", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	return start(cmd)
}

// Reveal shows path selected in the system file manager.
func Reveal(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		// explorer parses its own command line: /select, and the path must
		// reach it as one argument.
		cmd = exec.Command("explorer", "/select,"+path)
	case "darwin":
		cmd = exec.Command("open", "-R", path)
	default:
		cmd = exec.Command("xdg-open", filepath.Dir(path))
	}
	return start(cmd)
}

func start(cmd *exec.Cmd) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
