---
type: Concept
title: Preview Print / Save as PDF
description: "Print for markdown, html, text and json previews: the toolbar button opens an in-app Radix Dialog preview of the content-built document in a sandboxed iframe, with no native print dialog; the legacy browser and desktop print routes and their bridge rules."
resource: web/src/components/Preview/printPreview.tsx; web/src/components/Preview/PrintPreviewButton.tsx; web/src/lib/wails.ts; cmd/ocode-desktop/print.go; cmd/ocode-desktop/main.go
tags: [web, preview, print, pdf, desktop, wails, security]
timestamp: 2026-11-02T07:24:58Z
status: active
---
# Preview Print / Save as PDF

A Print button opens an in-app preview dialog for the rendered preview of a
file. The document is shown in a sandboxed iframe with only a Close button; no
native OS print dialog is opened automatically, and there is no separate PDF
export. PDF files are not covered here: `PdfViewer` has its own Print button,
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

## Print preview dialog

`PrintPreviewButton` opens `PrintPreviewDialog`
(`web/src/components/Preview/PrintPreviewDialog.tsx`), a Radix `Dialog` that
renders the document in an iframe:

- The button still calls `buildPrintDocument(kind, content, title)` to build the
  standalone HTML (see [Content, not DOM](#content-not-dom)).
- The HTML is passed to the dialog as `html` and rendered with
  `<iframe srcDoc={html} sandbox="" title="Print preview" />`.
- The dialog has a single **Close** button (`DialogClose`, marked
  `data-dialog-default-action`). It never calls `window.print()` and never opens
  a native print dialog; closing dismisses the preview without printing.

`printDocument` and `printPreview` remain exported from `printPreview.tsx` and
are still unit-tested, but the toolbar click path no longer calls them. The old
hidden-iframe route — post `"ocode:print"` to a
`sandbox="allow-scripts allow-modals"` frame whose bootstrap calls
`window.print()`, with `PRINT_LOAD_TIMEOUT_MS` (15 s) and a
`PRINT_FRAME_LIFETIME_MS` (60 s) reap — is retained only for those tests and is
not reachable from the UI.

`PdfViewer` reuses the same dialog with a `src` blob URL instead of `html`; see
[PDF viewer zoom, Space navigation, and find](pdf-viewer-zoom-find.md).

## Desktop route (unused from the toolbar)

The desktop app shows the same in-app `PrintPreviewDialog` as the browser. The
toolbar click no longer reaches the native print path: `printDesktopDocument`
(`web/src/lib/wails.ts`), `openPrintWindow` (`cmd/ocode-desktop/print.go`) and
`window.Print()` (Wails' `NSPrintOperation`) remain in the codebase and are
still covered by `print_test.go`, but `PrintPreviewButton` does not invoke them.

The retained legacy path, for reference:

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
   `consumePrintWindowReady` (one-shot `CompareAndSwap`) calls `window.Print()`
   via `crashguard.Go` (it is `InvokeSyncWithError` onto the main thread, which
   the raw handler may already be on). The window stays registered until it
   closes, so later messages are dropped (see Security invariant).

`printDesktopDocument` still throws over `MAX_PRINT_DOCUMENT_BYTES` (8 MiB,
UTF-8 bytes of prefix + nonce + document) and the shell still sends no
acknowledgement, so `true` only means the message was posted.

True silent PDF output on desktop is deferred. It would need `NSPrintOperation`
with `showsPrintPanel = false` (unverified) or a server-side render. See
`TODO.md` ("print preview replaces native OS print dialog", 2026-11-02).

## Security invariant

The preview document is untrusted (the `html` kind is the user's file
verbatim), so it is confined at the rendering boundary:

- **The dialog iframe is fully sandboxed: `sandbox=""`.** With no
  `allow-scripts`, `allow-same-origin` or `allow-modals`, a previewed `.html`
  file cannot run scripts, cannot reach the app origin's DOM, storage or
  `_wails` bridge, and cannot open a native print dialog. This is stricter than
  the legacy `HtmlViewer` frame (which allows scripts).
- **Only Close is wired.** The dialog calls neither `window.print()` nor the
  desktop bridge, so there is no print-trigger message for a document to forge.

The legacy desktop print window's bridge rules still apply to the retained,
unused-from-toolbar path:

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

- **Silent PDF output is deferred.** The dialog previews only; it does not
  produce a PDF. A true silent-PDF path needs native work on desktop (see
  [Desktop route](#desktop-route-unused-from-the-toolbar)) or a server-side
  render. See `TODO.md`.
- **`window.print()` cannot be suppressed.** A browser page cannot suppress the
  native print dialog that `window.print()` opens, which is why the preview
  dialog stops at review and never triggers printing.
- **Not verified live.** Coverage is unit tests only: `printPreview.test.tsx`,
  `PrintPreviewButton.test.tsx`, `PdfViewer.print.test.tsx` and `print_test.go`.
  The dialog is not yet checked in Chrome or WebKit, and the desktop shell is
  not yet checked in the running app.
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