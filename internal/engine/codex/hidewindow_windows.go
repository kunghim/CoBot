//go:build windows

package codex

import (
	"github.com/kunghim/CoBot/internal/platform"
	"os/exec"
)

// hideChildWindow keeps the Codex app-server from flashing a console window.
//
// The engine speaks to Codex over stdin/stdout pipes, so a visible console
// carries no information for anyone. When CoBot itself is a GUI program
// (the Wails desktop shell) Windows still allocates a console for every child
// process, which leaves a terminal window sitting next to the app for as long
// as the engine runs. CREATE_NO_WINDOW suppresses it without changing how the
// pipes work. This also keeps background children windowless in the CLI build.
func hideChildWindow(cmd *exec.Cmd) {
	platform.HideChildWindow(cmd)
}
