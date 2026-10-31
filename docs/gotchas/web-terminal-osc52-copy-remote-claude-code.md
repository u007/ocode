---
type: Gotcha
title: Web terminal OSC 52 copy refused — Claude Code fullscreen on a remote host never reached the clipboard
description: 'A program''s OSC 52 copy reaches the web terminal over the websocket, outside any user gesture. The browser''s transient-activation gate and WKWebView''s gesture-bound clipboard both refused it, so copying from Claude Code (`"tui": "fullscreen"`) in a remote SSH terminal silently did nothing.'
tags:
  - gotcha
  - web
  - desktop
  - terminal
  - clipboard
  - osc52
  - remote
timestamp: 2026-10-16T05:00:00Z
resource: web/src/components/Terminal/TerminalPanel.tsx; web/src/components/Terminal/TerminalPanel.copyOnSelect.test.tsx; web/src/lib/wails.ts; cmd/ocode-desktop/main.go; cmd/ocode-desktop/main_test.go
---
## Symptom

Reported 2026-10-16: selecting text in Claude Code inside a web/desktop terminal tab of a **remote SSH** project never reaches the local clipboard. The same thing works in a local project.

## Root cause

1. Claude Code with `"tui": "fullscreen"` in `~/.claude/settings.json` enables mouse tracking (`?1000h ?1002h ?1003h ?1006h`) and the alt screen (`?1049h`). xterm.js therefore never owns a selection: Claude Code draws it, and the panel's Cmd+C path (`copyViaShortcut`) has nothing to copy.
2. On release Claude Code's `setClipboard` always emits OSC 52. When it does not detect SSH it also calls a native tool. Locally that is `pbcopy`, which is why local projects "work". A headless remote host has no `xclip`/`xsel`/`wl-copy`, so OSC 52 is the only route.
3. The panel's OSC 52 handler refused the write unless `navigator.userActivation.isActive`. Under the spec, transient activation starts at **mousedown** (a mouse pointerup does not refresh it), so a slow drag plus the remote round trip can run it out.
4. Even when the gate passed, `navigator.clipboard.writeText` and the `execCommand("copy")` fallback run from a websocket message, outside any user gesture.

Steps 1, 2 and the missing remote clipboard tools were verified (pty capture of Claude Code 2.1.293, the `setClipboard` source in its binary, `command -v` on the host). Steps 3 and 4 are **inferred** from WebKit's gesture rules and were not reproduced in the WKWebView. If a later report shows the native write still not landing, start there.

xterm.js is not at fault: it has no built-in OSC 52 support, so the handler is ocode's own.

## Fix

- **Gate:** the handler accepts a write only within `OSC52_INPUT_WINDOW_MS` (5 s) of the user's last pointerdown, keydown, or window pointerup **for a press that began in this terminal**. Each panel tracks this itself. `readyRef` (no writes from replayed scrollback) and the 100 000-char payload cap are unchanged.
- **Writer:** `writeClipboardText` sends the text to the native clipboard in the desktop shell (`writeDesktopClipboard` → `ocode:clipboard-write:<text>` → `RawMessageHandler` → `app.Clipboard.SetText`) and does nothing else there. Plain browsers keep `navigator.clipboard` with the `execCommand` fallback.
- **Bridge guard:** `pageClipboardText` accepts the message only when the sender's URL has the same host:port the window was opened on (`desktopWindow.host`), so a page in an iframe (embedded browser, preview) cannot set the clipboard through the shell. It does not use `OriginInfo.IsMainFrame`: Wails fills that in only on darwin, so on Windows and Linux it would have refused every desktop copy. On Linux Wails reports the webview's top URL, so the host check cannot catch an iframe there. The copied text is never logged.
- **Plain browsers are only partly fixed.** The gate no longer refuses a slow drag, but `navigator.clipboard.writeText` itself still needs transient activation. A copy that arrives more than about 5 s after the mousedown can still fail in a browser tab. Only the desktop shell's native write avoids that.

## Rules

- Never gate a terminal OSC 52 write on `navigator.userActivation`. It is measured from the press, and the copy arrives after a round trip.
- Any clipboard write that can arrive outside a user gesture must use `writeDesktopClipboard` in the desktop shell. WKWebView refuses both browser paths there.
- A new page → shell bridge message that acts on the user's machine checks the sender's host against `desktopWindow.host`. Never rely on `OriginInfo.IsMainFrame`, which is darwin-only.

## Workaround (before the fix)

Remove `"tui": "fullscreen"` from the remote host's `~/.claude/settings.json`. xterm then handles selection natively and Cmd+C copies.

## Related

- `gotchas/tui-clipboard-remote-ssh-osc52.md`: the same remote-clipboard problem for ocode's own Bubble Tea TUI.
- `gotchas/radix-modal-focus-trap-breaks-execCommand-clipboard.md`, `gotchas/monaco-webkit-clipboard-workaround.md`: other web clipboard failures.
