# Runs the default Web/shared-core checks: formatting, contract generation
# drift, frontend typecheck/tests/build, Go build and tests. Does not call
# wails build, require a Mac, signing or WebView2 — those are explicit
# desktop checks and are reported separately when untested.
param(
    [switch]$Desktop  # additionally compile the desktop shell with its tag
)
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

Write-Host "==> gofmt"
$bad = gofmt -l cmd internal tests
if ($bad) { throw "gofmt needed on:`n$bad" }

Write-Host "==> contract generation (must be committed clean)"
& "$PSScriptRoot/generate.ps1"

Write-Host "==> generated-file drift"
git diff --exit-code -- api/openapi.yaml internal/contract frontend/src/api/generated | Out-Host
if ($LASTEXITCODE -ne 0) { throw "generated contract files differ from the committed ones; run scripts/generate.ps1 and commit" }

# The frontend must be built first: internal/webui/dist is generated and
# gitignored, and go:embed all:dist fails on a clean checkout otherwise.
Write-Host "==> frontend build (embedded assets)"
npm run build --prefix frontend
if ($LASTEXITCODE -ne 0) { throw "frontend build failed" }

Write-Host "==> go vet"
go vet ./...
if ($LASTEXITCODE -ne 0) { throw "go vet failed" }

Write-Host "==> go build"
go build ./...
if ($LASTEXITCODE -ne 0) { throw "go build failed" }

Write-Host "==> go test (unit + integration)"
go test ./internal/... ./tests/...
if ($LASTEXITCODE -ne 0) { throw "go test failed" }

Write-Host "==> frontend typecheck + unit tests"
npm run typecheck --prefix frontend
if ($LASTEXITCODE -ne 0) { throw "frontend typecheck failed" }
npm test --prefix frontend
if ($LASTEXITCODE -ne 0) { throw "frontend tests failed" }

Write-Host "==> go build with embedded assets"
go build ./...
if ($LASTEXITCODE -ne 0) { throw "go build after asset build failed" }

if ($Desktop) {
    Write-Host "==> desktop shell compile (tags=desktop)"
    go build -tags desktop ./cmd/cobot-desktop
    if ($LASTEXITCODE -ne 0) { throw "desktop build failed" }
}

Write-Host "==> check passed"
