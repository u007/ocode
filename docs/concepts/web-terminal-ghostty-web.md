---
type: Concept
title: Web/Desktop Terminal on ghostty-web (libghostty-vt)
description: |
  The web/desktop terminal renders with ghostty-web (libghostty-vt wasm, canvas 2D; ocode's u007/ghostty-web security-fixes fork of 0.4.0, vendored as a tarball in web/vendor) instead of xterm.js. Records the verified traps of that library (no OSC handlers, bell on OSC terminators, built-in mouse reporting without focus events, built-in copy-on-select, inverted key-handler convention, unzeroed wasm heap in upstream 0.4.0), the ocode wrapper modules that compensate, and the pty environment (TERM pinned to xterm-256color).
resource: ""
tags:
  - terminal
  - ghostty-web
  - libghostty
  - web
  - desktop
  - pulse
timestamp: 2026-10-30T16:23:00Z
---
# Web/Desktop Terminal on ghostty-web

Applies from 2026-10-28. Earlier xterm.js behaviour is recorded only in the gotchas marked historical.

The terminal panel uses `Terminal` and `FitAddon` from `ghostty-web` (libghostty-vt compiled to wasm, drawn on a canvas 2D), installed from ocode's fork rather than npm (see "Dependency source"). The ~400 KB wasm is embedded in the JS bundle as a `data:` URL, so there is no asset or MIME concern. Pulse reads terminal screens with the headless `GhosttyTerminal` from the same package.

## Dependency source

`web/package.json` pins `ghostty-web` to `file:vendor/ghostty-web-0.4.0-<sha>.tgz`, a tarball built from the `security-fixes` branch of [u007/ghostty-web](https://github.com/u007/ghostty-web), a fork of coder/ghostty-web based on upstream main after the 0.4.0 release (so it also carries upstream's mouse tracking, PR #106, which the npm 0.4.0 package lacked). The fork publishes no npm package and commits no `dist/`, and its wasm build needs Zig plus the ghostty submodule, so pnpm cannot install it from git; the checked-in tarball is what every checkout and CI installs. On top of upstream 0.4.0 the branch carries: pasted control characters replaced with spaces as native Ghostty does (CVE-2026-26982, upstream PR #193), OSC 8 links limited to http(s) plus a `linkHandler` option (PR #194), window titles stripped of control characters and capped at 255 bytes (PR #195), the inlined wasm decoded in memory instead of fetched so a strict CSP works (PR #196), zero-initialized wasm page buffers (PR #142, see the heap trap below), and dependency CVE bumps. To update: commit in the fork checkout, run `bun run build:wasm` there once (needs Zig), then `web/scripts/pack-ghostty-web.sh <fork-dir>`, point `package.json` at the printed tarball, run `pnpm install`, and delete the old tarball.

## Traps of ghostty-web 0.4.0

- **Load the wasm yourself.** `new Terminal` throws unless `options.ghostty` is passed or `init()` ran. ocode never calls `init()`; `ghosttyRuntime.ts` loads `Ghostty` and the panel passes it as `options.ghostty`.
- **Unzeroed wasm heap in upstream 0.4.0 (measured 2026-10-28).** Once any terminal in a `Ghostty` instance had been freed, a later terminal in the same instance read garbage codepoints and stale wide characters from cells it never wrote. On the Pulse parity fixtures one shared instance corrupted 9 of 20 cases and a fresh instance per terminal rendered all 20; it reproduces in plain Node. The stale data is in the terminal's own page memory, so zeroing the viewport export buffer does not help. The vendored fork zero-initializes page buffers (upstream PR #142) and the same probe shows 0 of 20 corrupt with a shared instance, so every panel and every Pulse screen read now shares the one instance `loadGhostty()` caches (a failed load is not cached). The panel calls `Terminal.reset()` only to apply a changed scrollback setting (see Traps) and never for the Reset action, because it frees and recreates the wasm terminal and drops the selection and link state under the panel: a full reset is written as RIS (`ESC c`), and Clear writes `ESC [3J` after `clear()` to drop the scrollback.
- **No OSC handler API.** There is no `parser`. Titles and OSC 9/777/99/52 come from `terminalStream.ts`, which scans the byte stream.
- **Bell on OSC terminators.** `onBell` fires when a written chunk contains `\x07` anywhere, including the BEL ending `ESC ] 0 ; title BEL`. `onTitleChange` only sees OSC 0/1/2 inside one chunk. Neither is used.
- **`write` parses synchronously.** The callback is deferred with `requestAnimationFrame` and never fires in a hidden tab. Never await it. ghostty-web 0.4.0 still ends every `write` with `if (this.viewportY !== 0) this.scrollToBottom();`, so a scrolled-up viewport would bounce to the bottom on each chunk; ocode re-anchors it to the same content via `web/src/components/Terminal/terminalScrollAnchor.ts` (`writePreservingScrollback`), which wraps every pty write and ocode banner. Raw `term.write` remains only for full-buffer operations — the RIS reset, Clear's ED 3, and the scrollback-setting/theme rebuild replays — which intentionally end at the bottom.
- **Built-in mouse reporting, but not the one ocode needs.** The fork's `InputHandler` listens for mousedown/mouseup/mousemove/wheel on the host and sends SGR/X10 reports when the program tracks the mouse. It sends no focus reports (1004), reports shift/alt presses that ocode reserves for selecting text under a mouse-owning program, reports every wheel pixel delta, and only sees releases inside the host. `disableBuiltinMouseReporting` (ghosttyInternals.ts) removes those four listeners after `open()`, and `terminalMouse.ts` is the single sender of SGR and X10 reports and `CSI I`/`CSI O`. The npm 0.4.0 release had no mouse reporting at all; with the fork, leaving both in place sends every release, motion and wheel report twice.
- **Built-in copy-on-select.** The selection manager copies on mouseup and double-click itself. ocode owns copy (dedupe against Cmd+C and Edit > Copy, desktop native clipboard), so `disableBuiltinSelectionCopy` replaces that path.
- **Built-in link providers `window.open` on ctrl/cmd-click.** They are removed (`clearBuiltinLinkProviders`) and ocode registers its own. `registerLinkProvider` returns nothing; providers live until `dispose()`.
- **Custom key handler convention is inverted from xterm.js.** Returning `true` means handled: skip ghostty-web and `preventDefault`. Cmd/Ctrl+V must return `false`, or the browser `paste` event dies.
- **The host element becomes `contenteditable`.** `open()` needs a dedicated inner div (`.terminal-host`), never the panel container that also holds the find bar.
- **Wheel is captured at the host.** `handleWheel` calls `stopPropagation`, so ancestor wheel listeners need `capture: true`.
- **Option changes after `open()`.** Only `cursorBlink`, `cursorStyle`, `fontSize`, `fontFamily`, `cols`, `rows` apply live. `theme` does not. `scrollback` does not either, but the panel applies a changed `scrollbackLines` to an open terminal itself: it serializes the buffer, sets `options.scrollback`, calls `Terminal.reset()` (which rebuilds the wasm terminal from that option) and replays the text, like the mount restore. The pty and socket are untouched. The rebuild drops terminal modes (mouse tracking, alternate screen, bracketed paste) that a running full-screen program had set, plus the selection and link cache; the program is not asked to re-send them, so it keeps running but looks idle to the mouse and redraws only when it next repaints. This is the one deliberate `reset()` call, acceptable because the setting changes rarely.
- **`select(col,row,len)` is wrong when scrollback exists.** Use `setSelectionAbsolute` from `ghosttyInternals.ts`.
- **`buffer.active.viewportY` is always 0.** Use `getViewportY()`.

### Phone soft-keyboard navigation keys

On a phone, SwiftKey's/Gboard's arrow row used to do nothing while ordinary typing worked. ghostty-web's `handleKeyDown` bails out immediately for `keyCode === 229` (the marker Android stamps on every soft-key event) and resolves a navigation key from `event.code` only, so a keyboard that delegates arrows through `InputConnection.sendKeyEvent` supplies no usable `code` and nothing is encoded (and no `preventDefault` happens). Text is unaffected because it rides `beforeinput`, not `keydown`.

`terminalSoftKeyboard.ts` (`softKeyboardNavigationSequence`, `terminalSoftKeyboard.ts:78`) resolves the key by `event.key` → `event.code` → `event.keyCode` and maps ArrowUp/Down/Left/Right, Home, End, PageUp, PageDown, Insert, Delete to their DEC (`\x1b[A`) or application-cursor (`\x1bOA`) forms. The panel claims the leftovers in a capture-phase `keydown` on the panel root (`TerminalPanel.tsx:1064`–`1081`): it guards with `term.element.contains(target)` so the find bar's input is untouched, calls `preventDefault` + `stopPropagation`, then `term.input(seq, true)` (`TerminalPanel.tsx:1077`), so the pty sees exactly what a hardware arrow would send and the attach guard in `term.onData` applies. DECCKM comes from `term.getMode(1, false)` (`TerminalPanel.tsx:1073`), selecting `\x1bOA` vs `\x1b[A`.

Rules:
- Anything with a usable physical `code` and a non-229 keyCode stays ghostty-web's — including codes this table does not name (keypad arrows, F-keys): `terminalSoftKeyboard.ts:99` only claims soft-keyboard-shaped events (`keyCode === 229`, or no usable `code`), so Kitty/CSI-u and keypad encodings are neither bypassed nor double-sent.
- A modified arrow (ctrl/alt/meta/**shift**) stays ghostty-web's (`terminalSoftKeyboard.ts:87`): only the unmodified form is synthesised, and a plain arrow for a modified one would break selection or word-wise movement. Phone keyboards produce no modifiers.
- A nav key during an IME composition is ignored (`TerminalPanel.tsx:1068`) — the arrows move between candidates there and an escape sequence would corrupt the composition.
- Enter/Tab/Backspace are deliberately not synthesised — they ride the text path and claiming them would double-send.
- The find-bar input is excluded by `term.element.contains(target)` (`TerminalPanel.tsx:1072`).
- `ghosttyWebMock.ts:105` mirrors the real `input(data, true)` by firing `_data` when `wasUserInput`, so the typed-input path is exercised in tests.

Regression suite: `web/src/components/Terminal/TerminalPanel.mobileKeys.test.tsx` (10 cases, mutation-verified).

## Module map (`web/src/components/Terminal/`)

| Module | Role |
| --- | --- |
| `ghosttyRuntime.ts` | `loadGhostty()`: the one shared wasm instance, loaded on first use |
| `terminalStream.ts` | Byte-stream scanner: OSC code/payload and real bells |
| `terminalSerialize.ts` | Buffer to SGR text for the localStorage snapshot |
| `terminalSearch.ts` | Plain case-insensitive find, no all-matches highlight; row text cached until the panel's next write (`invalidate()`) |
| `ghosttyInternals.ts` | The only file touching private ghostty-web members; throws if the vendored build's shape changed |
| `terminalMouse.ts` | Mouse and focus reporting; shift or alt drag selects under a mouse-owning program |
| `terminalLinkProvider.ts` | File `path:line` and URL providers on the ghostty-web link API |
| `terminalOptions.ts` | `ITerminalOptions` and the theme defaults used by the serializer |

Pulse: `usePulseTerminals.ts` replays the log into a headless `GhosttyTerminal` and `pulseScreenText.ts` (`ghosttyCellsToText`) converts rows to text. It passes the shared fixture `internal/server/testdata/pulse_screen_parity.json` against the real wasm.

## Behaviour changes versus xterm.js

- A terminal scrolled up no longer bounces to the bottom while output streams: the viewport follows output only when already at the bottom (xterm.js/native Ghostty behavior), via `terminalScrollAnchor.ts` / `writePreservingScrollback` wrapping `writeOutput` and `writeBanner` in `TerminalPanel.tsx`.
- A changed scrollback setting applies to open terminals too, by a serialize, `reset()` and replay; running programs lose their terminal modes (see Traps).
- Find has no all-matches highlight.
- Under a program that owns the mouse, text selection is shift+drag or alt+drag on every platform.
- The WebGL renderer is gone; rendering is canvas 2D.

## Pty environment

`terminalEnv` in `internal/server/handler_terminal.go` starts every pty shell with `TERM=xterm-256color` and `COLORTERM=truecolor`. `TERM`, `COLORTERM`, `TERM_PROGRAM`, `TERM_PROGRAM_VERSION` and `GHOSTTY_*` inherited from a server launched inside Ghostty are dropped first, so the user's rc does not load Ghostty shell integration and ssh hosts without the `xterm-ghostty` terminfo entry keep working. Remote projects run the same code on the host.
