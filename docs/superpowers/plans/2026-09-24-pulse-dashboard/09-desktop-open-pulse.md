# Part 09 — Desktop: open Pulse from dock / tray

Spec: `docs/superpowers/specs/2026-09-24-pulse-dashboard-design.md`
("Entry points → Desktop").

## Context

- Desktop app: `cmd/ocode-desktop/` (Wails v3). Dock service created at
  `main.go:176`; tray menu at `main.go:426-450` ("Show ocode", "Open
  DevTools", "Copy Debug URL"); badge/notifications wired in `native.go`
  via `wireNative` (`main.go:465`).
- The web app switches to Pulse when its `activeView` becomes `"pulse"`
  (in `web/src/App.tsx`). The desktop shell needs a way to tell the page to
  do that.

## Files

- Modify: `cmd/ocode-desktop/main.go` — tray item "Open Pulse"; dock
  activation hook if supported.
- Modify: `cmd/ocode-desktop/native.go` if dock click wiring lives there.
- Modify: `web/src/App.tsx` — listen for the shell's open-Pulse signal.
- Test: `cmd/ocode-desktop/*_test.go` for any pure helper; web test for the
  signal handler.

## Interfaces

- Produces: a shell→page signal named `ocode:open-pulse`. Transport: use
  whatever mechanism the shell already uses to message the page (check
  `native.go` / existing Wails events emitted to the window); the web side
  sets `activeView` to `"pulse"` and focuses the window.

## Steps

- [ ] **Verify capability first**: fetch current Wails v3 docs via `ctx7`
  for dock click / application-activate events. If the dock badge click is
  not exposed, STOP and ask the user whether a tray item alone is acceptable
  (do not silently substitute).
- [ ] **Failing web test**: dispatching the `ocode:open-pulse` signal sets
  the Pulse view.
- [ ] Run → FAIL; implement listener; run → PASS.
- [ ] **Implement** tray "Open Pulse" (shows window, emits signal) and dock
  activation (if supported) emitting the same signal only when there are
  sessions needing the user (`PendingPermissionAsks()` non-empty) — plain
  activation otherwise keeps current behavior.
- [ ] `go build ./cmd/ocode-desktop && go test ./cmd/ocode-desktop/...`.
- [ ] Manual: launch desktop, trigger a permission ask, click dock icon /
  tray item → Pulse shows.
- [ ] Commit: `feat(desktop): open Pulse from tray and dock`.
