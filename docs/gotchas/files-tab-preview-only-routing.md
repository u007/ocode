---
type: Gotcha
title: Files Tab Auto-Previews Binary/Office/Media Formats (Preview-Only Routing + Local Media Streaming)
description: 'Gotcha: Files-tab auto-preview routing for binary/Office/media formats plus markdown/MDX Edit/Preview/Split mode switch. Updated 2026-09-21 with .mdx support in kindByExt, isMarkdownPath, and the mode switch.'
resource: ""
tags: []
timestamp: 2026-09-21T16:07:14Z
---
# Files Tab Auto-Previews Binary/Office/Media Formats (Preview-Only Routing + Local Media Streaming)

**Type:** Gotcha  
**Description:** Gotcha: Files-tab auto-preview routing for binary/Office/media formats plus markdown/MDX Edit/Preview/Split mode switch. Updated 2026-09-21 with .mdx support in kindByExt, isMarkdownPath, and the mode switch.  
**Resource:** web/src/components/Files/FileTabContent.tsx  
**Tags:** gotcha, web, files-tab, editor, preview, monaco, binary, pdf, docx, pptx, excel, image, audio, video, media, routing, useEditorTabs, streaming, http-range, servecontent, capability-token, media-token, auth, markdown, mdx, split-mode, useResizableSplit  

---

# Files Tab Auto-Previews Binary/Office/Media Formats (Preview-Only Routing + Local Media Streaming)

**Type:** Gotcha  
**Description:** The web Files-tab editor auto-routes PDF, Word/PowerPoint/Excel, image, and audio/video files to the shared preview surface instead of Monaco; preview-only paths also skip the /api/files/content fetch, and the external-change watchers must stay in sync with that skip. Local audio/video now streams with HTTP range support (http.ServeContent) behind a short-lived single-file capability token (POST /api/files/media-token, mediaAuthMiddleware), so the local media byte cap is gone; the 32 MiB document cap and the 128 MiB cap for the REMOTE buffered path remain. Includes the .doc/.ppt/.mkv/.avi OS-open fallback and divergence from the 2026-09-10 preview design spec. .md, .markdown, and .mdx files keep Monaco with an Edit/Preview/Split mode switch (not preview-only routing).  
**Resource:** web/src/components/Files/FileTabContent.tsx  
**Tags:** gotcha, web, files-tab, editor, preview, monaco, binary, pdf, docx, pptx, excel, image, audio, video, media, routing, useEditorTabs, streaming, http-range, servecontent, capability-token, media-token, auth, markdown, mdx, split-mode, useResizableSplit  

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
(`previewKind.ts:72`). `.mdx` is also **deliberately excluded** and keeps the
Monaco editor (it routes to the same markdown mode switch as `.md`/`.markdown`).
`previewOnlyKindForPath` (`previewKind.ts:91`) is
therefore the narrow "must not open in Monaco" subset of the broader preview
allowlist; `previewKindForPath` remains the sidebar/`PreviewSurface` dispatch
helper.

`FileTabContent` also owns the PDF-page and PPTX-slide state
(`FileTabContent.tsx:78-79`) that the controlled viewers need; every editor
tab stays mounted (hidden, not unmounted — `App.tsx` renders panes for **all**
`editorTabs` and hides non-visible ones with the CSS `hidden` class, gated by a
`visitedEditorTabsRef` lazy-mount set), so this state survives tab switches
**and project switches** (2026-09-20). `visibleEditorTabs` still drives the tab
bar, the resolved active id, context attachments, and save/close; the per-tab
`session` prop is gated on a `visibleEditorTabIds` Set so a background
project's pane does not fetch diff decorations for the active session.

### Project switch (2026-09-20)

A project switch used to reset an open preview because App rendered panes from
`visibleEditorTabs = visibleEditorTabsForProject(editorTabs, activeProject)`,
removing the outgoing project's `FileTabContent` from the tree. Panes now stay
mounted and are hidden with CSS. The sidebar `PreviewHost` still remounts on a
project switch (its `key={sideStateKey}` changes with `activeTabId`), so it
persists shell state per project via `web/src/components/Preview/sidebarPreviewState.ts`,
and per-file viewer state lives in `web/src/lib/previewViewState.ts`. See
`gotchas/project-scope-is-mounting-not-visibility.md` for the full mechanism.

## Markdown mode switch (2026-09-17, .mdx added 2026-09-21)

`FileTabContent` now renders an **Edit / Preview / Split mode switch** for
`.md`/`.markdown`/`.mdx` files (when `!props.isBinary`). The mode control is a
`role="group"` (`aria-label="Markdown view mode"`) of `aria-pressed` toggle
buttons — the same pattern as `FileTree.tsx`'s list/column view switch. New
exported helper `isMarkdownPath(path)` (`previewKind.ts:92`) returns
`previewKindForPath(path) === "markdown"` — this is `true` for `.md`,
`.markdown`, AND `.mdx`.

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

- **`.md`/`.markdown`/`.mdx` remain deliberately excluded from
  `PREVIEW_ONLY_KINDS`** (`previewKind.ts:84`). The fetch-skip invariant for
  preview-only paths is unaffected — `/api/files/content` is still always
  fetched for markdown tabs.

- **`.mdx` is NOT evaluated.** `MarkdownViewer` uses `react-markdown` +
  `remark-gfm` only — ESM `import`/`export` lines and JSX components render as
  source-level text. Evaluating MDX would execute arbitrary JavaScript from a
  previewed file. See `docs/gotchas/mdx-preview-not-evaluated.md`.

## Audio/video were added to the preview-only set

`PreviewKind` (`previewKind.ts:13`) gained `audio` and `video`, and
`previewOpenKinds` (`internal/tool/preview.go`) and `previewRawTypes`
(`internal/server/handler_files.go:960`) gained matching types/captured
extensions. 2026-09-16: `.mmd` (Mermaid flow) was already in the markdown kind
and renders through `MermaidViewer`. 2026-09-21: `.mdx` added to the markdown
kind (previewable, editor default Edit with mode switch).

## Transport caps stayed in place

The 32 MiB document budget and the 128 MiB remote-buffered cap were kept; media
was carved out first (branching at `handler_files.go:1053`), so media streams
while documents stay buffered. The split is verified by
`TestPreviewRawCoversOfficeSet` and `TestPreviewRawCapSplitsMedia`
(`handler_preview_test.go`).

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

`mediaAuthMiddleware` (`server.go:632`) wraps `handleMediaToken` and enforces
three constraints that make the capability safe even if leaked:

- **No cross-host reuse.** A token granted for `(path, root, host_A)` fails
  validation on host B or a different root (the `mediaGrant` is triple-bound).
- **No wildcard grant.** There is no "all files under root" token — each token
  is one path. Multi-file browsing re-issues tokens per file via the UI.
- **No extension bypass.** The extension check lives at grant time, not just
  at use time; a media token cannot be reused for a document.

Together these mean leaking a token gives an attacker one file, on one host,
inside one project, for six hours — not arbitrary read access. The
`mediaTokenMaxLen` bound prevents header-injection-style long values, and the
in-memory store means there is no persistence layer to compromise.

**Why not just use the master `?token=` query form?** Because the master token
is the user's full credential — it must NEVER appear in URLs (browser history,
proxy logs, Referer headers, devtools network tab). The short-lived single-file
grant is a scope-limited derivative that the browser can attach to a URL
safely because it authorizes exactly one resource.

**Why not just serve media from a separate unauthenticated endpoint?** Because
media in this project can be private (private repos, private workspace files).
The capability token keeps the auth check at the edge while the browser gets
what it needs (URL-attachable, range-requestable).

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
path — it uses the local OS on the server machine, not the user's browser/OS),
so the user sees the server's default app for that type. The server must have a
GUI/desktop environment or a default handler registered for the MIME type.

`.mkv`, `.avi` and other media with no browser native support fall through to
`isLegacyOfficePath` too (they are not in `previewOpenKinds`), so the OS-open
path is the only option — there is no web player fallback. See the media
section above for the streaming exception for native formats (`.mp4`, `.webm`,
`.mp3`, `.wav`).

`isPreviewOnlyPath` (`useEditorTabs.ts:75`) combines preview-only kinds
(`previewOnlyKindForPath(path) !== null`) with the legacy Office fallback
(`isLegacyOfficePath(path)`), so both groups skip the content fetch and the
external-change watchers. The legacy group is binary and has no editor anyway.

## New/changed files (2026-09-21: .mdx support)

- `web/src/lib/previewKind.ts` — `kindByExt` gained `".mdx": "markdown"`;
  `isMarkdownPath` now true for `.md`, `.markdown`, `.mdx`
- `web/src/lib/editorLanguage.ts` (new) — `languageForFile(path)` extracted
  from duplicated Monaco maps; `.mdx` → `mdx`, `.md`/`.markdown` → `markdown`
- `web/src/lib/editorLanguage.test.ts` (new), `web/src/lib/previewKind.test.ts`
  (`.mdx` + `isMarkdownPath` describe),
  `web/src/components/Files/FileTabContent.test.tsx` ("treats .mdx as markdown
  too")
- `internal/tool/preview.go` `previewOpenKinds` gained `".mdx": "text"`,
  `".markdown": "text"`; `internal/server/handler_files.go` `previewRawTypes`
  gained `".markdown"`/`".mdx"` → `text/markdown; charset=utf-8`
- `internal/tool/preview_test.go`, `internal/server/handler_preview_test.go`,
  `internal/server/handler_files.go` tests updated

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

Re-verified 2026-09-21: `.mdx` support verified — `FileTabContent.test.tsx`
gains "treats .mdx as markdown too"; `previewKind.test.ts` gains `.mdx` →
markdown and `isMarkdownPath` describe; `editorLanguage.test.ts` (new) covers
`.mdx` → `mdx`, `.md`/`.markdown` → `markdown`; Go tests in
`internal/tool/preview_test.go` accept `notes.markdown`/`docs/page.mdx`;
`internal/server/handler_preview_test.go` covers markdown-family content
types. Mutation-verified: removing the `kindByExt` `.mdx` entry fails 3 tests.
Full web suite green; `tsc --noEmit` clean.

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
- `docs/gotchas/mdx-preview-not-evaluated.md` — MDX preview rule: rendered as
  Markdown, never evaluated (security).
- `docs/superpowers/specs/2026-09-10-preview-multipurpose-design.md` —
  superseded §2 for the Files-tab `.md` behavior (now Edit/Preview/Split in
  `FileTabContent`); sidebar `PreviewHost` path unchanged.
