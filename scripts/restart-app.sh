#!/usr/bin/env bash
# Close the running ocode.app (if any) and relaunch it.
# Detached: spawns a background killer that survives the caller, so this
# script can be invoked from inside the running app (menu item, etc.) as well
# as from `make restart-app`.
# Usage: make restart-app   (or scripts/restart-app.sh [path/to/ocode.app])
set -euo pipefail

APP="${1:-}"
if [[ -z "$APP" ]]; then
  if [[ -d "${PWD}/bin/ocode.app" ]]; then
    APP="${PWD}/bin/ocode.app"
  elif [[ -d "/Applications/ocode.app" ]]; then
    APP="/Applications/ocode.app"
  else
    echo "error: ocode.app not found (looked in ${PWD}/bin and /Applications)" >&2
    exit 1
  fi
fi
APP="$(cd "$(dirname "$APP")" && pwd)/$(basename "$APP")"

# Resolve the running app's PID (macOS). Fall back to an exact-path pgrep.
PID=""
if command -v lsappinfo >/dev/null 2>&1; then
  PID="$(lsappinfo info -only pid "ocode" 2>/dev/null | sed -n 's/.*pid = \([0-9]*\).*/\1/p' | head -n1)"
fi
if [[ -z "$PID" ]] || ! kill -0 "$PID" 2>/dev/null; then
  # Anchor to the main process only: an unanchored -f match also lists the
  # lsp-daemon children (they carry the same binary path), and head -n1 would
  # then pick a daemon instead of the app.
  PID="$(pgrep -f "^${APP}/Contents/MacOS/ocode\$" 2>/dev/null | head -n1 || true)"
fi

# Not running: there is nothing to close, and an empty or 0 PID would make the
# helper's kill/pgrep act on the whole process group. Just open the bundle.
if [[ -z "$PID" ]]; then
  open "$APP"
  echo "ocode.app is not running; opened ${APP}"
  exit 0
fi

# Spawn a detached killer that survives this process and the app's death.
# The `$$` guard stops kill_tree from reaping its own shell (the helper is a
# descendant of the app when this script is run from an ocode terminal tab).
# The app path is passed as argv, never interpolated into the script body.
nohup /bin/sh -c '
  sleep 2
  kill_tree() {
    for c in $(pgrep -P "$1" 2>/dev/null); do
      [ "$c" = "$$" ] && continue
      kill_tree "$c"
      kill "$c" 2>/dev/null || true
    done
  }
  kill_tree "$1"
  kill "$1" 2>/dev/null || true
  sleep 1
  kill -9 "$1" 2>/dev/null || true
  sleep 1
  open "$2"
' sh "$PID" "$APP" >/dev/null 2>&1 &
disown 2>/dev/null || true

echo "Restarting ocode.app (PID ${PID})..."
