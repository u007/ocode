---
type: Gotcha
title: Monaco's WebKit clipboard workaround spams NotAllowedError and lags every keystroke
description: In the Wails/WKWebView desktop shell, Monaco installs a click+keydown document.body listener that calls navigator.clipboard.write on every event. The write is denied, so every click and keystroke logs NotAllowedError (editor.api-*.js) and pays a pasteboard IPC round-trip — console spam plus input lag that makes chat feel like it hangs.
timestamp: 2026-09-28T20:30:00Z
---
## SYMPTOM

The desktop app spams the console while typing in chat:

```
ERR NotAllowedError: The request is not allowed by the user agent or the platform in the current context, possibly because the user denied permission.
write
e — editor.api-BoTh46su.js:805:12010
```

and typing in the chat input feels laggy / "hangs". The error appears even though the user never triggered a copy.

## ROOT CAUSE

`monaco-editor` (`esm/vs/platform/clipboard/browser/clipboardService.js`) installs a **WebKit clipboard workaround** when `isSafari || isWebkitWebView`. Wails' `WKWebView` UA contains `AppleWebKit` but not `Chrome`/`Safari`, so `isWebkitWebView` (`browser.js:34`) is `true`.

`installWebKitWriteTextWorkaround()` registers `click` and `keydown` listeners on the layout service's main container — for standalone Monaco that is `document.body` (`StandaloneLayoutService.mainContainer`, and `onDidAddContainer = Event.None`, so `runAndSubscribe` runs the callback once with the body). Each event calls:

```js
navigator.clipboard.write([new ClipboardItem({ "text/plain": <pending promise> })])
```

The workaround exists because Safari requires clipboard writes to happen inside the user-gesture stack; by pre-registering a pending `ClipboardItem` on `click`/`keydown` it lets an async `writeText` later resolve into that gesture. But in the Wails WKWebView the `write` is **denied**, so **every click and every keystroke** (including typing in the chat input, which is nowhere near the editor) produces a rejected `NotAllowedError` promise and a pasteboard IPC round-trip. The console message comes from the `.catch` in `installWebKitWriteTextWorkaround` itself, whose guard (`err.name === "NotAllowedError" && currentWritePromise.isRejected`) does not suppress this case.

**Monaco must have been loaded for the spam to start**: it is in the lazily-loaded `editor.api-*.js` chunk, pulled in by `FileEditor` (`web/src/components/FileEditor.tsx` imports `../../lib/monaco-setup`). Open any file in the editor once and the `body`-level listeners stay attached.

## EVIDENCE

- The reported stack frame `editor.api-BoTh46su.js:805:12010` decodes exactly to the `navigator.clipboard.write([new ClipboardItem(...)])` call inside `installWebKitWriteTextWorkaround`, in the `editor.api` chunk.
- `standaloneLayoutService.js:22-24` (`mainContainer` → `document.body`) + `onDidAddContainer = Event.None` explains the global `body` listeners.
- A post-fix production build (`editor.api-DAH1aP1m.js` + `FileEditor-B9OPOuOP.js`) shows the patch call site (`installWebKitWriteTextWorkaround=()=>{}`) in the same chunk as the class definition, and the exported alias used by the patch (`ya` ← `Y` ← `J6`) resolves to the same `J6` class that defines the method — i.e. the deep import and the internally instantiated service are one module instance, not a double-bundled copy.

## FIX

Before any editor mounts (`web/src/lib/monaco-setup.ts`), no-op the prototype method:

```ts
import { BrowserClipboardService } from "monaco-editor/esm/vs/platform/clipboard/browser/clipboardService.js";
BrowserClipboardService.prototype.installWebKitWriteTextWorkaround = () => {};
```

Wrapped in `disableMonacoWebKitClipboardWorkaround()` (`web/src/lib/monacoClipboardPatch.ts`) and guarded by the internal module's ambient declaration (`web/src/monaco-internal.d.ts`).

This keeps Monaco's UA detection untouched (no faking `Chrome`/`Safari`); it only removes the side effect ocode does not use. Monaco's own copy/paste then falls through to `navigator.clipboard.writeText` with the textarea `execCommand` fallback (`clipboardService.js:112-138`), which works on a real user gesture (Cmd/Ctrl+C). ocode's own copy paths never used Monaco's async clipboard API anyway — they go through `web/src/lib/clipboard.ts` / `TerminalPanel.writeClipboardText`.

## RULE

Do not let a dependency attach capture-phase workarounds to `document.body` that fire on **every** `click`/`keydown`. If a dependency's browser-quirk workaround is not needed (or is actively harmful) in the host shell, neutralize the prototype method **before the dependency's service is constructed** — a lazy-loaded editor can install the listener long after the app looks idle. Verify the deep import and the internally-instantiated service are the same module instance (inspect the production alias chain) rather than assuming Vite dedupes them.

## REFERENCES

- `web/src/lib/monacoClipboardPatch.ts`, `web/src/lib/monacoClipboardPatch.test.ts`
- `web/src/lib/monaco-setup.ts`, `web/src/monaco-internal.d.ts`
- `node_modules/monaco-editor/esm/vs/platform/clipboard/browser/clipboardService.js` (`installWebKitWriteTextWorkaround`)
- `node_modules/monaco-editor/esm/vs/editor/standalone/browser/standaloneLayoutService.js` (`mainContainer`, `onDidAddContainer`)
- `web/src/lib/desktopShell.ts` (`isDesktopShell`, the shell detection helper)
