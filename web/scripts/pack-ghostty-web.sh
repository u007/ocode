#!/usr/bin/env bash
# Build the ghostty-web fork (u007/ghostty-web, branch security-fixes) and pack it
# into web/vendor/ as the tarball web/package.json points at with a `file:` spec.
#
# The fork publishes no npm package and ships no committed dist/, and its wasm
# build needs Zig plus the ghostty submodule, so pnpm cannot install it straight
# from git. The checked-in tarball is what every checkout and CI installs.
#
# Usage: web/scripts/pack-ghostty-web.sh <path-to-ghostty-web-checkout>
# Prerequisite inside that checkout: `bun run build:wasm` already run once
# (ghostty-vt.wasm present); this script only rebuilds the JS library.
set -euo pipefail

fork_dir="${1:?usage: $0 <path-to-ghostty-web-checkout>}"
web_dir="$(cd "$(dirname "$0")/.." && pwd)"
vendor_dir="$web_dir/vendor"

if [ ! -f "$fork_dir/ghostty-vt.wasm" ]; then
  echo "error: $fork_dir/ghostty-vt.wasm missing; run 'bun run build:wasm' in the fork first" >&2
  exit 1
fi

sha="$(git -C "$fork_dir" rev-parse --short=7 HEAD)"
version="$(node -p "require('$fork_dir/package.json').version")"
if [ -n "$(git -C "$fork_dir" status --porcelain --untracked-files=no)" ]; then
  echo "error: $fork_dir has uncommitted changes; the tarball name must match a commit" >&2
  exit 1
fi

(cd "$fork_dir" && bun run build:lib && cp ghostty-vt.wasm dist/)

tmp="$(mktemp -d)"
(cd "$fork_dir" && npm pack --silent --pack-destination "$tmp" >/dev/null)
out="$vendor_dir/ghostty-web-$version-$sha.tgz"
mv "$tmp"/ghostty-web-*.tgz "$out"
rmdir "$tmp"

echo "$out"
echo "now set web/package.json: \"ghostty-web\": \"file:vendor/$(basename "$out")\" and run pnpm install"
