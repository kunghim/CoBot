#!/usr/bin/env bash
# Builds distributable artifacts (macOS equivalent of build.ps1).
#   scripts/build.sh --target server   -> standalone cobot executable
#   scripts/build.sh --target desktop  -> Wails desktop shell + helper cobot
# Output is summarized under .tmp/dist with SHA-256 checksums.
set -euo pipefail
cd "$(dirname "$0")/.."

TARGET=""
VERSION="0.1.0-dev"
while [ $# -gt 0 ]; do
  case "$1" in
    --target) TARGET="$2"; shift 2 ;;
    --version) VERSION="$2"; shift 2 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done
if [ "$TARGET" != "server" ] && [ "$TARGET" != "desktop" ]; then
  echo "usage: scripts/build.sh --target server|desktop [--version <v>]" >&2
  exit 2
fi

DIST=".tmp/dist"
mkdir -p "$DIST"

if [ ! -d frontend/node_modules ]; then
  echo "==> npm ci --prefix frontend"
  npm ci --prefix frontend
fi
echo "==> frontend build (embedded assets)"
npm run build --prefix frontend

checksum() {
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | tee "$1.sha256"
  else
    sha256sum "$1" | tee "$1.sha256"
  fi
}

case "$TARGET" in
  server)
    echo "==> go build (server, $VERSION)"
    go build -ldflags "-X main.version=$VERSION" -o "$DIST/cobot" ./cmd/cobot
    checksum "$DIST/cobot"
    ;;
  desktop)
    if [ "$(uname -s)" != "Darwin" ]; then
      echo "desktop build.sh requires macOS; use build.ps1 on Windows" >&2
      exit 1
    fi
    command -v wails >/dev/null 2>&1 || {
      echo "wails CLI not found; install Wails v2.15.0 to build the desktop shell" >&2
      exit 1
    }
    echo "==> wails build (desktop, production, $VERSION)"
    (cd cmd/cobot-desktop && wails build -tags desktop -nosyncgomod -skipembedcreate \
      -o cobot-desktop \
      -ldflags "-X main.version=$VERSION -X main.environment=production")
    # Wails packages into the configured build:dir, not a standalone shell.
    APP=".tmp/wails/bin/CoBot.app"
    test -x "$APP/Contents/MacOS/cobot-desktop"
    echo "==> go build (helper cobot, production profile default)"
    go build -ldflags "-X main.version=$VERSION" -o "$DIST/cobot" ./cmd/cobot
    checksum "$DIST/cobot"
    # The bridge resolves its helper beside os.Executable(), inside the bundle.
    cp "$DIST/cobot" "$APP/Contents/MacOS/cobot"
    # Adding the helper changes the bundle after Wails' ad-hoc signature.
    # This remains a local trial signature, not Developer ID / notarization.
    /usr/bin/codesign --force --sign - "$APP/Contents/MacOS/cobot"
    /usr/bin/codesign --force --deep --sign - "$APP"
    /usr/bin/codesign --verify --deep --strict "$APP"
    ARCHIVE="$DIST/CoBot-macos-$(go env GOARCH).zip"
    /usr/bin/ditto -c -k --sequesterRsrc --keepParent "$APP" "$ARCHIVE"
    checksum "$ARCHIVE"
    ;;
esac
echo "==> artifacts in $DIST"
