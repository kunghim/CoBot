#!/usr/bin/env bash
# Development loop (macOS/Linux equivalent of dev.ps1): starts the Go core
# (development profile) and the Vite dev server, hands the same-user runtime
# secret to Vite through process environment only, prints a fresh one-time
# web link, and stops both processes on exit.
set -euo pipefail
cd "$(dirname "$0")/.."

if [ ! -d frontend/node_modules ]; then
  echo "==> npm ci --prefix frontend"
  npm ci --prefix frontend
fi

PROFILE_DIR="$HOME/.cobot-dev"
INSTANCE_FILE="$PROFILE_DIR/runtime/instance.json"
SECRET_FILE="$PROFILE_DIR/runtime/credentials/instance-admin.secret"
rm -f "$INSTANCE_FILE"

mkdir -p .tmp
GO_LOG=".tmp/dev-core.log"
echo "==> starting core (development profile)"
go run ./cmd/cobot serve --profile development --listen 127.0.0.1:0 \
  --dev-origin http://127.0.0.1:5173 >"$GO_LOG" 2>"$GO_LOG.err" &
GO_PID=$!
trap 'kill $GO_PID 2>/dev/null || true' EXIT

endpoint=""
for _ in $(seq 1 120); do
  sleep 0.5
  if [ -f "$INSTANCE_FILE" ]; then
    endpoint=$(sed -n 's/.*"endpoint": *"\([^"]*\)".*/\1/p' "$INSTANCE_FILE" | head -1)
    [ -n "$endpoint" ] && break
  fi
  if ! kill -0 $GO_PID 2>/dev/null; then
    echo "core exited early; see $GO_LOG.err" >&2
    exit 1
  fi
done
[ -n "$endpoint" ] || { echo "core did not publish its endpoint" >&2; exit 1; }

export COBOT_API_ENDPOINT="$endpoint"
export COBOT_ADMIN_SECRET="$(tr -d '[:space:]' < "$SECRET_FILE")"
echo "==> core endpoint $endpoint"
echo "==> starting Vite on http://127.0.0.1:5173"
npm run dev --prefix frontend
