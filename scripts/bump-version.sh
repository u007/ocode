#!/bin/sh
# Bump the canonical ocode version and its [Unreleased] changelog entry.
# Usage: scripts/bump-version.sh <patch|minor>
set -eu

fail() {
  echo "error: $*" >&2
  exit 1
}

if [ "$#" -ne 1 ]; then
  echo "usage: $0 <patch|minor>" >&2
  exit 2
fi

case "$1" in
  patch|minor) KIND="$1" ;;
  *) fail "unknown bump kind '$1'; expected patch or minor" ;;
esac

ROOT="$(CDPATH='' cd -P "$(dirname "$0")/.." && pwd)"
VERSION_FILE="$ROOT/internal/version/version.go"
CHANGES_FILE="$ROOT/CHANGES.md"

[ -f "$VERSION_FILE" ] || fail "version file not found: $VERSION_FILE"
[ -f "$CHANGES_FILE" ] || fail "changelog not found: $CHANGES_FILE"

DECLARATION_COUNT="$(
  sed -n '/^const Version = "[^"]*"$/p' "$VERSION_FILE" | awk 'END { print NR + 0 }'
)"
[ "$DECLARATION_COUNT" -eq 1 ] \
  || fail "expected exactly one const Version declaration in $VERSION_FILE, found $DECLARATION_COUNT"

CURRENT="$(
  sed -n 's/^const Version = "\([^"]*\)"$/\1/p' "$VERSION_FILE"
)"
if ! printf '%s\n' "$CURRENT" | awk -F. '
  NF != 3 { invalid = 1 }
  {
    for (i = 1; i <= NF; i++) {
      if ($i !~ /^(0|[1-9][0-9]*)$/) invalid = 1
    }
  }
  END { exit invalid ? 1 : 0 }
'; then
  fail "version in $VERSION_FILE must be MAJOR.MINOR.PATCH with numeric components and no leading zeroes: got '$CURRENT'"
fi

increment_decimal() {
  awk -v value="$1" 'BEGIN {
    if (value == "0") {
      print "1"
      exit
    }
    i = length(value)
    carried = ""
    while (i > 0) {
      digit = substr(value, i, 1)
      if (digit != "9") {
        print substr(value, 1, i - 1) (digit + 1) substr(value, i + 1)
        exit
      }
      carried = carried "0"
      i--
    }
    print "1" carried
  }'
}

REST="${CURRENT#*.}"
OLD_MAJOR="${CURRENT%%.*}"
OLD_MINOR="${REST%%.*}"
OLD_PATCH="${REST#*.}"

case "$KIND" in
  patch) NEW="$OLD_MAJOR.$OLD_MINOR.$(increment_decimal "$OLD_PATCH")" ;;
  minor) NEW="$OLD_MAJOR.$(increment_decimal "$OLD_MINOR").0" ;;
esac

TMP_DIR="$(mktemp -d "$ROOT/.version-bump.XXXXXX")" \
  || fail "could not create a temporary directory under $ROOT"
KEEP_TMP=0
cleanup() {
  if [ "$KEEP_TMP" -eq 0 ]; then
    rm -rf "$TMP_DIR"
  fi
}
trap cleanup 0
trap 'exit 1' HUP INT TERM

VERSION_NEXT="$TMP_DIR/version.go"
CHANGES_NEXT="$TMP_DIR/CHANGES.md"

awk -v current="$CURRENT" -v replacement="const Version = \"$NEW\"" '
  !replaced && $0 == "const Version = \"" current "\"" { print replacement; replaced = 1; next }
  { print }
  END { if (!replaced) exit 1 }
' "$VERSION_FILE" > "$VERSION_NEXT" \
  || fail "could not prepare the updated version file"

if awk -v old="$CURRENT" -v new="$NEW" '
  BEGIN {
    in_unreleased = 0
    found_unreleased = 0
    replaced = 0
    exit_code = 0
  }
  !in_unreleased && !found_unreleased && $0 == "## [Unreleased]" {
    in_unreleased = 1
    found_unreleased = 1
    print
    next
  }
  in_unreleased && /^## / {
    if (!replaced) {
      exit_code = 2
      exit 2
    }
    in_unreleased = 0
    print
    next
  }
  in_unreleased && !replaced && index($0, "**Version Bump**") {
    updated = "**Version Bump** — " old " → " new
    if (!sub(/\*\*Version Bump\*\* — [0-9]+\.[0-9]+\.[0-9]+ → [0-9]+\.[0-9]+\.[0-9]+/, updated)) {
      exit_code = 3
      exit 3
    }
    replaced = 1
    print
    next
  }
  { print }
  END {
    if (exit_code != 0) exit exit_code
    if (!found_unreleased) exit 4
    if (!replaced) exit 5
  }
' "$CHANGES_FILE" > "$CHANGES_NEXT"; then
  :
else
  STATUS=$?
  case "$STATUS" in
    2) fail "the first ## [Unreleased] section has no **Version Bump** line" ;;
    3) fail "the first **Version Bump** line must use the form '**Version Bump** — MAJOR.MINOR.PATCH → MAJOR.MINOR.PATCH'" ;;
    4) fail "CHANGES.md has no '## [Unreleased]' section" ;;
    5) fail "the first ## [Unreleased] section has no **Version Bump** line" ;;
    *) fail "could not prepare the updated changelog (awk exit $STATUS)" ;;
  esac
fi

VERSION_BACKUP="$TMP_DIR/version.go.before"
CHANGES_BACKUP="$TMP_DIR/CHANGES.md.before"
cp "$VERSION_FILE" "$VERSION_BACKUP" || fail "could not back up $VERSION_FILE"
cp "$CHANGES_FILE" "$CHANGES_BACKUP" || fail "could not back up $CHANGES_FILE"

if cp "$VERSION_NEXT" "$VERSION_FILE"; then
  :
else
  fail "could not update $VERSION_FILE; CHANGES.md was not changed"
fi

if cp "$CHANGES_NEXT" "$CHANGES_FILE"; then
  :
else
  RESTORE_OK=1
  cp "$VERSION_BACKUP" "$VERSION_FILE" || RESTORE_OK=0
  cp "$CHANGES_BACKUP" "$CHANGES_FILE" || RESTORE_OK=0
  if [ "$RESTORE_OK" -eq 1 ]; then
    fail "could not update $CHANGES_FILE; restored both canonical files"
  fi
  KEEP_TMP=1
  fail "could not update $CHANGES_FILE and rollback was incomplete; backups retained in $TMP_DIR"
fi

echo "Version bumped: $CURRENT -> $NEW"
