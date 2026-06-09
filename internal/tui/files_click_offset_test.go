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

	// treeHint() always returns exactly 2 lines, so expectedTop is fixed.
	expectedTop := appHeaderHeight + 1 + 2

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
