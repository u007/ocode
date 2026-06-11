package tui

import (
	"strings"
	"testing"
)

func TestListBoxRender(t *testing.T) {
	lb := NewListBox([]string{"Alpha", "Beta", "Gamma"}, 20, 5)
	rendered := lb.Render()
	if rendered == "" {
		t.Fatal("expected non-empty render")
	}
	if !strings.Contains(rendered, "Alpha") {
		t.Error("rendered output missing Alpha")
	}
}

func TestListBoxSelectedHighlight(t *testing.T) {
	lb := NewListBox([]string{"Alpha", "Beta", "Gamma"}, 20, 5)
	lb.SetSelected(1)
	rendered := lb.Render()
	// Selected item should be highlighted (contains the text at minimum)
	if !strings.Contains(rendered, "Beta") {
		t.Error("selected item Beta not visible")
	}
}

func TestListBoxHover(t *testing.T) {
	lb := NewListBox([]string{"Alpha", "Beta", "Gamma"}, 20, 5)
	lb.SetHovered(2)
	lb.SetSelected(0)
	rendered := lb.Render()
	if !strings.Contains(rendered, "Gamma") {
		t.Error("hovered item Gamma not visible")
	}
}

func TestListBoxFilter(t *testing.T) {
	lb := NewListBox([]string{"Alpha", "Beta", "Gamma"}, 20, 5)
	lb.SetFilter("bet")
	rendered := lb.Render()
	if !strings.Contains(rendered, "Beta") {
		t.Error("filtered result should contain Beta")
	}
	if strings.Contains(rendered, "Alpha") {
		t.Error("filtered result should not contain Alpha")
	}
	if strings.Contains(rendered, "Gamma") {
		t.Error("filtered result should not contain Gamma")
	}
}

func TestListBoxFilterClampsSelection(t *testing.T) {
	lb := NewListBox([]string{"Alpha", "Beta", "Gamma", "Delta"}, 20, 5)
	lb.SetSelected(3) // Delta
	lb.SetFilter("a") // matches Alpha, Gamma, Delta — selection should stay valid
	if lb.Selected() >= lb.FilteredCount() {
		t.Errorf("selection %d out of range for filtered count %d", lb.Selected(), lb.FilteredCount())
	}
}

func TestListBoxFilterFollowsSelection(t *testing.T) {
	lb := NewListBox([]string{"Alpha", "Beta", "Gamma"}, 20, 5)
	lb.SetSelected(2) // Gamma
	lb.SetFilter("g") // only Gamma matches
	if lb.Selected() != 0 {
		t.Errorf("selection should be 0 after filter, got %d", lb.Selected())
	}
}

func TestListBoxHitTest(t *testing.T) {
	lb := NewListBox([]string{"Alpha", "Beta", "Gamma"}, 20, 5)
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

func TestListBoxHitTestOutside(t *testing.T) {
	lb := NewListBox([]string{"Alpha", "Beta"}, 20, 5)
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
	lb := NewListBox(items, 20, 5)
	lb.SetVisibleHeight(3)

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
	lb := NewListBox(nil, 20, 5)
	rendered := lb.Render()
	if rendered == "" {
		t.Fatal("empty list should still render")
	}
}

func TestListBoxEmptyFilterResult(t *testing.T) {
	lb := NewListBox([]string{"Alpha", "Beta"}, 20, 5)
	lb.SetFilter("zzz")
	rendered := lb.Render()
	// Should show an empty hint
	if !strings.Contains(rendered, "no") && !strings.Contains(rendered, "match") {
		t.Error("empty filter result should show hint")
	}
}

func TestListBoxScrollbar(t *testing.T) {
	items := make([]string, 30)
	for i := range items {
		items[i] = strings.Repeat("Y", 10)
	}
	lb := NewListBox(items, 20, 5)
	lb.SetVisibleHeight(5)
	rendered := lb.Render()
	// Should contain scrollbar characters
	if !strings.Contains(rendered, "█") && !strings.Contains(rendered, "┊") {
		t.Error("expected scrollbar in rendered output")
	}
}

func TestListBoxSelected(t *testing.T) {
	lb := NewListBox([]string{"A", "B", "C"}, 20, 5)
	lb.SetSelected(2)
	if lb.Selected() != 2 {
		t.Errorf("expected selected 2, got %d", lb.Selected())
	}
}

func TestListBoxFilteredCount(t *testing.T) {
	lb := NewListBox([]string{"Apple", "Banana", "Cherry", "Avocado"}, 20, 5)
	lb.SetFilter("a") // Apple, Banana, Avocado
	if lb.FilteredCount() != 3 {
		t.Errorf("expected 3 filtered items, got %d", lb.FilteredCount())
	}
}

func TestListBoxFilteredItems(t *testing.T) {
	lb := NewListBox([]string{"Apple", "Banana", "Cherry"}, 20, 5)
	lb.SetFilter("a") // Apple, Banana
	items := lb.FilteredItems()
	if len(items) != 2 {
		t.Fatalf("expected 2 filtered items, got %d", len(items))
	}
	if items[0] != "Apple" || items[1] != "Banana" {
		t.Errorf("unexpected filtered items: %v", items)
	}
}
