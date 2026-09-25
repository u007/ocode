#!/usr/bin/env bash
# Lightweight regression tests for the Makefile version-bump targets.
# Run: bash scripts/test-version-bump.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

[[ -f "$ROOT/scripts/bump-version.sh" ]] || fail 'scripts/bump-version.sh is missing'
grep -Eq '^up-(patch|minor):' "$ROOT/Makefile" || fail 'Makefile up-patch/up-minor target is missing'

assert_eq() {
  local want="$1"
  local got="$2"
  local context="$3"
  [[ "$got" == "$want" ]] || fail "$context: want '$want', got '$got'"
}

assert_contains() {
  local file="$1"
  local text="$2"
  local context="$3"
  grep -Fq -- "$text" "$file" || fail "$context: '$text' not found in $file"
}

write_version() {
  local root="$1"
  local version="$2"
  mkdir -p "$root/internal/version" "$root/scripts"
  cat > "$root/internal/version/version.go" <<EOF
package version

const Version = "$version"
EOF
}

write_changes() {
  local root="$1"
  local line="$2"
  cat > "$root/CHANGES.md" <<EOF
# Changelog

## 2026-09-24 — Unrelated release note

- Keep this content byte-for-byte.

## [Unreleased]

- **Version Bump** — $line: automated release metadata.
- Another unreleased item.

## Archived

## [Unreleased]

- **Version Bump** — must not be selected: 9.9.9 → 9.9.9.
EOF
}

new_fixture() {
  local name="$1"
  local version="$2"
  local line="$3"
  local fixture="$TMP/$name"
  mkdir -p "$fixture/scripts"
  cp "$ROOT/Makefile" "$fixture/Makefile"
  cp "$ROOT/scripts/bump-version.sh" "$fixture/scripts/bump-version.sh"
  chmod +x "$fixture/scripts/bump-version.sh"
  write_version "$fixture" "$version"
  write_changes "$fixture" "$line"
  printf 'unrelated\n' > "$fixture/unrelated.txt"
  printf '%s\n' "$fixture"
}

write_make_stubs() {
  local fixture="$1"
  cat > "$fixture/recursive-make" <<'STUB'
#!/bin/sh
set -eu
version="$(sed -n 's/^const Version = "\(.*\)"$/\1/p' internal/version/version.go)"
target="${1:-}"
case "$target" in
  install)
    echo "install $version" >> make-trace
    if [ "${FAIL_INSTALL:-}" = "1" ]; then
      echo "stub install failure" >&2
      exit 42
    fi
    ;;
  desktop-app)
    echo "desktop-app $version" >> make-trace
    ;;
  *)
    echo "unexpected recursive make target: $target" >&2
    exit 64
    ;;
esac
STUB
  chmod +x "$fixture/recursive-make"
}

run_make_target() {
  local fixture="$1"
  local target="$2"
  shift 2
  (
    cd "$fixture"
    env MAKE="$fixture/recursive-make" "$@" make --no-print-directory -f Makefile "$target"
  )
}

version_value() {
  sed -n 's/^const Version = "\(.*\)"$/\1/p' "$1/internal/version/version.go"
}

# up-patch increments only the patch component and rebuilds in order with the
# new version visible to both downstream targets.
patch_fixture="$(new_fixture patch 0.8.110 '0.8.97 → 0.8.110')"
write_make_stubs "$patch_fixture"
run_make_target "$patch_fixture" up-patch > "$TMP/patch.log" 2>&1 \
  || fail "up-patch fixture failed: $(cat "$TMP/patch.log")"
assert_eq '0.8.111' "$(version_value "$patch_fixture")" 'up-patch version'
assert_contains "$patch_fixture/CHANGES.md" '**Version Bump** — 0.8.110 → 0.8.111: automated release metadata.' 'up-patch changelog'
assert_eq $'install 0.8.111\ndesktop-app 0.8.111' "$(cat "$patch_fixture/make-trace")" 'up-patch command order'
assert_eq 'unrelated' "$(cat "$patch_fixture/unrelated.txt")" 'unrelated fixture file'
assert_contains "$patch_fixture/CHANGES.md" 'Keep this content byte-for-byte.' 'unrelated changelog content'
if grep -Fq 'must not be selected' <<<"$(grep -F '**Version Bump**' "$patch_fixture/CHANGES.md" | head -1)"; then
  fail 'updated the wrong Unreleased section'
fi

# up-minor increments minor and resets patch, then exercises the same ordered
# Make orchestration as up-patch.
minor_fixture="$(new_fixture minor 1.2.99 '1.2.97 → 1.2.99')"
write_make_stubs "$minor_fixture"
run_make_target "$minor_fixture" up-minor > "$TMP/minor.log" 2>&1 \
  || fail "up-minor fixture failed: $(cat "$TMP/minor.log")"
assert_eq '1.3.0' "$(version_value "$minor_fixture")" 'up-minor version'
assert_contains "$minor_fixture/CHANGES.md" '**Version Bump** — 1.2.99 → 1.3.0: automated release metadata.' 'minor changelog'
assert_eq $'install 1.3.0\ndesktop-app 1.3.0' "$(cat "$minor_fixture/make-trace")" 'up-minor command order'

# Decimal-string increment avoids shell integer overflow for valid large
# semver components.
large_version='1.2.999999999999999999999999'
large_line='1.2.999999999999999999999998 → 1.2.999999999999999999999999'
large_fixture="$(new_fixture large "$large_version" "$large_line")"
(
  cd /
  "$large_fixture/scripts/bump-version.sh" patch
) > "$TMP/large.log" 2>&1 || fail "large-version helper failed: $(cat "$TMP/large.log")"
assert_eq '1.2.1000000000000000000000000' "$(version_value "$large_fixture")" 'large-version increment'

# Invalid invocations and missing structural markers must fail before either
# canonical file is replaced.
assert_helper_failure() {
  local name="$1"
  local expected_error="$2"
  shift 2
  local fixture="$TMP/$name"
  cp -R "$patch_fixture" "$fixture"
  cp "$fixture/internal/version/version.go" "$TMP/$name.version.before"
  cp "$fixture/CHANGES.md" "$TMP/$name.changes.before"
  if "$@" > "$TMP/$name.log" 2>&1; then
    fail "$name unexpectedly succeeded"
  fi
  assert_contains "$TMP/$name.log" "$expected_error" "$name error"
  cmp -s "$fixture/internal/version/version.go" "$TMP/$name.version.before" \
    || fail "$name replaced version.go"
  cmp -s "$fixture/CHANGES.md" "$TMP/$name.changes.before" \
    || fail "$name replaced CHANGES.md"
}

malformed_fixture="$TMP/malformed"
mkdir -p "$malformed_fixture/internal/version" "$malformed_fixture/scripts"
cp "$ROOT/scripts/bump-version.sh" "$malformed_fixture/scripts/"
write_version "$malformed_fixture" '1.2'
write_changes "$malformed_fixture" '1.2 → 1.3'
assert_helper_failure malformed 'MAJOR.MINOR.PATCH' "$malformed_fixture/scripts/bump-version.sh" patch
assert_helper_failure usage 'usage:' "$ROOT/scripts/bump-version.sh"
assert_helper_failure kind 'patch or minor' "$ROOT/scripts/bump-version.sh" major

missing_section="$TMP/missing-section"
cp -R "$patch_fixture" "$missing_section"
awk '{ if ($0 == "## [Unreleased]") sub(/## \[Unreleased\]/, "## Not Unreleased"); print }' "$missing_section/CHANGES.md" > "$missing_section/CHANGES.md.tmp"
mv "$missing_section/CHANGES.md.tmp" "$missing_section/CHANGES.md"
assert_helper_failure missing-section '## [Unreleased]' "$missing_section/scripts/bump-version.sh" patch

missing_line="$TMP/missing-line"
cp -R "$patch_fixture" "$missing_line"
awk 'index($0, "**Version Bump**") == 0 { print }' "$missing_line/CHANGES.md" > "$missing_line/CHANGES.md.tmp"
mv "$missing_line/CHANGES.md.tmp" "$missing_line/CHANGES.md"
assert_helper_failure missing-line '**Version Bump**' "$missing_line/scripts/bump-version.sh" patch

# A failed bump must stop before either recursive build runs.
bump_failure_fixture="$(new_fixture bump-failure 1.2 '1.2 → 1.3')"
write_make_stubs "$bump_failure_fixture"
if run_make_target "$bump_failure_fixture" up-patch > "$TMP/bump-failure.log" 2>&1; then
  fail 'up-patch unexpectedly succeeded with a malformed version'
fi
[[ ! -e "$bump_failure_fixture/make-trace" ]] || fail 'build ran after a failed bump'

# A downstream install failure must stop desktop packaging; the already-committed
# version bump remains visible for diagnosis.
failure_fixture="$(new_fixture failure 2.3.4 '2.3.3 → 2.3.4')"
write_make_stubs "$failure_fixture"
if run_make_target "$failure_fixture" up-minor FAIL_INSTALL=1 > "$TMP/failure.log" 2>&1; then
  fail 'up-minor unexpectedly succeeded when install failed'
fi
assert_contains "$TMP/failure.log" 'stub install failure' 'install failure output'
assert_eq '2.4.0' "$(version_value "$failure_fixture")" 'version after downstream failure'
assert_eq 'install 2.4.0' "$(cat "$failure_fixture/make-trace")" 'desktop-app ran after failed install'

echo 'PASS: version bump helper and Makefile targets'
