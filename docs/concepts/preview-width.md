---
type: Concept
title: 'Markdown preview: persisted paper-width reading column'
description: How the markdown preview renders a centred, paper-width reading column (Auto/Letter/A4·F4/Full) chosen from a header control, with a global persisted preference, locale-based auto resolution, and a width-only preset decision.
tags:
  - web
  - preview
  - markdown
  - reading column
  - paper width
  - localStorage
  - typography
timestamp: 2026-10-31T12:33:57Z
---
# Markdown preview: persisted paper-width reading column

## What this is

The markdown preview (sidebar `PreviewHost`, the Preview tab, and the Files-tab
preview) used to render edge-to-edge prose — `max-w-none … p-3` — with no
margin at all. That class pair exists to defeat `@tailwindcss/typography`'s
65ch gutter (see the invariants below) and still does. The feature restores a
deliberate, **centred reading column whose width is a PAPER width**, chosen
from a header control: **Auto / Letter / A4·F4 / Full**. Code and plain-text
previews are unaffected and stay full-width.

Implementation lives in `web/src/lib/previewWidth.ts`, with the control in
`web/src/components/Preview/PreviewWidthMenu.tsx`.

## Presets and resolution

Preset type: `PreviewWidthPreset = "auto" | "letter" | "a4" | "full"`
(`previewWidth.ts:31`). The concrete column:
`ResolvedPreviewWidth = "letter" | "a4" | "full"` (`previewWidth.ts:34`).

`resolvePreviewWidth(preset, { locale })` (`previewWidth.ts`):

- `preset === "auto"` → `defaultPaperForLocale(locale)`.
- otherwise → the preset itself.

There is deliberately **no `prose` switch**. Code and plain text stay full
width by construction: `PreviewSurface` renders `MarkdownViewer` only for
`kind === "markdown"`, and the header control is gated on the same kind, so a
non-prose surface never calls the resolver. (An earlier draft had a
`prose: false → "full"` branch with no production caller; the docs claimed it
was the mechanism while nothing exercised it.)

`defaultPaperForLocale(locale)` (`previewWidth.ts`): the CLDR territory list of
countries whose default paper is US Letter (`us ca mx ph cl co cr sv gt ni pa
pr ve bz lr`) → Letter; everything else, including a bare `en` with no region →
A4. This is NOT the metric-vs-imperial list: Liberia uses Letter, but Myanmar
(MM) is an A4 market, and Canada/Mexico/the Philippines use Letter despite being
metric. Paper carries no page size, so the only honest signal is where the
reader is.

## Column classes

`previewColumnClass(width)` (`previewWidth.ts`):

- Letter → `mx-auto w-full max-w-[816px] px-[max(0.75rem,min(2rem,4%))] py-3` (8.5 in at 96 dpi)
- A4 → `mx-auto w-full max-w-[794px] px-[max(0.75rem,min(2rem,4%))] py-3` (210 mm)
- Full → `mx-auto w-full max-w-none px-3 py-3`

The padding is what delivers the margin the user actually asked for: when the
pane is narrower than the sheet (the ~400 px sidebar preview), `mx-auto` has
nothing to centre and the padding becomes the margin. It is **fluid** (4% of the
scroller width, clamped to 12–32 px) rather than a fixed `px-8`, because the
Files-tab SPLIT view renders the same column in a pane the user can drag down to
~20% of the tab; 64 px of fixed padding there left almost no measure. At the
~400 px sidebar this yields ~16 px per side; a full-width pane gets 32 px.

## Decision: presets are width-only

F4/folio is 210 mm wide — identical to A4 — and US folio shares Letter's
width. With width-only presets, a separate Folio entry would render
pixel-identically to A4, so F4 is folded into the A4 entry instead. The menu
label reads `A4 / F4 (210 mm)` (`previewWidth.ts:149`). A page-height /
page-break rendering was considered and explicitly declined by the user; it is
the only way to tell F4 apart from A4, and it was not built.

## Invariants

1. **`.prose` keeps `max-w-none`; the measure lives on a child wrapper.**
   `@tailwindcss/typography` sets `.prose { max-width: 65ch }`. The scroller
   must stay neutralised (`MarkdownViewer.tsx:226`:
   `prose prose-sm max-w-none …`), and the 816/794 px measure is carried by
   `previewColumnClass()` on the inner wrapper (`MarkdownViewer.tsx:227`).
   Don't merge the two: a 65ch cap on the scroller wins over any child width.
   The original `max-w-none` existed to fix a 65ch right-gutter bug and that
   guard stays.

2. **Control appears only for markdown.** `PreviewWidthMenu` is rendered only
   when `doc.kind === "markdown"` (`PreviewHost.tsx:288`) and
   `selected?.kind === "markdown"` (`PreviewTabPage.tsx:85`). Every other kind
   either ignores it (PDF/deck/image) or is code, which stays full width.

3. **One global preference, cross-window.** Storage key
   `ocode.ui.previewWidth.v1` (`PREVIEW_WIDTH_STORAGE_KEY`,
   `previewWidth.ts:28`). A single global localStorage key — a reading
   preference, not a property of a project or file — so the sidebar, the
   Preview tab and the Files-tab preview always agree. Cross-window sync via
   the `storage` event (`previewWidth.ts:183-191`): another window writing the
   preference moves this one too.

4. **Corrupt values fall back to `auto`.** `normalize` (`previewWidth.ts:46`)
   returns `null` for anything outside the four presets, and
   `getPreviewWidthPreset` (`previewWidth.ts:156`) does `readStored() ?? "auto"`.
   The cached snapshot (`current`, `previewWidth.ts:44`) stays referentially
   stable across renders via `useSyncExternalStore` (`previewWidth.ts:194`), which
   is the `editorAppearance.ts` pattern.

## Measured geometry (headless Chromium, built CSS)

- Pane 1000 px → Letter column 816 px, 124 px text margins each side.
- Pane 400 px → column fills, text `[32 … 368]`.
- Full → keeps 12 px (`px-3`).

## Tests

- `web/src/lib/previewWidth.test.ts` (14 cases)
- `web/src/components/Preview/PreviewWidthMenu.test.tsx` (4)
- `web/src/components/Preview/MarkdownViewer.width.test.tsx` (6: the column
  classes plus the two live-update cases that pin `subscribe` and the `storage`
  `emit()`)
- `web/src/components/Preview/PreviewHost.test.tsx` — "offers the width control
  for markdown and hides it for other kinds"

Mutants caught: the Auto row reusing the selected preset's column, a fixed
`px-8` instead of the fluid padding, `subscribe` becoming a no-op (the mounted
viewer stops updating), the `storage` handler dropping `emit()`, the control
shown for every kind, and the column wrapper dropped.

## Control

`PreviewWidthMenu` (`PreviewWidthMenu.tsx`) — one compact button showing the
current column that opens a popover, writing the global preference. Menu order:
`auto, letter, a4, full` (`PreviewWidthMenu.tsx:14`). The options are plain
toggle buttons (`aria-pressed`), NOT an ARIA menu: Radix Popover has no roving
focus or arrow-key navigation, so menu semantics would be a promise it does not
keep. Labels come from `previewWidthMenuLabel` / `previewWidthShortLabel`
(`previewWidth.ts:138-154`); the Auto label resolves Auto's own column.