# Part 04 — Build target, macOS bundle, docs, TODO entries

Global constraints from `INDEX.md` apply. This part has no Go code; it packages and documents what Parts 01–03 built.

**Interfaces consumed:** the built binary `./cmd/ocode-desktop` (Part 03); the Wails version pinned in Part 03 Task 5 Step 1 (read it from `go.mod`: `grep wailsapp go.mod`).

---

### Task 7: Makefile target, macOS bundle script, docs, TODO.md

**Files:**
- Modify: `Makefile` (add `desktop` + `desktop-app` targets; match existing target style — read the file first)
- Create: `scripts/bundle-macos.sh`
- Modify: `CHANGES.md` (new entry at top, matching existing entry format)
- Modify: `README.md` (new "Desktop app" section near the existing web/serve docs)
- Modify: `TODO.md` (deferred items — required by project rules)

- [ ] **Step 1: Add Makefile targets**

Read `Makefile` first and copy its existing style (tabs, `bin/` output dir, phony declarations). Add:

```makefile
.PHONY: desktop desktop-app

## desktop: build the ocode-desktop binary (requires cgo + platform webview SDK)
desktop:
	go build -o bin/ocode-desktop ./cmd/ocode-desktop

## desktop-app: build and bundle ocode.app (macOS only)
desktop-app: desktop
	./scripts/bundle-macos.sh bin/ocode-desktop bin/ocode.app
```

Run: `make desktop`
Expected: `bin/ocode-desktop` exists and launches.

- [ ] **Step 2: Write `scripts/bundle-macos.sh`**

(Separate script file, not an inline heredoc in the Makefile, per project script conventions.)

```bash
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
```

Then:

```bash
chmod +x scripts/bundle-macos.sh
make desktop-app
open bin/ocode.app
```

Expected: app launches from Finder/`open`, window shows the ocode UI, notifications carry the app name. (Note: unsigned — Gatekeeper prompts on other machines; signing is a deferred TODO below.)

- [ ] **Step 3: Update docs**

1. `CHANGES.md` — add an entry at the top in the file's existing format:
   - `feat: ocode-desktop — native desktop shell (Wails v3) over the built-in web server; tray, dock badge, run notifications; make desktop / make desktop-app`.
2. `README.md` — add a "Desktop app" section adjacent to the existing serve/web docs covering: what it is (native window over the same web UI), `make desktop`, `make desktop-app` (macOS), Linux prerequisite (`webkit2gtk` dev package), Windows prerequisite (WebView2 runtime, auto-bootstrapped by Wails), dev mode via `OCODE_DESKTOP_DEV_URL` (Vite must proxy `/api` to the logged API address), and the pinned Wails version (from `go.mod`).
3. `docs/superpowers/specs/2026-07-02-desktop-shell-design.md` — under Dev workflow & packaging, replace "Packaging via Wails v3 task tooling" with "macOS bundle via `scripts/bundle-macos.sh`; installer packaging deferred (TODO.md)". Keep the rest.

- [ ] **Step 4: Record deferred work in `TODO.md`**

Append (create the section if absent):

```markdown
## Desktop shell (ocode-desktop) — deferred from 2026-07-02 plan

- [ ] Badge count for pending permission prompts: no server-side data source
      exists yet (runs registry only exposes run status). Needs a pending-
      permissions snapshot on internal/server first.
- [ ] Windows installer + Linux packaging (deb/rpm/AppImage): only a raw
      binary and a macOS .app bundle ship today. Evaluate wails3 packaging
      tooling once out of alpha.
- [ ] macOS code signing/notarization for ocode.app.
- [ ] Notification click → focus window: wire only if the pinned Wails alpha
      exposes a notification-response callback (checked in Part 03 Task 6).
- [ ] External links opening in the default browser: confirm pinned Wails
      alpha behavior (checked in Part 03 Task 5 Step 2); add handling if the
      webview keeps them internal.
```

Delete any of these lines that were actually resolved during Part 03 (e.g. notification click handling existed and was wired).

- [ ] **Step 5: Final verification**

```bash
go build ./... && go test ./...
make desktop && make desktop-app
grep -rn "wailsapp" --include="*.go" . | grep -v cmd/ocode-desktop | grep -v _test
```

Expected: build/tests PASS; both artifacts produced; the grep prints nothing (main binary stayed pure-Go).

- [ ] **Step 6: Commit**

```bash
git add Makefile scripts/bundle-macos.sh CHANGES.md README.md TODO.md docs/superpowers/specs/2026-07-02-desktop-shell-design.md
git commit -m "build(desktop): make targets, macOS bundle script, docs

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```
