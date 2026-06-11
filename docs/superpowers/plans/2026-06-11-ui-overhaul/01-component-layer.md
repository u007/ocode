# Part 1 — TUI Component Layer (Tasks 1–6)

Self-contained part. Spec: `docs/superpowers/specs/2026-06-11-ui-overhaul-design.md`. All work in package `tui` (`/Users/james/www/ocode/internal/tui/`). High-level plan: files and behaviors only, no code. Every task is TDD: write failing test, see it fail, implement minimally, see it pass, commit. Run `go test ./internal/tui/...` before each commit. Nothing in this part changes visible behavior — components are built and tested standalone; wiring happens in Part 2.

### Task 1: Unified Scrollbar component

**Files:**
- Modify: `internal/tui/scrollbar.go` (currently two near-duplicate funcs: `renderScrollbar`, `renderListScrollbar`)
- Test: `internal/tui/scrollbar_test.go`

- [ ] **Step 1:** Write failing tests for a single `Scrollbar` type covering: thumb size/position math for (content ≤ viewport → hidden), top, middle, bottom positions; rendering height equals viewport height; drag hit-test mapping a clicked row back to a scroll offset.
- [ ] **Step 2:** Run `go test ./internal/tui/ -run TestScrollbar -v` — expect FAIL (type undefined).
- [ ] **Step 3:** Implement `Scrollbar` in `scrollbar.go` consolidating both existing functions; keep the two old functions as thin wrappers over it for now (call sites migrate in Part 2).
- [ ] **Step 4:** Run the test — expect PASS. Run full `go test ./internal/tui/...` — expect PASS (wrappers preserve behavior).
- [ ] **Step 5:** Commit: `refactor(tui): unify scrollbar rendering into Scrollbar component`.

### Task 2: Button component

**Files:**
- Create: `internal/tui/component_button.go`
- Test: `internal/tui/component_button_test.go`

- [ ] **Step 1:** Write failing tests for a `Button` type: label rendering with normal/primary/danger variants; hovered and focused visual states differ from idle (compare rendered output inequality, not exact ANSI); a hit-test rect (x, y, width, height) that `Contains(x,y)` answers correctly; styles come from the theme `Styles`/`ThemeColors` system in `theme.go`, not hardcoded colors.
- [ ] **Step 2:** Run `go test ./internal/tui/ -run TestButton -v` — expect FAIL.
- [ ] **Step 3:** Implement `Button` with render + bounds methods. Reuse the visual language of the existing permission buttons (`permBtnStyle`/`permBtnHoverStyle`, model.go ~line 8621) so Part 2 migration is a no-op visually.
- [ ] **Step 4:** Run tests — expect PASS.
- [ ] **Step 5:** Commit: `feat(tui): add Button component with variants, hover/focus states, hit-testing`.

### Task 3: ANSI-aware overlay compositor with dimmed backdrop

**Files:**
- Create: `internal/tui/component_overlay.go`
- Test: `internal/tui/component_overlay_test.go`

This is the riskiest unit — write the tests first and exhaustively.

- [ ] **Step 1:** Write failing tests for a `compositeOverlay(backdrop string, box string, x, y int) string` function and a `dimLines(lines []string) []string` helper:
  - box splices over plain backdrop at given coordinates; lines above/below/left/right preserved
  - backdrop lines containing ANSI sequences are split correctly (styled runs on the left of the box keep their styles; styles re-open after the box on the right)
  - double-width (CJK) characters at the splice boundary: no half-character artifacts, output visual width per line equals terminal width
  - box taller/wider than backdrop is clamped, never panics
  - dimming converts styled lines to a faint/dim rendition without destroying line count or width
- [ ] **Step 2:** Run `go test ./internal/tui/ -run TestOverlay -v` — expect FAIL.
- [ ] **Step 3:** Implement using the existing ANSI-handling helpers in the package (`stripANSI` and the width helpers already used by `applySelectionHighlight` — find them in `selection.go`/`pathlink.go` and reuse; do not write a second ANSI parser if one exists).
- [ ] **Step 4:** Add a caching wrapper: a `dimCache` keyed by a content-version counter so the dimmed backdrop is recomputed only when underlying content or scroll position changes (spec risk: transcript render perf must not regress). Test: same input twice → second call returns cached slice (assert via counter, not timing).
- [ ] **Step 5:** Run tests — expect PASS.
- [ ] **Step 6:** Commit: `feat(tui): add ANSI-aware overlay compositor with cached backdrop dimming`.

### Task 4: Dialog component

**Files:**
- Create: `internal/tui/component_dialog.go`
- Test: `internal/tui/component_dialog_test.go`

- [ ] **Step 1:** Write failing tests for a `Dialog` type: renders bordered box with title, body, and a button row of `Button`s (Task 2); body uses a `bubbles` viewport when content exceeds max height and exposes scroll up/down; width/height clamp to given terminal size; reports overall bounds and per-button bounds for mouse routing; scroll indicator arrows shown only when body overflows (match existing permission dialog behavior, model.go `renderPermissionDialog` ~line 8762).
- [ ] **Step 2:** Run `go test ./internal/tui/ -run TestDialog -v` — expect FAIL.
- [ ] **Step 3:** Implement `Dialog` composing `Button` and the package border style; no compositing here (the caller centers it via Task 3's compositor).
- [ ] **Step 4:** Run tests — expect PASS.
- [ ] **Step 5:** Commit: `feat(tui): add Dialog component (title, scrollable body, button row, bounds)`.

### Task 5: ListBox component

**Files:**
- Create: `internal/tui/component_listbox.go`
- Test: `internal/tui/component_listbox_test.go`

- [ ] **Step 1:** Write failing tests for a `ListBox` type: renders a window of items with selected row, hovered row (distinct from selected), and integrated `Scrollbar` (Task 1); filter string narrows items; selection clamps and follows filter changes; mouse hit-test maps a screen row to an item index (accounting for header offset); wheel up/down moves the window; empty-result state renders a hint line.
- [ ] **Step 2:** Run `go test ./internal/tui/ -run TestListBox -v` — expect FAIL.
- [ ] **Step 3:** Implement `ListBox`. Model the API on what `picker.go` (`renderPicker`, ~line 636) and `slash_popup.go` both need today, since both migrate onto it in Part 2 — check both call sites before finalizing fields.
- [ ] **Step 4:** Run tests — expect PASS.
- [ ] **Step 5:** Commit: `feat(tui): add ListBox component (filter, selection, hover, scrollbar, hit-test)`.

### Task 6: ModalStack

**Files:**
- Create: `internal/tui/modal_stack.go`
- Test: `internal/tui/modal_stack_test.go`

- [ ] **Step 1:** Write failing tests for a `Modal` interface (handle message → consumed bool, render → string, bounds → rect) and a `ModalStack`: push/pop/top; keyboard messages go to top modal only; a mouse message inside top modal's bounds is consumed; a mouse message outside bounds is NOT consumed (returned to caller for pane fall-through); stack empty → nothing consumed; closing the top modal restores the one beneath.
- [ ] **Step 2:** Run `go test ./internal/tui/ -run TestModalStack -v` — expect FAIL.
- [ ] **Step 3:** Implement `ModalStack`. Scope note from spec: detail drill-in views (`detail_view.go`) are full-screen view swaps and never enter this stack.
- [ ] **Step 4:** Run tests — expect PASS. Run full `go test ./internal/tui/...` — expect PASS.
- [ ] **Step 5:** Commit: `feat(tui): add ModalStack with key capture and mouse fall-through`.
