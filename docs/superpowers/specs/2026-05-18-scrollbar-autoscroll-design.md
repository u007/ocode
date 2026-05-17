# Scrollbar + Auto-scroll on Session Restore

## Scope

Two features:
1. Auto-scroll to bottom when restoring a prior session
2. Interactive terminal scrollbar (track + thumb) on all viewport surfaces

Surfaces covered:
- Transcript (messages) — `model.go`, `m.viewport`
- Debug log — `model.go`, `m.logViewport`
- Git diff — `git_model.go`, `m.diff`
- File preview — `files_model.go`, `m.preview`

---

## Feature 1: Auto-scroll on Session Restore

### Problem

In `New()`, session messages are appended to `m.messages` but `renderTranscript()` is never called and `GotoBottom()` is never invoked. On startup the viewport contains only the hint text and shows the top of the transcript.

### Fix

Two-part fix:

1. After the session restore loop in `New()`, call `m.renderTranscript()` so `SetContent` is called with the restored messages before the model is returned.

2. In the `WindowSizeMsg` handler (second switch, after `layout()` is called), add a `GotoBottom()` call for both `m.viewport` and `m.logViewport` when `len(m.messages) > 0`. This handles the race where terminal dimensions are unknown at init time — the viewport is properly sized only after the first `WindowSizeMsg`.

No new state needed.

---

## Feature 2: Scrollbar on All Viewport Surfaces

### Scrollbar rendering — shared helper

Add a pure function `renderScrollbar(height int, scrollPct float64, totalLines int, visibleLines int) string` in `model.go` (accessible to sub-models via package scope).

- Returns a single-column string of `height` lines
- When `totalLines <= visibleLines`: returns an empty string (scrollbar hidden)
- Otherwise: computes thumb size = `max(1, visibleLines * height / totalLines)` and thumb position from `scrollPct`
- Track character: `│` rendered in dim/hint style
- Thumb character: `█` rendered in accent color (blue `#7AA2F7`)

### Viewport width adjustment

Each panel that shows a scrollbar must subtract 1 from the viewport's content width. The scrollbar column sits *inside* the border (between content and right border wall). The border width is unchanged.

Each render call:
1. Render `vp.View()` at `width - 1`
2. Compute scrollbar column via helper
3. `lipgloss.JoinHorizontal(lipgloss.Top, content, scrollbar)`
4. Pass the joined string into `borderStyle.Width(width).Render(...)`

Width accounting:
- Transcript / log: in `layout()`, subtract 1 from `m.viewport.Width` and `m.logViewport.Width` when the scrollbar is visible (i.e. `TotalLineCount > VisibleLineCount`). Since visibility can change, always reserve the column (subtract 1 unconditionally when the panel is active).
- Git diff / file preview: subtract 1 in each panel's render method.

Simpler rule: **always subtract 1** — the scrollbar column is always present; when there's nothing to scroll, it renders as all-track characters (invisible-feeling, no thumb). This avoids layout reflow when scroll state changes.

### Mouse interaction — drag state

Add to `model` struct:
```
scrollbarDrag        scrollbarDragState  // which viewport is being dragged
scrollbarDragStartY  int                 // Y at drag start
```

```go
type scrollbarDragState int
const (
    scrollbarDragNone scrollbarDragState = iota
    scrollbarDragTranscript
    scrollbarDragLog
)
```

For `gitModel` and `filesModel`, add equivalent `scrollbarDrag bool` + `scrollbarDragStartY int` fields to those structs.

### Scrollbar hit detection

For each viewport, compute the scrollbar column's X position and the viewport's Y range. A click/motion is "on the scrollbar" when:
- `mouse.X == scrollbarX` (rightmost column inside the border)
- `mouse.Y` is within `[viewportTop, viewportTop + viewportHeight)`

Helper: `scrollbarXForPanel(panelX, panelWidth int) int` returns `panelX + panelWidth - 2` (inside right border wall).

### Click/drag → SetYOffset

When a click or motion lands on the scrollbar:
```
targetPct = float64(mouse.Y - viewportTop) / float64(viewportHeight)
targetOffset = int(targetPct * float64(totalLines - visibleLines))
vp.SetYOffset(targetOffset)
```

### Integration points

**`model.go` — `handleMouseAction`:**
- Before existing checks, test if click is on transcript scrollbar (tab == chat) or log scrollbar (tab == log)
- On press: set `scrollbarDrag` state and call `SetYOffset`
- Return `true` to consume event

**`model.go` — `handleMouseMotion`:**
- If `scrollbarDrag != scrollbarDragNone`, call `SetYOffset` for the active drag surface
- Return `true`

**`model.go` — `handleMouseAction` (release):**
- Clear `scrollbarDrag` state

**`git_model.go` — `Update`:**
- Add `tea.MouseClickMsg`, `tea.MouseReleaseMsg`, `tea.MouseMotionMsg` cases
- Same hit-detect-then-SetYOffset pattern for `m.diff`

**`files_model.go` — `Update`:**
- Add same mouse cases for `m.preview`

Mouse events flow into git/files models because `model.go` already forwards `msg` to `m.git.Update(msg, w, h)` and `m.files.Update(msg, w, h)` when those tabs are active.

### View changes

**`model.go` `View()`:**
- Transcript render: join `m.viewport.View()` + scrollbar column, then border-wrap
- Log render (`renderLogPane`): same pattern for `m.logViewport`

**`git_model.go` `View()`:**
- Diff pane: join `m.diff.View()` + scrollbar, then border-wrap

**`files_model.go` `View()`:**
- Preview pane: join `m.preview.View()` + scrollbar, then border-wrap

---

## Out of Scope

- Git sections list and git files list (plain `[]string`, not viewports) — no scrollbar
- Slash popup, picker, connect dialog — short lists, no scrollbar needed
