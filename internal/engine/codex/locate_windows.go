//go:build windows

package codex

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

func locatePlatformBinary() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	powershell := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	// The desktop app retains the OpenAI.Codex package identity after renaming.
	cmd := exec.CommandContext(ctx, powershell, "-NoProfile", "-NonInteractive", "-Command", `[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false); $ErrorActionPreference = 'Stop'; $apps = @(Get-AppxPackage | Where-Object { ($_.Name -eq 'OpenAI.Codex' -or $_.Name -match 'ChatGPT') -and $_.Publisher -match 'OpenAI' } | Sort-Object Version -Descending | ForEach-Object { $_.InstallLocation }); ConvertTo-Json -InputObject $apps -Compress`)
	hideChildWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	var roots []string
	if err := json.Unmarshal(out, &roots); err != nil {
		return "", err
	}
	return findDesktopEngine(roots)
}
