#!/bin/zsh
# Bygger dist/Devctl.app: Swift-skalet och devctl-binären, båda universella (arm64 + x86_64).
# Användning: desktop/macos/build.sh [version]
set -euo pipefail
HERE="${0:A:h}"
ROOT="${HERE:h:h}"
VERSION="${1:-$(git -C "$ROOT" describe --tags --always 2>/dev/null || echo dev)}"
VERSION="${VERSION#v}"
APP="$ROOT/dist/Devctl.app"
TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT

rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"

echo "▶ devctl (Go)"
for arch in arm64 amd64; do
  (cd "$ROOT" && GOOS=darwin GOARCH=$arch CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o "$TMP/devctl-$arch" ./cmd/devctl)
done
lipo -create -output "$APP/Contents/Resources/devctl" "$TMP/devctl-arm64" "$TMP/devctl-amd64"

echo "▶ appskal (Swift)"
for arch in arm64 x86_64; do
  swiftc -O -target "$arch-apple-macos13.0" -o "$TMP/Devctl-$arch" "$HERE/main.swift"
done
lipo -create -output "$APP/Contents/MacOS/Devctl" "$TMP/Devctl-arm64" "$TMP/Devctl-x86_64"

echo "▶ ikon"
swift "$HERE/make-icon.swift" "$TMP/AppIcon.iconset"
iconutil -c icns -o "$APP/Contents/Resources/AppIcon.icns" "$TMP/AppIcon.iconset"

sed "s/__VERSION__/$VERSION/g" "$HERE/Info.plist" > "$APP/Contents/Info.plist"

# Ad hoc-signering räcker för att köra lokalt; för distribution krävs Developer ID + notarisering
codesign --force --deep --sign - "$APP" >/dev/null
echo "✓ $APP ($VERSION)"
