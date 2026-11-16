#!/usr/bin/env bash
# Bundle the ocode-desktop binary into a minimal macOS .app.
# Usage: scripts/bundle-macos.sh <binary> <output.app> [remote-binaries/<version>] [version]
# The optional version directory must contain all four remote CLI targets.
# The optional version string becomes CFBundleShortVersionString — the value
# macOS's native "About ocode" panel displays. When omitted it is read from
# internal/version/version.go so the About panel always matches `ocode --version`.
set -euo pipefail

# macOS-only tooling (codesign, Info.plist semantics). Fail before `rm -rf` on
# another host, where the target app path is meaningless.
if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "error: bundle-macos.sh is macOS only" >&2
  exit 1
fi

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
    for artifact in ocode htrcli; do
      if [[ ! -f "$REMOTE_DIR/$artifact-$target" || ! -s "$REMOTE_DIR/$artifact-$target" ]]; then
        echo "error: remote binary missing or empty: $REMOTE_DIR/$artifact-$target" >&2
        exit 1
      fi
    done
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
    for artifact in ocode htrcli; do
      cp "$REMOTE_DIR/$artifact-$target" "$REMOTE_DEST/$artifact-$target"
      chmod +x "$REMOTE_DEST/$artifact-$target"
    done
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
	<key>NSMicrophoneUsageDescription</key>
	<string>ocode uses the microphone to dictate chat messages.</string>
	<key>NSUserNotificationAlertStyle</key>
	<string>banner</string>
</dict>
</plist>
PLIST_EOF

# A linker-signed Go binary is treated as unsigned by TCC: macOS lists the app
# in Privacy & Security and the toggle reads ON, but the grant never matches, so
# Accessibility, Screen Recording and Microphone stay denied. Re-sign the
# finished bundle with the bundle identifier baked in, which puts a real
# CodeDirectory and the Info.plist hash in the signature. `set -e` makes a
# signing failure stop the build instead of shipping an app whose grants are
# silently inert.
#
# TCC stores a grant as the code's DESIGNATED REQUIREMENT. An ad-hoc signature
# makes that requirement `cdhash H"…"`, which changes on every build, so each
# rebuild silently orphans Accessibility / Screen Recording / Files & Folders
# while the Privacy toggles keep reading ON. A certificate-anchored requirement
# (`identifier "com.u007.ocode" and certificate leaf = H"…"`) is stable across
# rebuilds, so use one whenever it exists. Resolution is deliberate: only the
# dedicated `ocode-dev` identity is auto-detected, so installing an unrelated
# certificate never changes ocode's signature behind the user's back.
#
# Detection omits `-v` on purpose: that flag lists only identities chaining to a
# trusted root, and a self-signed identity reports CSSMERR_TP_NOT_TRUSTED even
# though codesign and TCC both accept it. Trust is not required.
IDENTITY="${OCODE_CODESIGN_IDENTITY:-}"
# Capture first, then match. `security … | grep -q` can report "not found" when
# it did match: grep exits at the first hit and closes the pipe, and under
# `set -o pipefail` the producer's SIGPIPE (141) fails the whole pipeline.
IDENTITIES="$(security find-identity -p codesigning 2>/dev/null || true)"
if [[ -z "$IDENTITY" ]] && [[ "$IDENTITIES" == *'"ocode-dev"'* ]]; then
  IDENTITY="ocode-dev"
fi
if [[ -n "$IDENTITY" ]]; then
  codesign --force --sign "$IDENTITY" --identifier com.u007.ocode "$APP"
else
  codesign --force --sign - --identifier com.u007.ocode "$APP"
fi

# codesign reports the designated requirement on stderr; keep it so the caller
# can tell whether the OS grant will survive the next rebuild.
# Ad-hoc signatures have no STORED requirement; codesign synthesizes one and
# marks it with a leading "# ". Strip that too, or the cdhash check misses.
REQUIREMENT="$(codesign -d -r- "$APP" 2>&1 | sed -n 's/^#* *designated => //p')"
echo "bundled: $APP"
echo "signed with: ${IDENTITY:-ad-hoc}"
echo "designated requirement: $REQUIREMENT"
case "$REQUIREMENT" in
  cdhash*)
    cat <<'WARN'
warning: ad-hoc signature — macOS keys TCC grants to the cdhash, so every
         rebuild orphans Accessibility, Screen Recording and Files & Folders
         (the Privacy toggles keep reading ON). Fix once with
         `make signing-cert`, then `make desktop-app` and re-grant once;
         later rebuilds keep the grants.
WARN
    ;;
esac

# The requirement above is what TCC keys the grant to; this note covers the
# other half — whether macOS considers the identity trusted. A self-signed
# identity reports CSSMERR_TP_NOT_TRUSTED and is deliberately accepted here, so
# say so instead of letting "console reads fine, grants stay dead" recur.
if [[ -n "$IDENTITY" ]] && [[ "$(security find-identity -v -p codesigning 2>/dev/null || true)" != *"\"$IDENTITY\""* ]]; then
  echo "note: $IDENTITY is not a macOS-trusted identity (expected for a self-signed one)."
  echo "      If a grant still does not stick after re-granting, trust the"
  echo "      certificate in Keychain Access, or re-run \`make signing-cert\`."
fi
