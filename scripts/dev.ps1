# Development loop: starts the Go core (development profile) and the Vite dev
# server, hands the same-user runtime secret to Vite through process
# environment only (never logs or files), prints a fresh one-time web link,
# and stops both processes on exit.
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

if (-not (Test-Path "frontend\node_modules")) {
    Write-Host "==> npm ci --prefix frontend"
    npm ci --prefix frontend
    if ($LASTEXITCODE -ne 0) { throw "npm ci failed" }
}

$profileDir = Join-Path $env:USERPROFILE ".cobot-dev"
$instanceFile = Join-Path $profileDir "runtime\instance.json"
$secretFile = Join-Path $profileDir "runtime\credentials\instance-admin.secret"
if (Test-Path $instanceFile) { Remove-Item $instanceFile -Force }

New-Item -ItemType Directory -Force -Path ".tmp" | Out-Null
$goLog = Join-Path $root ".tmp\dev-core.log"
Write-Host "==> starting core (development profile)"
$go = Start-Process -FilePath "go" -ArgumentList @(
    "run", "./cmd/cobot", "serve",
    "--profile", "development",
    "--listen", "127.0.0.1:0",
    "--dev-origin", "http://127.0.0.1:5173"
) -RedirectStandardOutput $goLog -RedirectStandardError "$goLog.err" -NoNewWindow -PassThru

try {
    $endpoint = $null
    for ($i = 0; $i -lt 120; $i++) {
        Start-Sleep -Milliseconds 500
        if (Test-Path $instanceFile) {
            $info = Get-Content $instanceFile -Raw | ConvertFrom-Json
            if ($info.endpoint) { $endpoint = $info.endpoint; break }
        }
        if ($go.HasExited) { throw "core exited early; see $goLog.err" }
    }
    if (-not $endpoint) { throw "core did not publish its endpoint; see $goLog.err" }

    $adminSecret = (Get-Content $secretFile -Raw).Trim()
    Write-Host "==> core endpoint $endpoint"
    Get-Content $goLog -Tail 5 | ForEach-Object { Write-Host "    $_" }

    $env:COBOT_API_ENDPOINT = $endpoint
    $env:COBOT_ADMIN_SECRET = $adminSecret
    Write-Host "==> starting Vite on http://127.0.0.1:5173"
    npm run dev --prefix frontend
}
finally {
    Write-Host "==> stopping development processes"
    if ($go -and -not $go.HasExited) { Stop-Process -Id $go.Id -Force -ErrorAction SilentlyContinue }
}
