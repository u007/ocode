---
type: Concept
title: PDF viewer zoom, Space navigation, and find
description: "PDF viewer zoom-at-cursor, Space zoom+pan modifier, cross-page find with overlays, Print/Download, per-file view-state persistence across project switches, and jsdom test gotchas."
resource: web/src/components/Preview/PdfViewer.tsx; web/src/components/Preview/usePdfFind.ts; web/src/lib/previewViewState.ts
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
timestamp: 2026-09-20T00:38:52Z
---
# PDF viewer zoom, Space navigation, and find

**Type:** Concept
**Description:** PDF viewer zoom-at-cursor, Space zoom+pan modifier, cross-page find with overlays, Print/Download, per-file view-state persistence across project switches, and jsdom test gotchas.
**Resource:** web/src/components/Preview/PdfViewer.tsx; web/src/components/Preview/usePdfFind.ts; web/src/lib/previewViewState.ts; web/src/components/Preview/sidebarPreviewState.ts
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

Rotation: 90° steps; re-measures every page's base dims via `getViewport({scale: PAGE_SCALE, rotation: (pg.rotate + rot) % 360})` and re-renders the visible window (unrenders all pages first).

## Find (`usePdfFind.ts`)

- **Index**: per-page text index built once per document from the SAME `getTextContent()` items joined with single spaces that the DOM text-layer spans use (viewer appends `" "` text nodes between item spans) — match character offsets map 1:1 onto DOM spans.
- **Search**: regex over ALL pages (not just rendered). Toggles: Aa (case-sensitive), ab (whole-word `\b(?:escaped)\b`). Counter n/total; Enter/Shift+Enter, F3/Shift+F3 walk with wrap; Esc closes; Ctrl/⌘F opens the bar from anywhere (window keydown; editable targets keep browser-native find).
- **Highlights**: DOM Range over live text-layer spans → `getClientRects()` → page-relative overlay divs over each page (yellow = all matches, orange = current), recomputed on renderTick bumps. Current match scrolls to ~⅓ viewport height; if its page isn't rendered, falls back to scrolling to the page via `onPageChange`.
- **Hook guards**: `Range.getClientRects` wrapped in try/catch + optional call (jsdom lacks it); search runs use `runRef` generation so stale async runs can't commit; index cached per document object (identity check); `docVersion` bump on new document resets all find state.

## Print & Download

- Both reuse an independent `blobRef` Blob snapshot of the PDF bytes taken at load — pdf.js may transfer (detach) the ArrayBuffer handed to `getDocument`, so always snapshot `new Blob([buf.slice(0)])` BEFORE calling `getDocument`; reset `blobRef.current = null` in the load effect's cleanup.
- **Download**: object URL → temp anchor with `download` = path basename (appends `.pdf` if missing) → revoke after 10s (Safari navigates to `about:blank` if revoked synchronously).
- **Print**: hidden 1px iframe with the blob URL; onload → `contentWindow.print()`. Fallback chain: `print()` throw → `window.open(url)` popup tab → popup blocked (`open` returns null) → `downloadPdf()`. 1s watchdog: if the frame is still connected (WKWebView sometimes never fires blob: iframe onload), remove it and open the tab; 60s timer reaps frame + URL.
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
2. **`Range.prototype.getClientRects` undefined** in jsdom: stub on the prototype, restore in `afterEach`.
3. **CSS-escaped class names are invalid selectors**: `.bg-yellow-300/45` is NOT valid for `querySelectorAll` — match by `[aria-hidden="true"]` + `className.includes` instead.
4. **`getByLabelText` must come from the render result object**, not `container`.
5. **`vi.mock` of `pdfjs-dist` must implement `Util.transform` as a real matrix product** when the viewer builds text spans — `vi.fn()` returning undefined causes a `tx[4]` crash.
6. **No `URL.createObjectURL` in jsdom**: install fakes on `URL` and restore natively (don't `vi.spyOn` — it throws "does not exist"). jsdom never fires blob: iframe onload → the 1s watchdog path is what runs; the watchdog timer is scheduled at CLICK time, so `vi.useFakeTimers()` must be installed BEFORE the click for `advanceTimersByTime` to drive it. `window.open` logs "Not implemented" and returns undefined (= popup-blocked fallback to download); stub `window.open` to control the path.

### Test hygiene

PDF viewer suites (`PdfViewer.persistence.test.tsx`, `PreviewHost.test.tsx`, `FileTabContent.test.tsx`, `previewViewState.test.ts`) call `localStorage.clear()` in `beforeEach` because several reuse `path="doc.pdf"` and would otherwise inherit the previous case's stored position.

## Validation

Preview dir: 11 test files / 44 tests green (includes 3 pre-existing PdfViewer suites). `tsgo --noEmit` clean. `vite build` clean (PdfViewer chunk 448.31 kB).
