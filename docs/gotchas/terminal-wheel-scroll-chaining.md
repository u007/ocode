---
type: Gotcha
title: Desktop/Web Terminal Wheel Scroll Chains to the App Page (xterm.js Escape Gestures)
description: Wheel gestures over the terminal that xterm.js cannot use (scrollback at an edge, deltaY===0) escape to the browser and scroll the whole app window instead of the terminal. Fixed with a container-level non-passive wheel guard + overscroll-contain on the terminal container and overscroll-behavior:none on html/body.
tags:
  - gotcha
  - terminal
  - wheel
  - scroll
  - xterm
  - desktop
  - web-ui
  - overscroll
  - scroll-chaining
timestamp: 2026-09-23T01:15:51Z
---
## Symptom

User report (confirmed): on the terminal tab in the web UI and desktop, cannot scroll Claude Code running inside the terminal — only the window scrolls. Reproduced over a **fullscreen Claude Code session** (alternate screen + mouse capture) in the **desktop app**: the app page moved instead of the terminal.

## Mechanism

xterm.js consumes wheel gestures it can use and calls `preventDefault()` + `stopPropagation()` when it does — in two places:

1. **Scrolling its own scrollback** — `Viewport`'s `SmoothScrollableElement._onMouseWheel`, which only consumes when the scroll position actually changes.
2. **Forwarding a mouse report to a TUI that owns the mouse** — `CoreBrowserTerminal.bindMouse`'s wheel listener, which cancels unconditionally when the mouse protocol reports wheel events.

Gestures xterm leaves alone are handed to the browser, which scrolls the nearest scrollable ancestor. In the desktop shell (Wails/WKWebView) even the non-scrollable document rubber-bands, so leftover gestures visibly moved the whole app window.

Leftover cases observed live:
- The normal-buffer scrollback at either edge (58/60 wheel events unconsumed in one run).
- `deltaY === 0`.

Note: the app document itself is never scrollable in a browser (App root is `h-screen`; measured `scrollHeight == innerHeight` at 600–1440px wide and 340–1200px tall), so Chrome just ignores the escape while WKWebView bounces.

## Fix

**`web/src/components/Terminal/TerminalPanel.tsx`**: container-level, non-passive `onWheelGuard` (`el.addEventListener("wheel", onWheelGuard, { passive: false })`, removed in the effect cleanup). It sits on the fit-parent container, not `.xterm`, so xterm's own (deeper) handlers run first and stop propagation when they consume; the guard therefore only sees gestures xterm ignored and calls `preventDefault()` on them — except when the container's own `overflow-y-auto` fallback can still scroll in that direction (`deltaY < 0 ? scrollTop > 0 : deltaY > 0 ? scrollTop < scrollHeight - clientHeight : false`), which keeps the "terminal taller than its box between a font-size change and the next fit" fallback reachable. The container className also gained `overscroll-contain`.

**`web/src/index.css`**: `overscroll-behavior: none` on `html, body` inside the existing `@layer base` (the app is a full-height SPA; this kills the WKWebView document-level bounce for leftover gestures anywhere in the app while leaving inner panels' own scrolling intact).

**Regression test**: `web/src/components/Terminal/TerminalPanel.wheelGuard.test.tsx` (4 tests; mutation-verified — removing the listener fails the two swallow assertions). The stubbed Terminal creates a child `.xterm` surface and installs a consuming wheel listener there (`preventDefault` + `stopPropagation`) so the real propagation ordering is exercised.

## Background: Claude Code fullscreen wheel handling

Claude Code 2.1.280's fullscreen renderer enables the alternate screen + mouse capture (`\x1b[?1049h` + `\x1b[?1000h/?1002h/?1003h/?1006h`, no 1016 pixel mode, no 1007) and parses SGR wheel reports into `wheelup`/`wheeldown` keys bound to `scroll:lineUp`/`scroll:lineDown` in its "Scroll" keybinding context (active only when the transcript view is focused and its content overflows the viewport).

In **classic mode** (`CLAUDE_CODE_DISABLE_ALTERNATE_SCREEN=1`) it does NOT capture the mouse, so the wheel scrolls the terminal's own scrollback instead. ocode's `TERMINAL_MOUSE_RESET` (`TerminalPanel.tsx`, written on a fresh attach or a history-cursor reattach) clears xterm's mouse modes so a dead TUI's stale DECSET can't turn mouse moves into shell garbage — relevant because while the mouse protocol is off, a fullscreen TUI receives no wheel reports.

## How to verify

**Unit test** — run `web/src/components/Terminal/TerminalPanel.wheelGuard.test.tsx` (4 tests).

**Live recipe**:
1. Build and serve (`go build -o /tmp/ocode-test .` + `OPENCODE_SERVER_USERNAME=test OPENCODE_SERVER_PASSWORD=testpass /tmp/ocode-test serve -port <port>`), or use the running desktop server's token from `~/.config/opencode/desktop-debug-handle`.
2. Insert a spacer div above the app to make the page scrollable.
3. Compare a control gesture over the sidebar (should scroll the page) with one over the terminal (should NOT move the page; the terminal should scroll internally, and a fullscreen Claude Code session should still receive SGR wheel reports).

## Live evidence

Headless Chromium against the running desktop app's server on 127.0.0.1:59658, real pty, real Claude Code v2.1.280, 2000px of scrollable room inserted above the app:
- Wheel over the sidebar moved the page 2000→800 (control, 6/6 unconsumed).
- Every gesture over the terminal kept `winY` pinned at 2000 while the shell scrollback still scrolled to the top/bottom and a fullscreen Claude Code conversation still received 10/60/20/60 SGR wheel reports and scrolled.
- The guard fired only at the scrollback edges.

`tsgo --noEmit`, `vite build`, and the Terminal+Layout suites (306 tests) green; built CSS contains both `overscroll-behavior:none` and `overscroll-behavior:contain`.
