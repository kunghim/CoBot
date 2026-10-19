#!/usr/bin/env bash
# Builds distributable artifacts (macOS equivalent of build.ps1).
#   scripts/build.sh --target server   -> standalone cobot executable
#   scripts/build.sh --target desktop  -> Wails desktop shell + helper cobot
# Use --arch arm64|amd64 to select the macOS CPU architecture.
# Desktop builds produce a DMG and ZIP under .tmp/dist with SHA-256 checksums.
set -euo pipefail
cd "$(dirname "$0")/.."

TARGET=""
VERSION="0.1.0-dev"
BUILD_ARCH="$(go env GOARCH)"
while [ $# -gt 0 ]; do
  case "$1" in
    --target) TARGET="$2"; shift 2 ;;
    --version) VERSION="$2"; shift 2 ;;
    --arch) BUILD_ARCH="$2"; shift 2 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done
if [ "$TARGET" != "server" ] && [ "$TARGET" != "desktop" ]; then
  echo "usage: scripts/build.sh --target server|desktop [--version <v>] [--arch arm64|amd64]" >&2
  exit 2
fi
if [ "$BUILD_ARCH" != "arm64" ] && [ "$BUILD_ARCH" != "amd64" ]; then
  echo "unsupported architecture: $BUILD_ARCH (want arm64 or amd64)" >&2
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
    (cd "$(dirname "$1")" && shasum -a 256 "$(basename "$1")") | tee "$1.sha256"
  else
    (cd "$(dirname "$1")" && sha256sum "$(basename "$1")") | tee "$1.sha256"
  fi
}

case "$TARGET" in
  server)
    echo "==> go build (server, $VERSION)"
    GOARCH="$BUILD_ARCH" go build -ldflags "-X main.version=$VERSION" -o "$DIST/cobot" ./cmd/cobot
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
    echo "==> wails build (desktop, production, $VERSION, darwin/$BUILD_ARCH)"
    # -skipbindings mirrors build.ps1: the bindings phase builds
    # cmd/cobot-desktop with only the "bindings" tag while internal/desktop is
    # guarded by "desktop", and the frontend declares the bridge structurally.
    (cd cmd/cobot-desktop && wails build -tags desktop -nosyncgomod -skipembedcreate \
      -skipbindings -m -s \
      -platform "darwin/$BUILD_ARCH" \
      -o cobot-desktop \
      -ldflags "-X main.version=$VERSION -X main.environment=production")
    # Wails packages into the configured build:dir, not a standalone shell.
    APP=".tmp/wails/bin/CoBot.app"
    test -x "$APP/Contents/MacOS/cobot-desktop"
    echo "==> go build (helper cobot, production profile default)"
    GOOS=darwin GOARCH="$BUILD_ARCH" go build -ldflags "-X main.version=$VERSION" -o "$DIST/cobot" ./cmd/cobot
    checksum "$DIST/cobot"
    # The bridge resolves its helper beside os.Executable(), inside the bundle.
    cp "$DIST/cobot" "$APP/Contents/MacOS/cobot"
    # macOS has no console-subsystem equivalent, so there is no separate
    # windowless helper to ship; cmd/cobot-helper exists for Windows only.
    echo "==> bundle metadata and license notices"
    PLIST="$APP/Contents/Info.plist"
    BUNDLE_VERSION="${VERSION#v}"
    BUNDLE_VERSION="${BUNDLE_VERSION%%-*}"
    BUNDLE_VERSION="${BUNDLE_VERSION%%+*}"
    if [[ ! "$BUNDLE_VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
      echo "version must contain a numeric major.minor.patch: $VERSION" >&2
      exit 2
    fi
    /usr/bin/plutil -replace CFBundleIdentifier -string com.kunghim.cobot "$PLIST"
    /usr/bin/plutil -replace CFBundleShortVersionString -string "$BUNDLE_VERSION" "$PLIST"
    /usr/bin/plutil -replace CFBundleVersion -string "$BUNDLE_VERSION" "$PLIST"
    /usr/bin/plutil -replace CoBotVersion -string "$VERSION" "$PLIST"
    /usr/bin/plutil -replace NSHumanReadableCopyright -string 'Copyright 2026 CoBot contributors' "$PLIST"
    cp LICENSE "$APP/Contents/Resources/LICENSE.txt"
    GOOS=darwin GOARCH="$BUILD_ARCH" CGO_ENABLED=1 node scripts/third-party-notices.mjs "$APP/Contents/Resources/THIRD-PARTY-NOTICES.txt"
    # Adding the helper changes the bundle after Wails' ad-hoc signature.
    # This remains a local trial signature, not Developer ID / notarization.
    /usr/bin/codesign --force --sign - "$APP/Contents/MacOS/cobot"
    /usr/bin/codesign --force --deep --sign - "$APP"
    /usr/bin/codesign --verify --deep --strict "$APP"
    ARCHIVE="$DIST/CoBot-${VERSION#v}-macos-$BUILD_ARCH.zip"
    /usr/bin/ditto -c -k --sequesterRsrc --keepParent "$APP" "$ARCHIVE"
    checksum "$ARCHIVE"
    bash scripts/build-dmg.sh --app "$APP" --version "$VERSION" --arch "$BUILD_ARCH"
    ;;
esac
echo "==> artifacts in $DIST"
