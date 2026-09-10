---
type: Gotcha
title: macOS Headless Chrome Freezes on Page-Unhandled Keys (nativeVirtualKeyCode)
description: 'Gotcha: in the embedded Chrome tab, a second Escape/Tab/Enter or a letter typed outside an input froze the whole browser process (all tabs, screencast, every CDP call) until a real mouse move. Cause: Input.dispatchKeyEvent with nativeVirtualKeyCode makes Chrome re-dispatch unhandled keys to its hidden AppKit window, which blocks in SLSObscureCursor.'
tags:
  - gotcha
  - browse
  - cdp
  - chrome
  - macos
  - hang
  - keyboard
timestamp: 2026-09-10T00:00:00Z
---
## Problem

Embedded browser tab (headless Chrome over CDP) on macOS: pressing a key the
page does not consume — a second Escape, Tab on the body, Enter with nothing
focused, a letter typed outside an input — froze Chrome entirely. Every CDP
call on every session (browser and page) timed out, the screencast stopped,
and `Conn.Call`'s 30 s deadline eventually closed the pipe. The freeze
sometimes cleared after 0.2–2 s, which turned out to be the physical mouse
moving; otherwise it lasted 30 s+.

This is very likely one of the "sometimes hung: no input, frozen picture"
reports behind `chrome-tab-hang-unbounded-cdp-call.md`.

## Root cause

`sample` of the frozen browser process: main thread inside a Chrome task
calling `-[NSApplication sendEvent:]` → `routeKeyEquivalent` →
`-[NSView performKeyEquivalent:]` → Chrome → `SLSObscureCursor` blocked in
`mach_msg` (plus some time in `-[NSMenu _enableItems]`). That is Chrome
re-dispatching a page-unhandled keyDown to its hidden headless NSWindow.

Chromium `content/browser/devtools/protocol/input_handler.cc`,
`DispatchKeyEvent`:

```cpp
if (event.native_key_code && allow_sending_input_to_browser_)
  event.os_event = NativeInputEventBuilder::CreateEvent(event);
else
  event.skip_if_unhandled = true;
```

ocode's `keyEventParams` sent `nativeVirtualKeyCode` alongside
`windowsVirtualKeyCode`, so every unhandled key built an OS event and took
the AppKit path. Headless Chrome's hidden window never gets real mouse
events, so the WindowServer cursor call never returns.

Reproduced on Chrome 152 and Chromium, macOS 26. Not specific to Escape or
to any input type; date/time/datalist popups were red herrings.

## Fix

`keyEventParams` sends only `windowsVirtualKeyCode`. Unhandled keys now stop
at the renderer (`skip_if_unhandled`). Verified: typing, Backspace,
arrows, Cmd+A, Tab focus traversal, macOS editing `commands`, the in-page
`<select>` picker, date/time segment editing all still work; ten
back-to-back unhandled keys cause no stall.

## Trade-off

The `<datalist>` suggestion popup is a browser-side popup that took keys via
the same OS-event path, so ArrowDown + Enter no longer picks a suggestion.
Typing into the input still works, and the popup was invisible in the
screencast anyway. Browser-level shortcuts (Cmd+L etc.) are also never
reached, which is the desired behaviour for a remote-viewed page.
