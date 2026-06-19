package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// ListBox renders a filterable, scrollable list of items with selection,
// hover, scrollbar, and mouse hit-testing. It is designed to replace the
// inline rendering in picker.go and slash_popup.go.
type ListBox struct {
	items     []string
	width     int
	maxHeight int // maximum visible rows (including header/footer)

	// Filter state.
	filter   string
	filtered []int // indices into items that match filter

	// Selection state.
	selected int // index into filtered[]
	hovered  int // index into filtered[], -1 for none

	// Scroll state.
	scrollOffset int

	// Layout (populated by layout/Render).
	visibleHeight int
	contentTopY   int // screen row where first item starts
}

// NewListBox creates a new ListBox with the given items and dimensions.
func NewListBox(items []string, width, maxHeight int) *ListBox {
	lb := &ListBox{
		items:         items,
		width:         width,
		maxHeight:     maxHeight,
		visibleHeight: maxHeight,
		hovered:       -1,
	}
	lb.rebuildFiltered()
	return lb
}

// SetVisibleHeight sets the number of visible rows for the item list.
func (lb *ListBox) SetVisibleHeight(h int) {
	lb.visibleHeight = h
	lb.clampScroll()
}

// SetFilter sets the filter string and rebuilds the filtered list.
func (lb *ListBox) SetFilter(f string) {
	lb.filter = f
	lb.rebuildFiltered()
	lb.clampSelection()
	lb.clampScroll()
}

// SetSelected sets the selected index (into the filtered list).
func (lb *ListBox) SetSelected(i int) {
	lb.selected = i
	lb.clampSelection()
	lb.ensureVisible()
}

// SetHovered sets the hovered index (into the filtered list), -1 for none.
func (lb *ListBox) SetHovered(i int) {
	lb.hovered = i
}

// Selected returns the current selected index.
func (lb *ListBox) Selected() int {
	return lb.selected
}

// FilteredCount returns the number of items matching the current filter.
func (lb *ListBox) FilteredCount() int {
	return len(lb.filtered)
}

// FilteredItems returns the items matching the current filter.
func (lb *ListBox) FilteredItems() []string {
	result := make([]string, len(lb.filtered))
	for i, idx := range lb.filtered {
		result[i] = lb.items[idx]
	}
	return result
}

// ScrollOffset returns the current scroll offset.
func (lb *ListBox) ScrollOffset() int {
	return lb.scrollOffset
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

// HitTest maps a screen coordinate (relative to the list box origin) to
// an item index in the filtered list. Returns -1 if the point is outside
// the content area.
func (lb *ListBox) HitTest(x, y int) int {
	if y < lb.contentTopY || y >= lb.contentTopY+lb.visibleHeight {
		return -1
	}
	itemIdx := lb.scrollOffset + (y - lb.contentTopY)
	if itemIdx < 0 || itemIdx >= len(lb.filtered) {
		return -1
	}
	return itemIdx
}

// Render renders the list box with items, selection highlight, hover, scrollbar,
// and empty-state hint.
func (lb *ListBox) Render() string {
	lb.layout()

	var lines []string

	if len(lb.filtered) == 0 {
		// Empty state.
		hint := "(no items)"
		if lb.filter != "" {
			hint = "(no matches)"
		}
		padded := hintStyle.Render(padRight(hint, lb.width))
		lines = append(lines, padded)
	} else {
		// Render visible items.
		end := lb.scrollOffset + lb.visibleHeight
		if end > len(lb.filtered) {
			end = len(lb.filtered)
		}

		for i := lb.scrollOffset; i < end; i++ {
			item := lb.items[lb.filtered[i]]
			line := lb.renderItem(item, i)
			lines = append(lines, line)
		}
	}

	// Pad to visible height if short.
	for len(lines) < lb.visibleHeight {
		lines = append(lines, strings.Repeat(" ", lb.width))
	}

	// Append scrollbar alongside items.
	if len(lb.filtered) > lb.visibleHeight {
		sb := NewScrollbar()
		sbStr := sb.RenderList(lb.visibleHeight, len(lb.filtered), lb.scrollOffset, lb.visibleHeight)
		sbLines := strings.Split(sbStr, "\n")
		for i, line := range lines {
			if i < len(sbLines) {
				lines[i] = line + sbLines[i]
			} else {
				lines[i] = line + scrollbarTrackStyle.Render(scrollbarTrack)
			}
		}
	}

	return strings.Join(lines, "\n")
}

// layout computes layout parameters.
func (lb *ListBox) layout() {
	if lb.visibleHeight > lb.maxHeight {
		lb.visibleHeight = lb.maxHeight
	}
	if lb.visibleHeight < 1 {
		lb.visibleHeight = 1
	}
	lb.contentTopY = 0
	lb.clampScroll()
}

// renderItem renders a single item row with selection/hover styling.
func (lb *ListBox) renderItem(item string, filteredIdx int) string {
	// Truncate to width.
	itemWidth := ansi.StringWidth(item)
	maxWidth := lb.width - 2 // leave room for padding
	if itemWidth > maxWidth {
		item = ansi.Truncate(item, maxWidth, "…")
	} else {
		item = item + strings.Repeat(" ", maxWidth-itemWidth)
	}

	switch {
	case filteredIdx == lb.selected:
		return lb.styles().Selected.Render(" " + item + " ")
	case filteredIdx == lb.hovered:
		return lb.styles().Hint.Render(" " + item + " ")
	default:
		return " " + item + " "
	}
}

// styles returns the theme styles (using defaults if not available).
func (lb *ListBox) styles() Styles {
	// Use default styles — the caller can override by setting fields directly.
	return defaultStyles()
}

// defaultStyles returns a basic set of styles for the ListBox.
func defaultStyles() Styles {
	return Styles{
		Selected: lipgloss.NewStyle().
			Background(lipgloss.Color("#7AA2F7")).
			Foreground(lipgloss.Color("#1a1b26")).
			Bold(true),
		Hint: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#565f89")),
	}
}

// rebuildFiltered rebuilds the filtered index list from items and filter.
func (lb *ListBox) rebuildFiltered() {
	if lb.filter == "" {
		lb.filtered = make([]int, len(lb.items))
		for i := range lb.items {
			lb.filtered[i] = i
		}
		return
	}

	query := strings.ToLower(lb.filter)
	lb.filtered = lb.filtered[:0]
	for i, item := range lb.items {
		if fuzzyMatch(strings.ToLower(item), query) {
			lb.filtered = append(lb.filtered, i)
		}
	}
}

// fuzzyMatch checks if query is a subsequence of s.
func fuzzyMatch(s, query string) bool {
	if query == "" {
		return true
	}
	qi := 0
	for i := 0; i < len(s) && qi < len(query); i++ {
		if s[i] == query[qi] {
			qi++
		}
	}
	return qi == len(query)
}

// clampSelection ensures selected is within [0, len(filtered)).
func (lb *ListBox) clampSelection() {
	if len(lb.filtered) == 0 {
		lb.selected = 0
		return
	}
	if lb.selected < 0 {
		lb.selected = 0
	}
	if lb.selected >= len(lb.filtered) {
		lb.selected = len(lb.filtered) - 1
	}
}

// clampScroll ensures scrollOffset is valid.
func (lb *ListBox) clampScroll() {
	if lb.scrollOffset < 0 {
		lb.scrollOffset = 0
	}
	maxScroll := len(lb.filtered) - lb.visibleHeight
	if maxScroll < 0 {
		maxScroll = 0
	}
	if lb.scrollOffset > maxScroll {
		lb.scrollOffset = maxScroll
	}
}

// ensureVisible adjusts scroll so the selected item is visible.
func (lb *ListBox) ensureVisible() {
	if lb.selected < lb.scrollOffset {
		lb.scrollOffset = lb.selected
	}
	if lb.selected >= lb.scrollOffset+lb.visibleHeight {
		lb.scrollOffset = lb.selected - lb.visibleHeight + 1
	}
	lb.clampScroll()
}
