---
type: Gotcha
title: Files Tab Auto-Previews Binary/Office Formats (Preview-Only Routing)
description: The web Files-tab editor auto-routes PDF, Word/PowerPoint/Excel, and image files to the shared preview surface instead of Monaco; preview-only paths also skip the /api/files/content fetch, and the external-change watchers must stay in sync with that skip. Includes the .doc/.ppt OS-open fallback and divergence from the 2026-09-10 preview design spec.
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
  - routing
  - useEditorTabs
timestamp: 2026-09-16T09:11:32Z
status: deprecated
deprecated_reason: 'Misplaced by a path error: doc tools take bundle-relative paths (root is docs/), so this write landed at docs/docs/gotchas/... instead of docs/gotchas/.... Superseded by the correct doc at gotchas/files-tab-preview-only-routing.md; safe to remove via /docs cleanup.'
---
# Files Tab Auto-Previews Binary/Office Formats (Preview-Only Routing)

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
   `PreviewSurface` (`FileTabContent.tsx:27`).
2. `isLegacyOfficePath(path)` → render `LegacyOfficePane`
   (`FileTabContent.tsx:40`).
3. otherwise → render `FileEditor` (Monaco) (`FileTabContent.tsx:44`).

The **preview-only kind set** is `pdf`, `docx`, `pptx`, `excel`, `image`
(`PREVIEW_ONLY_KINDS`, `web/src/lib/previewKind.ts:66`). These are binary
containers with no editable text representation. `markdown`, `text`, and
`mermaid` are **deliberately excluded** and keep the Monaco editor, even though
`previewKindForPath` still resolves them as previewable
(`previewKind.ts:54`). `previewOnlyKindForPath` (`previewKind.ts:73`) is
therefore the narrow "must not open in Monaco" subset of the broader preview
allowlist; `previewKindForPath` remains the sidebar/`PreviewSurface` dispatch
helper.

`FileTabContent` also owns the PDF-page and PPTX-slide state
(`FileTabContent.tsx:19`) that the controlled viewers need; every editor tab
stays mounted (hidden, not unmounted — see `App.tsx:1013`), so this state
survives tab switches.

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

## Legacy `.doc` / `.ppt` OS-open fallback

Legacy binary Office formats (`.doc`, `.ppt`) have no in-browser renderer
(`docx-preview` handles only `.docx`; there is no server-side conversion in v1).
`isLegacyOfficePath` (`previewKind.ts:83`) matches them and
`LegacyOfficePane` (`web/src/components/Preview/LegacyOfficePane.tsx`) renders
the OS-open fallback instead of a "Binary File" editor dead-end. The pane was
extracted out of `PreviewHost`, which now shares it
(`web/src/components/Preview/PreviewHost.tsx:7`, used at `:144`).
"Open in app" runs on the **server** host (`POST /api/files/open` has no remote
branch), so the button is hidden for a remote project
(`LegacyOfficePane.tsx:8`, `:45`) — showing it would open an unrelated
server-local file while implying the remote file opened.

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
   (`web/src/components/Preview/PreviewSurface.tsx:49`), i.e. rendered markdown
   is the default. This divergence is pre-existing and out of scope for the
   Files-tab change; do not silently "fix" the spec.
3. **`PreviewSurface` signature differs.** The spec proposes
   `(path, kind, projectRoot, onSave)` (§4.1); the shipped component takes
   `(path, kind, projectRoot, projectHost, page, onPageChange, slide,
   onSlideChange, onOpenFile)` and has no `onSave`

The new Files-tab behavior deliberately reuses the shipped `PreviewSurface`
rather than following the spec's proposed signature, so future readers should
treat the spec as design history for the sidebar/sub-tab work, and this doc as
the description of the Files-tab routing.

## Files and tests

- `web/src/lib/previewKind.ts` — `previewOnlyKindForPath` (`:73`),
  `isLegacyOfficePath` (`:83`).
- `web/src/components/Files/FileTabContent.tsx` (new) — tab-body routing +
  page/slide state.
- `web/src/components/Preview/LegacyOfficePane.tsx` (new) — `.doc`/`.ppt`
  OS-open fallback, shared with `PreviewHost`.
- `web/src/hooks/useEditorTabs.ts` — open-time fetch skip (`:125`) +
  `isPreviewOnlyPath` (`:75`) guarding the three watchers (`:446`, `:522`,
  `:589`).
- Tests: `web/src/components/Files/FileTabContent.test.tsx` (routing),
  `web/src/lib/previewKind.test.ts` (helpers),
  `web/src/hooks/useEditorTabs.test.ts` (no `fetch` for preview-only opens).
- `CHANGES.md` — 2026-09-16 entry.

## Related

- `docs/gotchas/browser-panel-transition-inconsistency.md` — another
  documentation-vs-landed-behavior mismatch in the web preview/browser surface.
- `docs/knowledge-bundle.md` — why this doc needs explicit frontmatter: the
  2026-09-10 spec has no OKF frontmatter, so it is listed unlinked under
  "Unclassified" in `docs/index.md:375` and is **not** search-indexed, which is
  why a `doc_search` for "preview" returned no hits before this doc existed.
