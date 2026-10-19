#!/usr/bin/env bash
# Inspect a mounted ARM64 DMG, then verify its copied app after ejecting.
# Does not launch the GUI, initialize a profile or change Gatekeeper settings.
set -euo pipefail
cd "$(dirname "$0")/.."
VERSION="${1:?usage: scripts/verify-macos-package.sh <version>}"
[[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]]
NAME="CoBot-$VERSION-macos-arm64.dmg"
(cd .tmp/dist && /usr/bin/shasum -a 256 -c "$NAME.sha256")
/usr/bin/hdiutil verify ".tmp/dist/$NAME"
WORK="$(mktemp -d "$PWD/.tmp/dmg-verify.XXXXXX")"
MOUNT="$WORK/mount"
ATTACHED=0
cleanup() {
  if [ "$ATTACHED" = 1 ]; then
    /usr/bin/hdiutil detach "$MOUNT" -quiet || return
  fi
  rm -rf "$WORK"
}
trap cleanup EXIT
mkdir "$MOUNT"
/usr/bin/hdiutil attach ".tmp/dist/$NAME" -readonly -nobrowse -mountpoint "$MOUNT" -quiet
ATTACHED=1
test "$(ls -A "$MOUNT" | sort)" = $'Applications\nCoBot.app'
test "$(readlink "$MOUNT/Applications")" = /Applications
/usr/bin/codesign --verify --deep --strict "$MOUNT/CoBot.app"
/usr/bin/ditto "$MOUNT/CoBot.app" "$WORK/CoBot.app"
/usr/bin/hdiutil detach "$MOUNT" -quiet
ATTACHED=0
APP="$WORK/CoBot.app"
/usr/bin/codesign --verify --deep --strict "$APP"
test "$(/usr/bin/plutil -extract CoBotVersion raw -o - "$APP/Contents/Info.plist")" = "$VERSION"
test "$(/usr/bin/plutil -extract CFBundleIdentifier raw -o - "$APP/Contents/Info.plist")" = com.kunghim.cobot
test "$(/usr/bin/plutil -extract CFBundleIconFile raw -o - "$APP/Contents/Info.plist")" = iconfile
test -s "$APP/Contents/Resources/iconfile.icns"
for BINARY in cobot-desktop cobot; do
  test -x "$APP/Contents/MacOS/$BINARY"
  test "$(/usr/bin/lipo -archs "$APP/Contents/MacOS/$BINARY")" = arm64
done
test -s "$APP/Contents/Resources/LICENSE.txt"
test -s "$APP/Contents/Resources/THIRD-PARTY-NOTICES.txt"
test "$("$APP/Contents/MacOS/cobot" version)" = "$VERSION"
echo "ARM64 DMG payload, copied app and helper version verified; GUI acceptance is separate."
