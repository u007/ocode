package tui

import (
	"strings"
	"testing"
)

func TestListBoxRender(t *testing.T) {
	items := []string{"Alpha", "Beta", "Gamma"}
	lb := NewListBox(20, 5)
	lb.SetData(len(items), func(idx, width int, selected bool) string {
		return items[idx]
	})
	rendered := lb.Render()
	if rendered == "" {
		t.Fatal("expected non-empty render")
	}
	if !strings.Contains(rendered, "Alpha") {
		t.Error("rendered output missing Alpha")
	}
}

func TestListBoxSelectedHighlight(t *testing.T) {
	items := []string{"Alpha", "Beta", "Gamma"}
	lb := NewListBox(20, 5)
	lb.SetData(len(items), func(idx, width int, selected bool) string {
		if selected {
			return "[SELECTED] " + items[idx]
		}
		return items[idx]
	})
	lb.SetSelected(1)
	rendered := lb.Render()
	if !strings.Contains(rendered, "Beta") {
		t.Error("selected item Beta not visible")
	}
	if !strings.Contains(rendered, "[SELECTED]") {
		t.Error("selected item not marked as selected")
	}
}

func TestListBoxHover(t *testing.T) {
	items := []string{"Alpha", "Beta", "Gamma"}
	lb := NewListBox(20, 5)
	lb.SetData(len(items), func(idx, width int, selected bool) string {
		return items[idx]
	})
	lb.SetHovered(2)
	lb.SetSelected(0)
	rendered := lb.Render()
	if !strings.Contains(rendered, "Gamma") {
		t.Error("hovered item Gamma not visible")
	}
}

func TestListBoxHitTest(t *testing.T) {
	items := []string{"Alpha", "Beta", "Gamma"}
	lb := NewListBox(20, 5)
	lb.SetData(len(items), func(idx, width int, selected bool) string {
		return items[idx]
	})
	lb.Render() // populate layout

	// Hit test on first item row (contentTopY=0)
	idx := lb.HitTest(0, 0)
	if idx != 0 {
		t.Errorf("hit test at y=0: expected index 0, got %d", idx)
	}

	// Hit test on second item row
	idx = lb.HitTest(0, 1)
	if idx != 1 {
		t.Errorf("hit test at y=1: expected index 1, got %d", idx)
	}
}

// TestListBoxWrapHitTest verifies that in wrap mode, a click on any
// physical line belonging to a wrapped item resolves to that item, and
// that the item below a wrapped item is offset by the extra line(s).
func TestListBoxWrapHitTest(t *testing.T) {
	items := []string{
		"M this is a very long file path that will need to wrap across lines/pkg/subpkg/file.go",
		"A short.go",
	}
	lb := NewListBox(20, 10)
	lb.SetWrapEnabled(true)
	lb.SetData(len(items), func(idx, width int, selected bool) string {
		return items[idx]
	})
	rendered := lb.Render()
	lines := strings.Split(rendered, "\n")
	if len(lines) < 3 {
		t.Fatalf("expected wrapped item to span multiple lines, got %d lines:\n%s", len(lines), rendered)
	}

	// First line of the wrapped item.
	if idx := lb.HitTest(0, 0); idx != 0 {
		t.Errorf("hit test on wrapped item's first line: expected index 0, got %d", idx)
	}
	// Second physical line still belongs to the same wrapped item.
	if idx := lb.HitTest(0, 1); idx != 0 {
		t.Errorf("hit test on wrapped item's second line: expected index 0, got %d", idx)
	}

	// Whatever line the second item actually starts on must resolve to index 1,
	// not be mistaken for another line of item 0.
	itemTwoLine := -1
	for i, l := range lines {
		if strings.Contains(l, "short.go") {
			itemTwoLine = i
			break
		}
	}
	if itemTwoLine < 0 {
		t.Fatalf("could not locate item 1's rendered line in output:\n%s", rendered)
	}
	if idx := lb.HitTest(0, itemTwoLine); idx != 1 {
		t.Errorf("hit test at wrapped item's following row (y=%d): expected index 1, got %d", itemTwoLine, idx)
	}
}

func TestListBoxHitTestOutside(t *testing.T) {
	items := []string{"Alpha", "Beta"}
	lb := NewListBox(20, 5)
	lb.SetData(len(items), func(idx, width int, selected bool) string {
		return items[idx]
	})
	lb.Render()

	// Hit test outside content area
	idx := lb.HitTest(0, 100)
	if idx != -1 {
		t.Errorf("hit test outside bounds: expected -1, got %d", idx)
	}
}

func TestListBoxScroll(t *testing.T) {
	items := make([]string, 20)
	for i := range items {
		items[i] = strings.Repeat("X", 10)
	}
	lb := NewListBox(20, 5)
	lb.SetData(len(items), func(idx, width int, selected bool) string {
		return items[idx]
	})

	// Initially at top
	if lb.ScrollOffset() != 0 {
		t.Errorf("expected initial scroll 0, got %d", lb.ScrollOffset())
	}

	// Scroll down
	lb.ScrollDown(2)
	if lb.ScrollOffset() != 2 {
		t.Errorf("expected scroll 2, got %d", lb.ScrollOffset())
	}

	// Scroll up
	lb.ScrollUp(1)
	if lb.ScrollOffset() != 1 {
		t.Errorf("expected scroll 1, got %d", lb.ScrollOffset())
	}

	// Scroll to top
	lb.ScrollUp(100)
	if lb.ScrollOffset() != 0 {
		t.Errorf("expected scroll 0 at top, got %d", lb.ScrollOffset())
	}
}

func TestListBoxEmptyItems(t *testing.T) {
	lb := NewListBox(20, 5)
	lb.SetData(0, nil)
	rendered := lb.Render()
	if rendered == "" {
		t.Fatal("empty list should still render")
	}
}

func TestListBoxWithHeaders(t *testing.T) {
	items := []string{"file1.go", "file2.go", "file3.go"}
	lb := NewListBox(20, 8)
	lb.SetData(len(items), func(idx, width int, selected bool) string {
		return items[idx]
	})
	lb.SetHeaderRows([]string{"● staged", "○ unstaged"})
	lb.Render()

	// Content should start after headers
	if lb.ContentTopY() != 2 {
		t.Errorf("expected contentTopY=2, got %d", lb.ContentTopY())
	}

	// Hit test should account for headers
	idx := lb.HitTest(0, 2) // first item row
	if idx != 0 {
		t.Errorf("hit test at y=2: expected index 0, got %d", idx)
	}

	idx = lb.HitTest(0, 0) // header row
	if idx != -1 {
		t.Errorf("hit test on header row: expected -1, got %d", idx)
	}
}

func TestListBoxWithFilterBar(t *testing.T) {
	items := []string{"file1.go", "file2.go"}
	lb := NewListBox(20, 6)
	lb.SetData(len(items), func(idx, width int, selected bool) string {
		return items[idx]
	})
	lb.SetFilterRow("filter: test")
	lb.Render()

	// Content should start after filter bar
	if lb.ContentTopY() != 1 {
		t.Errorf("expected contentTopY=1, got %d", lb.ContentTopY())
	}

	// Hit test should account for filter bar
	idx := lb.HitTest(0, 1) // first item row
	if idx != 0 {
		t.Errorf("hit test at y=1: expected index 0, got %d", idx)
	}

	idx = lb.HitTest(0, 0) // filter bar row
	if idx != -1 {
		t.Errorf("hit test on filter bar: expected -1, got %d", idx)
	}
}

func TestListBoxSingleLineGuarantee(t *testing.T) {
	// Test that long rows are truncated to fit width
	items := []string{strings.Repeat("X", 100)}
	lb := NewListBox(20, 5)
	lb.SetData(len(items), func(idx, width int, selected bool) string {
		return items[idx]
	})
	rendered := lb.Render()
	lines := strings.Split(rendered, "\n")
	
	// Each line should be exactly width (20) or less
	for i, line := range lines {
		lineWidth := visualLineWidth(line)
		if lineWidth > 20 {
			t.Errorf("line %d width %d exceeds listbox width 20", i, lineWidth)
		}
	}
}

func TestListBoxEnsureVisible(t *testing.T) {
	items := make([]string, 20)
	for i := range items {
		items[i] = "item"
	}
	lb := NewListBox(20, 5)
	lb.SetData(len(items), func(idx, width int, selected bool) string {
		return items[idx]
	})

	// Select item 15, should scroll to make it visible
	lb.SetSelected(15)
	if lb.ScrollOffset() == 0 {
		t.Error("expected scroll to adjust for selected item 15")
	}

	// Item 15 should be visible
	if lb.ScrollOffset() > 15 || lb.ScrollOffset()+lb.ContentHeight() <= 15 {
		t.Errorf("item 15 not visible: scroll=%d, contentHeight=%d", lb.ScrollOffset(), lb.ContentHeight())
	}
}
