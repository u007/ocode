#!/usr/bin/env bash
# Lightweight packaging regression tests: no real Go/web builds or signing.
# Run: bash scripts/test-desktop-packaging.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
fail() { echo "FAIL: $*" >&2; exit 1; }
TARGETS=(linux-amd64 linux-arm64 darwin-amd64 darwin-arm64)

# The canonical version the macOS About panel must display.
EXPECTED_VERSION="$(sed -n 's/^const Version = "\(.*\)"$/\1/p' "$ROOT/internal/version/version.go")"
[[ -n "$EXPECTED_VERSION" ]] || fail 'could not read canonical version from internal/version/version.go'
plist_version() {
  sed -n '/CFBundleShortVersionString/{n;s/.*<string>\(.*\)<\/string>.*/\1/p;}' "$1"
}
REMOTE="$TMP/remote binaries/1.2.3"
APP="$TMP/test app.app"
mkdir -p "$REMOTE"
printf 'desktop\n' > "$TMP/desktop"
for target in "${TARGETS[@]}"; do
  printf '%s\n' "$target" > "$REMOTE/ocode-$target"
  printf '%s\n' "htrcli-$target" > "$REMOTE/htrcli-$target"
done
bash "$ROOT/scripts/bundle-macos.sh" "$TMP/desktop" "$APP" "$REMOTE/"
for target in "${TARGETS[@]}"; do
  dest="$APP/Contents/Resources/remote-binaries/1.2.3/ocode-$target"
  cmp "$REMOTE/ocode-$target" "$dest"
  [[ -x "$dest" ]] || fail "not executable: $target"
  cmp "$REMOTE/htrcli-$target" "$APP/Contents/Resources/remote-binaries/1.2.3/htrcli-$target"
done
cmp "$TMP/desktop" "$APP/Contents/MacOS/ocode"

# The About panel version comes from Info.plist; it must match the canonical
# Go version when the 4th argument is omitted.
[[ "$(plist_version "$APP/Contents/Info.plist")" == "$EXPECTED_VERSION" ]] \
  || fail "Info.plist version '$(plist_version "$APP/Contents/Info.plist")' != '$EXPECTED_VERSION'"

# ── Signing ladder ─────────────────────────────────────────────────────────
# Stub the two tools so the identity choice and the ad-hoc warning can be
# asserted without touching a real keychain. Every codesign call is logged.
SIGNBIN="$TMP/signbin"
mkdir -p "$SIGNBIN"
cat > "$SIGNBIN/security" <<'STUB'
#!/usr/bin/env bash
if [[ "${SECURITY_FINDS_IDENTITY:-0}" == "1" ]]; then
  printf '  1) 0123456789ABCDEF0123456789ABCDEF01234567 "ocode-dev"\n'
  exit 0
fi
exit 1
STUB
cat > "$SIGNBIN/codesign" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$CODESIGN_LOG"
case "$*" in
  *"--sign nope"*) echo 'error: no identity found for "nope"' >&2; exit 1 ;;
esac
if [[ "$1" == "-d" ]]; then
  printf '%s\n' "$CODESIGN_REQUIREMENT" >&2
fi
exit 0
STUB
chmod +x "$SIGNBIN/security" "$SIGNBIN/codesign"

# bundle_with <designated-requirement> [VAR=value ...]
bundle_with() {
  local requirement="$1"
  shift
  env PATH="$SIGNBIN:$PATH" CODESIGN_LOG="$TMP/codesign.log" \
    CODESIGN_REQUIREMENT="$requirement" "$@" \
    bash "$ROOT/scripts/bundle-macos.sh" "$TMP/desktop" "$APP" "$REMOTE/"
}

: > "$TMP/codesign.log"
bundle_with '# designated => cdhash H"0123456789abcdef"' > "$TMP/ad-hoc.out" 2>&1
grep -q 'signed with: ad-hoc' "$TMP/ad-hoc.out" || fail 'ad-hoc signing not reported'
grep -q 'designated requirement: cdhash' "$TMP/ad-hoc.out" || fail 'designated requirement not reported'
grep -q 'rebuild orphans' "$TMP/ad-hoc.out" || fail 'ad-hoc grant-loss warning missing'
grep -q -- '--sign -' "$TMP/codesign.log" || fail 'ad-hoc codesign not invoked'

: > "$TMP/codesign.log"
SECURITY_FINDS_IDENTITY=1 bundle_with \
  '# designated => identifier "com.u007.ocode" and certificate leaf = H"0123"' > "$TMP/cert.out" 2>&1
grep -q 'signed with: ocode-dev' "$TMP/cert.out" || fail 'detected identity not used'
grep -q -- '--sign ocode-dev' "$TMP/codesign.log" || fail 'certificate codesign not invoked'
if grep -q 'rebuild orphans' "$TMP/cert.out"; then
  fail 'warned about grant loss while a certificate was used'
fi

# An explicit unknown identity must fail the build, not silently sign ad-hoc.
if bundle_with 'designated => cdhash H"x"' OCODE_CODESIGN_IDENTITY=nope > "$TMP/bogus.out" 2>&1; then
  fail 'accepted an unknown signing identity'
fi
grep -q 'no identity found' "$TMP/bogus.out" || fail 'codesign failure not surfaced'
if grep -q 'bundled:' "$TMP/bogus.out"; then
  fail 'reported success after a signing failure'
fi

# Each missing target must fail before destroying the previous app.
for target in "${TARGETS[@]}"; do
  mv "$REMOTE/ocode-$target" "$TMP/saved"
  printf 'preserve\n' > "$APP/sentinel"
  if bash "$ROOT/scripts/bundle-macos.sh" "$TMP/desktop" "$APP" "$REMOTE" > "$TMP/error" 2>&1; then
    fail "accepted missing $target"
  fi
  grep -q "ocode-$target" "$TMP/error"
  [[ -f "$APP/sentinel" ]] || fail "removed old app on missing $target"
  mv "$TMP/saved" "$REMOTE/ocode-$target"
done
: > "$REMOTE/ocode-linux-amd64"
if bash "$ROOT/scripts/bundle-macos.sh" "$TMP/desktop" "$APP" "$REMOTE" > "$TMP/error" 2>&1; then
  fail 'accepted empty artifact'
fi
[[ -f "$APP/sentinel" ]] || fail 'removed old app on empty artifact'

# Legacy callers omit the third parameter; no resources should be inherited.
bash "$ROOT/scripts/bundle-macos.sh" "$TMP/desktop" "$APP"
[[ ! -e "$APP/Contents/Resources/remote-binaries" ]] || fail 'legacy bundle retained remote binaries'
[[ -f "$APP/Contents/Info.plist" ]] || fail 'missing plist'
[[ "$(plist_version "$APP/Contents/Info.plist")" == "$EXPECTED_VERSION" ]] \
  || fail 'legacy bundle plist version mismatch'

# Exercise the real Makefile in an isolated fixture with stub prerequisites
# and Go. The shared prerequisites must finish exactly once under make -j.
mkdir -p "$TMP/build/tools" "$TMP/build/scripts"
# desktop-remote-binaries also builds htrcli from HTRCLI_ROOT; the stub `go`
# below covers it, so a checkout with a go.mod is all the fixture needs.
mkdir -p "$TMP/fakehtrcli/cmd/htrcli"
: > "$TMP/fakehtrcli/go.mod"
cp "$ROOT/Makefile" "$TMP/build/Makefile"
cp "$ROOT/scripts/bundle-macos.sh" "$TMP/build/scripts/"
chmod +x "$TMP/build/scripts/bundle-macos.sh"
cat > "$TMP/build/stubs.mk" <<'STUBS'
web-build:
	@sleep 0.1; echo ready >> web-ready
prepare-htr-assets:
	@sleep 0.1; echo ready >> htr-ready
models-snapshot:
	@:
bundle-desktop-assets:
	@:
STUBS
cat > "$TMP/build/tools/go" <<'GO'
#!/usr/bin/env bash
set -euo pipefail
[[ "$1" == build ]]
[[ -f web-ready && -f htr-ready ]]
out=''
while [[ $# -gt 0 ]]; do
  if [[ "$1" == -o ]]; then out="$2"; shift; fi
  last="$1"
  shift
done
if [[ "$last" == . ]]; then
  [[ "$CGO_ENABLED" == 0 ]]
  echo "$GOOS-$GOARCH" >> builds
  [[ "${FAIL_TARGET:-}" != "$GOOS-$GOARCH" ]] || exit 42
fi
mkdir -p "$(dirname "$out")"
printf 'dummy\n' > "$out"
GO
chmod +x "$TMP/build/tools/go"
(
  cd "$TMP/build"
  export PATH="$PWD/tools:$PATH"
  export HTRCLI_ROOT="$TMP/fakehtrcli"
  if ! make -j8 -f Makefile -f stubs.mk VERSION=1.2.3 desktop-app > "$TMP/make.log" 2>&1; then
    cat "$TMP/make.log" >&2
    fail 'make desktop-app failed'
  fi
  [[ "$(wc -l < web-ready | tr -d ' ')" == 1 ]] || fail 'web built more than once'
  [[ "$(wc -l < htr-ready | tr -d ' ')" == 1 ]] || fail 'HTR prepared more than once'
  [[ "$(wc -l < builds | tr -d ' ')" == 4 ]] || fail 'wrong build count'
  for target in "${TARGETS[@]}"; do
    grep -qx "$target" builds
    [[ -x "bin/ocode.app/Contents/Resources/remote-binaries/1.2.3/ocode-$target" ]] || fail "not bundled: $target"
  done
  # `make desktop-app` passes VERSION through to the plist (About panel).
  [[ "$(plist_version bin/ocode.app/Contents/Info.plist)" == "1.2.3" ]] \
    || fail "make bundle plist version '$(plist_version bin/ocode.app/Contents/Info.plist)' != '1.2.3'"
  for target in "${TARGETS[@]}"; do
    printf 'preserve\n' > bin/ocode.app/sentinel
    if FAIL_TARGET="$target" make -j8 -f Makefile -f stubs.mk VERSION=1.2.3 desktop-app > "$TMP/make.log" 2>&1; then
      fail "make ignored failure for $target"
    fi
    [[ -f bin/ocode.app/sentinel ]] || fail "bundled after failed $target build"
  done
)
echo 'PASS: desktop packaging smoke tests'
