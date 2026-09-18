#!/usr/bin/env bash
# Bundle the ocode-desktop binary into a minimal macOS .app.
# Usage: scripts/bundle-macos.sh <binary> <output.app> [remote-binaries/<version>] [version]
# The optional version directory must contain all four remote CLI targets.
# The optional version string becomes CFBundleShortVersionString — the value
# macOS's native "About ocode" panel displays. When omitted it is read from
# internal/version/version.go so the About panel always matches `ocode --version`.
set -euo pipefail

if [[ $# -lt 2 || $# -gt 4 ]]; then
  echo "usage: $0 <binary> <output.app> [remote-binaries/<version>] [version]" >&2
  exit 1
fi

BINARY="$1"
APP="$2"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ICON="$SCRIPT_DIR/../build/appicon.icns"

VERSION="${4:-}"
if [[ -z "$VERSION" ]]; then
  VERSION_FILE="$SCRIPT_DIR/../internal/version/version.go"
  if [[ -f "$VERSION_FILE" ]]; then
    VERSION="$(sed -n 's/^const Version = "\(.*\)"$/\1/p' "$VERSION_FILE")"
  fi
fi
if [[ -z "$VERSION" ]]; then
  echo "error: could not determine version; pass it as the 4th argument" >&2
  exit 1
fi

if [[ ! -f "$BINARY" ]]; then
  echo "error: binary not found: $BINARY" >&2
  exit 1
fi

# Validate the complete set before removing an existing app. Two-argument
# callers continue to produce the original desktop-only bundle.
REMOTE_DIR=""
TARGETS=(linux-amd64 linux-arm64 darwin-amd64 darwin-arm64)
if [[ $# -ge 3 ]]; then
  REMOTE_DIR="$3"
  if [[ ! -d "$REMOTE_DIR" ]]; then
    echo "error: remote binary version directory not found: $REMOTE_DIR" >&2
    exit 1
  fi
  REMOTE_DIR="$(cd "$REMOTE_DIR" && pwd)"
  REMOTE_VERSION="$(basename "$REMOTE_DIR")"
  for target in "${TARGETS[@]}"; do
    if [[ ! -f "$REMOTE_DIR/ocode-$target" || ! -s "$REMOTE_DIR/ocode-$target" ]]; then
      echo "error: remote binary missing or empty: $REMOTE_DIR/ocode-$target" >&2
      exit 1
    fi
  done
fi

rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"

cp "$BINARY" "$APP/Contents/MacOS/ocode"
chmod +x "$APP/Contents/MacOS/ocode"

# Resources must be in place before any downstream bundle codesigning.
if [[ -n "$REMOTE_DIR" ]]; then
  REMOTE_DEST="$APP/Contents/Resources/remote-binaries/$REMOTE_VERSION"
  mkdir -p "$REMOTE_DEST"
  for target in "${TARGETS[@]}"; do
    cp "$REMOTE_DIR/ocode-$target" "$REMOTE_DEST/ocode-$target"
    chmod +x "$REMOTE_DEST/ocode-$target"
  done
fi

if [[ -f "$ICON" ]]; then
  cp "$ICON" "$APP/Contents/Resources/appicon.icns"
fi

PLIST="$APP/Contents/Info.plist"
cat > "$PLIST" <<PLIST_EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleExecutable</key>
	<string>ocode</string>
	<key>CFBundleIconFile</key>
	<string>appicon.icns</string>
	<key>CFBundleIdentifier</key>
	<string>com.u007.ocode</string>
	<key>CFBundleName</key>
	<string>ocode</string>
	<key>CFBundlePackageType</key>
	<string>APPL</string>
	<key>CFBundleShortVersionString</key>
	<string>${VERSION}</string>
	<key>NSHighResolutionCapable</key>
	<true/>
	<key>NSUserNotificationAlertStyle</key>
	<string>banner</string>
</dict>
</plist>
PLIST_EOF

echo "bundled: $APP"
