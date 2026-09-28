---
type: Gotcha
title: Desktop webview has no Wails event system — shell→page signals must use ExecJS + DOM CustomEvent
description: Desktop webview is served over plain http:// (embed.FS), so Wails window._wails/EmitEvent never exist — shell→page signals must use win.ExecJS + DOM CustomEvent with a React receiver.
tags:
  - gotcha
  - desktop
  - wails
  - execjs
  - customevent
  - webview
  - embed-fs
  - pulse
  - settings
  - menu
  - tray
  - shell-signals
timestamp: 2026-09-28T11:44:23Z
---
# Desktop shell → page signals: no Wails events, use ExecJS + DOM CustomEvent

**Type:** Gotcha  
**Description:** The desktop webview is served over plain http:// by ocode's embed.FS server, so window._wails / window.EmitEvent / window.wails.Events never exist (structural no-op, not timing) — shell→page signals must use win.ExecJS dispatching a plain DOM CustomEvent, with a React receiver.  
**Tags:** gotcha, desktop, wails, execjs, customevent, webview, embed-fs, pulse, settings, menu, tray, shell-signals  

---

# Desktop webview has no Wails event system — shell → page must be ExecJS + CustomEvent

Confirmed while wiring the Pulse dashboard open path (`openPulseInPage` in
`cmd/ocode-desktop/main.go`), but it applies to **every** shell → page signal
(app menu items, tray menu, anything native that wants to poke the SPA).

## The fact

The desktop webview loads a **plain `http://127.0.0.1:PORT` URL** served by
ocode's own `embed.FS`-backed HTTP server — **not** the `wails://` scheme. Wails
injects `window._wails` (and hence `window.EmitEvent` / `window.wails.Events`)
via that scheme handler, so in this app those APIs are **structurally
unavailable**:

- It is a **no-op, not a timing issue**. Waiting longer, listening for
  `wails:runtime:ready`, or re-trying after load never helps — the injection
  path simply does not run for an `http://` URL.
- Do **not** wire shell → page signalling through `window.EmitEvent` /
  `Events.On`; it will compile, register listeners, and silently deliver
  nothing.

The canonical comment lives at `cmd/ocode-desktop/main.go:582-586` (above
`msgFocusWindow`).

## The pattern (shell → page)

Shell side: `window.ExecJS(...)` dispatching a **bare DOM `CustomEvent`** (no
`detail` unless truly needed — keeping it payload-free leaves the meaning
entirely to the page):

```go
// cmd/ocode-desktop/main.go — openPulseInPage()
window.ExecJS(`window.dispatchEvent(new CustomEvent("ocode:open-pulse"))`)
```

`ExecJS` runs arbitrary JS in the page **regardless of how the page was
loaded**, which is exactly why it works where `EmitEvent` does not
(`main.go:629-632`).

Existing signals using this exact mechanism:

| Event | Emitted from | Receiver |
|---|---|---|
| `ocode:open-pulse` | `openPulseInPage` (`main.go:637`), tray/app menu | `web/src/components/Pulse/PulseShellSignal.tsx` (`PULSE_SHELL_EVENT`) |
| `ocode:open-settings` | app menu Settings… (`main.go:710`, `:742`) | `web/src/components/Settings/SettingsPanel.tsx` |
| `ocode:share-session` / `ocode:share-desktop` / `ocode:copy-desktop-url` | share menu + tray (`main.go:538`, `:541`, `:773`, `:777`, `:782`) | mounted listeners in the SPA |
| `ocode:quit-blocked` | `notifyQuitBlocked` (`main.go:75-76`) | `installQuitBlockedListener()` in `web/src/App.tsx` |

Page side: a **React receiver component** that registers
`window.addEventListener(EVENT, handler)` in a `useEffect` and removes it on
cleanup (see `PulseShellSignal.tsx` — a renderless component returning `null`,
taking `onOpen`/`onFocus` callbacks). The event name string is the **contract**
between Go and TS; there is no type sharing, so keep it in a named exported
constant on the web side (`export const PULSE_SHELL_EVENT = "ocode:open-pulse"`).

**Inert in plain browsers:** a browser build never dispatches these events, so
the receiver's listener simply never fires — no guard for "am I in desktop?"
is needed.

## The other direction (page → shell) is different

Page → shell does **not** use this pattern: it travels the minimal
`window._wails.invoke` bridge (`web/src/lib/wails.ts`, e.g.
`focusDesktopWindow`) into `application.Options.RawMessageHandler`
(`main.go:578-580`), because the *full* Wails runtime module is also
unavailable in the page. So:

- **page → shell**: minimal `_wails.invoke` raw messages (`ocode:focus-window`,
  `ocode:open-external:`, quit-guard messages).
- **shell → page**: `win.ExecJS` + DOM `CustomEvent`.

## Why this is worth recording

Future work that wires "native thing happened → open a view / notify the SPA"
through Wails' event system will **silently no-op**: no error, no console
warning, listeners registered and never called. The failure mode is invisible
in development unless you test from the actual native trigger (menu/tray),
which is easy to skip. Reach for `ExecJS` + `CustomEvent` + a React receiver
instead.

## Related

- `concepts/pulse-dashboard.md` — desktop wiring section records the same
  transport fact in the context of the Pulse open path.
- `gotchas/desktop-quit-guard-and-sticky-port-fallback.md` — sibling gotcha
  covering the quit-guard `ocode:quit-blocked` ExecJS signal and the sticky-port
  localStorage-origin issue.
- Skills: `skills/ocode-desktop/SKILL.md`, `skills/ocode-web/SKILL.md` (keep
  in sync if the pattern changes).
