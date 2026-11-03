---
type: Concept
title: Preview Print / Save as PDF
description: "Print for markdown, html, text and json previews: the toolbar button prints the content-built document natively (desktop: the native print panel from a print window over the Wails bridge, macOS via `ocode_printWindow`, elsewhere via Wails `Print()`; browser: a hidden sandboxed iframe that calls window.print()), plus the bridge rules that guard the desktop path."
resource: web/src/components/Preview/printPreview.tsx; web/src/components/Preview/PrintPreviewButton.tsx; web/src/lib/wails.ts; cmd/ocode-desktop/print.go; cmd/ocode-desktop/main.go
tags: [web, preview, print, pdf, desktop, wails, security]
timestamp: 2026-11-02T12:22:31Z
status: active
---
# Preview Print / Save as PDF

A Print button prints the rendered preview of a file through the native system
print dialog. There is no in-app preview and no separate PDF export. PDF files
are not covered here: `PdfViewer` has its own Print button,
see [PDF viewer zoom, Space navigation, and find](pdf-viewer-zoom-find.md).

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

## Print routes

`PrintPreviewButton` calls `printPreview({ kind, title, content })`
(`web/src/components/Preview/printPreview.tsx`). That builds the document with
`buildPrintDocument` and passes it to `printDocument`, which takes one of two
routes:

- **Desktop shell** (`isDesktopShell()`): `printDesktopDocument` sends the
  document to the native print window. See [Desktop route](#desktop-route).
- **Browser:** `printDocument` appends a 1px, `opacity: 0`, `aria-hidden` iframe
  with `sandbox="allow-scripts allow-modals"` and the document as `srcdoc`. On
  `load` it posts `"ocode:print"` to the frame. The document's bootstrap then
  calls `window.focus()` and `window.print()`, so the browser's print dialog
  opens. The frame is removed 60 s after load. If it does not load within 15 s
  (`PRINT_LOAD_TIMEOUT_MS`), the promise rejects.

`PdfViewer` has its own print path; see
[PDF viewer zoom, Space navigation, and find](pdf-viewer-zoom-find.md).

## Desktop route

On the desktop shell the toolbar click reaches the native print path:
`printDesktopDocument` (`web/src/lib/wails.ts`), `openPrintWindow`
(`cmd/ocode-desktop/print.go`) and the platform print call: `ocode_printWindow` on
macOS, Wails' `window.Print()` elsewhere.
`printDocument` takes this route when `isDesktopShell()` is true. `print_test.go`
covers the Go side.

The steps:

1. `printDesktopDocument` sends `ocode:print-document:<nonce>:<html>` over
   `_wails.invoke`. The nonce is the per-launch `?clip=` value the shell appends
   to the window URL.
2. The `RawMessageHandler` in `cmd/ocode-desktop/main.go` calls
   `pagePrintHTML`, which applies `pageBridgePayload` — the same nonce and
   app-host origin check as the clipboard write.
3. `openPrintWindow` registers the name (`ocode-print-<n>`) in `printWindows`
   BEFORE `NewWithOptions` creates the window, because Wails runs each window
   message on its own goroutine and the ready message can arrive first.
4. The print window's bootstrap sends `ocode:print-window-ready` on `load`;
   `consumePrintWindowReady` (one-shot `CompareAndSwap`) calls the platform print
   path (`printWindow`) via `crashguard.Go` (it is `InvokeSyncWithError` onto the main thread, which
   the raw handler may already be on). The window stays registered until it
   closes, so later messages are dropped (see Security invariant).

`printDesktopDocument` still throws over `MAX_PRINT_DOCUMENT_BYTES` (8 MiB,
UTF-8 bytes of prefix + nonce + document) and the shell still sends no
acknowledgement, so `true` only means the message was posted.

On macOS, `printWindow` (`cmd/ocode-desktop/native_darwin.go`) calls
`ocode_printWindow` in `cmd/ocode-desktop/native_darwin.m`, which opens the system
print panel using the user's saved print settings on the largest visible WKWebView (error if none). It replaces Wails' `windowPrint`,
which forced landscape and 30pt margins. Other platforms use Wails' `Print()`
(`cmd/ocode-desktop/native_stub.go`). The macOS panel is unverified live; see
`TODO.md` ("Open: desktop print unverified live", 2026-11-02).

## Security invariant

The preview document is untrusted (the `html` kind is the user's file
verbatim), so it is confined at the rendering boundary:

- **The browser frame has an opaque origin.** `printDocument` uses
  `sandbox="allow-scripts allow-modals"` with no `allow-same-origin`, so a
  previewed `.html` cannot reach the app origin's DOM, storage or `_wails`
  bridge. It runs scripts and has `allow-modals`, so it can call `window.print()`
  on its own, as it could in any tab.

The desktop print window's bridge rules apply to the toolbar path:

- `RawMessageHandler`'s FIRST statement routes any message from a registered
  print window (`isPrintWindow`, matched by name) to `handlePrintWindowMessage`,
  and returns. Only the first ready message is honoured. A repeat and every
  other message are dropped. The window stays registered until it closes, so a
  printed .html file cannot reach the clipboard, external-URL or quit-guard
  handling.
- Wails itself honours only that window's own `wails:drag`,
  `wails:resize:` and `wails:runtime:ready` messages. These are handled in
  `webview_window.go` of the Wails module (v3 beta.12, per `go.mod`).

## Known gaps

- **macOS desktop print is unverified live.** The native panel uses the user's saved print settings
  (`ocode_printWindow`), but it has not been checked in the running
  app. See [Desktop route](#desktop-route) and `TODO.md`.
- **`window.print()` cannot be suppressed.** A page cannot suppress the native
  print dialog that `window.print()` opens, so both routes always show it. There
  is no silent print path.
- **Not verified live.** Coverage is unit tests only: `printPreview.test.tsx`,
  `PrintPreviewButton.test.tsx`, `PdfViewer.print.test.tsx` and `print_test.go`.
  The print routes are not yet checked in Chrome or WebKit, and the desktop
  shell is not yet checked in the running app.
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