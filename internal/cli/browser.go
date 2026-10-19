package cli

import (
	"os/exec"
	"runtime"
)

// openBrowser hands the one-time startup link to the user's default browser,
// which redeems it at once, so starting CoBot lands on a usable page. The link
// is still printed as a fallback when no browser can be launched.
func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		// url.dll keeps the fragment intact; cmd /c start would reparse it.
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
