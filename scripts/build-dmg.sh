#!/usr/bin/env bash
# Package an already-built macOS app without launching it or initializing data.
set -euo pipefail
cd "$(dirname "$0")/.."

APP=""
VERSION=""
BUILD_ARCH=""
while [ $# -gt 0 ]; do
  case "$1" in
    --app|--version|--arch)
      if [ $# -lt 2 ]; then echo "missing value for $1" >&2; exit 2; fi
      case "$1" in
        --app) APP="$2" ;;
        --version) VERSION="$2" ;;
        --arch) BUILD_ARCH="$2" ;;
      esac
      shift 2 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done
if [ "$(uname -s)" != Darwin ]; then
  echo "DMG packaging requires macOS" >&2; exit 1
fi
if [ -z "$APP" ] || [[ ! "$VERSION" =~ ^v?[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$ ]] ||
   { [ "$BUILD_ARCH" != arm64 ] && [ "$BUILD_ARCH" != amd64 ]; }; then
  echo "usage: scripts/build-dmg.sh --app <CoBot.app> --version <version> --arch arm64|amd64" >&2
  exit 2
fi
test -d "$APP"
test "$(/usr/bin/plutil -extract CoBotVersion raw -o - "$APP/Contents/Info.plist")" = "$VERSION"
MACH_ARCH="$BUILD_ARCH"
if [ "$MACH_ARCH" = amd64 ]; then MACH_ARCH=x86_64; fi
for BINARY in cobot-desktop cobot; do
  test -x "$APP/Contents/MacOS/$BINARY"
  test "$(/usr/bin/lipo -archs "$APP/Contents/MacOS/$BINARY")" = "$MACH_ARCH"
done
test -f "$APP/Contents/Resources/LICENSE.txt"
test -f "$APP/Contents/Resources/THIRD-PARTY-NOTICES.txt"
/usr/bin/codesign --verify --deep --strict "$APP"

DIST=".tmp/dist"
mkdir -p "$DIST"
WORK="$(mktemp -d "$DIST/dmg.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT
mkdir "$WORK/staging"
/usr/bin/ditto "$APP" "$WORK/staging/CoBot.app"
ln -s /Applications "$WORK/staging/Applications"
NAME="CoBot-${VERSION#v}-macos-$BUILD_ARCH.dmg"
# HFS+ / UDZO is a read-only compressed image. Only the app and the standard
# Applications shortcut are staged; profiles and build directories stay out.
/usr/bin/hdiutil create -volname "CoBot ${VERSION#v}" -fs HFS+ -format UDZO \
  -srcfolder "$WORK/staging" "$WORK/$NAME"
/usr/bin/hdiutil verify "$WORK/$NAME"
mv -f "$WORK/$NAME" "$DIST/$NAME"
(cd "$DIST" && /usr/bin/shasum -a 256 "$NAME") | tee "$DIST/$NAME.sha256"
echo "==> DMG: $DIST/$NAME"
