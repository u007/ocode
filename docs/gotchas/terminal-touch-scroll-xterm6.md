---
type: Gotcha
title: Terminal Does Not Scroll On Touch Devices (xterm.js 6 Has No Touch Scrolling)
description: xterm.js 6.0.0 bundles VS Code's touch Gesture class but never registers the viewport as a target, so a one-finger drag does nothing on phones/tablets. Synthetic wheel events do not work around it (xterm ignores untrusted wheels). Fixed with touch listeners that drive term.scrollLines / SGR wheel reports, plus touch-none on the container.
tags:
  - gotcha
  - terminal
  - touch
  - mobile
  - scroll
  - xterm
  - web-ui
timestamp: 2026-10-09T10:15:00Z
---
## Symptom

Mobile browser pointed at a shared desktop server (Tailscale URL): the Terminal tab cannot be scrolled with a finger drag. Mouse wheel on desktop works.

## Mechanism

`@xterm/xterm` 6.0.0 replaced its own viewport with VS Code's smooth scrollable element. The bundle contains VS Code's `Gesture` (touch → scroll) but `Gesture.addTarget` has no caller, so no touch handling is attached. The wheel path (see `terminal-wheel-scroll-chaining.md`) is the only scroll path.

## Dead end: synthetic wheel events

Dispatching `new WheelEvent("wheel", …)` on the touched element does nothing: measured in a real Chromium against the live dev UI, a trusted CDP `mouseWheel` moved `buffer.active.viewportY` 381 → 371, while untrusted synthetic wheel events on the same element (and on `.xterm-screen`) left it at 381. Do not re-attempt this.

## Fix

`web/src/components/Terminal/TerminalPanel.tsx`, next to `onWheelGuard`: passive `touchstart`/`touchmove`/`touchend`/`touchcancel` listeners on the container. One-finger drag accumulates pixels, converts whole cell rows (`.xterm-screen` height / `term.rows`), carries the remainder, and then:

- `mouseTrackingMode === "none"` → `term.scrollLines(rows)` (drag down = older content = negative).
- otherwise → one SGR wheel report (`ESC [ < 64|65 ; col ; row M`) per row via `term.input(…, true)`.

The container also gets `touch-none` so the browser never claims the pan (which would fire `touchcancel` and make the handlers racy); that disables pinch-zoom inside the terminal only. Limits are tracked in `TODO.md` (SGR-only reports, no alt-screen-without-mouse arrows, no momentum).

## How to verify

Unit: `web/src/components/Terminal/TerminalPanel.wheelGuard.test.tsx` ("touch drags scroll the terminal").

Live (headless Chromium over CDP, dev serve + Vite): enable `Emulation.setTouchEmulationEnabled`, fill the buffer by writing to the xterm instance (found via the React fiber of `.xterm`'s parent — pty input may not reach a sandboxed dev server), then `Input.dispatchTouchEvent` touchStart/touchMove×N/touchEnd inside `.xterm-screen`, and read `term.buffer.active.viewportY`. Keep the touch off any floating overlay (e.g. the TTS bar), which intercepts it. Observed: drag down 381 → 364, drag up 364 → 376, a tap does not scroll.
