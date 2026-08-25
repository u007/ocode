#!/bin/bash
# Capture diagnostic state from a hung ocode desktop app.
# Run this WHILE the desktop chat appears frozen: bash scripts/capture-desktop-hang.sh
# Output lands in /tmp/ocode-hang-<timestamp>/ — attach the directory to the bug report.
set -euo pipefail

HANDLE="$HOME/.config/opencode/desktop-debug-handle"
if [[ ! -f "$HANDLE" ]]; then
  echo "no $HANDLE — is the desktop app running?" >&2
  exit 1
fi
URL=$(grep '^url=' "$HANDLE" | cut -d= -f2)
TOK=$(grep '^token=' "$HANDLE" | cut -d= -f2)
OUT="/tmp/ocode-hang-$(date +%Y%m%d-%H%M%S)"
mkdir -p "$OUT"
echo "capturing to $OUT (server $URL)"

PORT="${URL##*:}"

# 1. Server runtime stats + full goroutine dump (shows any stuck turn/lock).
curl -s -m 5 "$URL/api/debug/runtime?token=$TOK" > "$OUT/runtime.json" \
  || echo "runtime fetch FAILED" | tee "$OUT/runtime.json"
curl -s -m 10 "$URL/debug/pprof/goroutine?debug=2&token=$TOK" > "$OUT/goroutines.txt" \
  || echo "goroutine fetch FAILED" >> "$OUT/goroutines.txt"

# 2. Connection census: is the webview's network process at the 6-conn cap?
lsof -nP -iTCP:"$PORT" > "$OUT/connections.txt" 2>&1 || true
lsof -nP -iTCP:"$PORT" | awk 'NR>1{print $1, $2}' | sort | uniq -c >> "$OUT/connections.txt" || true

# 3. Is the server still publishing? 8s sample of the live event stream.
#    A healthy idle stream shows at least ": ping" keepalives.
curl -s -N -m 8 "$URL/api/events?token=$TOK" > "$OUT/events-sample.txt" 2>&1 || true
echo "--- $(wc -l < "$OUT/events-sample.txt") lines captured ---" >> "$OUT/events-sample.txt"

# 4. Can the server still answer a trivial new chat? (proves per-session turn
#    path is alive server-side while the UI is frozen)
curl -s -m 10 -X POST "$URL/api/chat" \
  -H "Authorization: Bearer $TOK" -H "Content-Type: application/json" \
  -d '{"content":"reply with exactly: hangprobe","project_path":"'"$PWD"'","windowId":"win-hangprobe","async":true}' \
  > "$OUT/chat-probe.json" 2>&1 || echo "chat POST FAILED" >> "$OUT/chat-probe.json"

# 5. Desktop process CPU snapshot (webview main-thread jam shows here).
ps aux | grep -E "ocode|WebKit" | grep -v grep > "$OUT/processes.txt" || true

echo "done: $OUT"
ls -la "$OUT"
