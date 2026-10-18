# Builds distributable artifacts.
#   scripts/build.ps1 -Target server   -> standalone cobot executable with
#                                          embedded frontend/migrations/roles
#   scripts/build.ps1 -Target desktop  -> Wails desktop shell + same-version
#                                          helper cobot.exe (release profile)
# Output is summarized under .tmp/dist with SHA-256 checksums.
param(
    [Parameter(Mandatory = $true)][ValidateSet("server", "desktop")][string]$Target,
    [string]$Version = "0.1.0-dev",
    [string]$OutputDirectory
)
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root
$dist = if ($OutputDirectory) { [IO.Path]::GetFullPath($OutputDirectory) } else { Join-Path $root ".tmp\dist" }
New-Item -ItemType Directory -Force -Path $dist | Out-Null

if (-not (Test-Path "frontend\node_modules")) {
    Write-Host "==> npm ci --prefix frontend"
    npm ci --prefix frontend
    if ($LASTEXITCODE -ne 0) { throw "npm ci failed" }
}
Write-Host "==> frontend build (embedded assets)"
npm run build --prefix frontend
if ($LASTEXITCODE -ne 0) { throw "frontend build failed" }

function Write-Checksum([string]$file) {
    $hash = (Get-FileHash -Algorithm SHA256 $file).Hash.ToLower()
    "$hash  $(Split-Path -Leaf $file)" | Out-File -FilePath "$file.sha256" -Encoding ascii
    Write-Host "    sha256 $hash  $(Split-Path -Leaf $file)"
}

switch ($Target) {
    "server" {
        $out = Join-Path $dist "cobot.exe"
        Write-Host "==> go build (server, $Version)"
        go build -ldflags "-X main.version=$Version" -o $out ./cmd/cobot
        if ($LASTEXITCODE -ne 0) { throw "server build failed" }
        Write-Checksum $out
    }
    "desktop" {
        if (-not (Get-Command wails -ErrorAction SilentlyContinue)) {
            throw "wails CLI not found; install Wails v2.15.0 to build the desktop shell (server target stays available)"
        }
        Write-Host "==> wails build (desktop, production, $Version)"
        Push-Location cmd\cobot-desktop
        # -skipbindings: the shell's bindings phase compiles cmd/cobot-desktop
        # with only the "bindings" tag, but internal/desktop is guarded by
        # "desktop", so that phase fails with "build constraints exclude all
        # Go files". The frontend declares the bridge structurally
        # (frontend/src/platform/native-bridge.ts) and frontend/wailsjs/ is
        # gitignored, so no generated binding is needed at runtime.
        # -o stays relative: Wails joins it with build:dir, and an absolute
        # Windows path produces "mkdir ...\bin\E::" on Windows.
        # -m prevents Wails' go mod tidy; -s reuses the frontend built above.
        # Never restore dependency files from Git: they may contain user edits.
        try {
            wails build -tags desktop -nosyncgomod -skipembedcreate -skipbindings -m -s `
                -o "cobot-desktop.exe" `
                -ldflags "-X main.version=$Version -X main.environment=production"
            if ($LASTEXITCODE -ne 0) { throw "desktop build failed" }
        } finally {
            Pop-Location
        }
        # Wails emits into build:dir (.tmp/wails/bin), not into .tmp/dist.
        $built = Join-Path $root ".tmp\wails\bin\cobot-desktop.exe"
        if (-not (Test-Path $built)) { throw "desktop artifact not found at $built" }
        $out = Join-Path $dist "cobot-desktop.exe"
        Move-Item -Force $built $out
        Write-Checksum $out
        # Production desktop packages include the same-version helper binary
        # (MCP/hook) next to the shell; located relative to the shell, not PATH.
        $helper = Join-Path $dist "cobot.exe"
        Write-Host "==> go build (helper cobot, production profile default)"
        go build -ldflags "-X main.version=$Version" -o $helper ./cmd/cobot
        if ($LASTEXITCODE -ne 0) { throw "helper build failed" }
        Write-Checksum $helper
        # Codex launches the MCP/hook bridge as a child process, and Windows
        # gives every console-subsystem child a console window, which flashed a
        # terminal next to the app on each team start. This helper is the same
        # bridge linked with the GUI subsystem, so it never asks for one.
        $guiHelper = Join-Path $dist "cobot-helper.exe"
        Write-Host "==> go build (windowless helper, GUI subsystem)"
        go build -ldflags "-X main.version=$Version -H=windowsgui" -o $guiHelper ./cmd/cobot-helper
        if ($LASTEXITCODE -ne 0) { throw "windowless helper build failed" }
        Write-Checksum $guiHelper
    }
}
Write-Host "==> artifacts in $dist"
