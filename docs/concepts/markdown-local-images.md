---
type: Concept
title: 'Markdown preview: local images via /api/files/raw'
description: 'How a Markdown preview renders an image that lives on the project''s disk — relative sources resolve against the markdown file''s directory, the bytes come from GET /api/files/raw as a blob URL because a plain img cannot send the Bearer token, raw HTML is parsed and sanitized with the default schema (data: images are not supported), fetches are lazy, and print still does not resolve them.'
resource: web/src/components/Preview/MarkdownViewer.tsx; web/src/components/Preview/MarkdownViewer.images.test.tsx; web/src/api/client.ts; internal/server/handler_files.go
tags: [web, preview, markdown, images, blob, auth]
timestamp: 2026-11-16T02:27:07Z
status: active
---
# Markdown preview: local images via /api/files/raw

A markdown image whose `src` points at a file on the project's disk renders in
the preview. `MarkdownViewer` merges an `img` component into its `components`
object (`web/src/components/Preview/MarkdownViewer.tsx:391-394`), which renders
every image through `MarkdownImage` (`:127`). Source handling is
`localImagePath(docPath, src)` (`:70`).

## Which sources are local

`WEB_IMAGE_SRC` (`MarkdownViewer.tsx:50`) matches `http:`, `https:`, `data:`
and `blob:`; a leading `#` is a fragment, and `//host/x` is protocol-relative
(another origin). Those return `null` and render as a plain `<img src>`
untouched — but note react-markdown's `defaultUrlTransform` allowlists only
`http(s)`, `ircs?`, `mailto` and `xmpp`, so an `http(s)` source is loaded by
the browser with nothing fetched, while a `data:` or `blob:` source written in
the markdown is blanked before render and does not display (the `blob:` object
URLs the viewer creates for local files are set by the `img` override itself,
not through markdown parsing). See "Raw HTML is parsed and sanitized" below.

Everything else is a disk path:

- **Relative** (`./preview-screenshots/home_1440.png`, `images/a.png`,
  `../shots/a.png`): joined to the MARKDOWN FILE's directory, the CommonMark
  base — the document's own folder, not the project root. `normalizeJoin`
  (`:98`) resolves `.` and `..` lexically before the request, because the
  server refuses any path that carries a `..` segment (`containsDotDot`,
  `internal/server/handler_files.go:927`).
- **Absolute** (`/shots/a.png`), **home** (`~/shots/a.png`) and **Windows**
  (`C:\shots\a.png`, `\\host\share\a.png`) pass through unchanged. The server
  owns `~/` expansion and its own containment checks.

## Bytes: fetch + blob, never a plain src

A plain `<img src="/api/files/raw?path=…">` cannot send the `Authorization`
header, so it 401s on `/rc` and on auth-configured servers (the desktop app runs
with auth). A bare disk path is also answered by the SPA fallback — an HTML
page, not bytes — so the image would be broken rather than forbidden.

`MarkdownImage` therefore fetches `api.fetchFileRaw(local, projectRoot,
projectHost)` (`web/src/api/client.ts:3813`) — `GET /api/files/raw`, Bearer
auth, `?host=` for a remote project — and renders
`URL.createObjectURL(new Blob([bytes]))` (`MarkdownViewer.tsx:183`). The object
URL is revoked when the source changes or the image unmounts (`:191`).

A local fetch waits until the image nears the viewport: an
`IntersectionObserver` with `rootMargin: "400px"` (`:167`) gates it, and with no
observer (jsdom, an ancient webview) it loads at once (`:155-158`). A screenshot
report carries dozens of images, and the desktop webview allows ~6 concurrent
connections per origin, so fetching them all on mount queues the transfers and
starves the session's SSE stream and polls. While it waits, the placeholder
span shows the alt text or path (`:203-209`); the `<img>` keeps any `width` and
`height` the source set. An in-flight fetch is not aborted on unmount —
`api.fetchFileRaw` takes no `signal` — only its result is dropped (`:182`).

On the server, `HandleFileRaw` (`internal/server/handler_files.go:1266`) uses
the same anchoring and containment rules as `HandleFileContent`, and the
extension must be in `previewRawTypes` (`:1224`) — `png`, `jpg`, `jpeg`, `gif`,
`webp`, `svg`, plus the document and media types. Anything else is rejected.

## Raw HTML is parsed and sanitized

A raw HTML tag in the markdown renders as markup, not literal text:
`<img src="./shots/a.png" width="260">` in a table cell goes through the same
`img` override, and a self-closing `<img …>` works. `MarkdownViewer` passes
`rehypePlugins={[rehypeRaw, rehypeSanitize]}` (`MarkdownViewer.tsx:558`), so the
HTML is parsed first and then filtered by the default GitHub allowlist; every
other raw tag renders through that allowlist.

The default `defaultSchema` is used as-is: `attributes["*"]` already carries
`alt`, `align`, `start`, `width` and `height`, so markdown-emitted table
alignment and image sizing survive without widening `attributes`; its `code`
className regex `/^language-./` keeps `language-mermaid`, so the mermaid fence
override still fires. `img` keeps `src` and `longDesc`, and
`protocols.src` allows only `http` and `https`.

**`data:` images are NOT supported.** react-markdown's `defaultUrlTransform`
(`safeProtocol = /^(https?|ircs?|mailto|xmpp)$/i`,
`react-markdown/lib/index.js`) blanks every other scheme, a raw-HTML `src`
included, before the hast tree becomes React elements — so an embedded
`data:` image never reaches the `<img>`, whichever schema the sanitizer uses.
Widening `protocols.src` to allow `data:` would be inert, so it is deliberately
not done. To embed an image, put it on disk (the `/api/files/raw` path above)
or reference an `http(s)` URL.

The sanitizer is not optional. A previewed markdown file is untrusted content,
and the allowlist is what drops `<script>`, `on*` handlers and `javascript:`
URLs.

## Failure is visible

While the bytes load, the image renders its `alt` text (or its path). A failed
fetch renders an inline red `<alt> (could not load: <error>)` span whose `title`
is the resolved path (`MarkdownViewer.tsx:196-201`), never a silent gap. The
error text is the server's message, e.g. `path is outside the project root`.

## Query, fragment and percent-encoding

A `?query` or trailing `#fragment` is stripped from a disk path
(`MarkdownViewer.tsx:74`; a source that STARTS with `#` is a fragment and stays
external, `:71`). The remaining path is `decodeURIComponent`-decoded (`:89`),
because an image source is a URL: `./shots/a%20b.png` resolves to
`shots/a b.png`. A `100%.png` is not a valid escape, so it is used as written
(`:92`) and the server reports the miss, rather than the viewer guessing a
different path.

## Print does not follow

The print builder re-renders the markdown source with plain `ReactMarkdown` and
no `img` override (`web/src/components/Preview/printPreview.tsx:345`), so a
relative or local image is printed as written and does not resolve. See
[Preview Print / Save as PDF](preview-print-save-as-pdf.md).

## Tests

`web/src/components/Preview/MarkdownViewer.images.test.tsx` pins relative
resolution, absolute/home/Windows passthrough, external-source passthrough with
no fetch, query/fragment dropping and percent-decoding, the inline failure, a
raw `<img width="260">` that fetches its local path and keeps its width, the
sanitizer (no `<script>`, and no fetch for a `javascript:`/`onerror` source),
and GFM table alignment surviving the sanitizer.
