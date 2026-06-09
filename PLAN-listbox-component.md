# PLAN: Reusable ListBox component for TUI list surfaces

## Motivation

Every scrollable list surface in the TUI re-implements the same three concerns by
hand: render header/footer chrome, render a scrollable item list, and map a mouse Y
back to an item index. Because render and hit-test are computed independently, the
offset math drifts whenever chrome height changes (the files-tab narrow-screen
hit-box bug, fixed by `treeHeaderRows` as the single source of truth). The same
drift risk lives in:

- `model.go` sidebar: `sidebarFileForClick`, `sidebarAllowedHeaderForClick`,
  `sidebarAdvisorToggleForClick`, `sidebarIDEToggleForClick` — each hardcodes Y
  offsets re-derived from the render layout.
- `model.go` content-search results: `previewBodyTop` + a literal `headerLines := 5`.
- `files_model.go` tree: now correct via `treeHeaderRows`, but still bespoke.
- git diff, log tab, transcript: `selectionState` + `contentTopY` repeated per surface.

ListBox extracts the render+geometry contract once so adopters can never desync,
and so a new surface gets correct mouse mapping for free.

## Goals

- One type owns: optional top rows (toolbar/hints), a scrollable item region,
  optional bottom rows (footer), cursor, and vertical scroll offset.
- `View()` and the hit-test derive the item region from the **same** computed
  geometry — desync is structurally impossible.
- Support the in-app selection recipe (press/motion/release → highlight → copy) that
  CLAUDE.md mandates, since mouse capture is global.
- Drop-in for at least the files tree and sidebar without behavior change.

## Non-goals

- Not a data-owning widget. ListBox renders **already-rendered** rows (strings);
  callers keep owning their data, styling, and item model.
- Not replacing `fastviewport` (transcript fast path) or the bubbles `viewport`
  (soft-wrap preview/diff). ListBox is for hard-wrapped, no-soft-wrap row lists with
  chrome — the case the `*ForClick` helpers cover.
- No horizontal scroll in v1 (files tree keeps its own `treeScrollX` for now).

## Component contract (described, not coded)

Location: new package `internal/tui/listbox` (sibling to `fastviewport`).

State the component holds:
- Geometry: width, height.
- Content regions: top rows, item rows, bottom rows — all pre-rendered `[]string`,
  each guaranteed one visual line (caller clamps with `MaxHeight(1)`, mirroring
  `treeHeaderRows`).
- Navigation: cursor index, vertical scroll offset.

Behavior the component exposes:
- A render method that joins top rows + the visible item window + bottom rows,
  padded to width×height (same lipgloss padding as `fastviewport.View`).
- A hit-test that, given a mouse X/Y and the surface's screen origin, returns the
  item index under the cursor (or not-found). The offset it subtracts is
  `len(topRows)` plus its own border/padding accounting — the identical numbers the
  render method used. This is the invariant that kills the drift class.
- Scroll/cursor helpers paralleling `fastviewport` (ScrollUp/Down, GotoTop/Bottom,
  visible window) so the item region scrolls independently of fixed chrome.
- A selection hook: expose the visible item rows + their `contentTopY` so the
  existing `selectionState` / `applySelectionHighlight` / `extractSelectionText`
  flow can drive in-app text selection against the same coordinate space.

## Adoption order

1. **Files tree** — already has the single-source pattern; reframe `treeHeaderRows`
   as ListBox top rows. Lowest risk, proves the API. (treeScrollX stays external.)
2. **Sidebar** — highest payoff: collapses four `*ForClick` helpers + their
   hardcoded offsets into header rows / item rows / footer rows with one hit-test.
   Toggle rows (allowed-header, advisor, IDE) become identifiable bottom/top rows.
3. **Content-search results** — replaces the `headerLines := 5` literal.

Stop after each adopter and verify before moving on. Do not migrate transcript,
git diff, or log tab in v1 — they use soft-wrap viewports; revisit only if the
contract proves a clean fit.

## Tasks

1. Create `internal/tui/listbox` package with the geometry + render + hit-test
   contract above. → verify: package builds; unit test asserts render line count ==
   hit-test offset for 0/1/2 top rows and with bottom rows, at wide and narrow width.
2. Add selection-surface accessors (visible rows + contentTopY). → verify: unit test
   that a known mouse Y maps to the expected item across scroll offsets.
3. Migrate files tree to ListBox top rows; delete `treeHeaderRows` duplication if
   subsumed. → verify: existing `files_click_offset_test.go` suite still passes,
   including the narrow-screen regression.
4. Migrate sidebar; replace the four `*ForClick` helpers with ListBox hit-tests for
   file rows and toggle rows. → verify: add a sidebar click test (file row + each
   toggle) at wide and narrow width; manual TUI smoke for clickable toggles.
5. Migrate content-search results; remove the `headerLines := 5` literal. → verify:
   click test mapping a result row to its index.
6. Sweep `model.go` for remaining hardcoded list Y offsets; document any surface
   deliberately left on the old path in TODO.md. → verify: grep shows no stray
   magic-number list offsets in migrated surfaces.

## Risks / watch-items

- **Selection coordinate space.** Bordered boxes have left chrome of 2 cols
  (border+padding) and `contentTopY` includes top chrome — the hit-test must use the
  same origin the selection highlighter uses, or selection and clicks disagree. Reuse
  the existing convention exactly.
- **Mouse capture is global.** ListBox must keep capture ON and do in-app selection;
  never disable MouseMode to regain native selection (CLAUDE.md rule).
- **One-line clamp is a truncation, not a wrap.** Long hints get clipped on narrow
  screens — accepted tradeoff for stable geometry; note it where rows are built.
- **Over-extraction.** If sidebar toggles don't fit "item rows" cleanly, keep them as
  named top/bottom rows rather than forcing a generic abstraction. Simplicity first.
