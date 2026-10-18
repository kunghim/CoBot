# Generates the public wire-contract types from api/openapi.yaml:
#   - TS types into frontend/src/api/generated (openapi-typescript, pinned in package-lock)
#   - Go DTOs into internal/contract (oapi-codegen, pinned version)
# Requires Node.js and Go. Never fetches floating "latest" versions.
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

if (-not (Test-Path "frontend/node_modules")) {
    Write-Host "==> npm ci --prefix frontend"
    npm ci --prefix frontend
}

Write-Host "==> openapi-typescript (frontend)"
npm run generate:api --prefix frontend

Write-Host "==> oapi-codegen v2.4.1 (Go DTOs)"
$env:GOFLAGS = "-mod=mod"
go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.4.1 -config api/oapi-codegen.yaml api/openapi.yaml
if ($LASTEXITCODE -ne 0) { throw "oapi-codegen failed with exit code $LASTEXITCODE" }

Write-Host "==> generate complete"
