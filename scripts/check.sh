#!/usr/bin/env bash
# Runs the default Web/shared-core checks: formatting, contract generation
# drift, frontend typecheck/tests/build, Go build and tests. Does not call
# wails build or require native desktop toolchains.
# Usage: scripts/check.sh [--desktop]
set -euo pipefail
cd "$(dirname "$0")/.."

echo "==> gofmt"
bad=$(gofmt -l cmd internal tests || true)
if [ -n "$bad" ]; then
  echo "gofmt needed on:" && echo "$bad" && exit 1
fi

echo "==> contract generation (must be committed clean)"
./scripts/generate.sh

echo "==> generated-file drift"
git diff --exit-code -- api/openapi.yaml internal/contract frontend/src/api/generated

echo "==> go vet"
go vet ./...

echo "==> go build"
go build ./...

echo "==> go test (unit + integration)"
go test ./internal/... ./tests/...

echo "==> frontend typecheck + unit tests"
npm run typecheck --prefix frontend
npm test --prefix frontend

echo "==> frontend build (embed assets)"
npm run build --prefix frontend

echo "==> go build with embedded assets"
go build ./...

if [ "${1:-}" = "--desktop" ]; then
  echo "==> desktop shell compile (tags=desktop)"
  go build -tags desktop ./cmd/cobot-desktop
fi

echo "==> check passed"
