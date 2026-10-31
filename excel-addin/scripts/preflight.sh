#!/bin/bash
# Checks every setup step the Excel add-in needs, in order, and prints the fix for the first failures.
# Exit 0 only when a task pane is connected and the excel_* tools can run. Full steps: skills/ocode-excel/SKILL.md.
# Env: BRIDGE_URL (default https://localhost:3443), OCODE_URL (default http://127.0.0.1:4096),
#      EXCEL_BRIDGE_PANE_KEY (needed for the MCP check), CERT_DIR (default ~/.office-addin-dev-certs).
set -u
HERE=$(cd "$(dirname "$0")/.." && pwd)
BRIDGE_URL=${BRIDGE_URL:-https://localhost:3443}
OCODE_URL=${OCODE_URL:-http://127.0.0.1:4096}
CERT_DIR=${CERT_DIR:-$HOME/.office-addin-dev-certs}
bad=0
ok()   { echo "PASS  $1"; }
fail() { echo "FAIL  $1"; echo "      fix: $2"; bad=1; }

[ -x "$HERE/bin/excel-bridge" ] && [ -f "$HERE/taskpane/dist/taskpane.html" ] \
  && ok "built (bin/excel-bridge, taskpane/dist)" \
  || fail "not built" "cd excel-addin && make test build"

# The taskpane build runs `bun check .` (same convention as web/package.json).
if command -v bun >/dev/null 2>&1; then ok "bun present (taskpane build runs bun check .)"
else fail "bun not found" "install Bun (https://bun.sh): the taskpane build runs 'bun check .'"; fi

if [ -f "$CERT_DIR/localhost.crt" ] && [ -f "$CERT_DIR/localhost.key" ]; then ok "dev certificate files exist"
else fail "no dev certificate in $CERT_DIR" "USER must run (admin password): ! npx office-addin-dev-certs@3 install"; fi

health=$(curl -s -m 5 "$BRIDGE_URL/bridge/health" 2>/dev/null)
if [ -n "$health" ]; then ok "bridge reachable over trusted HTTPS ($BRIDGE_URL)"
else fail "bridge not reachable or certificate not trusted at $BRIDGE_URL" \
  "start it (skills/ocode-excel step 5); if curl says the certificate is untrusted, USER re-runs: ! npx office-addin-dev-certs@3 install"; fi

curl -s -m 5 -o /dev/null -w '%{http_code}' "$OCODE_URL/api/health" 2>/dev/null | grep -q '^200$' \
  && ok "ocode serve reachable ($OCODE_URL)" \
  || fail "ocode serve not reachable at $OCODE_URL" "start it: ocode serve -host 127.0.0.1 -port <port>, and pass -ocode http://127.0.0.1:<port> to the bridge"

if [ -f "$HERE/manifest/manifest.xml" ]; then
  ver=$(sed -n 's:.*<Version>\([0-9]*\)\..*</Version>.*:\1:p' "$HERE/manifest/manifest.xml" | head -1)
  case "$ver" in
    ''|*[!0-9]*)
      fail "manifest <Version> is not numeric (${ver:-empty})" "set <Version> to 1.0.0.0 in manifest/manifest.template.xml, then regenerate" ;;
    *)
      if [ "$ver" -ge 1 ]; then ok "manifest generated, version >= 1.0"
      else fail "manifest version below 1.0 (Microsoft's validator rejects it)" "set <Version> in manifest/manifest.template.xml to 1.0.0.0 and regenerate"; fi ;;
  esac
else fail "manifest not generated" "EXCEL_BRIDGE_PANE_KEY=<key> node scripts/gen-manifest.mjs --base https://localhost:3443"; fi

if [ -n "${EXCEL_BRIDGE_PANE_KEY:-}" ]; then
  tools=$(curl -s -m 5 -X POST "$BRIDGE_URL/mcp" -H "X-Bridge-Key: $EXCEL_BRIDGE_PANE_KEY" -H 'content-type: application/json' \
    -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' 2>/dev/null)
  echo "$tools" | grep -q describe_workbook && ok "bridge serves the excel_* tools over /mcp with this pane key" \
    || fail "/mcp did not list the tools (wrong pane key?)" "use the same EXCEL_BRIDGE_PANE_KEY for the bridge, the manifest and the ocode MCP entry"
else echo "SKIP  /mcp check (EXCEL_BRIDGE_PANE_KEY not set)"; fi

wef="$HOME/Library/Containers/com.microsoft.Excel/Data/Documents/wef"
if [ "$(uname)" = Darwin ]; then
  if [ -f "$wef/ocode-excel.xml" ]; then ok "manifest sideloaded as $wef/ocode-excel.xml"
  else echo "UNKNOWN sideload: macOS blocks terminals from Excel's container, so this cannot be verified from a shell"
       echo "      fix: USER grants the terminal Full Disk Access (System Settings > Privacy & Security), restarts it, then:"
       echo "           mkdir -p \"$wef\" && cp $HERE/manifest/manifest.xml \"$wef/ocode-excel.xml\""
       echo "           (or copy the file in Finder, or use Excel on the web: Insert > Add-ins > Upload My Add-in)"; fi
  # Excel reads EVERY .xml in wef, so a second manifest shows the add-in twice.
  stale=$(ls "$wef"/*.xml 2>/dev/null | grep -v '/ocode-excel.xml$' || true)
  if [ -n "$stale" ]; then fail "stale manifest(s) in $wef" "rm $stale (Excel reads every .xml there, so the add-in appears twice)"; fi
fi

case "$health" in
  *'"workbooks":[]'*|"") fail "no task pane connected (no workbook in /bridge/health)" \
    "USER quits and reopens Excel, opens a workbook, then Home > Add-ins > Ask ocode. If it is missing, the sideload step failed" ;;
  *) ok "a task pane is connected: $health" ;;
esac
exit $bad
