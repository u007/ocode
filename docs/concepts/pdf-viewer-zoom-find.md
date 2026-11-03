---
type: Concept
title: PDF viewer zoom, Space navigation, and find
description: "PDF viewer zoom-at-cursor, Space zoom+pan modifier, cross-page find (rendered through the shared preview FindBar) with overlays, Print/Download, per-file view-state persistence across project switches, and jsdom test gotchas."
resource: web/src/components/Preview/PdfViewer.tsx; web/src/components/Preview/usePdfFind.ts; web/src/components/Preview/FindBar.tsx; web/src/lib/previewViewState.ts
tags:
  - web
  - pdf
  - pdfjs
  - viewer
  - zoom
  - find
  - preview
  - jsdom
  - persistence
  - project-switch
timestamp: 2026-11-03T05:46:54Z
---
# PDF viewer zoom, Space navigation, and find

**Type:** Concept
**Description:** PDF viewer zoom-at-cursor, Space zoom+pan modifier, cross-page find (rendered through the shared preview FindBar) with overlays, Print/Download, per-file view-state persistence across project switches, and jsdom test gotchas.
**Resource:** web/src/components/Preview/PdfViewer.tsx; web/src/components/Preview/usePdfFind.ts; web/src/components/Preview/FindBar.tsx; web/src/lib/previewViewState.ts; web/src/components/Preview/sidebarPreviewState.ts
**Tags:**
  - web
  - pdf
  - pdfjs
  - viewer
  - zoom
  - find
  - preview
  - jsdom
  - persistence
  - project-switch

---

## Zoom architecture

`PdfViewer.tsx` renders a continuous pdf.js viewer. Pages are laid out from **zoom-invariant base geometry**: each page's `getViewport` at the default scale is stored in `dims` and divided by the active zoom on correction. Every displayed size = base × zoom, so zooming never requires re-measuring (pdf.js viewports are linear in scale). Bounds 25%–400%; exponential steps (×1.25 for keys/buttons, `exp(-Δy/450)` for wheel).

`renderPage` no longer sets `canvas.width` directly (which cleared the live bitmap → blank flash). Renders go to an offscreen canvas, then `drawImage` onto the live one; the old bitmap stays CSS-stretched until the new raster lands. A `renderGenRef` generation counter aborts state mutation for renders started under older zoom/rotation. The `canBlit()` probe skips the 2D-context copy in jsdom (tests) but still sizes backing stores.

Cursor-anchored scroll remap after zoom: `scroll = anchor·ratio − cursorOffset` on both axes, applied in a `useLayoutEffect` committed before paint. Ctrl/⌘+wheel and trackpad pinch (arrives as ctrl+wheel) anchor at the mouse cursor.

## Space interactions

Holding Space turns the plain wheel into **zoom-at-cursor** (same anchor math as ctrl+wheel) and left-drag into **panning** (pointer capture, scroll follows −Δ of pointer, cursor-grab/grabbing CSS classes, `select-none`). Space is eaten (`preventDefault`) only when the target isn't editable (INPUT/TEXTAREA/SELECT/contentEditable) and no Ctrl/⌘/Alt held. `window`-level keyup + blur clear the modifier so Space can't stick.

## Bindings

| Action | Toolbar | Keys (viewer) | Pointer |
|---|---|---|---|
| Zoom out | − | Ctrl/⌘−, bare − | Ctrl/⌘+wheel, Space+wheel |
| Zoom in | + | Ctrl/⌘+, bare + | |
| Reset | % label (click → 100%) | 0 | |
| Fit width | Fit width | | |
| Fit page | Fit page | | |
| Rotate | ⟳ | | |
| Find | Find | Ctrl/⌘F | |
| First/last page | — | Home / End | |
| Prev/next page | — | ArrowLeft / ArrowRight (pre-existing) | |

The **Find** toolbar button is the shared `"Find in document"` control
(`PdfViewer.tsx:1024`), not a PDF-only widget.

Rotation: 90° steps; re-measures every page's base dims via `getViewport({scale: PAGE_SCALE, rotation: (pg.rotate + rot) % 360})` and re-renders the visible window (unrenders all pages first).

## Find (`usePdfFind.ts`) — PDF engine behind the shared FindBar

- **The bar is the shared `FindBar`, not a PDF-specific one.** `PdfViewer` renders the same component the Markdown, Word and HTML previews use (`PdfViewer.tsx:1027`) and imports `isEditableTarget` from `findDom.ts` (`PdfViewer.tsx:9`). Labels, key handling and the highlight palette are documented once in [preview-find.md](preview-find.md).
- **`usePdfFind` is the PDF-ONLY engine** (`usePdfFind.ts:29`): pdf.js text items are not DOM text, so a PDF cannot use the DOM engine (`useDomFind`) that serves Markdown/Word.
- **Index**: per-page text index built once per document from the SAME `getTextContent()` items joined with single spaces that the DOM text-layer spans use (viewer appends `" "` text nodes between item spans) — match character offsets map 1:1 onto DOM spans.
- **Search**: regex over ALL pages (not just rendered). Toggles: Aa (case-sensitive), ab (whole-word `\b(?:escaped)\b`). Counter n/total; Enter/Shift+Enter, F3/Shift+F3 walk with wrap; Esc closes. Ctrl/⌘F is claimed only by the **visible** viewer: the window keydown effect returns early unless the `active` prop is set (`PdfViewer.tsx:818`), so N mounted-but-hidden PDF panes cannot all flip `find.active` or race on F3. (The DOM viewers use `previewWantsFindShortcut` instead — see preview-find.md.)
- **Highlights**: DOM Range over live text-layer spans → `getClientRects()` → page-relative overlay divs over each page (yellow = all matches, orange = current), recomputed on renderTick bumps. The colours are deliberately identical to `FindHighlights.tsx` so find looks the same on every surface. Current match scrolls to ~⅓ viewport height; if its page isn't rendered, falls back to scrolling to the page via `onPageChange`.
- **Hook guards**: `Range.getClientRects` wrapped in try/catch + optional call (jsdom lacks it); search runs use `runRef` generation so stale async runs can't commit (`usePdfFind.ts:48`); index cached per document object (identity check); `docVersion` bump on new document resets all find state.
- **State is per-mount, not persisted**: `previewViewState.ts` stores only `page`/`zoom`/`scrollTop`, so an active search survives a close/reopen within the mount but not a remount.

## Print & Download

- Both reuse an independent `blobRef` Blob snapshot of the PDF bytes taken at load — pdf.js may transfer (detach) the ArrayBuffer handed to `getDocument`, so always snapshot `new Blob([buf.slice(0)])` BEFORE calling `getDocument`; reset `blobRef.current = null` in the load effect's cleanup.
- **Download**: `saveBlobAsFile(blob, basename)` (`web/src/lib/saveBlob.ts`; the basename appends `.pdf` if missing). In a browser that is an object URL on a temp anchor, revoked after 10s (Safari navigates to `about:blank` if revoked synchronously). In the desktop app the bytes go to the shell over the `ocode:save-file:` bridge message and the native save panel opens (`cmd/ocode-desktop/save_file.go`), because WKWebView drops blob `<a download>` clicks without any error. The desktop save is unverified live; see `TODO.md`.
- **Print**: `printPdf` (`PdfViewer.tsx`) makes an object URL from the blob and loads it in a hidden 1px iframe. On `load` it waits 500 ms (`PDF_PRINT_SETTLE_MS`) for the PDF viewer to render, then calls `contentWindow.print()`, so the browser's PDF engine opens the print dialog. If `print()` throws, it logs the error with `console.error` (what was attempted, the file name, and the error) and opens the URL in a popup tab. A 30 s watchdog (`PDF_PRINT_LOAD_TIMEOUT_MS`, also wired to `onerror`) opens a popup tab only if the frame's `onload` never fired (the `loaded` flag), and falls back to `downloadPdf` if the popup is blocked; the object URL is revoked on the 10 min timer (`PDF_PRINT_CLEANUP_MS`) on either branch. A successful print therefore opens no popup tab. The frame and URL are cleaned up on the iframe's `afterprint` event, with a 10 min safety timer (`PDF_PRINT_CLEANUP_MS`) if it never fires.
  - Desktop PDF print inside the WKWebView blob frame is unverified live; see `TODO.md`.
- Buttons in the toolbar next to Find: Print, Download (disabled until `total > 0`).

## View-state persistence (2026-09-20)

A project switch used to unmount the Files-tab pane and still remounts the sidebar `PreviewHost` (its `key` changes with the active tab id), so viewer state must survive via localStorage, not component state. The Files-tab panes now stay mounted (hidden with CSS) across project switches — see `gotchas/project-scope-is-mounting-not-visibility.md` — but the per-file store remains the mechanism for the app-reload / sidebar-remount case.

### Per-file viewer state — `web/src/lib/previewViewState.ts`

- localStorage key `ocode.ui.previewViewState.v1`; entry key = `previewViewKey(path, projectRoot, projectHost)` = `host::projectRoot::path` (mirrors editor-tab scoping so a remote `report.pdf` never shares state with a local one).
- Shape: `{page?, zoom?, scrollTop?}`. Bounded to 200 entries (MRU eviction, stable insertion order). Prototype-pollution keys (`__proto__`, `constructor`, `prototype`) are dropped at load; non-finite values are rejected.
- `PdfViewer` restores zoom + a stored `scrollTop` (consistent with the target page's band) on its first-placement effect. Persistence is **armed only after** the first-placement restore (`restoreDoneRef`): `schedulePersist` no-ops until `restoreDoneRef.current` is true, so pre-restore state cannot overwrite the stored position. Debounced 250 ms via `saveTimerRef`; flushes on unmount (uses `lastScrollTopRef` because React detaches refs before passive-effect cleanup).
- `FileTabContent` restores/persists page (or slide for pptx) under the same key, so all viewers for one file share a single position.

### Per-project sidebar shell state — `web/src/components/Preview/sidebarPreviewState.ts`

localStorage `ocode.ui.sidebarPreview.v1`, keyed `host::root`. Shape `{surface, path, kind, projectRoot, projectHost, page, unsupportedPath}`. `PreviewHost` seeds initial state from it once per mount (via `initialRef`) and writes back on change; a containment guard (`docBelongsHere` / `unsupportedBelongsHere`) refuses to store a doc whose anchor differs from the pane's project, so a foreign-rooted request cannot poison the slot.

### One-shot activation

`usePreviewActivation` returns `consume()` (and a monotonic nonce); App threads `onConsumeActivation` to PreviewHost. Without it a remount would replay a stale activation over the just-restored state.

## ⚠️ jsdom gotchas (save debugging time)

1. **No PointerEvent**: `fireEvent.pointerDown` yields no button/clientX — dispatch `MouseEvent` with `type: "pointerdown"` instead.
2. **`Range.prototype.getClientRects` undefined** in jsdom: stub on the prototype, restore in `afterEach`. This affects BOTH engines (`usePdfFind` and `useDomFind`).
3. **CSS-escaped class names are invalid selectors**: `.bg-yellow-300/45` is NOT valid for `querySelectorAll` — match by `[aria-hidden="true"]` + `className.includes` instead.
4. **`getByLabelText` must come from the render result object**, not `container`.
5. **`vi.mock` of `pdfjs-dist` must implement `Util.transform` as a real matrix product** when the viewer builds text spans — `vi.fn()` returning undefined causes a `tx[4]` crash.
6. **No `URL.createObjectURL` in jsdom**: install fakes on `URL` and restore natively (don't `vi.spyOn` — it throws "does not exist"). Print assertions check the hidden iframe's `src`. The object URL is revoked on `afterprint` or a 10 min timer, so use fake timers to observe the revoke.

### Test hygiene

PDF viewer suites (`PdfViewer.persistence.test.tsx`, `PreviewHost.test.tsx`, `FileTabContent.test.tsx`, `previewViewState.test.ts`) call `localStorage.clear()` in `beforeEach` because several reuse `path="doc.pdf"` and would otherwise inherit the previous case's stored position. Find-specific coverage lives in `PdfViewer.find.test.tsx` (see preview-find.md).

## Validation

Preview dir: 11 test files / 44 tests green (includes 3 pre-existing PdfViewer suites). `tsgo --noEmit` clean. `vite build` clean (PdfViewer chunk 448.31 kB).
