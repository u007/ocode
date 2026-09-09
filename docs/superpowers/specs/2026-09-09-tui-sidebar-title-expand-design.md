---
type: Decision
title: TUI Sidebar Title Expand/Collapse Design
description: Design specification for expanding/collapsing the session title in the TUI sidebar
tags:
  - tui
  - sidebar
  - design
  - expand
  - collapse
timestamp: 2026-09-09T04:02:12Z
---
# TUI Sidebar Title Expand/Collapse Design

## Problem / Current Behavior

The TUI sidebar currently displays a fixed-width (38-column) sidebar with the session title always visible, wrapped to a maximum of 3 lines (`sidebarMaxTitleLines = 3`). The "✦ gen" button rides the last row of the title and is the only interactive element in the title row. The title cannot be expanded to show more content or collapsed below the 3-line limit — it either wraps at 3 lines or truncates with "…".

The current `sidebarHeaderHeight()` function (model.go:20957) caps the header height at `sidebarMaxTitleLines` (3 rows), and `renderSidebar()` (model.go:20686) applies the same truncation. Clicking the title area does not toggle any expand/collapse state; only the "✦ gen" button at the far right of the last title row is a separate interactive element.

## Fixed-Width 38-Column Sidebar

The sidebar column width is defined as a constant `sidebarColumnWidth = 38` (model.go:1936) and is used throughout the sidebar render pipeline for:
- Title text wrapping width (innerWidth = sidebarColumnWidth - 4)
- Gen button hit-box positioning
- Overall sidebar layout constants

This width must remain unchanged. Any expand/collapse behavior must operate within this fixed column width.

## Collapsed-By-Default Title Wrapping Capped at 3 Lines

When collapsed (the default state), the session title behaves exactly as today:
- The title text is word-wrapped across up to `sidebarMaxTitleLines = 3` rows
- If the wrapped title exceeds 3 lines, it is truncated with "…" on the last visible row
- The "✦ gen" button rides the last row, right-aligned, within the 38-column width
- `sidebarHeaderHeight()` returns the number of rows (1–3) the title occupies

This collapsed behavior is the initial state for every new session and must persist unless the user actively expands the title.

## Clicking the Title Area Toggles Expanded/Collapsed

Clicking anywhere on the title row (excluding the "✦ gen" button) toggles the sidebar title between expanded and collapsed states. The click handling must:

1. **Distinguish title-click from gen-button-click**: The gen button at the far right of the last title row remains a separate, dedicated click target. A click on the gen button must NOT toggle expansion — it must always trigger regeneration.

2. **Track expansion state per-session**: An `expandedTitle` boolean (or equivalent) is stored in the session state. When `true`, the title expands to show all wrapped lines without the 3-line cap. When `false` (collapsed), the 3-line cap applies.

3. **Click geometry**: The title click area spans from the left edge of the header row to the left edge of the gen button hit-box. The gen button hit-box starts at `innerRight - btnW` where `innerRight = panelWidth() + sidebarColumnWidth - 2` and `btnW = lipgloss.Width(" ✦ gen")`.

4. **Toggle semantics**: A single click on the title area toggles `expandedTitle`. If currently collapsed, it becomes expanded; if expanded, it becomes collapsed.

## Expanded Mode: Wraps and Shows Complete Title Vertically

When expanded, the session title displays all wrapped lines without the 3-line cap:

- The title is word-wrapped across the full available width (`innerWidth = sidebarColumnWidth - 4`)
- All wrapped lines are shown, allowing the title to consume as many rows as needed
- The "✦ gen" button remains visible on the last row, right-aligned
- The header height dynamically increases to accommodate the full title
- `sidebarHeaderHeight()` returns the actual number of wrapped rows (capped only by terminal height, not by `sidebarMaxTitleLines`)

The expanded height must be reflected in layout helpers:
- `sidebarHeaderHeight()` must return the correct height for both collapsed (≤3) and expanded (>3) states
- `sidebarSelectableLines()` must account for the expanded header height so selection highlights and copy operations remain correct
- `contentHeight` calculations in `renderSidebar()` must use the actual header height, not a fixed cap

## The Existing ✦ Gen Button Remains a Separate Click Target

The "✦ gen" button must remain visually and interactively distinct from the title expansion toggle:

- It is rendered at the far right of the last title row
- Its hit-box is `btnW` columns wide at the right edge of the inner content area
- A click on the gen button must **always** trigger title regeneration, regardless of the expanded/collapsed state
- The gen button hit-test (`sidebarTitleGenForClick`, model.go:21145) must not be affected by the expansion state — it always checks `mouse.X >= innerRight - btnW && mouse.X < innerRight`

## Layout and Hit-Testing Helpers Must Mirror Expanded Height

All layout and hit-testing functions that reference the sidebar header height must be consistent across collapsed and expanded states:

- `sidebarHeaderHeight()` (model.go:20957): Must return the actual wrapped row count, respecting the expanded state. When expanded, it returns the full wrap count (no cap at 3).
- `sidebarDisplayTitle()` (model.go:20937): Must return the full title text unchanged in both states; only the rendering height differs.
- `sidebarSelectableLines()` (model.go:20983): Uses `sidebarHeaderHeight()` to compute `contentTopY`; must reflect expanded height when expanded.
- `renderSidebar()` (model.go:20686): Uses `effectiveHeaderHeight = maxInt(1, headerHeight)` (model.go:20769) for content budgeting; must use the actual height.
- `sidebarTitleGenForClick()` (model.go:21145): Must continue to work independently of expansion state; the gen button hit-box geometry does not change.

## Title Expansion Is Transient and Resets for a New Session

The expanded/collapsed state of the title is **session-ephemeral**, not persisted across `/new` sessions or restarts:

- When a new session starts (via `/new` or restart), `expandedTitle` resets to `false` (collapsed)
- The session title `sessionTitle` is re-extracted from the new session's context
- No persistence mechanism is required or desired — the state lives only for the current TUI session
- This transience ensures that each new chat starts with a clean, collapsed title row

## Drag/Selection Behavior Must Not Accidentally Toggle

Mouse drag selection and hover interactions with the sidebar must not inadvertently toggle the expand/collapse state:

- **Drag-to-select**: When the user presses inside the title area and drags, the drag should initiate selection as normal. The toggle should only occur on a *release* that has minimal movement (i.e., a click, not a drag). The existing selection infrastructure in `selectionState` (model.go:1499) handles this: if the drag distance exceeds a small threshold, it is treated as a selection drag, not a toggle.
- **Hover**: Hover effects (underline-on-hover) must not trigger expansion. The `hoverSidebarTitleGen` flag (model.go:1507) tracks gen-button hover separately; title hover should not toggle state.
- **Press-starts-drag / release-decides-click-vs-copy pattern**: The correct pattern (per AGENTS.md TUI mouse safety guidelines) is: press inside content → record start + `dragging:true`; motion while dragging → update end, set `active` only after anchor moves; release → if `active`, extract selection text + clipboard write; if **not active** (no drag distance), clear and **fall through to the click handler** so a plain click still toggles expansion. This pattern must be implemented for the title area.

## Tests

The following tests must pass:

1. **Toggle test**: Clicking the title area (not the gen button) toggles `expandedTitle` state. Verified by checking `sidebarHeaderHeight()` before and after click.

2. **Full rendering test**: In expanded mode, the complete title wraps across multiple rows without truncation "…". Verified by rendering the sidebar and checking that all title words appear on screen.

3. **Layout height test**: `sidebarHeaderHeight()` returns the correct row count for both collapsed (≤3) and expanded (>3) states. Verified by asserting the returned height matches the actual on-screen row count.

4. **Gen-button isolation test**: Clicking the "✦ gen" button always triggers regeneration, regardless of expanded/collapsed state. Verified by checking that a gen-click does not change `expandedTitle` and that regeneration logic fires.

5. **Session reset test**: Starting a new session (`/new`) resets `expandedTitle` to collapsed (`false`). Verified by checking the state after session restart.

## Implementation Notes

### New State Variable

Add an `expandedTitle` boolean field to the TUI model (model.go). Default: `false` (collapsed). This field tracks whether the title is currently expanded for the active session.

### Click Handling in `handleMouseAction`

Modify `handleMouseAction` (or add a new helper) to distinguish title clicks from gen-button clicks:

1. Check `sidebarTitleGenForClick(mouse)` first — if true, handle gen button (existing behavior, unchanged).
2. If not a gen-button click, check if the click lands on the title area:
   - `mouse` is over the sidebar (`mouseOverSidebar`)
   - `mouse.Y` is within the header rows (`appHeaderHeight + 1` to `appHeaderHeight + sidebarHeaderHeight()`)
   - `mouse.X` is to the left of the gen button hit-box (`mouse.X < innerRight - btnW`)
3. If all conditions met, toggle `m.expandedTitle = !m.expandedTitle` and re-render.

### Updated `sidebarHeaderHeight()`

Modify `sidebarHeaderHeight()` (model.go:20957) to check `m.expandedTitle`:

```go
func (m model) sidebarHeaderHeight() int {
    title := m.sidebarDisplayTitle()
    if title == "" {
        if len(m.messages) > 0 {
            return 1
        }
        return 0
    }
    title = strings.ReplaceAll(title, "\n", " ")
    wrapped := wordWrap(title, sidebarColumnWidth-4-ansi.StringWidth("◆ "))
    n := len(strings.Split(wrapped, "\n"))
    if m.expandedTitle {
        return n  // show all rows, no cap
    }
    if n > sidebarMaxTitleLines {
        return sidebarMaxTitleLines
    }
    return n
}
```

### Updated `sidebarTitleGenForClick()`

The gen-button hit-test must remain independent of `expandedTitle`. No change needed if the button geometry (last row, right-aligned) stays the same. Verify that `mouse.Y == appHeaderHeight + m.sidebarHeaderHeight()` still correctly targets the last rendered row even when expanded.

### Selection/Drag Safety

Implement the press-starts-drag / release-decides-click-vs-copy pattern for the title area, per the TUI mouse safety guidelines. The title click handler should only fire on release when the drag distance is negligible. This prevents mouse-selection-drags from accidentally toggling expansion.

### Session Reset

In the `/new` command handler or session reset logic, explicitly set `m.expandedTitle = false` to ensure a collapsed title on fresh sessions.

## Non-Goals (Explicitly Out of Scope)

- **Persisting expansion state across sessions**: The expanded/collapsed state does not persist across `/new`, session restarts, or process exits. It is transient per-session only.
- **Changing the sidebar column width**: The 38-column fixed width (`sidebarColumnWidth`) remains unchanged.
- **Modifying the gen button behavior or appearance**: The "✦ gen" button remains a separate, dedicated regeneration trigger. Its hit-box, rendering, and click handling are unchanged.
- **Adding permanent UI chrome for expand/collapse**: No new buttons, toggles, or keyboard shortcuts are added. Expansion is triggered solely by clicking the title area.
- **Cross-session or persistent storage**: The state lives only in the TUI model for the current session.
- **Programmatic API for controlling expansion**: No API endpoints or external controls for expansion state.