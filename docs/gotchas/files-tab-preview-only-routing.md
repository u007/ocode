---
type: Gotcha
title: Files Tab Auto-Previews Binary/Office/Media Formats (Preview-Only Routing + Local Media Streaming)
description: 'Gotcha: Files-tab auto-preview routing for binary/Office/media formats plus markdown Edit/Preview/Split mode switch. Updated 2026-09-17 with markdown mode switch section, useResizableSplit, live content propagation, automaticLayout, and re-verified test-suite status.'
resource: web/src/components/Files/FileTabContent.tsx
tags:
  - gotcha
  - web
  - files-tab
  - editor
  - preview
  - monaco
  - binary
  - pdf
  - docx
  - pptx
  - excel
  - image
  - audio
  - video
  - media
  - routing
  - useEditorTabs
  - streaming
  - http-range
  - servecontent
  - capability-token
  - media-token
  - auth
  - markdown
  - split-mode
  - useResizableSplit
timestamp: 2026-09-17T07:53:28Z
---
# Files Tab Auto-Previews Binary/Office/Media Formats (Preview-Only Routing + Local Media Streaming)

**Type:** Gotcha  
**Description:** The web Files-tab editor auto-routes PDF, Word/PowerPoint/Excel, image, and audio/video files to the shared preview surface instead of Monaco; preview-only paths also skip the /api/files/content fetch, and the external-change watchers must stay in sync with that skip. Local audio/video now streams with HTTP range support (http.ServeContent) behind a short-lived single-file capability token (POST /api/files/media-token, mediaAuthMiddleware), so the local media byte cap is gone; the 32 MiB document cap and the 128 MiB cap for the REMOTE buffered path remain. Includes the .doc/.ppt/.mkv/.avi OS-open fallback and divergence from the 2026-09-10 preview design spec.  
**Resource:** web/src/components/Files/FileTabContent.tsx  
**Tags:** gotcha, web, files-tab, editor, preview, monaco, binary, pdf, docx, pptx, excel, image, audio, video, media, routing, useEditorTabs, streaming, http-range, servecontent, capability-token, media-token, auth  

---

# Files Tab Auto-Previews Binary/Office/Media Formats (Preview-Only Routing + Local Media Streaming)

Implemented **2026-09-16**. Before this change, clicking any file in the web
**Files tab** created a Monaco editor tab: binary files hit the
"Binary File — Edit anyway" dead-end placeholder, because preview rendering
existed only in explicitly-activated surfaces (the sidebar `PreviewHost` and
the session-level `Preview` sub-tab).

Now the Files-tab editor tab body is routed by file kind, and the
binary/Office/media kinds never open Monaco.

## Routing rule

`FileTabContent` (`web/src/components/Files/FileTabContent.tsx:17`) is the sole
routing point for a Files-tab editor tab body, in priority order:

1. `previewOnlyKindForPath(path)` returns a kind → render the shared
   `PreviewSurface` (`FileTabContent.tsx:32`).
2. `isLegacyOfficePath(path)` → render `LegacyOfficePane`
   (`FileTabContent.tsx:47`).
3. otherwise → render `FileEditor` (Monaco) (`FileTabContent.tsx:50`).

The **preview-only kind set** is `pdf`, `docx`, `pptx`, `excel`, `image`,
`audio`, `video` (`PREVIEW_ONLY_KINDS`, `web/src/lib/previewKind.ts:84`). These
are binary containers with no editable text representation. `markdown`, `text`,
and `mermaid` are **deliberately excluded** and keep the Monaco editor, even
though `previewKindForPath` still resolves them as previewable
(`previewKind.ts:72`). `previewOnlyKindForPath` (`previewKind.ts:91`) is
therefore the narrow "must not open in Monaco" subset of the broader preview
allowlist; `previewKindForPath` remains the sidebar/`PreviewSurface` dispatch
helper.

`FileTabContent` also owns the PDF-page and PPTX-slide state
(`FileTabContent.tsx:22`, `:23`) that the controlled viewers need; every editor
tab stays mounted (hidden, not unmounted — see `App.tsx:1013`), so this state
survives tab switches.

## Markdown mode switch (2026-09-17)

`FileTabContent` now renders an **Edit / Preview / Split mode switch** for
`.md`/`.markdown` files (when `!props.isBinary`). The mode control is a
`role="group"` (`aria-label="Markdown view mode"`) of `aria-pressed` toggle
buttons — the same pattern as `FileTree.tsx`'s list/column view switch. New
exported helper `isMarkdownPath(path)` (`previewKind.ts:92`) returns
`previewKindForPath(path) === "markdown"`.

**Three modes:**

| Mode | Default | Behavior |
|------|---------|----------|
| `edit` | **yes** | Pure Monaco (same as before) |
| `preview` | no | Full-width `PreviewSurface kind="markdown"` |
| `split` | no | Monaco left, preview right, `role="separator"` divider |

### Key invariants

- **Monaco is hidden with CSS, never unmounted** when the mode changes, so
  cursor, scroll, and undo history survive. `FileEditor` now sets
  `automaticLayout: true` in its `editorOptions` (`FileEditor.tsx:296`) because
  the pane width changes (split drag + the pre-existing file-tree pane resize);
  without it Monaco paints against stale geometry.

- **`.md`/`.markdown` remain deliberately excluded from
  `PREVIEW_ONLY_KINDS`** (`previewKind.ts:84`). The fetch-skip invariant for
  preview-only paths is unaffected — `/api/files/content` is still always
  fetched for markdown tabs.

- **Live content propagation to preview.** `PreviewSurfaceProps` gained an
  optional `content?: string` (`PreviewSurface.tsx:53`), forwarded **only** to
  `MarkdownViewer` (`PreviewSurface.tsx:79`). `MarkdownViewer` has a controlled
  mode (`content !== undefined` ⇒ skip the `/api/files/content` fetch) with a
  separate effect mirroring the caller's source (`MarkdownViewer.tsx:39`).
  `FileTabContent` debounces it 200 ms (`useDebouncedValue`, local helper at
  `FileTabContent.tsx:41`) so a typing burst does not re-parse react-markdown
  every keystroke.

- **`MermaidViewer` render is stable on prose edits.** Its render effect keys on
  `[code, renderId]`, and an unchanged mermaid fence is string-identical, so it
  does not re-render on prose edits.

- **Architectural constraint:** the mode state must NOT live in `FileEditor`
  (pure Monaco surface, reused by `TextViewer`/`MarkdownViewer` via the binary
  `forceEdit` fallback at `MarkdownViewer.tsx:7`) nor in `MarkdownViewer` (it
  imports `FileEditor` — circular). `FileTabContent` owns the mode state.

### Split resize

New hook `useResizableSplit` (`web/src/hooks/useResizableSplit.ts`):
ratio-based (0.2–0.8, default 0.5), pointer-capture drag, persisted at
localStorage key `ocode.ui.split_ratio`, double-click resets. Mirrors
`useResizableSidebar`'s pointer contract. Mode is per-tab component state
(survives tab switches via keep-alive); ratio is global.

### `onOpenFile` for preview internal links

`FileEditorProps` gained an optional
`onOpenFile?: (path, projectRoot?) => void` (`FileEditor.tsx:62`), used only by
the markdown preview pane (internal links in the rendered doc); `App.tsx` wires
it to `openFileAndShow`. `FileEditor` itself ignores it.

### Bundle impact: none

`MarkdownViewer` and `FileEditor` remain separate `React.lazy` chunks; entry
chunk still ~1.91 MB, `modulepreload` still 1. No static import of
Monaco/pdfjs/xlsx/docx-preview/mermaid was added to the entry path.

### Tests

`web/src/components/Files/FileTabContent.test.tsx` now has 10 tests covering:
Edit default; preview while Monaco stays mounted-but-hidden; split renders both
panes + separator; live-content propagation; divider drag/pup/double-click
reset; `.markdown` extension; no mode chrome on code/plain-text tabs.

## Audio/video were added to the preview-only set

`PreviewKind` (`previewKind.ts:13`) gained `audio` and `video`, and
`kindByExt` (`previewKind.ts:25`) maps them to browser-playable containers
only:

- **audio** — `mp3`, `m4a`, `aac`, `wav`, `ogg`, `oga`, `opus`, `flac`
- **video** — `mp4`, `m4v`, `webm`, `ogv`, `mov`

`.mkv` and `.avi` are **deliberately excluded** — there is no reliable browser
renderer, so they keep the legacy OS-open fallback rather than showing a broken
player. Do not describe the set as "all media files".

Any change to this set must land in **three synchronized allowlists**:

- `kindByExt` / `PREVIEW_ONLY_KINDS` — `web/src/lib/previewKind.ts` (renderer
  routing).
- `previewRawTypes` — `internal/server/handler_files.go:960` (the bytes the raw
  endpoint will serve).
- `previewOpenKinds` — `internal/tool/preview.go:20` (the `preview_open`
  tool's allowlist).

Go pin tests assert the media entries and the `.mkv`/`.avi` omissions:
`TestPreviewRawCoversOfficeSet` asserts `previewRawTypes` must NOT serve
`.doc`, `.ppt`, `.exe`, `.mkv`, `.avi` (`internal/server/handler_preview_test.go:224`),
`TestHandleFileRaw` covers the media MIME mapping, and `previewOpenKinds`
rejects the excluded extensions in `internal/tool/preview_test.go`.

## Raw byte serving: buffered documents, streamed local media

`GET /api/files/raw` now has **two transports**, chosen by content type in
`HandleFileRaw` (`internal/server/handler_files.go:999`):

- **Local audio/video streams with HTTP range support.** `isMediaContentType(ct)`
  (`handler_files.go:935`, `audio/*` or `video/*`) sends the request to
  `serveMediaFile` (`handler_files.go:1079`) → `http.ServeContent(w, r,
  filepath.Base(path), info.ModTime(), f)`. Range / If-Range / HEAD work,
  seeking is instant, the server never buffers the file, and **there is no
  local media byte cap** — nothing is held in memory. `serveMediaFile` sets the
  allowlist `Content-Type` first (ServeContent respects an already-set header)
  and `Cache-Control: no-store` so a tokenized URL's response is not kept in
  shared caches (`handler_files.go:1086-1088`).
- **Everything else stays buffered.** `previewRawCap(contentType)`
  (`handler_files.go:949`) still resolves the two budgets —
  `previewRawMediaMaxBytes` (`128 << 20`, `handler_files.go:930`) for
  audio/video, `previewRawMaxBytes` (`32 << 20`, `handler_files.go:923`)
  otherwise — and the 400 message is **derived from the resolved cap**
  (`"file exceeds the " + strconv.Itoa(int(capBytes>>20)) + " MiB preview limit"`)
  rather than hard-coded, so the two tiers cannot drift.

The effective caps after the streaming change:

| Transport | Documents/images | Audio/video |
|---|---|---|
| Local (`HandleFileRaw`) | 32 MiB buffered (`previewRawMaxBytes`) | **uncapped, streamed** (`serveMediaFile`) |
| Remote (`remoteFileRaw`) | 32 MiB buffered | 128 MiB buffered (`previewRawMediaMaxBytes`) |

The local non-media path only ever sees the 32 MiB document budget
(`handler_files.go:1058`), because media already branched to streaming at
`:1053`. `previewRawMediaMaxBytes` therefore now applies **only to the remote
buffered path**: `remoteFileRaw`
(`internal/server/handler_remote_files.go:419`) calls
`remoteReadFileCapped(..., previewRawCap(ct))` (`:449`) — remote media is
still fetch-into-memory + base64 over the transport, so it keeps the 128 MiB
budget and the stat-before-read guard that stops a huge binary OOMing the
server. There is **no range transport over SSH**, so remote media cannot use
`serveMediaFile`.

`TestPreviewRawCapSplitsMedia` (`handler_preview_test.go:233`) still pins
`previewRawCap`'s media/document split (the helper itself is unchanged even
though the local media caller no longer consults it).

## Local media capability tokens (`POST /api/files/media-token`)

A browser `<video>`/`<audio>` element **cannot attach an `Authorization`
header**, and the master `?token=` query form is deliberately forbidden in
`--remote` mode (it leaks the whole credential into logs, proxies, and
devtools). The local streaming path therefore uses a **short-lived, single-file
capability** in `?media_token=`:

- **Store** — `internal/server/media_tokens.go`, an in-memory `mediaTokenStore`
  held on the `Handler` (`internal/server/handler.go:220`, initialized `:382`).
  `mediaTokenTTL = 6h`, `mediaTokenMaxLen = 128`, and tokens are 32 bytes of
  `crypto/rand` base64url (**256 bits**). A `mediaGrant` is bound to the exact
  `(path, project_root, host)` triple and authorizes nothing else. Pruning is
  lazy, on `issue` (the only growth point). Tokens are in-process only and die
  with the server — no on-disk store to leak; a restart drops them by design.
- **Issue endpoint** — `POST /api/files/media-token` → `Handler.HandleMediaToken`
  (`internal/server/handler_files.go:1105`), registered as
  `s.authMiddleware(s.handleMediaToken)` (`internal/server/server.go:252`), i.e.
  issuing **requires the normal credential**. Validation: non-empty `path`;
  `host` must be empty (`"media streaming is local-only"` — remote is
  blob-only); the extension must be media (`previewExtIsMedia`); `project_root`,
  when present, must pass `fileContentRootFor`; and the path must not contain
  `..`. Returns `{"token": "..."}`; a 500 (never a predictable token) if the
  system RNG fails.
- **Grant validation happens at use time, in the middleware**, against the exact
  triple (`server.go:655`) — the token is a capability, never a substitute for
  the raw handler's own anchoring/containment checks.

## `mediaAuthMiddleware` — the rejection policy is load-bearing

`GET /api/files/raw` is guarded by `mediaAuthMiddleware`
(`internal/server/server.go:632`), which distinguishes three cases:

1. **No token, no master credential** → 401.
2. **Master credential present** → pass through (normal auth flow).
3. **`?media_token=` present** → validate the capability grant; reject if
   the token is expired, unknown, or does not match the exact
   `(path, project_root, host)` triple.

The middleware is applied to the raw endpoint **before** `HandleFileRaw`
(`server.go:251`), so the token never reaches the handler logic — it is a
gate, not a parameter. This means `HandleFileRaw` does not need to know
about tokens at all; it always sees a validated, anchored path.

## MediaViewer — local streaming with blob fallback

`MediaViewer` (`web/src/components/Preview/MediaViewer.tsx`) drives the
`<audio>`/`<video>` element:

- **Local files** (`!projectHost`): fetch a capability token via
  `api.getMediaToken(path, projectRoot)` (`client.ts:1385`), then set the
  element `src` to `/api/files/raw?path=…&project_root=…&media_token=…`.
  Range requests and seeking work natively; the browser downloads only what
  the user plays.
- **Remote or token-failure fallback**: fetch the whole blob via
  `api.getFileRaw(path, projectRoot, projectHost)`, create an object URL,
  and set it as `src`. The blob fallback still sets an explicit **Blob MIME**
  from `MEDIA_MIME` (`MediaViewer.tsx:7`), because some browsers will not
  demux a typeless blob URL; the raw endpoint sets the same types server-side.
- **One-shot retry on element error**: if the element fires `error` on the
  first `src` set (e.g. a token that expired between issue and use), the
  viewer discards the token, re-fetches a fresh one, and retries once.

The blob fallback still sets an explicit **Blob MIME** from `MEDIA_MIME`
  (`MediaViewer.tsx:7`), because some browsers will not demux a typeless blob
  URL; the raw endpoint sets the same types server-side.

New test: `web/src/components/Preview/MediaViewer.test.tsx`.

## Known limits

- **Remote media is still blob + 128 MiB cap.** There is no range transport
  over SSH, so `projectHost` media cannot stream and cannot exceed the remote
  media budget.
- **The tokenized URL is visible** in browser devtools, history, and any
  intermediate logs. That is acceptable *only* because the token is a
  single-file, media-only, 6-hour capability rather than the master credential
  — which is why it can be used where the master `?token=` form is forbidden in
  remote mode. Do not extend `?media_token=` to non-media paths, non-media
  extensions, or multi-file grants: the blast-radius argument depends on all
  three constraints holding.
- **Local media has no size cap.** A very large local file will be streamed
  range-by-range (bounded memory), but the browser still downloads what the
  user plays; the previous 128 MiB local guard is intentionally gone.

## Why `FileEditor` was left pure

Routing lives in `FileTabContent`, **not** inside `FileEditor`. `FileEditor`
stays a pure Monaco surface because it is reused for real text by the preview
viewers: `TextViewer` (`web/src/components/Preview/TextViewer.tsx:112`) and
`MarkdownViewer` (`web/src/components/Preview/MarkdownViewer.tsx:73`). Putting
the preview/legacy routing inside `FileEditor` would make those text viewers
inherit binary-preview branches they must never take.

## Load-bearing invariant: the fetch skip must stay in sync with the watcher guards

`useEditorTabs` (`web/src/hooks/useEditorTabs.ts`) treats a preview-only path as
having **no editable buffer**, so it must not fetch `/api/files/content` for it.
The viewers read their own bytes through `/api/files/raw`.

- `handleOpenFile` skips the content fetch entirely for preview-only paths
  (`useEditorTabs.ts:125`), so a large binary is not transferred and
  JSON-decoded into a JS string a second time.
- The module-scope helper `isPreviewOnlyPath` (`useEditorTabs.ts:75`, =
  `previewOnlyKindForPath(path) !== null || isLegacyOfficePath(path)`) guards
  **all three external-change watchers** so they do not re-download the whole
  binary just to compare an unused hash:
  - `reloadTabFromDisk` — `useEditorTabs.ts:446`
  - `checkAll` (the 10s / focus / visibilitychange poll) — the per-tab
    `checkOne` early return at `useEditorTabs.ts:522`
  - the **activate-tab re-check effect** — `useEditorTabs.ts:589`

**These guards are load-bearing and must stay in sync with the open-time skip.**
Without the activate-tab guard, switching to a preview tab re-ran the
external-change re-check, repopulated the tab's `content` from
`/api/files/content`, and re-introduced the duplicate fetch the skip exists to
avoid. The skip and the three watcher guards are one invariant expressed in
four places: a preview-only path is never fetched for content, only opened.

## Legacy `.doc` / `.ppt` / media OS-open fallback

Legacy binary Office formats (`.doc`, `.ppt`) have no in-browser renderer
(`docx-preview` handles only `.docx`; there is no server-side conversion in v1).
`isLegacyOfficePath` (`previewKind.ts:101`) matches them and
`LegacyOfficePane` (`web/src/components/Preview/LegacyOfficePane.tsx`) renders
the OS-open fallback instead of a "Binary File" editor dead-end. The pane was
extracted out of `PreviewHost`, which now shares it
(`web/src/components/Preview/PreviewHost.tsx:7`, used at `:144`).
"Open in app" runs on the **server** host (`POST /api/files/open` has no remote
branch), so the button is hidden for a remote project
(`LegacyOfficePane.tsx:8`, `:45`) — showing it would open an unrelated
server-local file while implying the remote file opened. `.mkv`/`.avi` take
this same fallback (no preview kind maps them).

## Divergence from the 2026-09-10 draft preview spec

`docs/superpowers/specs/2026-09-10-preview-multipurpose-design.md` is a Draft
design for sidebar `PreviewHost` + a full session-level `Preview` sub-tab. It
predates this Files-tab behavior and does **not** describe it. Known
divergences (recorded, not silently fixed — the spec is historical):

1. **This Files-tab auto-preview behavior is not in the spec at all.** The spec
   only covers the sidebar surface and the session `Preview` sub-tab; it never
   routes the Files-tab editor tabs by kind, and has no concept of
   `previewOnlyKindForPath` / `FileTabContent` / `LegacyOfficePane`.
2. **`.md` default differs.** The spec (§2, §7, §9) says "Monaco by default for
   both sidebar and full tab" with `MarkdownViewer` behind a toggle. Shipped
   code renders `MarkdownViewer` when `kind === "markdown"`
   (`web/src/components/Preview/PreviewSurface.tsx:52`), i.e. rendered markdown
   is the default. This divergence is pre-existing and out of scope for the
   Files-tab change; do not silently "fix" the spec.
3. **`PreviewSurface` signature differs.** The spec proposes
   `(path, kind, projectRoot, onSave)` (§4.1); the shipped component takes
   `(path, kind, projectRoot, projectHost, page, onPageChange, slide,
   onSlideChange, onOpenFile)` and has no `onSave`.
4. **Media streaming is newer than the spec.** The spec has no capability-token
   or range-streaming concept; its media handling (if any) predates this doc.

The new Files-tab behavior deliberately reuses the shipped `PreviewSurface`
rather than following the spec's proposed signature, so future readers should
treat the spec as design history for the sidebar/sub-tab work, and this doc as
the description of the Files-tab routing and local media streaming.

## Files and tests

- `web/src/lib/previewKind.ts` — `PREVIEW_ONLY_KINDS` (`:84`),
  `previewOnlyKindForPath` (`:91`), `isMarkdownPath` (`:92`),
  `isLegacyOfficePath` (`:101`).
- `web/src/components/Files/FileTabContent.tsx` (new) — tab-body routing +
  page/slide state + **markdown mode switch** (Edit/Preview/Split) +
  `useResizableSplit` + `useDebouncedValue` for live preview.
- `web/src/components/Preview/MediaViewer.tsx` (rewritten) — local capability →
  range-streamed element `src`; remote/degraded → blob URL with explicit MIME;
  one-shot retry on element error; test `MediaViewer.test.tsx`.
- `web/src/api/client.ts:1385` — `api.getMediaToken(path, projectRoot)`.
- `web/src/components/Preview/LegacyOfficePane.tsx` (new) — `.doc`/`.ppt`
  (and `.mkv`/`.avi`) OS-open fallback, shared with `PreviewHost`.
- `web/src/hooks/useEditorTabs.ts` — open-time fetch skip (`:125`) +
  `isPreviewOnlyPath` (`:75`) guarding the three watchers (`:446`, `:522`,
  `:589`).
- `web/src/hooks/useResizableSplit.ts` (new) — ratio-based pointer-capture
  split resize hook, localStorage persistence.
- `web/src/components/Preview/PreviewSurface.tsx` — `content?: string` prop
  (`:53`), forwarded to `MarkdownViewer` only (`:79`).
- `web/src/components/Preview/MarkdownViewer.tsx` — controlled mode
  (`content !== undefined` ⇒ skip fetch, `:39`).
- `web/src/components/Files/FileEditor.tsx` — `automaticLayout: true`
  (`:296`), `onOpenFile` prop (`:62`).
- `internal/server/handler_files.go` — `previewRawTypes` (`:960`),
  `previewRawCap` (`:949`), `isMediaContentType` (`:935`),
  `serveMediaFile` (`:1079`); `HandleFileRaw` media branch (`:1053`).
- `internal/server/handler_remote_files.go:449` — remote path uses
  `previewRawCap` (media stays buffered at 128 MiB).
- `internal/server/media_tokens.go` (new) — `mediaTokenStore`, TTL/len
  constants, triple-bound grants; `media_tokens_test.go`.
- `internal/server/server.go` — `mediaAuthMiddleware` (`:632`), route
  registrations (`:251`, `:252`), `handleMediaToken` (`:1299`).
- `internal/server/handler_files.go:1105` — `HandleMediaToken`.
- `internal/tool/preview.go` — `previewOpenKinds` (`:20`) + `preview_open`
  description now mention audio/video.
- Tests: `web/src/components/Files/FileTabContent.test.tsx` (routing +
  markdown mode switch, 10 tests), `web/src/lib/previewKind.test.ts`
  (helpers), `web/src/hooks/useEditorTabs.test.ts` (no `fetch` for
  preview-only opens), `web/src/components/Preview/MediaViewer.test.tsx`
  (capability/stream/retry), `internal/server/handler_preview_test.go`
  (`TestPreviewRawCoversOfficeSet`, `TestPreviewRawCapSplitsMedia`),
  `internal/server/media_tokens_test.go`,
  `internal/tool/preview_test.go`.
- `CHANGES.md` — 2026-09-16 entry.

## Test-suite status

The pre-existing `PreviewSurface.integration.test.tsx` failure is **fixed**:
importing `PreviewSurface` pulled `TextViewer → FileEditor → monaco-setup`,
which vitest's resolver could not load. The test now `vi.mock`s every viewer
(including `MediaViewer`) and covers only `PreviewSurface`'s kind routing
(`PreviewSurface.integration.test.tsx:7-15`).

Re-verified 2026-09-16 with a fresh full `vitest run`: the **full web suite is
green** — every test file and test passes, including every preview test and
`PreviewSurface.integration.test.tsx` — and `tsc --noEmit` is clean.

Re-verified 2026-09-17: `FileTabContent.test.tsx` expanded to 10 tests
covering the markdown mode switch (Edit default, preview with Monaco hidden,
split pane rendering, live-content propagation, divider interaction,
`.markdown` extension, no mode chrome on code/plain-text). Full web suite
green; `tsc --noEmit` clean; `npm run build` green.

Treat this as a **point-in-time** result, not a standing fact. A suite-status
note captured while another file is mid-edit does not describe the committed
tree — this section once briefly carried unrelated failures from a
concurrently-modified hook, which were gone on a clean re-run. Re-run
`pnpm vitest run` and `tsc --noEmit` in `web/` before repeating any
green/failing summary, and do not pin volatile file/test totals into the
bundle.

## Related

- `docs/gotchas/browser-panel-transition-inconsistency.md` — another
  documentation-vs-landed-behavior mismatch in the web preview/browser surface.
- `docs/superpowers/specs/2026-09-10-preview-multipurpose-design.md` —
  superseded §2 for the Files-tab `.md` behavior (now Edit/Preview/Split in
  `FileTabContent`); sidebar `PreviewHost` path unchanged.
