package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// ListBox is a shared row-list primitive that owns vertical scrolling,
// hit-testing, and a structural single-line guarantee. Callers provide
// their own data and row-rendering; the component ensures every rendered
// row is clamped to one physical line so click offsets can never drift
// from render offsets.
//
// The component does NOT own the data model or styling — callers pass a
// count and renderRow callback that returns pre-styled rows. The component
// truncates each row to width to enforce the single-line invariant.
type ListBox struct {
	// Geometry
	width  int
	height int // total visible height in rows

	// Data source — caller owns the data
	count     int                                          // number of items
	renderRow func(idx, width int, selected bool) string // render one item row

	// Optional fixed header rows (rendered above scrollable items)
	headerRows []string // caller provides pre-rendered, clamped rows

	// Optional filter bar row (rendered above headers)
	filterRow string // empty = no filter bar

	// State
	scrollOffset int
	selected     int // index into items [0, count)
	hovered      int // index into items, -1 for none

	// Layout (populated by Layout/Render)
	contentTopY   int // screen row where first item starts (relative to listbox origin)
	contentHeight int // number of rows available for items

	// Wrap mode: when true, each item's rendered row may word-wrap across
	// multiple physical lines instead of being truncated to one line. Off
	// by default so other ListBox callers keep the strict single-line
	// invariant described above.
	wrap bool

	// lineMap is the materialized (itemIdx, text) pairs for the visible
	// content area in wrap mode, built once in Layout() so Render and
	// HitTest read the same data and can never drift from each other.
	lineMap []listBoxLine
}

// listBoxLine is one physical screen line belonging to an item, used in
// wrap mode where an item can span more than one line.
type listBoxLine struct {
	itemIdx int
	text    string
}

// NewListBox creates a new ListBox with the given dimensions.
func NewListBox(width, height int) *ListBox {
	return &ListBox{
		width:   width,
		height:  height,
		hovered: -1,
	}
}

// SetSize sets the width and height of the list box.
func (lb *ListBox) SetSize(width, height int) {
	lb.width = width
	lb.height = height
}

// SetData sets the item count and row renderer callback.
func (lb *ListBox) SetData(count int, renderRow func(idx, width int, selected bool) string) {
	lb.count = count
	lb.renderRow = renderRow
}

// SetHeaderRows sets the fixed header rows (rendered above scrollable items).
func (lb *ListBox) SetHeaderRows(rows []string) {
	lb.headerRows = rows
}

// SetFilterRow sets the optional filter bar row (rendered above headers).
func (lb *ListBox) SetFilterRow(row string) {
	lb.filterRow = row
}

// SetSelected sets the selected index and ensures it's visible. Use this
// for explicit navigation (keyboard/click) — it forces the scroll to
// follow the selection.
func (lb *ListBox) SetSelected(i int) {
	lb.selected = i
	lb.clampSelected()
	lb.EnsureVisible(lb.selected)
}

// SetSelectedForRender sets the selected index without adjusting scroll.
// Use this in a pure-render path (called every frame) so that wheel-driven
// scrolling stays decoupled from the selection — only explicit navigation
// (SetSelected/EnsureVisible) should move the scroll offset.
func (lb *ListBox) SetSelectedForRender(i int) {
	lb.selected = i
	lb.clampSelected()
}

// SetWrapEnabled enables word-wrapping of item rows across multiple
// physical lines instead of truncating each item to a single line.
func (lb *ListBox) SetWrapEnabled(wrap bool) {
	lb.wrap = wrap
}

// SetHovered sets the hovered index, -1 for none.
func (lb *ListBox) SetHovered(i int) {
	lb.hovered = i
}

// Selected returns the current selected index.
func (lb *ListBox) Selected() int {
	return lb.selected
}

// Count returns the number of items.
func (lb *ListBox) Count() int {
	return lb.count
}

// ScrollOffset returns the current scroll offset.
func (lb *ListBox) ScrollOffset() int {
	return lb.scrollOffset
}

// SetScrollOffset sets the scroll offset directly.
func (lb *ListBox) SetScrollOffset(offset int) {
	lb.scrollOffset = offset
	lb.clampScroll()
}

// ScrollDown scrolls down by n lines.
func (lb *ListBox) ScrollDown(n int) {
	lb.scrollOffset += n
	lb.clampScroll()
}

// ScrollUp scrolls up by n lines.
func (lb *ListBox) ScrollUp(n int) {
	lb.scrollOffset -= n
	lb.clampScroll()
}

// ContentTopY returns the screen row where the first item starts.
func (lb *ListBox) ContentTopY() int {
	return lb.contentTopY
}

// ContentHeight returns the number of rows available for items.
func (lb *ListBox) ContentHeight() int {
	return lb.contentHeight
}

// Layout computes the layout geometry (contentTopY, contentHeight) and clamps scroll.
func (lb *ListBox) Layout() {
	// Calculate chrome height
	chromeHeight := len(lb.headerRows)
	if lb.filterRow != "" {
		chromeHeight++
	}
	
	// Calculate content height
	lb.contentHeight = lb.height - chromeHeight
	if lb.contentHeight < 1 {
		lb.contentHeight = 1
	}
	
	// Content starts after chrome
	lb.contentTopY = chromeHeight

	// Clamp scroll
	lb.clampScroll()

	// In wrap mode, materialize the (itemIdx, line) pairs now so Render
	// and HitTest both read the same data and can never drift.
	if lb.wrap {
		lb.buildLineMap()
	}
}

// buildLineMap fills lb.lineMap with the wrapped physical lines for items
// starting at scrollOffset, up to contentHeight lines total.
func (lb *ListBox) buildLineMap() {
	lb.lineMap = lb.lineMap[:0]
	if lb.count == 0 || lb.renderRow == nil {
		return
	}
	for i := lb.scrollOffset; i < lb.count && len(lb.lineMap) < lb.contentHeight; i++ {
		selected := i == lb.selected
		raw := lb.renderRow(i, lb.width, selected)
		for _, w := range strings.Split(wordWrap(raw, lb.width), "\n") {
			if len(lb.lineMap) >= lb.contentHeight {
				break
			}
			lb.lineMap = append(lb.lineMap, listBoxLine{itemIdx: i, text: w})
		}
	}
}

// Render renders the list box with headers, filter bar, items, and scrollbar.
func (lb *ListBox) Render() string {
	lb.Layout()
	
	var lines []string
	
	// Render filter bar if present
	if lb.filterRow != "" {
		lines = append(lines, truncateToWidth(lb.filterRow, lb.width))
	}
	
	// Render header rows
	for _, row := range lb.headerRows {
		lines = append(lines, truncateToWidth(row, lb.width))
	}
	
	// Render items
	if lb.count == 0 || lb.renderRow == nil {
		// Empty state
		hint := "(no items)"
		if lb.filterRow != "" {
			hint = "(no matches)"
		}
		lines = append(lines, truncateToWidth(hint, lb.width))
	} else if lb.wrap {
		// lineMap was built in Layout() — Render and HitTest share it so
		// click offsets can never drift from what's on screen.
		for _, ln := range lb.lineMap {
			lines = append(lines, lipgloss.NewStyle().Width(lb.width).Render(ln.text))
		}
	} else {
		// Render visible items
		end := lb.scrollOffset + lb.contentHeight
		if end > lb.count {
			end = lb.count
		}

		for i := lb.scrollOffset; i < end; i++ {
			selected := i == lb.selected
			line := lb.renderRow(i, lb.width, selected)
			// Enforce single-line invariant: truncate to width
			line = truncateToWidth(line, lb.width)
			lines = append(lines, line)
		}
	}
	
	// Pad to total height if short
	for len(lines) < lb.height {
		lines = append(lines, strings.Repeat(" ", lb.width))
	}
	
	// Append scrollbar alongside items if needed
	if lb.count > lb.contentHeight {
		sb := NewScrollbar()
		sbStr := sb.RenderList(lb.contentHeight, lb.count, lb.scrollOffset, lb.contentHeight)
		sbLines := strings.Split(sbStr, "\n")
		
		// Scrollbar only appears alongside item rows, not chrome
		itemStart := len(lb.headerRows)
		if lb.filterRow != "" {
			itemStart++
		}
		
		for i := itemStart; i < itemStart+lb.contentHeight && i < len(lines); i++ {
			sbIdx := i - itemStart
			if sbIdx < len(sbLines) {
				lines[i] = lines[i] + sbLines[sbIdx]
			} else {
				lines[i] = lines[i] + scrollbarTrackStyle.Render(scrollbarTrack)
			}
		}
	}
	
	return strings.Join(lines, "\n")
}

// HitTest maps a screen coordinate (relative to the list box origin) to
// an item index. Returns -1 if the point is outside the content area.
func (lb *ListBox) HitTest(x, y int) int {
	// Must be within item area (not chrome)
	if y < lb.contentTopY || y >= lb.contentTopY+lb.contentHeight {
		return -1
	}

	if lb.wrap {
		// Read the same materialized lines Render() drew from, so a click
		// on any physical line of a wrapped item resolves to that item.
		lineIdx := y - lb.contentTopY
		if lineIdx < 0 || lineIdx >= len(lb.lineMap) {
			return -1
		}
		return lb.lineMap[lineIdx].itemIdx
	}

	// Map to item index
	itemIdx := lb.scrollOffset + (y - lb.contentTopY)
	if itemIdx < 0 || itemIdx >= lb.count {
		return -1
	}

	return itemIdx
}

// EnsureVisible adjusts scroll so the given item index is visible.
func (lb *ListBox) EnsureVisible(idx int) {
	// Ensure layout is computed before using contentHeight
	if lb.contentHeight == 0 {
		lb.Layout()
	}
	
	if idx < lb.scrollOffset {
		lb.scrollOffset = idx
	}
	if idx >= lb.scrollOffset+lb.contentHeight {
		lb.scrollOffset = idx - lb.contentHeight + 1
	}
	lb.clampScroll()
}

// clampScroll ensures scrollOffset is valid.
func (lb *ListBox) clampScroll() {
	if lb.scrollOffset < 0 {
		lb.scrollOffset = 0
	}
	maxScroll := lb.count - lb.contentHeight
	if maxScroll < 0 {
		maxScroll = 0
	}
	if lb.scrollOffset > maxScroll {
		lb.scrollOffset = maxScroll
	}
}

// clampSelected ensures selected is within [0, count).
func (lb *ListBox) clampSelected() {
	if lb.count == 0 {
		lb.selected = 0
		return
	}
	if lb.selected < 0 {
		lb.selected = 0
	}
	if lb.selected >= lb.count {
		lb.selected = lb.count - 1
	}
}
