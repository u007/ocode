---
type: Concept
title: 'Preview find: shared FindBar, DOM and PDF engines, iframe agent'
description: 'How in-document find works across the PDF, Markdown, Word and HTML previews: one shared FindBar, a DOM engine (findDom + useDomFind + FindHighlights) for rendered text, the PDF-specific usePdfFind engine, and a self-contained postMessage agent for the sandboxed HTML iframe. Covers highlight overlay geometry, shortcut ownership and coexistence with the chat find bar, how to add find to a new preview kind, and the caps/limits.'
resource: web/src/components/Preview/FindBar.tsx; web/src/components/Preview/findDom.ts; web/src/components/Preview/useDomFind.ts; web/src/components/Preview/FindHighlights.tsx; web/src/components/Preview/usePdfFind.ts; web/src/components/Preview/htmlFindAgent.ts; web/src/components/Preview/useIframeFind.ts
tags:
  - web
  - preview
  - find
  - search
  - pdf
  - markdown
  - docx
  - html
  - iframe
  - postmessage
  - keyboard
  - shortcuts
timestamp: 2026-11-03T05:52:42Z
---
# Preview find: shared FindBar, DOM and PDF engines, iframe agent

In-document find (Ctrl/⌘F inside a preview) is one UI over three rendering
realities. Every previewable text surface — **PDF, Markdown, Word (docx), HTML** —
renders the same `FindBar`, but the matching engine differs because the text
lives in different places: pdf.js text items, the live DOM, or an opaque-origin
iframe the parent cannot read.

Every document preview in the Files tab, sidebar `PreviewHost` and session
Preview tab can search its **rendered** text — not the raw source.

| Preview kind | Available | Engine |
|---|---|---|
| Markdown (`.md`, `.markdown`, `.mdx`) | yes | DOM text index (`useDomFind`) |
| Word (`.docx`) | yes | DOM text index (`useDomFind`) |
| HTML | yes | serialized in-frame agent (`useIframeFind`) |
| PDF | yes | pdf.js text items (`usePdfFind`) — see [pdf-viewer-zoom-find.md](pdf-viewer-zoom-find.md) |
| plain text / code / media / office-deck | no | no find bar is rendered |

Find is **not** part of `FileTabContent`'s toolbar (which holds Print and the
light/dark toggle, `FileTabContent.tsx:151`). Each viewer owns its own header row
with a visible **Find** button (`aria-label="Find in document"`), so availability
follows the viewer, not the routing layer. See
[../gotchas/files-tab-preview-only-routing.md](../gotchas/files-tab-preview-only-routing.md)
for which kind renders which viewer.

## Shared UI — `FindBar.tsx`

`FindBar` (`FindBar.tsx:9`) is the single search widget for all four viewers,
and its module comment spells out the contract: same markup and aria labels
everywhere so the toolbar, shortcuts and tests stay uniform
(`FindBar.tsx:4-8`).

- Root is `role="search"` (`FindBar.tsx:40`), so it is one landmark per viewer.
- Query input: `aria-label="Find text"` (`FindBar.tsx:57`), placeholder
  `Find in document`.
- Counter `"<current+1>/<count>"`, or `"…"` while a search is in flight, or
  `"0/0"` with no matches (`FindBar.tsx:61`).
- Aria labels (the stable test/documentation contract):
  - `"Find text"` — the input (`FindBar.tsx:57`)
  - `"Previous match"` (`FindBar.tsx:63`) / `"Next match"` (`FindBar.tsx:66`)
  - `"Match case"` (`FindBar.tsx:71`) / `"Whole word"` (`FindBar.tsx:81`)
    (both `aria-pressed` toggles)
  - `"Close find"` (`FindBar.tsx:89`)
- Keys owned by the input (`FindBar.tsx:46-55`): `Enter` → next, `Shift+Enter` →
  previous, `Escape` → close. `F3`/`Shift+F3` are handled by the viewer-level
  window listener.
- Walking wraps at both ends (`useDomFind.ts:63`; the frame agent mirrors it).

The bar is rendered only while `find.active`. **Closing keeps the query**, so
reopening re-searches immediately: `hide()` clears only active/rects/matches
(`useDomFind.ts:50-56`) and active/pageRects (`usePdfFind.ts:64-68`) while
leaving `query` intact; the search effect is keyed on `active`, so reopening
re-runs it. Nothing here is persisted — see Limits.

Each viewer also puts a slim header row above its scroller with a
`"Find in document"` button (`title="Find (Ctrl/⌘ F)"`), because the keyboard
shortcut is not discoverable on its own and may be claimed by another surface:
`MarkdownViewer.tsx:252`, `DocxViewer.tsx:80`, `HtmlViewer.tsx:57`,
`PdfViewer.tsx:1024`.

## Engine A — DOM text (Markdown and Word)

Three pieces, all in `web/src/components/Preview/`:

1. **`findDom.ts` — pure index/range math; never mutates the DOM.**
   - `buildTextIndex(root)` (`findDom.ts:70`) flattens the rendered tree into one
     string plus a `{node, start, len}` map back to every `Text` node
     (`findDom.ts:16-19`).
   - **Block boundaries emit `\n`** before and after the element
     (`findDom.ts:87`, `findDom.ts:89`), and `SCRIPT`/`STYLE`/`NOSCRIPT`/`TEMPLATE`
     are skipped (`SKIP_TAGS`, `findDom.ts:63`). A query therefore cannot match
     across a block boundary, while a match stays whole across inline elements
     (`<code>`, `<em>`).
   - `findMatches(index, query, opts)` (`findDom.ts:97`) escapes the query, uses
     `gi`/`g` per the case toggle, and wraps it as `\b(?:…)\b` for whole-word
     (`findDom.ts:100`). A regex that fails to compile yields no matches rather
     than throwing.
   - `rangeForMatch(index, match)` (`findDom.ts:144`) maps offsets back to a live
     `Range`; offsets landing on a block separator resolve to the nearest real
     node instead of failing.
2. **`useDomFind.ts` — state, rects, scrolling.** `useDomFind({containerRef,
   textRootRef, version, enabled})` (`useDomFind.ts:27`) owns `active`, `query`,
   `opts`, `matches`, `current`, `rects`. It rebuilds the index whenever the
   query, options or `version` change (`useDomFind.ts:72-85`) — old nodes are
   detached, and setting a `Range` on them would throw. `version`
   (`useDomFind.ts:33`) is the index key: any content change must bump it or the
   next search indexes stale (or empty) text. Rects are measured
   **content-relative** (`cr.left - box.left + scrollLeft`,
   `useDomFind.ts:107-117`), capped at `MAX_HIGHLIGHT_RECTS = 4000`
   (`useDomFind.ts:16`; matches past the cap are still counted and walkable).
   Measurement runs in a `requestAnimationFrame` (`useDomFind.ts:121`) and
   re-runs from a `ResizeObserver`; because the rects are content-relative,
   scrolling needs no handler. The current match scrolls to ~35 % viewport height
   via `container.scrollTo` (`useDomFind.ts:142-150`).
3. **`FindHighlights.tsx` — the overlay.** `FindHighlights`
   (`FindHighlights.tsx:11`) renders a `pointer-events-none` absolutely
   positioned layer with `aria-hidden="true"` (`FindHighlights.tsx:14`) as the
   **last child of the scroller**. Current match is `bg-orange-500/60`, all
   others `bg-yellow-300/45` (`FindHighlights.tsx:20`) — the same colours as the
   PDF overlay so find looks identical everywhere.

Because the layer is inside the scroller and the rects are content-relative, the
overlay scrolls with the document and needs **no scroll handler**. That is why
the scroller must be `relative` and also gets `tabIndex={0}` so a click focuses
it (focus feeds the shortcut gate): Markdown
`MarkdownViewer.tsx:277-278`, Word `DocxViewer.tsx:97`.

Both scrollers are `position: relative` + `tabIndex={0}`:

- Markdown: `MarkdownViewer.tsx:75` (`useDomFind` with `textRootRef =
  columnRef`, `version = md`), scroller `MarkdownViewer.tsx:274-278`, column
  wrapper `:280`, highlights `:283`.
- Word: `DocxViewer.tsx:26` (`version = docVersion`), scroller
  `DocxViewer.tsx:97`, highlights `DocxViewer.tsx:101`.

**Word gotcha:** `docx-preview` writes the body asynchronously. `docVersion`
(`DocxViewer.tsx:23`) is bumped only after `renderAsync` resolves
(`DocxViewer.tsx:44`), and `useDomFind` keys its index on it — without that bump
the first find indexes an empty body and reports 0 matches.

`useDomFind` is told to stay out of the way of non-document modes via
`enabled` — e.g. Markdown passes `!forceEdit && !isBinary`
(`MarkdownViewer.tsx:75`), so raw-source editing and binary files do not search
rendered prose.

## Engine B — PDF (`usePdfFind.ts`)

The PDF is not a DOM text surface: text exists only in pdf.js text items and the
text-layer spans built from them. `usePdfFind` is therefore **PDF-only** and
keeps its own index/rect machinery (pdf.js `getTextContent()` items → per-page
text index → `Range` → `getClientRects()` → page-relative overlay divs), search
over all pages, and `runRef` generation guards (`usePdfFind.ts:29`,
`usePdfFind.ts:48`). It also integrates with the viewer's page rendering
(`renderTick`) and falls back to `onPageChange` when the current match is on an
unrendered page.

`PdfViewer` renders the shared `FindBar` (`PdfViewer.tsx:1027`) and imports
`isEditableTarget` from `findDom.ts` (`PdfViewer.tsx:9`). Full detail of the PDF
engine, zoom and print/download lives in
[pdf-viewer-zoom-find.md](pdf-viewer-zoom-find.md).

## Engine C — HTML iframe (postMessage agent)

The HTML preview renders in `sandbox="allow-scripts"`
(`HtmlViewer.tsx:78`) **without** `allow-same-origin`, so it is an opaque origin
and the parent cannot read its DOM (`useIframeFind.ts:11-16`). Find therefore
travels inside the document:

- `htmlFindAgent()` (`htmlFindAgent.ts:29`) installs the agent;
  `buildHtmlFindSrcDoc` (`htmlFindAgent.ts:319`) serializes it with
  `htmlFindAgent.toString()` and injects it before `</body>` into the `srcDoc`
  (`HtmlViewer.tsx:29`).
- **The agent must be fully self-contained.** `htmlFindAgent.toString()` is the
  entire program that runs in the frame: any module-scope identifier (a shared
  constant, an imported helper) survives typechecking and becomes an undefined
  identifier in the frame. Keep every constant and helper **inside** the
  function — this exact bug existed during implementation with the rect cap.
  `MAX_RECTS = 3000` (`htmlFindAgent.ts:40`) is the canonical in-function
  constant.
- Protocol constants are module exports for the parent and duplicated literals
  inside the agent: `__ocodeFind` (request), `__ocodeFindResult`
  (`{count, current}`), `__ocodeFindReady` (`htmlFindAgent.ts:21-23`, documented
  `:11-15`). Actions are `search` (query + both toggles), `step` (dir ±1) and
  `close` (`useIframeFind.ts:47-54`).
- `useIframeFind` (`useIframeFind.ts:18`) owns the bar state. One effect re-sends
  the search whenever the bar opens, the query/options change, or the frame
  regenerates (`useIframeFind.ts:62-73`), because every debounced `srcDoc` edit
  reloads it; `onFrameLoad` bumps the generation (`useIframeFind.ts:56`). Each
  post arms a 1200 ms reply timer (`useIframeFind.ts:8`); a frame that never
  answers leaves the counter on `…` and then settles at 0.
- Inside the frame the overlay is a document-coordinate layer with id
  `__ocode_find_layer` appended to `document.documentElement`
  (`htmlFindAgent.ts:161-169`), offset by the current scroll
  (`htmlFindAgent.ts:166`), coloured `rgba(249,115,22,0.60)` for the current
  match and `rgba(250,204,21,0.45)` otherwise (`htmlFindAgent.ts:183`), capped at
  `MAX_RECTS = 3000`. It is repainted rAF-throttled on `scroll` (capture phase,
  so inner scrollers count) and `resize` (`htmlFindAgent.ts:206-227`), and steps
  call `scrollIntoView({block:"center"})` (`htmlFindAgent.ts:193-204`).

The parent never sees match offsets in the frame; it only receives count/current.
`htmlFindAgent.test.ts` executes the **serialized** source, not the imported
function, so a scope leak fails the test. A built-chunk eval was used as the
production check.

## Highlight overlay contract

All four surfaces use the same colour language: **yellow = every match, orange =
the current match**. PDF paints page-relative divs (`PdfViewer.tsx:1134`);
Markdown/Word paint content-relative divs inside the scroller; HTML paints
document-coordinate divs inside the frame. Only one engine ever owns a surface,
so the layers never stack.

## Shortcut ownership and coexistence with the chat find bar

Every viewer binds a **window-level** `keydown` — the documented exception to the
"no raw listeners in child components" rule in
[web-keyboard-shortcuts.md](web-keyboard-shortcuts.md):

| Key | Effect |
|---|---|
| `Ctrl/⌘F` | open this preview's find bar — only when the reader is in this preview and the target is not editable |
| `F3` / `Shift+F3` | next / previous, only while the bar is active |
| `Escape` | close, only while the bar is active and the target is not editable |

`ChatPanel` also owns a window-level Ctrl/⌘F. Without a gate, one keypress would
open **both** bars — and hidden keep-alive panes would all react at once. Two
gate shapes prevent that:

- **PDF: only the visible viewer attaches the listener.** The effect returns
  early unless the `active` prop (`PdfViewer.tsx:169-172`) says this viewer is
  the visible one (`PdfViewer.tsx:818`), so N mounted-but-hidden PDF panes cannot
  race on F3. PDF is the documented exception: it does **not** use
  `previewWantsFindShortcut` because a hidden pane is already known from the
  host. It still guards editable targets.
- **Markdown / Word / HTML: `previewWantsFindShortcut(root)`**
  (`findDom.ts:175`), true only when:
  1. `root.offsetParent !== null` — hidden keep-alive panes (every visited
     tab/project stays mounted) must never react (`findDom.ts:178`);
  2. **and** focus is inside `root`, **or** the current selection is anchored
     inside it, **or** `root.matches(":hover")` (the `:hover` probe is wrapped in
     try/catch — hover is a bonus, not a gate).

  Call sites: `MarkdownViewer.tsx:213-229` (gate `:217`), `DocxViewer.tsx:59-75`
  (gate `:63`), `HtmlViewer.tsx:36-52` (gate `:40`).

`isEditableTarget` (`findDom.ts:158`) keeps native browser find inside
`input`/`textarea`/`select`/contenteditable. The visible per-viewer **Find**
button is the discoverable, keyboard-free path.

## Adding find to a new text preview kind

Pick the engine by where the text lives:

1. Reuse `FindBar` — never write a second bar; the labels and tests assume this
   markup.
2. **Rendered DOM text** → wire the DOM engine. Keep a `relative` scroller with
   `tabIndex={0}` and a stable text-root ref, then:
   1. `const find = useDomFind({ containerRef, textRootRef, version, enabled })`
   2. render the header `"Find in document"` button calling `find.show`
   3. render `{find.active && <FindBar … />}` and, last inside the scroller,
      `<FindHighlights rects={find.rects} current={find.current} />`
   4. bump `version` whenever the rendered DOM is replaced (the
      `DocxViewer.docVersion` pattern)
   5. add the window keydown gated exactly like the three existing ones
      (`previewWantsFindShortcut` + `isEditableTarget`). Do not add an ungated
      Ctrl+F.
3. **Opaque iframe / non-DOM surface** → follow the agent pattern: a
   self-contained `toString()`-serialised function plus a parent hook that owns
   the bar and speaks postMessage. Do not try to reach into the frame.
4. **A new text format that pdf.js-like libraries own** → write a dedicated hook
   (the `usePdfFind` shape) and reuse `FindBar` + the overlay colours.

## Limits and boundaries

- **Highlight caps:** 4000 rects for DOM previews (`MAX_HIGHLIGHT_RECTS`,
  `useDomFind.ts:16`), 3000 for the iframe agent (`MAX_RECTS`,
  `htmlFindAgent.ts:40`). Matches past a cap are still counted and steppable;
  only painting stops, so `FindBar`'s count can exceed painted highlights.
- **State is per-mount, not persisted.** `previewViewState.ts` stores only
  `page` / `zoom` / `scrollTop` (`previewViewState.ts:16-24`); find state is
  deliberately absent. A remount (app reload, sidebar change) reopens with an
  empty bar.
- **HTML with a page CSP that blocks inline scripts** never runs the agent: the
  frame reports 0 matches and the bar shows `"…"` until
  `IFRAME_FIND_REPLY_TIMEOUT_MS` (1200 ms) stops the spinner
  (`useIframeFind.ts:8`). The parent does not fall back to text-mode find.
- **MDX is rendered as Markdown** (`previewKind.ts:76` maps `.mdx` → markdown),
  so find matches the rendered prose, never unevaluated JSX. See
  [../gotchas/mdx-preview-not-evaluated.md](../gotchas/mdx-preview-not-evaluated.md).
- **Word body only:** the index covers the rendered document body (tables and
  repeated headers included) but no text `docx-preview` did not render.
- Rects are measured from live nodes, so a viewer that swaps content without
  bumping `version` highlights stale geometry instead of failing visibly.

## Tests

- `web/src/components/Preview/findDom.test.ts` — index flattening, block
  separators, case/whole-word, offset→range mapping.
- `web/src/components/Preview/htmlFindAgent.test.ts` — runs the **serialized**
  agent source and drives it over the postMessage protocol.
- `web/src/components/Preview/MarkdownViewer.find.test.tsx`
- `web/src/components/Preview/DocxViewer.find.test.tsx`
- `web/src/components/Preview/HtmlViewer.find.test.tsx`
- `web/src/components/Preview/PdfViewer.find.test.tsx`

Per-viewer suites cover bar/counter/walking/closing and highlight classes (match
by `[aria-hidden="true"]` + `className.includes("bg-yellow-300/45")` —
CSS-escaped class selectors are invalid in jsdom). Full web suite at the time of
writing: 491 files / 4834 tests green, and a live Chromium/Playwright pass
against a built binary confirmed pixel-accurate overlay geometry and scroll sync
for Markdown and Word, the toggles, and the sandboxed-iframe postMessage path for
HTML.

## Related

- [pdf-viewer-zoom-find.md](pdf-viewer-zoom-find.md) — the PDF engine
  (`usePdfFind`), zoom/Space and view-state persistence.
- [preview-width.md](preview-width.md) — the Markdown reading column and the
  header control that shares the tab with Find.
- [preview-print-save-as-pdf.md](preview-print-save-as-pdf.md) — printing builds
  a fresh document; none of the find overlay is printed.
- [web-keyboard-shortcuts.md](web-keyboard-shortcuts.md) — global bindings and the
  window-listener rule this feature is an exception to.
- [../gotchas/files-tab-preview-only-routing.md](../gotchas/files-tab-preview-only-routing.md)
  — which kind renders which viewer.
