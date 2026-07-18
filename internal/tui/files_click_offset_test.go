package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestFilesClickOffsetDiagnostic(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.go", "b.go", "c.go", "d.go", "e.go"} {
		os.WriteFile(filepath.Join(dir, name), []byte("package main\n"), 0644)
	}

	m := newFilesModel(dir)
	m.Resize(100, 30)
	styles := ApplyThemeColors("tokyonight")

	// Use treeHeaderRows (the single source of truth) so the offset adapts
	// to any number of hint lines — no more hardcoded "exactly 2 lines".
	treeW := 100 * 35 / 100
	hintLines := len(m.treeHeaderRows(treeW, styles))
	expectedTop := appHeaderHeight + 1 + hintLines

	fmt.Printf("expectedTop=%d\n", expectedTop)

	// Verify each click position
	for y := appHeaderHeight; y < appHeaderHeight+12; y++ {
		idx, ok := m.treeNodeForClick(tea.Mouse{X: 2, Y: y}, appHeaderHeight, styles)
		status := "miss"
		if ok {
			status = fmt.Sprintf("node=%d (%s)", idx, m.nodes[idx].name)
		}
		fmt.Printf("Y=%2d (rel=%2d): %s\n", y, y-appHeaderHeight, status)
	}

	// Verify first node is at expectedTop
	idx, ok := m.treeNodeForClick(tea.Mouse{X: 2, Y: expectedTop}, appHeaderHeight, styles)
	if !ok || idx != 0 {
		t.Errorf("expected first node at Y=%d, got ok=%v idx=%d", expectedTop, ok, idx)
	}

	// Verify second node
	idx2, ok2 := m.treeNodeForClick(tea.Mouse{X: 2, Y: expectedTop + 1}, appHeaderHeight, styles)
	if !ok2 || idx2 != 1 {
		t.Errorf("expected second node at Y=%d, got ok=%v idx=%d", expectedTop+1, ok2, idx2)
	}
}

func TestFilesClickOffsetNoHint(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.go", "b.go"} {
		os.WriteFile(filepath.Join(dir, name), []byte("package main\n"), 0644)
	}

	m := newFilesModel(dir)
	m.Resize(100, 30)
	styles := ApplyThemeColors("tokyonight")
	// When preview is focused, no hint is shown
	m.panel = filesPanelPreview

	// No hint: first node should be at headerHeight + 1 (border)
	idx, ok := m.treeNodeForClick(tea.Mouse{X: 2, Y: appHeaderHeight + 1}, appHeaderHeight, styles)
	if !ok || idx != 0 {
		t.Errorf("no hint: expected first node at Y=%d, got ok=%v idx=%d", appHeaderHeight+1, ok, idx)
	}
}

func TestFilesClickOffsetSelectionHint(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.go", "b.go", "c.go"} {
		os.WriteFile(filepath.Join(dir, name), []byte("content\n"), 0644)
	}

	m := newFilesModel(dir)
	m.Resize(100, 30)
	// Select one file to trigger selectionHint
	m.selectedFiles = map[int]bool{0: true}
	m.panel = filesPanelPicker

	hintText := m.selectionHint()
	treeW := 100 * 35 / 100
	styles := ApplyThemeColors("tokyonight")
	// selectionHint is now clamped to MaxHeight(1) — always exactly one row.
	hintLines := len(m.treeHeaderRows(treeW, styles))
	expectedTop := appHeaderHeight + 1 + hintLines

	fmt.Printf("selectionHint=%q  hintLines=%d  expectedTop=%d\n", hintText, hintLines, expectedTop)

	idx, ok := m.treeNodeForClick(tea.Mouse{X: 2, Y: expectedTop}, appHeaderHeight, styles)
	if !ok || idx != 0 {
		t.Errorf("selection hint: expected first node at Y=%d, got ok=%v idx=%d", expectedTop, ok, idx)
	}
}

// TestFilesClickOffsetNarrowScreen guards the original bug: on a narrow screen
// the hint text used to wrap differently in View() than the click math assumed,
// pushing every node's hit-box off by one. Now both derive the offset from the
// same treeHeaderRows (clamped to MaxHeight(1)), so the hint stays one row and
// the offset is stable at any width.
func TestFilesClickOffsetNarrowScreen(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.go", "b.go", "c.go"} {
		os.WriteFile(filepath.Join(dir, name), []byte("x\n"), 0644)
	}

	m := newFilesModel(dir)
	m.Resize(40, 20) // narrow: hint text is far wider than the tree pane
	styles := ApplyThemeColors("tokyonight")
	m.selectedFiles = map[int]bool{0: true}
	m.panel = filesPanelPicker

	treeW := 40 * 35 / 100
	if rows := len(m.treeHeaderRows(treeW, styles)); rows != 1 {
		t.Fatalf("narrow selection hint must stay 1 row, got %d", rows)
	}
	expectedTop := appHeaderHeight + 1 + 1
	idx, ok := m.treeNodeForClick(tea.Mouse{X: 1, Y: expectedTop}, appHeaderHeight, styles)
	if !ok || idx != 0 {
		t.Errorf("narrow screen: expected first node at Y=%d, got ok=%v idx=%d", expectedTop, ok, idx)
	}
}

// TestFilesClickOffsetAfterScroll guards the regression where clicking a row
// after scrolling the tree down selected the wrong node: the hit-test ignored
// the persisted vertical scroll offset, so the first visible row always mapped
// to node 0 instead of node treeScrollY.
func TestFilesClickOffsetAfterScroll(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 40; i++ {
		os.WriteFile(filepath.Join(dir, fmt.Sprintf("file_%02d.go", i)), []byte("x\n"), 0644)
	}
	m := newFilesModel(dir)
	m.Resize(100, 30)
	m.panel = filesPanelPreview // no hint rows: offset is purely the border
	styles := ApplyThemeColors("tokyonight")

	// Establish the tree ListBox's size/count the way a real render would
	// before any scroll event arrives (View() runs before the user can
	// interact, so this always happens in practice).
	m.reconcileTreeScroll(100, 30)

	// Scroll down by 10 rows (wheel-style: offset only, cursor unchanged).
	m.tree.SetScrollOffset(10)
	m.reconcileTreeScroll(100, 30)
	if m.tree.ScrollOffset() != 10 {
		t.Fatalf("expected scroll to persist at 10, got %d", m.tree.ScrollOffset())
	}

	top := appHeaderHeight + 1 + m.treeHeaderRowCount()
	// The first visible row must now map to node 10, not node 0.
	idx, ok := m.treeNodeForClick(tea.Mouse{X: 2, Y: top}, appHeaderHeight, styles)
	if !ok || idx != 10 {
		t.Fatalf("first visible row after scroll: expected node 10, got ok=%v idx=%d", ok, idx)
	}
	idx2, ok2 := m.treeNodeForClick(tea.Mouse{X: 2, Y: top + 3}, appHeaderHeight, styles)
	if !ok2 || idx2 != 13 {
		t.Fatalf("4th visible row after scroll: expected node 13, got ok=%v idx=%d", ok2, idx2)
	}
}

// TestTreeHeaderRowCountMatchesRows pins the invariant that the style-free
// treeHeaderRowCount (used by scroll reconcile + click hit-test) always equals
// the number of rows treeHeaderRows actually renders, across the relevant
// states. If these drift, hit-boxes shift by the difference.
func TestTreeHeaderRowCountMatchesRows(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.go", "b.go"} {
		os.WriteFile(filepath.Join(dir, name), []byte("x\n"), 0644)
	}
	styles := ApplyThemeColors("tokyonight")
	treeW := 100 * 35 / 100

	cases := []struct {
		name  string
		setup func(m *filesModel)
	}{
		{"picker-normal", func(m *filesModel) { m.panel = filesPanelPicker; m.mode = filesModeNormal }},
		{"preview-focused", func(m *filesModel) { m.panel = filesPanelPreview }},
		{"selection-active", func(m *filesModel) { m.panel = filesPanelPicker; m.selectedFiles = map[int]bool{0: true} }},
	}
	for _, tc := range cases {
		m := newFilesModel(dir)
		m.Resize(100, 30)
		tc.setup(&m)
		want := len(m.treeHeaderRows(treeW, styles))
		if got := m.treeHeaderRowCount(); got != want {
			t.Errorf("%s: treeHeaderRowCount=%d but treeHeaderRows rendered %d rows", tc.name, got, want)
		}
	}
}
