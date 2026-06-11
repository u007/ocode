# File Tab Scrollbars Design

## Overview

Add interactive vertical scrollbars to both the file list (tree pane) and file preview pane in the files tab. The preview scrollbar already renders but is not interactive; the tree pane currently has no scrollbar. Both will become fully clickable/draggable.

## Requirements

- **File list (tree pane):** Add vertical scrolling when file list exceeds visible height
- **Scrollbar visibility:** Only show scrollbars when content overflows (doesn't fit on screen)
- **Scrollbar interaction:** Fully interactive — clickable to jump, draggable thumb
- **Keyboard support:** Arrow up/down to scroll tree, mouse wheel scrolling
- **Safety gate:** Scrollbar interaction must not interfere with text selection on preview or tree node clicks

## Architecture

### 1. Tree Vertical Scrolling

**Current state:** Tree pane renders all nodes at once, truncates/wraps if too tall.

**Change:** 
- Add `treeScrollY int` field to `filesModel` (tracks top line offset, like `treeScrollX` for horizontal)
- In `View()`, calculate visible lines based on `treeScrollY` and available height
- Only render lines `[treeScrollY : treeScrollY + visibleHeight]`
- Clamp `treeScrollY` to valid range: `[0, max(0, totalLines - visibleHeight)]`

**Inputs that adjust `treeScrollY`:**
- Arrow up/down keys (already handled by `updateTree()`)
- Mouse wheel (via Bubble Tea key events, if emitted; may require adding handlers)
- Scrollbar click/drag (via `handleMouseAction()` in model.go)

### 2. Interactive Scrollbars

**Preview scrollbar:**
- Already renders via `renderScrollbar()` at line 1880 of files_model.go
- Mouse handling already in place (lines 4331-4339 of model.go)
- No changes needed

**Tree scrollbar:**
- Add rendering in `View()` using `renderScrollbar(treeScrollY, totalLines, visibleHeight, ...)`
- Place at right edge of tree pane (via `JoinHorizontal`)
- Only render if content overflows

**Mouse handling (both panes):**
- Add scrollbar hit-test in `handleMouseAction()` BEFORE node/content click handlers
- Check if `mouse.X` is at scrollbar column
- If scrollbar press: detect thumb (use `scrollbarThumbOffset()`) or track (use `scrollbarSetOffset()`)
- Set appropriate `scrollbarDrag` state (`scrollbarDragFilesTree` or `scrollbarDragFilesPreview`)
- Hit-test runs before content hit-test, cleanly separated by column boundary

**Coordinate separation:**
- Preview: scrollbar at `x == scrollX` (rightmost column), content at `x < scrollX` ✓ already working
- Tree: scrollbar at `x == treeScrollX` (rightmost of tree pane), nodes at `x < treeScrollX`

### 3. Motion/Release Handlers

**On mouse motion:**
- If `scrollbarDrag == scrollbarDragFilesTree` (or `...FilesPreview`), call `scrollbarSetOffset()` to update viewport Y-offset
- Tree motion: update `treeScrollY`, don't touch `filesSel.dragging` (selection state)

**On mouse release:**
- If scrollbar was dragging, clear `scrollbarDrag` state
- Selection drag (if active) is independent — handled separately at lines 4401–4412

### 4. Rendering Geometry

**Tree pane layout:**
- Total width: `treeW = w * 35 / 100`
- Content width: `treeContentWidth = treeW - 6` (accounts for border 2 + padding 2 + margin 2)
- Scrollbar width: 1 column, positioned at `treeW - 2 - 1` (inside the bordered box)
- Content rendered via `JoinHorizontal(lipgloss.Top, treeContent, scrollbarIfOverflow)`

**Preview pane layout:**
- Already correct; scrollbar already at `previewRight - 1`

## Testing Checklist

1. **Tree scrolling:**
   - [ ] File list scrolls vertically when tall enough to overflow
   - [ ] Arrow up/down moves cursor and scrolls tree
   - [ ] Mouse wheel scrolls (if implemented)
   - [ ] Scrollbar appears only when content > viewport height
   - [ ] Scrollbar disappears when content fits

2. **Scrollbar interaction:**
   - [ ] Clicking track jumps to that position
   - [ ] Dragging thumb scrolls smoothly
   - [ ] Scrollbar bounds clamp correctly (no overshooting)

3. **Safety:**
   - [ ] Tree node clicks work (not intercepted by scrollbar)
   - [ ] Preview text selection still works (drag in content, not on scrollbar)
   - [ ] No hairwire or rendering corruption

## Files to Modify

- `internal/tui/files_model.go`: Add `treeScrollY`, update `View()` to render visible lines only
- `internal/tui/model.go`: Add tree scrollbar hit-test and drag handling in `handleMouseAction()`
- `internal/tui/model.go`: Add `scrollbarDragFilesTree` case in motion/release handlers

## Edge Cases

1. **Window resize:** `treeScrollY` may exceed bounds after resize; clamp in `View()`
2. **Directory expand/collapse:** Tree size changes; cursor may need adjustment
3. **Empty tree:** No files; scrollbar should not render
4. **Very tall single line:** If one filename is taller than viewport (wrapped), scrollbar still correct (treats as N logical lines)

## Implementation Order

1. Add `treeScrollY` field and rendering logic
2. Add tree scrollbar hit-test in `handleMouseAction()`
3. Wire motion/release handlers
4. Test tree scrolling with arrow keys
5. Test scrollbar clicks and drags
6. Verify preview selection still works
7. Test edge cases (resize, empty tree, tall lines)
