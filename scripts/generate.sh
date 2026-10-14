#!/usr/bin/env bash
# Generates the public wire-contract types from api/openapi.yaml:
#   - TS types into frontend/src/api/generated (openapi-typescript, pinned in package-lock)
#   - Go DTOs into internal/contract (oapi-codegen, pinned version)
# Requires Node.js and Go. Never fetches floating "latest" versions.
set -euo pipefail
cd "$(dirname "$0")/.."

if [ ! -d frontend/node_modules ]; then
  echo "==> npm ci --prefix frontend"
  npm ci --prefix frontend
fi

echo "==> openapi-typescript (frontend)"
npm run generate:api --prefix frontend

echo "==> oapi-codegen v2.4.1 (Go DTOs)"
GOFLAGS="-mod=mod" go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.4.1 \
  -config api/oapi-codegen.yaml api/openapi.yaml

echo "==> generate complete"
