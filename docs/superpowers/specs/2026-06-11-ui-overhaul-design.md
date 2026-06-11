# UI Overhaul Design — TUI Component Layer, UX Polish, Web Alignment

Date: 2026-06-11
Status: Approved (all sections) — pending spec review

## Goal

Make the ocode TUI and web UI more intuitive, better looking, and consistent; make the TUI feel more responsive (hover feedback); render dialogs as true overlays; allow scrolling other panes while a permission request is showing; extract reusable TUI components so future surfaces stop hand-rolling rendering.

## Current State (findings)

- TUI: ~37k lines, no shared component layer. Every surface hand-rolls borders, buttons, lists, scrollbars, selection, and hover.
- Seven separate modal systems (`showPicker`, `showConnect`, `showPalette`, `showPermDialog`, `showRetryDialog`, `showQuestionDialog`, detail stack), each a boolean plus if/else branches in `View()` and input routing.
- All panes are frozen while any modal is open (`shouldForwardToTranscriptViewport` and mouse-wheel guards reject input when `modalOpen()`).
- Permission dialog is an inline box in the bottom chrome, not an overlay.
- Hover exists only on permission buttons, sidebar files, and path links — each hand-rolled.
- Two near-duplicate scrollbar renderers (`renderScrollbar`, `renderListScrollbar`).
- Web UI: React 18 + Vite + Tailwind + Radix/shadcn (~2.7k lines). Git and Logs panels are stubs. Fixed dark theme, no link to TUI themes.

## Phase 1 — TUI Component Layer (foundation)

New files in the existing `tui` package (separate package would create an import cycle with `theme.go`; reusability comes from clean interfaces, not package boundaries):

- `component_scrollbar.go` — single Scrollbar replacing `renderScrollbar` and `renderListScrollbar`; one set of glyphs and theme colors; supports drag hit-testing.
- `component_button.go` — Button with label, variant (normal / primary / danger), hovered and focused states, render plus hit-test rect. Replaces hand-rolled permission and confirm buttons.
- `component_listbox.go` — filterable, scrollable list with selection, hover row, integrated scrollbar, and mouse hit-testing. Backs the picker, slash popup, connect provider list, and session-delete confirm.
- `component_dialog.go` — Dialog with title, viewport-scrollable body, button row, and width/height clamps, rendering to a bordered box. Includes an ANSI-aware overlay compositor that splices a rendered box over a dimmed backdrop at a given position.
- `modal_stack.go` — ModalStack replacing the per-modal booleans. Each modal implements a small interface: handle a message (reporting whether it consumed it), render to string, report its screen bounds. The top modal gets keyboard first; mouse events outside its bounds fall through to the pane underneath. This is the mechanism enabling scroll-through during permission requests.

Migration is incremental: one surface per commit moves onto the components; unmigrated surfaces keep their existing rendering until their turn. Suggested order: permission dialog → picker → slash popup → question prompt → connect → theme picker → retry dialog.

## Phase 2 — TUI UX Polish

- **True overlays.** Permission dialog, pickers, connect, slash popup, question prompt, and theme picker render as centered boxes composited over a dimmed live backdrop. Chat remains visible behind the dialog.
- **Scroll-through during permission requests.** Mouse wheel scrolls whichever pane is under the cursor (transcript, sidebar, files/git tabs). Scroll keys (PgUp/PgDn, ctrl+u/ctrl+d) scroll the transcript. The dialog keeps only its decision keys (y/n/a/esc, arrow keys for button focus) and scrolls its own body when the cursor is over it.
- **Hover everywhere.** Consistent hover treatment (theme-driven background tint or underline) on picker rows, slash-popup rows, sidebar files, tabs, buttons, and path links. Implemented via cached per-render hit-test maps so AllMotion handling stays cheap (per CLAUDE.md TUI mouse rules).
- **Consistency pass.** One border style, uniform padding, uniform hint-bar format and key labels across all modals, one scrollbar appearance.

## Phase 3 — Web UI

- Finish the **Git panel**: real diff rendering using the existing REST API.
- Finish the **Logs panel**: streaming log view using the existing REST API.
- **Consistency pass**: permission dialog and all pickers built on the existing shadcn/Radix dialog and command primitives (no hand-rolled modals), uniform hover/focus states, spacing and typography audit.
- **Theme sync**: expose the active TUI ThemeColors via an API endpoint and map them onto the existing Tailwind CSS variables so the web UI matches the terminal theme.

## Testing

- Unit tests per component: render snapshots and hit-test math, including ANSI/double-width character cases for the overlay compositor.
- Existing property tests in `model_test.go` must pass before and after each surface migration.
- Mouse routing (fall-through, hover, drag) covered by extending the existing mouse property tests.

## Risks

- ANSI-aware overlay splicing: width math with wide characters and styled runs — mitigated with dedicated compositor tests.
- Mouse fall-through routing regressions — mitigated by property tests and one-surface-per-commit migration.
- `model.go` is 13k lines — no big-bang edits; every migration is a small, reviewable diff.

## Out of Scope

- Light/dark toggle for the web UI.
- Rewriting the transcript fastviewport.
- Any change to the agent/permission backend logic — UI only.

## Execution Order

Phase 1 → Phase 2 → Phase 3. Each phase lands as a series of small commits; the TUI remains shippable after every commit.
