---
type: Gotcha
title: Chrome Tab Hang — Unbounded CDP Calls, JS Dialogs, Invisible Popups
description: 'Gotcha: an embedded Chrome tab froze permanently when a CDP command never got a reply (alert() blocking the renderer, dead session); popups and middle-click tabs were never surfaced because page-level auto-attach does not cover them.'
tags:
  - gotcha
  - browse
  - cdp
  - chrome
  - hang
  - popup
timestamp: 2026-09-09T00:00:00Z
---
## Problem

The Chrome-mode browser panel sometimes hung: no input, frozen picture,
address bar stuck on loading, websocket still "connected". Separately,
`window.open`, `target="_blank"` and middle-click never produced a tab.

## Root causes (verified against real Chrome, `internal/browse/cdp/hang_integration_test.go`)

1. **`Conn.Call` waited forever** when a caller passed a context without a
   deadline, and nearly every caller did. The callers sit on single
   serialized goroutines: the websocket reader (`cdpsocket.go`), the Fetch
   interception loop (every request paused behind it), the screencast ack
   loop. One unanswered reply wedged all three. The websocket read
   deadline only refreshes inside `ReadMessage`, which the parked reader
   never re-entered, while the writer kept pinging, so the SPA never saw a
   disconnect and never reconnected.
2. **No `Page.javascriptDialogOpening` handler.** With the Page domain
   enabled, `alert()` blocks the renderer until the client replies to
   `Page.handleJavaScriptDialog`. The test showed even the mouse-up
   `Input.dispatchMouseEvent` never returns, so a click on a button that
   alerts hung the reader on the very click that caused it.
3. **Page-level `Target.setAutoAttach` does not cover popups.** Chrome
   creates the popup target (`openerId` set) but never attaches it and
   never emits `attachedToTarget` for it. Browser-opened tabs
   (middle-click) carry no `openerId` at all; they share the opener's
   browser context.
4. **Handler cancels were collected but never invoked**, so every revoked
   tab leaked ~13 goroutines still issuing commands to a dead session.

## Fix

- `Conn.Call` applies a 30 s default deadline when the context has none.
- Per-target `Page.javascriptDialogOpening` handler auto-accepts and
  emits a `warning` console row.
- Browser-level `Target.setDiscoverTargets`; `Target.targetCreated` for an
  unattached page with `openerId`, or in a browser context a live target
  owns, is attached and registered as a new tab. URL read back with
  `Target.getTargetInfo` because the document request precedes
  `Network.enable`.
- `Target.stopHandlers` runs on `Revoke`.

- The caller's deadline also covers the pipe write (`SetWriteDeadline`
  on the `*os.File`); a timed-out write closes the connection, since the
  stream may hold a partial frame, and Chrome is relaunched on next nav.
- Browser-level `Target.targetDestroyed` drops a tab whose page closed
  itself (`window.close()`), emitting nav error "tab closed".
- Popup/middle-click tabs are flagged `sharedContext`; `Revoke` and the
  crash handler no longer dispose the opener's browser context for them.
- `Server.setBypassed` releases `bypassMu` before calling `TrustHost`.

## Still open

- Dialogs are auto-accepted; a real dialog UI is a follow-up.
