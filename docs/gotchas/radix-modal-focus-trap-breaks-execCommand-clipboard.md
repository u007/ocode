---
type: Gotcha
title: Radix modal focus trap silently breaks the execCommand clipboard fallback
description: Desktop Share Dialog Copy button reports success but clipboard stays empty because Radix's FocusScope bounces focus away from the scratch textarea before execCommand("copy") runs.
timestamp: 2026-09-20T12:53:14Z
---
## SYMPTOM
Desktop "Share Entire Desktop" dialog Copy button flips to "Copied" but the system clipboard stays empty (macOS, Wails/WKWebView shell).

## ROOT CAUSE
`ShareDialog.copyTextToClipboard` falls back to `document.execCommand("copy")` when `navigator.clipboard.writeText` is undefined or rejects. It appended the scratch `<textarea>` to `document.body`. `ShareDialog` is a Radix modal, whose `FocusScope` bounces focus back into the dialog, so `ta.focus()` never took effect, the scratch field was never the active selection, and WebKit's `execCommand("copy")` returned `true` while copying nothing. The helper trusted that `true` and the UI reported a false "Copied".

## EVIDENCE
Live WKWebView reproduction against the running app — old fallback gave `{"activeAfterFocus":"BUTTON#share-dialog-copy","copied":true,"inDialog":false}` with an **EMPTY** pasteboard; fixed fallback gave `{"host":"DIV[dialog]","focused":true,"copied":true}` with the URL on the pasteboard. jsdom regression test "copies the URL via the execCommand fallback from inside the modal focus scope" fails pre-fix (empty `copied` array).

## FIX
Mount the scratch `<textarea>` inside the active focus scope (`document.activeElement.closest('[role="dialog"]') ?? document.body`); only trust `execCommand` when `document.activeElement === ta`; restore focus to the previously focused element; always remove the scratch node. When neither path writes, show the honest "copy was blocked — press Cmd/Ctrl+C" hint.

## RULE
Any `execCommand("copy")` fallback (a) must mount its scratch textarea inside the open Radix modal / focus scope, not `document.body`, and (b) must verify the scratch element actually holds focus before believing `execCommand`'s return value — it returns `true` even when nothing was copied. The same latent pattern in `writeClipboardText` (`web/src/components/Terminal/TerminalPanel.tsx`) is safe only because the terminal is **not** inside a modal.

## REFERENCES
- `web/src/components/Layout/ShareDialog.tsx` (`clipboardScratchHost`, `copyTextToClipboard`)
- `web/src/components/Layout/ShareDialog.test.tsx`
- CHANGES.md entry "2026-09-20 — Share dialog's 'Copied' was a lie inside the modal (clipboard focus trap)"
