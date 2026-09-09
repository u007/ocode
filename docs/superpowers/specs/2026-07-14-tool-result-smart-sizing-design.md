# Tool Result Smart Auto-Sizing — Design

## Overview

Improve the expand/collapse UX for tool result boxes in both the TUI and web
UI. Three changes: a visible expand icon in every collapsible box header, a
bumped line-count threshold so common results (imagegen, read) are fully
visible by default, and removal of the web UI's `max-h-64` scroll cap.

## Motivation

The `imagegen` tool result is truncated in both UIs:

- **TUI**: Tool output boxes show only the last 12 lines by default; the
  imagegen JSON (12–18 lines for 1–2 images) is cut off.
- **Web**: The output `<pre>` has `max-h-64` (256px cap) with `overflow-auto`,
  hiding part of the result even when the toggle is open.

The expand/collapse mechanism exists in both UIs but is not discoverable (no
visible icon in the TUI header) and uses overly conservative thresholds.

## Changes

### 1. TUI — Expand icon in collapsible box headers

**Files:**
- `internal/tui/model.go` — `buildToolOutputBox()`, `renderCompactionSummaryBox()`, `renderOrphanWarningBox()`

**Before:**
```
  read output
┌────────────────────┐
│ (content)          │
└────────────────────┘
  … 5 earlier lines · click to expand
```

**After:**
```
  ▾ read output                     ← expanded
┌────────────────────┐
│ (full content)     │
└────────────────────┘
  ▲ click to collapse

  ▸ bash output                     ← collapsed
┌────────────────────┐
│ (last N lines)     │
└────────────────────┘
  … 5 earlier lines · click to expand
```

**Rules:**
- `▾` when `expanded = true` (box is fully visible)
- `▸` when `expanded = false` and the box content exceeds the preview limit
- No icon when the box fits entirely within the preview limit (no
  collapse/expand needed — already shows all content)
- Icon is in the same `m.styles.Hint.Render(...)` span as the header text
- Applies to all three collapsible box types: tool output, compaction
  summaries, and orphan warnings

### 2. TUI — Bump auto-size threshold

**File:** `internal/tui/tool_render.go`

```
- const toolOutputPreviewLines = 12
+ const toolOutputPreviewLines = 20
```

**Effect:**
- Results ≤ 20 lines are fully visible by default (no truncation).
- The common `imagegen` case (1–2 images, ~12–18 lines) now renders in full.
- Results > 20 lines get the truncated preview showing the last 20 lines.
- All three collapsible box types share this constant, so they all benefit.

### 3. Web UI — Remove height cap + smart default collapse

**File:** `web/src/components/Chat/TurnParts.tsx` — `ToolBlock`

**Changes:**

1. **Remove `max-h-64`** from the output `<pre>` — the toggle button is now
   the only show/hide mechanism. When open, all content is visible.

2. **Smart default collapse**: Only auto-expand results under a threshold.
   ```tsx
   const lineCount = output ? output.split('\n').length : 0;
   const [open, setOpen] = useState(lineCount <= 50);
   ```
   - ≤ 50 lines: expanded by default (current behavior for most results)
   - > 50 lines: collapsed by default (avoids overwhelming the viewport)

3. **Line count badge in header**:
   ```
   ▾ 🔧 imagegen output  ·  18 lines      ← expanded (≤50 lines)
   ▸ 🔧 read output  ·  142 lines         ← collapsed (>50 lines)
   ```
   Gives the user a quick sense of the result size before opening.

## Implementation Order

1. `internal/tui/tool_render.go` — bump constant (1-line change)
2. `internal/tui/model.go` — add `▸`/`▾` icon in `buildToolOutputBox`,
   `renderCompactionSummaryBox`, `renderOrphanWarningBox`
3. `web/src/components/Chat/TurnParts.tsx` — remove `max-h-64`, add smart
   default collapse, add line count badge

## Testing

- **TUI**: Existing `tool_output_test.go` tests render paths with the bumped
  constant. Verify the `TestRenderToolResultPreservesFullOutput` test still
  passes (it tests with 1000 lines, which exceeds the new threshold).
- **Web**: Manual verification — generate an imagegem result and confirm the
  full JSON is visible without scrolling.

## Open Questions

None — the design is fully specified.
