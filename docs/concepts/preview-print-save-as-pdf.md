---
type: Concept
title: Preview Print / Save as PDF
description: "Print and Save as PDF for markdown, html, text and json previews: the content-built document, the sandboxed-iframe route in browsers, the per-window route in the desktop shell, and the bridge rules that keep an untrusted print window unprivileged."
resource: web/src/components/Preview/printPreview.tsx; web/src/components/Preview/PrintPreviewButton.tsx; web/src/lib/wails.ts; cmd/ocode-desktop/print.go; cmd/ocode-desktop/main.go
tags: [web, preview, print, pdf, desktop, wails, security]
timestamp: 2026-11-01T15:53:23Z
status: active
---

# Preview Print / Save as PDF

A Print button prints the rendered preview of a file. The print dialog's own
destination provides Save as PDF; there is no separate PDF export. PDF files
are not covered here: `PdfViewer` has its own Print button, see
[PDF viewer zoom, Space navigation, and find](pdf-viewer-zoom-find.md).

## Scope

- **Hosts.** `PrintPreviewButton` appears in three places:
  - the sidebar preview header (`PreviewHost.tsx`), with a text label;
  - the session Preview tab (`PreviewTabPage.tsx`), icon-only;
  - the Files-tab Edit / Preview / Split toolbar (`FileTabContent.tsx`).
- **Kinds.** `PRINTABLE_KINDS` is `markdown`, `html`, `text`, `json`. The
  button renders nothing for any other kind (`isPrintableKind`).
- **Not covered.** The embedded browser panel (`BrowserPanel`). Its page is a
  cross-origin iframe or a CDP canvas, so it cannot be printed from the parent.
  Covering it would need a server-side `Page.printToPDF`.

## Content, not DOM

`buildPrintDocument(kind, content, title)` builds a standalone HTML document
from the file content, never from the live DOM. The content is passed in from
the host through the `load` prop of `PrintPreviewButton`:

- `FileTabContent` passes `props.content`, so Edit mode prints unsaved editor
  text.
- `PreviewHost` and `PreviewTabPage` fetch the file with `api.getFileContent`
  at click time.

Per kind:

- **markdown:** `renderToStaticMarkup` of `ReactMarkdown` with `remarkGfm`,
  wrapped in `<article class="prose max-w-none">`. The app CSS is inlined by
  `collectAppStyles`, see [Style collection](#style-collection). It is inlined,
  not linked, because the desktop print window has no origin to resolve a
  relative `href` against. The `<html>` tag has no `dark` class.
- **text / json:** the escaped content in a `<pre>`.
- **html:** the untrusted file verbatim. Only the bootstrap script is appended.

`react-dom/server`, `react-markdown` and `remark-gfm` are dynamic imports, so
they stay out of the entry chunk.

## Style collection

`collectAppStyles` (`web/src/components/Preview/printPreview.tsx`) builds the
`<style>` block for the markdown document.

- **Same-origin `link[rel=stylesheet]` only.** Each such sheet is fetched and
  inlined. A cross-origin sheet is not the app's CSS and its fetch is
  CORS-blocked, so it is skipped without a log. A failed fetch is logged with
  `console.error` and skipped.
- **Relative `url(...)` rewritten.** `absolutizeCssUrls` resolves each relative
  `url()` against the sheet's `href`. `data:`, `blob:` and `#fragment` refs are
  left exactly as written. Sheets are re-fetched on every print; nothing is
  cached between prints.
- **Vite-marked inline styles only.** Vite dev injects the app's CSS as
  `<style data-vite-dev-id>`. Only those are inlined, rewritten against
  `document.baseURI`. Other head `<style>` elements belong to runtime libraries
  (Monaco, toasts) and are left out.

## Browser route

`printDocument` creates a hidden iframe with
`sandbox="allow-scripts allow-modals"` and `srcdoc` set to the document.

- On `load`, the parent posts the string `"ocode:print"` to the frame.
- The bootstrap script inside the frame listens for that message and calls
  `window.print()`.

A parent cannot call `print()` on a sandboxed frame, and `allow-modals` is what
permits `print()` inside one. One sandboxed path serves every kind, because
the html kind is untrusted; this is the same stance as `HtmlViewer`.

**Load timeout.** `PRINT_LOAD_TIMEOUT_MS` (15 s) removes the frame and rejects
`printDocument` if it has not loaded. `onload` clears that timer. The frame is
removed `PRINT_FRAME_LIFETIME_MS` (60 s) after a successful post, so the dialog
can stay open. `printDocument` also rejects when the loaded frame has no
`contentWindow`. `PrintPreviewButton` shows the failed state for any rejection.

## Desktop route

WKWebView ignores `window.print()`, and Wails does not implement the
`_webView:printFrame:` UI-delegate hook. Wails' `WebviewWindow.Print()` runs an
`NSPrintOperation` over the WHOLE webview, which would print the SPA itself.
The document therefore gets its own window:

1. `printDesktopDocument` (`web/src/lib/wails.ts`) sends
   `ocode:print-document:<nonce>:<html>` over `_wails.invoke`. The nonce is the
   per-launch `?clip=` value the shell appends to the window URL.
2. The `RawMessageHandler` in `cmd/ocode-desktop/main.go` calls
   `pagePrintHTML`, which applies `pageBridgePayload`. That is the same nonce
   and app-host origin check the clipboard write uses. It is shared between
   the two.
3. `openPrintWindow` (`cmd/ocode-desktop/print.go`) generates a name
   (`ocode-print-<n>`) and registers it in `printWindows` BEFORE
   `NewWithOptions` creates the `WebviewWindow` with that name and `HTML: html`.
   The registry is keyed by name, not id, because the id does not exist until
   the window does. Wails runs each window message on its own goroutine, so the
   ready message can arrive before `NewWithOptions` returns. Registering after
   creation raced it.
4. The bootstrap in the print window sends `ocode:print-window-ready` on
   `load`. `handlePrintWindowMessage` calls `consumePrintWindowReady`, a
   one-shot `CompareAndSwap`, so only the first ready message prints. It then
   calls `window.Print()` in a goroutine started with `crashguard.Go`. `Print()`
   is `InvokeSyncWithError` onto the main thread, and the raw handler may already
   be running there, so an inline call could block the thread on itself. The
   window stays registered after the ready message, so its later messages are
   still dropped (see Security invariant).

The desktop route has no acknowledgement. `printDesktopDocument` throws when the
message is over `MAX_PRINT_DOCUMENT_BYTES` (8 MiB, UTF-8 bytes of the whole
message: prefix, nonce and document), and nothing is sent. Otherwise `true`
means only that the message was posted. The shell sends no reply, so a bad
nonce or a sender outside the ocode page is just logged on the Go side.
`PrintPreviewButton` shows a failure state (warning icon, reason in the tooltip,
reset after 2.5 s) when `printPreview` throws. A posted document that later fails
in the shell does not show it.

The window stays open as a print preview. The native print sheet offers Save as
PDF. The user closes the window, and the `events.Common.WindowClosing` listener
removes its name from `printWindows`.

## Security invariant

The print window is the only window that shows a user's untrusted document in
the desktop shell, so its bridge access is strictly limited.

- `RawMessageHandler`'s FIRST statement routes any message from a registered
  print window (`isPrintWindow`, matched by name) to `handlePrintWindowMessage`,
  and returns. Only the first ready message is honoured. A repeat and every
  other message are dropped. The window stays registered until it closes, so a
  printed .html file cannot reach the clipboard, external-URL or quit-guard
  handling.
- Wails itself honours only that window's own `wails:drag`,
  `wails:resize:` and `wails:runtime:ready` messages. These are handled in
  `webview_window.go` of the Wails module (v3 beta.12, per `go.mod`).
- The print window is therefore no more privileged than the sandboxed
  HtmlViewer frame. Scripts still run, and the network is open in both.

## Known gaps

- **Not verified live.** Coverage is unit tests only: `printPreview.test.tsx`,
  `PrintPreviewButton.test.tsx` and `print_test.go`. The browser route is not
  yet checked in Chrome or WebKit, and the desktop route is not yet checked in
  the running app.
- **No Go-side size cap.** `print.go` accepts any length of document. The only
  bound is the SPA's `MAX_PRINT_DOCUMENT_BYTES` check in `wails.ts`.
- **`@import` string form not rewritten.** `absolutizeCssUrls` rewrites
  `url(...)` only. A relative `@import "x.css"` in a fetched sheet is left as
  written, so it resolves against the print window's base and does not load.
- **Font CORS unverified.** Fonts reached through the rewritten `url()` are
  fetched by the desktop print window. Whether they load across origin there
  is not checked.
- **Relative image paths.** Markdown images with relative paths will not
  resolve in the desktop print window, because it has no origin.
- **Document shares the bridge's JS world.** Scripts in a desktop print window
  run in the same JS world as the Wails bridge bootstrap. A previewed .html can
  therefore post `ocode:print-window-ready` itself and open the native print
  sheet early, for a window the user just asked to print. It has no other bridge
  access: every other message is dropped by `handlePrintWindowMessage`.
- **Isolation options evaluated and rejected.**
  - A per-launch nonce on the window: document scripts can read it, so it does
    not separate the document from the bootstrap.
  - CSP `script-src 'none'`: breaks the inline bootstrap and drops the author's
    JS. Dropping author JS is intended, so this is documented, not fixed.
  - A native load-finished event as the print trigger: not available on Windows
    in Wails beta.12.
