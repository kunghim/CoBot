#!/usr/bin/env bash
# Runs the default Web/shared-core checks: formatting, contract generation
# drift, frontend typecheck/tests/build, Go build and tests. Does not call
# wails build or require native desktop toolchains.
# Usage: scripts/check.sh [--desktop]
set -euo pipefail
cd "$(dirname "$0")/.."

echo "==> public file policy"
node scripts/check-public-files.mjs
node --test scripts/repository-policy.test.mjs scripts/issue-commands.test.mjs scripts/release.test.mjs scripts/check-docs.test.mjs scripts/check-upgrade.test.mjs

echo "==> docs and locale keys"
node scripts/check-docs.mjs

echo "==> avatar catalog"
node assets/avatars/build.mjs --check

echo "==> gofmt"
bad=$(gofmt -l cmd internal tests || true)
if [ -n "$bad" ]; then
  echo "gofmt needed on:" && echo "$bad" && exit 1
fi

echo "==> contract generation (must be committed clean)"
./scripts/generate.sh

echo "==> generated-file drift"
git diff --exit-code -- api/openapi.yaml internal/contract frontend/src/api/generated
# Also catch brand-new generated files that git diff would not show.
untracked=$(git status --porcelain -- api/openapi.yaml internal/contract frontend/src/api/generated | grep '^??' || true)
if [ -n "$untracked" ]; then
  echo "generated contract files include untracked changes:" && echo "$untracked" && exit 1
fi

# Frontend first: internal/webui/dist is generated and gitignored, so
# go:embed all:dist fails on a clean checkout until vite has run.
echo "==> frontend build (embedded assets)"
npm run build --prefix frontend || exit 1

echo "==> go vet"
go vet ./...

echo "==> go build"
go build ./...

echo "==> go test (unit + integration)"
go test ./internal/... ./tests/...

echo "==> frontend typecheck + unit tests"
npm run typecheck --prefix frontend
npm test --prefix frontend

echo "==> go build with embedded assets"
go build ./...

if [ "${1:-}" = "--desktop" ]; then
  echo "==> desktop shell compile (tags=desktop)"
  mkdir -p .tmp/bin
  go build -tags desktop -o .tmp/bin/check-cobot-desktop ./cmd/cobot-desktop
fi

echo "==> check passed"
