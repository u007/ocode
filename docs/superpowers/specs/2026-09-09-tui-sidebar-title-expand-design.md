---
type: Decision
title: TUI Sidebar Title Expand/Collapse Design
description: 'Design for sidebar title expand/collapse behavior, updated to match user decision: transient expansion, reset on session changes, no session JSON persistence.'
timestamp: 2026-09-09T06:15:32Z
status: active
---

# TUI Sidebar Title Expand/Collapse Design

## Problem / Current Behavior

The TUI sidebar currently displays a fixed-width (38-column) sidebar with the session title always visible, wrapped to a maximum of 3 lines (`sidebarMaxTitleLines = 3`). The "✦ gen" button rides the last row of the title and is the only interactive element in the title row. The title cannot be expanded to show more content or collapsed below the 3-line limit — it either wraps at 3 lines or truncates with "...".

The current `sidebarHeaderHeight()` function (model.go:20957) caps the header height at `sidebarMaxTitleLines` (3 rows), and `renderSidebar()` (model.go:20686) applies the same truncation. Clicking the title area does not toggle any expand/collapse state; only the "✦ gen" button at the far right of the last title row is a separate interactive element.

## Fixed-Width 38-Column Sidebar

The sidebar column width is defined as a constant `sidebarColumnWidth = 38` (model.go:1936) and is used throughout the sidebar render pipeline for:

- Title text wrapping width (innerWidth = sidebarColumnWidth - 4)
- Gen button hit-box positioning
- Overall sidebar layout constants

This width must remain unchanged. Any expand/collapse behavior must operate within this fixed column width.

## Shared Title-Layout Result

A single `titleLayout` structure is the authoritative source for all title-related geometry and rendering. It is computed once per render cycle and passed to:

- `sidebarHeaderHeight()` — returns `layout.rows`
- `sidebarTitleGenForClick(mouse)` — uses `layout.genButtonStartX` and `layout.genButtonRow`
- Hit-testing and selection — uses `layout.titleRows`, `layout.genButtonBounds`, and `layout.titleStartX`
- Rendering — uses `layout.wrappedLines` for drawing

``\go
type titleLayout struct {
	rows            int              // number of rows the title occupies (collapsed: ≤3, expanded: all)
	genButtonRow    int              // Y-position of the gen button (always the last title row)
	genButtonStartX int              // X-offset within the sidebar where the gen button begins
	genButtonBounds rect             // precise hit-box for the gen button
	titleStartX     int              // left column inside sidebar where title text begins
	titleRows       []string         // each rendered row of wrapped title text (may be shorter than 3 in collapsed mode)
	wrappedLines    []string         // full word-wrapped lines (used for expansion logic)
}
```

This layout is the *only* source of truth for title geometry; no independent calculations are performed elsewhere.

## Collapsed-By-Default Title Wrapping Capped at 3 Lines

When collapsed (the default state), the session title behaves exactly as today:

- The title text is word-wrapped across up to `sidebarMaxTitleLines = 3` rows
- If the wrapped title exceeds 3 lines, it is truncated with "…" on the last visible row
- The "✦ gen" button rides the last row, right-aligned, within the 38-column width
- `sidebarHeaderHeight()` returns the number of rows (1–3) the title occupies

This collapsed behavior is the initial state for every new session and must persist unless the user actively expands the title.

## Clicking the Title Area Toggles Expanded/Collapsed

Clicking the title area (anywhere outside the "✦ gen" button hit-box) toggles `expandedTitle`. When expanded, the title wraps across multiple rows without the 3-line cap. Clicking the title again collapses it back to ≤3 rows. The "✦ gen" button remains a separate, dedicated regeneration trigger — clicking it always fires regeneration and never toggles expansion.

## Drag-Select vs. Toggle Gesture

Press-hold-drag on the title area:

- **With ≥3-cell Manhattan distance** from press to release: selection is extracted and `expandedTitle` is **not** toggled. The drag takes precedence as a selection action.
- **With <3-cell Manhattan distance** (same origin/destination): `expandedTitle` toggles as if a plain click occurred.

The "✦ gen" button hit-box is excluded from drag selection — presses starting within its bounds always trigger regeneration, regardless of drag distance.

## Final-Row Reflow Around Gen Button (No Title Loss)

In collapsed mode, the title text width is `sidebarColumnWidth - 4` regardless of gen button presence. The gen button occupies its constant width on the last row without reducing the title's wrap width. The layout engine ensures the gen button is positioned at the far right of the last title row, and the title wraps within the remaining space. No title content is lost or shifted by the gen button's presence; the button is rendered *within* the title row's allocated width, not encroaching on the text area.

## Expanded Mode and Short-Terminal Title Priority with Frame-Height Invariant

When `expandedTitle` is true, the title wraps across as many rows as needed without the 3-line cap. The render pipeline enforces a frame-height invariant:

- Total sidebar height is clamped to `terminalHeight - 2` to preserve one-row status/footer margin.
- If the expanded title would cause the sidebar to exceed this height, rows are truncated from the bottom.
- Truncated rows append an ellipsis row (…) if any rows were removed, signaling to the user that content continues off-screen.
- The gen button remains anchored to the last title row (which may now be the visible/truncated row).
- On terminals with height ≤ headerRows + genButtonRow + 1, the title falls back to a single-line truncated display, and the gen button moves to the next row (or stays on the same row if space permits). This ensures the sidebar never overflows the terminal geometry.

## Reset Behavior for /new, /session Load, /clear, and Other Active Session Replacement Paths

- **`/new`**: Explicitly sets `m.expandedTitle = false` (collapsed) and recomputes `titleLayout` from scratch. The sidebar starts every new session with the title collapsed at ≤3 lines.
- **`/session load`** (resuming a saved session): `expandedTitle` is reset to `false` (collapsed) on load. No session JSON persistence — the expanded state does not persist across process restarts.
- **`/clear`**: Clears the current session state, including `expandedTitle = false`. The new empty session starts with a collapsed title.
- **Active-session replacement** (e.g., switching projects via the session tab): `expandedTitle` is reset to `false` (collapsed). The `titleLayout` is recomputed based on the new session's data.
- In all cases, the `titleLayout` is recomputed after the state change, ensuring that rendering, header height, and hit-tests are consistent with the new state.

## Exact Test Cases

The following test cases must pass. Each test names the specific behavior being verified.

| # | Test | Description |
|---|---|---|
| 1 | **Toggle test** | Clicking the title area (not the gen button) toggles `expandedTitle`. Verified by checking `sidebarHeaderHeight()` before and after click. |
| 2 | **Full rendering test** | In expanded mode, the complete title wraps across multiple rows without truncation "…". Verified by rendering the sidebar and checking that all title words appear on screen (up to terminal height limit). |
| 3 | **Layout height test** | `sidebarHeaderHeight()` returns the correct row count for both collapsed (≤3) and expanded (>3) states. Verified by asserting the returned height matches the actual on-screen row count from `titleLayout.rows`. |
| 4 | **Gen-button isolation test** | Clicking the "✦ gen" button always triggers regeneration, regardless of expanded/collapsed state. Verified by checking that a gen-click does not change `expandedTitle` and that regeneration logic fires. |
| 5 | **Session reset test** | Starting a new session (`/new`) resets `expandedTitle` to collapsed (`false`). Verified by checking the state after session restart. |
| 6 | **Drag-select vs toggle test** | Press-hold-drag on the title area with ≥3-cell Manhattan distance results in selection extraction and **does not** toggle `expandedTitle`. Press-without-drag (same origin/destination) toggles `expandedTitle`. |
| 7 | **Gen-button click does not toggle** | Clicking the gen button (hit-box) always triggers regeneration and never toggles `expandedTitle`. Verified by checking state before/after gen-click. |
| 8 | **Reflow preserves title width** | In collapsed mode, the title text width is `sidebarColumnWidth - 4` regardless of gen button presence. The gen button occupies its constant width on the last row without reducing the title's wrap width. |
| 9 | **Terminal-height clamp test** | When the title in expanded mode would cause the sidebar to exceed terminal height, the render pipeline clamps total height to `terminalHeight - 2` and truncates title rows from the bottom, appending an ellipsis row if any rows were removed. |
| 10 | **Short-terminal fallback test** | On a terminal with height ≤ headerRows + genButtonRow + 1, the title falls back to a single-line truncated display, and the gen button moves to the next row (or same row if space permits). |
| 11 | **Reset /new test** | After `/new`, `expandedTitle` is `false` and `sidebarHeaderHeight()` returns ≤3 even if the title text is long. |
| 12 | **Reset /session load test** | After `/session load`, `expandedTitle` is `false` and `sidebarHeaderHeight()` returns ≤3 (collapsed). |

## Non-Goals (Explicitly Out of Scope)

- **Persisting expansion state across arbitrary session boundaries**: State persists only within the lifetime of a session. Cross-process or cross-machine persistence is not supported.
- **Changing the sidebar column width**: The 38-column fixed width (`sidebarColumnWidth`) remains unchanged.
- **Modifying the gen button behavior or appearance**: The "✦ gen" button remains a separate, dedicated regeneration trigger. Its hit-box, rendering, and click handling are unchanged.
- **Adding permanent UI chrome for expand/collapse**: No new buttons, toggles, or keyboard shortcuts are added. Expansion is triggered solely by clicking the title area.
- **Cross-session or persistent storage**: The state lives only in the TUI model for the current session. No session JSON persistence.
- **Programmatic API for controlling expansion**: No API endpoints or external controls for expansion state.
