# Scrollbar + Auto-scroll on Session Restore

## Scope

Two features:
1. Auto-scroll to bottom when restoring a prior session
2. Interactive terminal scrollbar (track + thumb) on all scrollable surfaces

### All scrollable surfaces

**Viewport-based** (use `viewport.Model` math):
- Transcript (messages) — `model.go`, `m.viewport`
- Debug log — `model.go`, `m.logViewport`
- Git diff — `git_model.go`, `m.diff`
- File preview — `files_model.go`, `m.preview`

**List-based** (windowed plain `[]string`, not viewport.Model):
- Picker dialog — `picker.go`, `m.pickerItems`, `pickerVisibleRange()`, maxRows=15
- Slash popup — `slash_popup.go`, `m.slashPopupItems`, `slashPopupVisibleRange()`, maxRows=8
- Connect dialog — `connect.go`, `m.connect.providerIdx` / `m.connect.methodIdx` (small fixed lists, include for consistency)

---

## Feature 1: Auto-scroll on Session Restore

### Problem

In `New()`, session messages are appended to `m.messages` but `renderTranscript()` is never called and `GotoBottom()` is never invoked. The viewport also has no correct dimensions yet — those arrive with the first `WindowSizeMsg`. Calling `renderTranscript()` at `New()` time would wrap at the stub width of 80 cols and then not re-wrap on resize.

### Fix

One-shot flag approach:

1. After the session restore loop in `New()`, set `m.restoredPendingScroll = true` on the model (new `bool` field).
2. In the `WindowSizeMsg` handler, after `layout()` resizes the viewports, check `m.restoredPendingScroll`: if true, call `m.renderTranscript()` then `m.viewport.GotoBottom()` then set `m.restoredPendingScroll = false`.

This fires exactly once after the first resize event (correct dimensions), then never again — so subsequent terminal resizes do not snap the user back to the bottom while they are scrolled up.

---

## Feature 2: Scrollbar on All Scrollable Surfaces

### Scrollbar rendering — shared helpers

Add two pure functions in a new file `internal/tui/scrollbar.go`:

**`renderScrollbar(height int, totalLines int, visibleLines int, offsetLines int) string`**
- Returns a single-column string of `height` rune lines
- When `totalLines <= visibleLines`: returns a column of dim track chars (no thumb) — width is always present so layout never reflows
- Thumb size: `max(1, visibleLines * height / totalLines)` lines
- Thumb top: `int(float64(offsetLines) / float64(max(1, totalLines-visibleLines)) * float64(height-thumbSize))`
- Track character: `┊` (dim style) — visually distinct from border `│`
- Thumb character: `█` (accent color `#7AA2F7`)

**`renderListScrollbar(height int, totalItems int, visibleStart int, visibleCount int) string`**
- Same character/style rules
- For list-based surfaces where there's no `viewport.Model`
- Thumb position derived from `visibleStart / max(1, totalItems - visibleCount)`

### Width rule

**Always reserve the scrollbar column.** Every scrollable panel is allocated 1 column narrower than its border interior. When there is nothing to scroll, the scrollbar column renders as all-track characters. This prevents layout reflow when scroll state changes.

### Mouse interaction — viewport surfaces

**Scroll behavior: jump-to-position.** Clicking or dragging the scrollbar sets `YOffset` to the position proportional to where the mouse is within the track. The thumb does not preserve a grab offset — it follows the mouse Y directly. This is simpler and sufficient for a terminal UI.

**Drag state** — add to `model` struct:
```
scrollbarDrag       scrollbarDragTarget
```
```go
type scrollbarDragTarget int
const (
    scrollbarDragNone scrollbarDragTarget = iota
    scrollbarDragTranscript
    scrollbarDragLog
)
```

For `gitModel`: add `diffScrollbarDrag bool` field.
For `filesModel`: add `previewScrollbarDrag bool` field.

**Hit detection** — a mouse event hits a scrollbar when:
- `mouse.X == panelRight - 1` (last column inside border, before right border wall)
- `mouse.Y` is within `[viewportTop, viewportTop + viewportHeight)`

Sub-models (`gitModel`, `filesModel`) do not know their screen-absolute X position. The parent `model.go` must do hit detection for git diff and file preview scrollbars before forwarding mouse events, and then set a flag / call `SetYOffset` directly rather than relying on the sub-model to detect it. Alternatively, pass `panelOriginX` into the sub-model's `Update` — the cleaner approach is to handle scrollbar clicks in `model.go` for all surfaces.

**SetYOffset calculation:**
```
trackTop    = viewportTop   (first row of viewport inside border)
trackHeight = viewportHeight
clickRow    = mouse.Y - trackTop
targetPct   = float64(clickRow) / float64(trackHeight)
totalLines  = vp.TotalLineCount()
visible     = vp.VisibleLineCount()
offset      = int(targetPct * float64(max(0, totalLines - visible)))
vp.SetYOffset(offset)
```

**Integration in `model.go`:**

`handleMouseAction` (click/press):
- Before existing checks, test if click hits transcript scrollbar (tab == chat) or log scrollbar (tab == log) or git diff scrollbar (tab == git) or file preview scrollbar (tab == files)
- Set `scrollbarDrag` state, call `SetYOffset`, return `true`
- For git diff / file preview: call `SetYOffset` on `m.git.diff` / `m.files.preview` directly

`handleMouseAction` (release):
- Clear `scrollbarDrag`

`handleMouseMotion`:
- If `scrollbarDrag != scrollbarDragNone`, compute and apply `SetYOffset` for the dragged surface
- Return `true`

`gitModel.Update` and `filesModel.Update` do not need mouse cases for scrollbar — all handled in `model.go`.

### Mouse interaction — list-based surfaces

Picker and slash popup use index-based scrolling (no `viewport.Model`). Clicking the scrollbar column maps to an item index:

```
clickRow    = mouse.Y - listTop
targetPct   = float64(clickRow) / float64(listHeight)
targetIndex = int(targetPct * float64(totalItems))
m.pickerIndex = clamp(targetIndex, 0, len(items)-1)
```

Drag state for list surfaces: add `listScrollbarDrag bool` to `model` struct, set/clear in same handlers.

Connect dialog has very few items (≤5 providers, ≤3 methods) — include a visual scrollbar for consistency but no drag needed (it will always show no-thumb state in practice).

### Render changes

**Viewport surfaces** — each render site:
1. Compute scrollbar: `sb := renderScrollbar(vp.Height(), vp.TotalLineCount(), vp.VisibleLineCount(), vp.YOffset())`
2. Join: `joined := lipgloss.JoinHorizontal(lipgloss.Top, vp.View(), sb)`
3. Wrap: `borderStyle.Width(panelWidth - 2).Render(joined)`
4. Viewport content width = `panelWidth - 2 - 1` (subtract border + scrollbar col)

Affected sites:
- `model.go` `View()`: transcript panel (viewport width set in `layout()`)
- `model.go` `renderLogPane()` or equivalent log render site
- `git_model.go` diff pane render
- `files_model.go` preview pane render

**List surfaces** — each render site:
1. Render the list body as today
2. Compute scrollbar column: `sb := renderListScrollbar(bodyHeight, total, start, end-start)`
3. Join body + scrollbar, then border-wrap

Affected sites:
- `picker.go` `renderPicker()`
- `slash_popup.go` `renderSlashPopup()`
- `connect.go` `renderConnect()` (provider and method list stages)

---

## Out of Scope

- Git sections list and git files list — plain `[]string` rendered inline in `git_model.go` with no windowing; always short; no scrollbar
