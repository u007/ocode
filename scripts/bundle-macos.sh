#!/usr/bin/env bash
# Bundle the ocode-desktop binary into a minimal macOS .app.
# Usage: scripts/bundle-macos.sh <binary> <output.app>
set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: $0 <binary> <output.app>" >&2
  exit 1
fi

BINARY="$1"
APP="$2"

if [[ ! -f "$BINARY" ]]; then
  echo "error: binary not found: $BINARY" >&2
  exit 1
fi

rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"

cp "$BINARY" "$APP/Contents/MacOS/ocode"
chmod +x "$APP/Contents/MacOS/ocode"

PLIST="$APP/Contents/Info.plist"
cat > "$PLIST" <<'PLIST_EOF'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleExecutable</key>
	<string>ocode</string>
	<key>CFBundleIdentifier</key>
	<string>com.u007.ocode</string>
	<key>CFBundleName</key>
	<string>ocode</string>
	<key>CFBundlePackageType</key>
	<string>APPL</string>
	<key>CFBundleShortVersionString</key>
	<string>0.1.0</string>
	<key>NSHighResolutionCapable</key>
	<true/>
	<key>NSUserNotificationAlertStyle</key>
	<string>banner</string>
</dict>
</plist>
PLIST_EOF

echo "bundled: $APP"
