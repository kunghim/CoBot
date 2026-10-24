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

Write-Host "==> public file policy"
node scripts/check-public-files.mjs
if ($LASTEXITCODE -ne 0) { throw "public file policy failed" }
node --test scripts/repository-policy.test.mjs scripts/issue-commands.test.mjs scripts/release.test.mjs scripts/check-docs.test.mjs scripts/check-upgrade.test.mjs
if ($LASTEXITCODE -ne 0) { throw "repository policy tests failed" }

Write-Host "==> docs and locale keys"
node scripts/check-docs.mjs
if ($LASTEXITCODE -ne 0) { throw "docs or locale keys check failed" }

Write-Host "==> avatar catalog"
node assets/avatars/build.mjs --check
if ($LASTEXITCODE -ne 0) { throw "avatar catalog is out of date; run node assets/avatars/build.mjs" }

Write-Host "==> gofmt"
$bad = gofmt -l cmd internal tests
if ($bad) { throw "gofmt needed on:`n$bad" }

Write-Host "==> contract generation (must be committed clean)"
& "$PSScriptRoot/generate.ps1"

Write-Host "==> generated-file drift"
git diff --exit-code -- api/openapi.yaml internal/contract frontend/src/api/generated | Out-Host
if ($LASTEXITCODE -ne 0) { throw "generated contract files differ from the committed ones; run scripts/generate.ps1 and commit" }
# Only NEW untracked files count as drift here; staged/working changes to
# tracked files are what the git diff above already verified.
$untracked = git status --porcelain -- api/openapi.yaml internal/contract frontend/src/api/generated | Where-Object { $_.StartsWith('??') }
if ($untracked) { throw "generated contract files include untracked changes: $untracked" }

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
    New-Item -ItemType Directory -Force -Path ".tmp/bin" | Out-Null
    go build -tags desktop -o .tmp/bin/check-cobot-desktop.exe ./cmd/cobot-desktop
    if ($LASTEXITCODE -ne 0) { throw "desktop build failed" }
}

Write-Host "==> check passed"
