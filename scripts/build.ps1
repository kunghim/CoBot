# Builds distributable artifacts.
#   scripts/build.ps1 -Target server   -> standalone cobot executable with
#                                          embedded frontend/migrations/roles
#   scripts/build.ps1 -Target desktop  -> Wails desktop shell + same-version
#                                          helper cobot.exe (release profile)
# Output is summarized under .tmp/dist with SHA-256 checksums.
param(
    [Parameter(Mandatory = $true)][ValidateSet("server", "desktop")][string]$Target,
    [string]$Version = "0.1.0-dev"
)
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root
$dist = Join-Path $root ".tmp\dist"
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
        $out = Join-Path $dist "cobot-desktop.exe"
        Write-Host "==> wails build (desktop, production, $Version)"
        Push-Location cmd\cobot-desktop
        wails build -tags desktop -nosyncgomod -skipembedcreate -o $out `
            -ldflags "-X main.version=$Version -X main.environment=production"
        if ($LASTEXITCODE -ne 0) { Pop-Location; throw "desktop build failed" }
        Pop-Location
        Write-Checksum $out
        # Production desktop packages include the same-version helper binary
        # (MCP/hook) next to the shell; located relative to the shell, not PATH.
        $helper = Join-Path $dist "cobot.exe"
        Write-Host "==> go build (helper cobot, production profile default)"
        go build -ldflags "-X main.version=$Version" -o $helper ./cmd/cobot
        if ($LASTEXITCODE -ne 0) { throw "helper build failed" }
        Write-Checksum $helper
    }
}
Write-Host "==> artifacts in $dist"
