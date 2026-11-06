---
type: Concept
title: Preview Print / Save as PDF
description: "Print for markdown, html, text, json, mermaid, image, excel, docx and pptx previews: the toolbar button prints a document built from the file (binary kinds through the same api their viewer uses, each rendered by its viewer's own parser or renderer), natively on the desktop (the native print panel from a print window over the Wails bridge, macOS via `ocode_printWindow`, elsewhere via Wails `Print()`) or in the browser (a hidden sandboxed iframe that calls window.print()), plus the bridge rules that guard the desktop path. Markdown ```mermaid fences render in place in the viewer and as inline SVG (pre-rendered in the browser) in print."
resource: web/src/components/Preview/printPreview.tsx; web/src/components/Preview/printDocument.ts; web/src/components/Preview/PrintPreviewButton.tsx; web/src/components/Preview/MarkdownViewer.tsx; web/src/components/Preview/pptxDeck.ts; web/src/components/Preview/univerData.ts; web/src/components/Preview/mermaidQueue.ts; web/src/lib/wails.ts; web/src/lib/previewKind.ts; cmd/ocode-desktop/print.go; cmd/ocode-desktop/main.go
tags: [web, preview, print, pdf, desktop, wails, security]
timestamp: 2026-11-06T04:11:19Z
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
- **Kinds.** `PRINTABLE_KINDS` is `markdown`, `html`, `text`, `json`, `mermaid`,
  `image`, `excel`, `docx` and `pptx`. The button renders nothing for any other
  kind (`isPrintableKind`). `audio`, `video` and `sqlite` are not printed. `pdf`
  has its own print path, see below.
- **Not covered.** The embedded browser panel (`BrowserPanel`). Its page is a
  cross-origin iframe or a CDP canvas, so it cannot be printed from the parent.
  Covering it would need a server-side `Page.printToPDF`.
- **Not covered.** The Files tab's binary preview-only branch (`.xlsx`, `.docx`,
  `.pptx`, images opened as a whole file) has no toolbar, so it has no Print
  button. Tracked in `TODO.md`.

## Content, not DOM

`buildPrintDocument(kind, source, title)` builds a standalone HTML document
from the file, never from the live DOM. The host passes a `PrintSource` through
the `source` prop of `PrintPreviewButton`:

- `{ origin: "editor", path, content }`: the unsaved text of the Files-tab split
  view (`FileTabContent`), so Edit mode prints what is typed. Only the kinds
  that have a split view use it: the text kinds, `image` (SVG) and `excel`
  (CSV/TSV).
- `{ origin: "disk", path, projectRoot, projectHost }`: the saved file
  (`PreviewHost`, `PreviewTabPage`). The document is read at click time, with the
  same api its viewer uses: `api.getFileContent` for the text kinds,
  `api.fetchFileRaw` for the binary ones, both with the project's host so remote
  SSH projects work.

Per kind:

- **markdown:** `renderToStaticMarkup` of `ReactMarkdown` with `remarkGfm`,
  wrapped in `<article class="prose max-w-none">`. The app CSS is inlined by
  `collectAppStyles`, see [Style collection](#style-collection). It is inlined,
  not linked, because the desktop print window has no origin to resolve a
  relative `href` against. The `<html>` tag has no `dark` class. Embedded
  ```mermaid fences are pre-rendered and inlined in place; see
  [Markdown mermaid fences](#markdown-mermaid-fences).
- **text / json:** the escaped content in a `<pre>`.
- **html:** the untrusted file, sanitized with DOMPurify as a whole document
  (`WHOLE_DOCUMENT`), so `<head>` styles and `<link>` stylesheet references
  survive and style attributes stay. The author's `<!doctype html>` is kept, so
  the page is in standards mode like the viewer. Scripts, `on*` handlers and
  `javascript:` URLs are removed. Only the bootstrap script is appended.
- **mermaid:** the source is rendered again with mermaid on the light theme
  (`theme: "default"`, `mermaid.render`), and the SVG is placed in the document.
  The viewer's own SVG is not reused: it is drawn on the dark theme by default.
  The render and its config restore run through `runMermaid`
  (`web/src/components/Preview/mermaidQueue.ts`), the same queue as the viewer,
  so no viewer render interleaves with them. The print render uses
  `securityLevel: "strict"`, and the viewer's theme and security level are
  restored in a `finally`. `MermaidViewer` takes the same queue, so any new
  mermaid `initialize` or `render` must go through `runMermaid`. Embedded fences
  in a Markdown file use the same `mermaidSvg` on this light theme; see
  [Markdown mermaid fences](#markdown-mermaid-fences). The SVG is sanitized with DOMPurify
  under `MERMAID_SVG_CONFIG`: DOMPurify drops HTML inside `foreignObject` unless
  `foreignObject` is an HTML integration point, and mermaid's flowchart labels
  are that HTML, so the config enables it along with the html profile.
- **image:** one `<img>` with the file as a `data:` URL. The MIME type comes from
  the extension (`imageMimeForPath`, `IMAGE_MIME_BY_EXT` in
  `web/src/lib/previewKind.ts`, the single list of image extensions), and the image
  is fitted to the page (`max-height: 100vh; object-fit: contain`). Live SVG text
  is percent-encoded into the URL, never inlined as markup.
  `printPreview.kinds.test.ts` asserts that every `IMAGE_MIME_BY_EXT` entry
  classifies as `image` and prints with its MIME type.
- **excel:** the converter the viewer runs. Saved files go through
  `workbookFromFileBytes`, live CSV/TSV through `workbookFromDelimitedText`
  (`univerData.ts`, see [Excel preview](excel-preview-univer-sheet.md)). One
  `<section>` per sheet: the sheet name as an `<h2>`, then a `<table>` of its
  cells. Sheet names and cell text are escaped, and each sheet starts on a new
  page. Values print as the viewer shows them. Cell styles, merges and column
  widths are not printed. Rows are walked by index up to the last populated
  one, so blank rows keep their place, and an empty cell prints `&nbsp;` so its
  row keeps its height. A sheet with no cells prints `(empty sheet)`, and a
  capped sheet carries a note that only its first rows and columns are printed.
- **docx:** docx-preview's `renderAsync` on a detached container, with
  `inWrapper` and `useBase64URL` (images are inlined, because `blob:` URLs do not
  resolve in the print window). Its `<style>` goes to `<head>` and its body into
  `<body>`. The print CSS sets `@page` margin 0, drops the grey wrapper, and
  breaks the page after each section. Hyperlinks keep only `http(s):`,
  `mailto:` and `#` targets: docx-preview copies each target verbatim, and a
  `javascript:` href would be live in the print window. The body and the styles
  are both sanitized with DOMPurify. The styles need it because docx-preview
  writes font names from the file into `<style>` text, unescaped, so a
  `</style><script>` in a font name would break out when the string is
  embedded. The styles call uses `FORCE_BODY` so a leading `<style>` survives.
- **pptx:** `parsePptx` (`web/src/components/Preview/pptxDeck.ts`) reads the
  slides, and the viewer's own `SlideCard` (`PptxViewer.tsx`) draws each one, one
  per page, under the app CSS from `collectAppStyles`. Images are inlined as
  `data:` URLs. Each card's markup is sanitized with DOMPurify before it is
  embedded. Slides render one at a time. A deck over `PPTX_PRINT_MAX_SLIDES`
  (200) throws rather than printing a cut-short document.

`react-dom/server`, `react-markdown`, `remark-gfm`, `mermaid`, `docx-preview`,
`univerData` (with ExcelJS and SheetJS behind it) and `pptxDeck` are dynamic
imports, so they stay out of the entry chunk.

## Markdown mermaid fences

A ```mermaid fence in a Markdown preview is a diagram in place, both on screen
and on paper. The viewer and the print builder each handle it independently,
and neither prints the fence as code.

**Viewer.** `MarkdownViewer` (`web/src/components/Preview/MarkdownViewer.tsx:153`)
overrides ReactMarkdown's `pre` component in one memoized `components` object,
with `a: MarkdownLink` merged into the same object. `mermaidFenceSource`
(`web/src/components/Preview/MarkdownViewer.tsx:28`) reads the
`language-mermaid` code child, so every fence renders as the interactive
`MermaidViewer` where the author wrote it. A non-mermaid fenced block still
renders as `<pre>{children}</pre>`. There is no longer an `extractMermaid`
helper or a separate `diagram` memo hoisting the first fence into a strip above
the prose: all fences render in document order, and a document with no fence
renders no viewer. `components` is passed to `ReactMarkdown`
(`web/src/components/Preview/MarkdownViewer.tsx:327`).

**Print.** `buildPrintDocument`'s markdown branch renders the prose with
`renderToStaticMarkup`, then calls
`inlineMermaid(markup, mermaidSources(markdown))`
(`web/src/components/Preview/printPreview.tsx:346`):

- `mermaidSources` (`web/src/components/Preview/printPreview.tsx:181`) extracts
  every fence in document order with `MERMAID_FENCE`
  (`web/src/components/Preview/printPreview.tsx:152`).
- `inlineMermaid` (`web/src/components/Preview/printPreview.tsx:193`) matches
  the `<pre><code class="language-mermaid">…` blocks react-markdown emits
  (`MERMAID_CODE_HTML`, `web/src/components/Preview/printPreview.tsx:155`) and
  replaces only those with `<div class="print-mermaid">SVG</div>`. Other code
  blocks are left untouched.
- Each diagram is rendered by the existing `mermaidSvg`
  (`web/src/components/Preview/printPreview.tsx:162`) on the light theme,
  through `runMermaid`, and sanitized with DOMPurify under
  `MERMAID_SVG_CONFIG`. `MARKDOWN_PRINT_CSS`
  (`web/src/components/Preview/printPreview.tsx:148`) gives `.print-mermaid`
  `margin: 1em 0; break-inside: avoid` and its `svg` `max-width: 100%;
  height: auto`, so a diagram is not split across pages and is scaled to the
  column.
- A diagram that fails to render keeps its code block and logs
  `"print preview: could not render a mermaid diagram, printing its source"`.
  One broken fence does not abort the whole document. The `.mmd` preview kind
  still propagates the render error
  (`web/src/components/Preview/printPreview.tsx:366`), because there the diagram
  IS the document.

**No server-side PDF engine.** Mermaid is pre-rendered in the browser (the
viewer draws it live; print runs `mermaid.render` in the SPA) and embedded as
inline SVG markup in the document. A non-JS PDF engine such as WeasyPrint is
never asked to lay out mermaid source, and this path does not involve one.

Covered by `MarkdownViewer.mermaid.test.tsx` (two fences render in document
order, a fence is not printed as code, other code blocks are untouched, and no
fence means no viewer) and the `buildPrintDocument: markdown with mermaid
fences` cases in `printPreview.kinds.test.ts`.

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

`PrintPreviewButton` calls `printPreview({ kind, title, source })`
(`web/src/components/Preview/printPreview.tsx`). That builds the document with
`buildPrintDocument` and passes it to `printDocument` (`web/src/components/Preview/printDocument.ts`), which takes one of two
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

`printDocument.ts` holds the hand-off shared by every printable surface:
`documentShell`, `escapeHtml`, `PRINT_BOOTSTRAP` and `printDocument`. It imports
only `lib/desktopShell` and `lib/wails`, so `PdfViewer` can use it without
pulling in `printPreview.tsx` or the preview-kind graph.

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
4. The print window's bootstrap polls for the Wails bridge. It checks for
   `window._wails.invoke` every 30 ms, sends `ocode:print-window-ready` once when
   the bridge appears, and stops after 15 s (`PRINT_BRIDGE_WAIT_MS`).
   `consumePrintWindowReady` (one-shot `CompareAndSwap`) calls the platform print
   path (`printWindow`) via `crashguard.Go` (it is `InvokeSyncWithError` onto the main thread, which
   the raw handler may already be on). The window stays registered until it
   closes, so later messages are dropped (see Security invariant).

   The poll exists because a one-shot check on `load` never finds the bridge.
   Wails injects `window._wails.invoke` from its `WebViewDidFinishNavigation`
   hook, which runs after the document's `load` event. A `load` check therefore
   sends no ready message, the window opens as a preview with no print panel,
   and the shell logs no "print window N ready" line. The 15 s bound is needed
   because the browser print frame has no bridge at all.

> **Gotcha: do not move the ready check back to `load`.** Any single check that
> runs before `WebViewDidFinishNavigation` misses the bridge on macOS, and the
> failure is silent. The http:// no-op in
> [desktop-shell-webview-no-wails-events.md](../gotchas/desktop-shell-webview-no-wails-events.md)
> does not apply here: the print window is created from `HTML` content, not an
> `http://` URL, so Wails does run the injection for it. The poll fix is covered
> by `printDocument.test.ts` and the existing print tests only. The macOS panel is
> still unverified live (see Known gaps).

`printDesktopDocument` still throws over `MAX_PRINT_DOCUMENT_BYTES` (8 MiB,
UTF-8 bytes of prefix + nonce + document) and the shell still sends no
acknowledgement, so `true` only means the message was posted.

On macOS, `printWindow` (`cmd/ocode-desktop/native_darwin.go`) calls
`ocode_printWindow` in `cmd/ocode-desktop/native_darwin.m`, which opens the system
print panel on the largest visible WKWebView (error if none), starting from the
user's saved `NSPrintInfo`. WKWebView's operation ships a panel limited to copies,
page range and preview (`options` 0x20003), which hides the paper size and
orientation controls, so `ocode_printWindow` ORs
`NSPrintPanelShowsPaperSize | NSPrintPanelShowsOrientation` into
`op.printPanel.options` (0x2000f) before running it. The operation's `NSPrintInfo`
is what WebKit lays the page out from — setting it to landscape swaps the
imageable bounds, A4 portrait 577x797 to 797x577 — so those panel controls drive
real geometry. It replaces Wails' `windowPrint`, which forced landscape and 30pt
margins. Other platforms use Wails' `Print()`
(`cmd/ocode-desktop/native_stub.go`). The macOS panel is unverified live; see
`TODO.md` ("Open: desktop print unverified live", 2026-11-02).

## Security invariant

The preview document is untrusted (the `html` kind is the user's file), so it is
confined at two boundaries:

- **Every embedded body is sanitized.** The `html` file, the docx body and styles,
  the pptx slide cards and the mermaid SVG each pass through DOMPurify, with
  per-call config only. The shared instance gets no hooks or global settings,
  because mermaid uses the same instance for its own labels. Scripts, `on*`
  handlers and `javascript:` URLs are removed from all four. The print bootstrap
  is appended after sanitizing, and it is the only script in the document.
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
  `webview_window.go` of the Wails module (v3.1.0, per `go.mod`).

## Known gaps

- **macOS desktop print is unverified live.** The native panel starts from the user's saved
  `NSPrintInfo` (`ocode_printWindow`), but it has not been checked in the running
  app. See [Desktop route](#desktop-route) and `TODO.md`.
- **`window.print()` cannot be suppressed.** A page cannot suppress the native
  print dialog that `window.print()` opens, so both routes always show it. There
  is no silent print path.
- **Not verified live.** Coverage is unit tests only: `printPreview.test.tsx`,
  `printPreview.kinds.test.ts` (one block per kind, plus the `buildPrintDocument:
  markdown with mermaid fences` cases), `MarkdownViewer.mermaid.test.tsx` (fence
  placement in the viewer), `printPreview.pptx.test.tsx`
  (a stand-in card that emits raw markup, since the real `SlideCard` escapes its
  text), `mermaidQueue.test.ts`, `PrintPreviewButton.test.tsx`,
  `PdfViewer.print.test.tsx` and `print_test.go`. The print routes are not yet
  checked in Chrome or WebKit, and the desktop shell is not yet checked in the
  running app. The docx, pptx, excel, image and mermaid documents have not been
  looked at as printed pages.
- **Sanitizing is a removal list, not a proof.** DOMPurify's defaults decide what
  survives, and the config adds `link` (html), `foreignObject` plus its HTML
  integration point (mermaid). `img` `data:` URLs and inline `style` attributes are
  kept, so an `<img>` loads whatever its URL points at, as it does in the viewer.
  The docx hyperlinks are also filtered (see [Content, not DOM](#content-not-dom)).
- **Mermaid and SVG print on a fixed theme.** Mermaid prints on the light theme
  whatever the app theme, and mermaid's own HTML labels are printed as the
  library draws them. Embedded-fence diagrams in a Markdown file use that same
  light theme. Not checked against a real diagram set.
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
  run in the same JS world as the Wails bridge bootstrap. The sanitizer removes
  the author's scripts from every previewed document, so only the app's own
  bootstrap runs there. It has no other bridge access: every other message is
  dropped by `handlePrintWindowMessage`. The sanitizer is the only thing removing
  author script, so a sanitizer regression reopens this.
- **Isolation options evaluated and rejected.**
  - A per-launch nonce on the window: document scripts can read it, so it does
    not separate the document from the bootstrap.
  - CSP `script-src 'none'`: breaks the inline bootstrap. The sanitizer now drops
    the author's JS, so the CSP would add nothing the sanitizer does not already do.
  - A native load-finished event as the print trigger: the Windows event exists
    (`events.Windows.WebViewNavigationCompleted`; platform-specific, not in
    `events.Common`), so a native trigger is possible, but the implementation
    keeps the `ocode:print-window-ready` bootstrap message because it is the same
    path on every platform.
