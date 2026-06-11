package tui

import (
	"testing"
)

func TestHoverPickerRow(t *testing.T) {
	m := model{
		showPicker:     true,
		pickerItems:    []string{"Alpha", "Beta", "Gamma"},
		pickerValues:   []string{"a", "b", "c"},
		pickerIndex:    0,
		width:          80,
		height:         24,
		hoverPickerIdx: -1,
		styles:         ApplyThemeColors("tokyonight"),
		input:          newTestTextarea(),
		scrollSpeed:    3,
	}
	m.layout()

	pickerTopY := 3
	mouse := mockMouseMsg{x: 10, y: pickerTopY, btn: 0}
	result, _, changed := m.handleMouseMotion(mouse.Mouse())

	resultModel := result.(model)
	if resultModel.hoverPickerIdx != 0 {
		t.Errorf("expected hoverPickerIdx 0, got %d", resultModel.hoverPickerIdx)
	}
	if !changed {
		t.Error("expected redraw on hover change")
	}
}

func TestHoverPickerNoRedrawSameTarget(t *testing.T) {
	m := model{
		showPicker:     true,
		pickerItems:    []string{"Alpha", "Beta", "Gamma"},
		pickerValues:   []string{"a", "b", "c"},
		pickerIndex:    0,
		width:          80,
		height:         24,
		hoverPickerIdx: -1,
		styles:         ApplyThemeColors("tokyonight"),
		input:          newTestTextarea(),
		scrollSpeed:    3,
	}
	m.layout()

	pickerTopY := 3
	mouse := mockMouseMsg{x: 10, y: pickerTopY, btn: 0}

	result1, _, changed1 := m.handleMouseMotion(mouse.Mouse())
	if !changed1 {
		t.Error("expected redraw on first hover")
	}

	// Use the returned model for the second call
	result2, _, changed2 := result1.(model).handleMouseMotion(mouse.Mouse())
	_ = result2
	if changed2 {
		t.Error("no redraw expected when hovering same target")
	}
}

func TestHoverPickerClearsOnExit(t *testing.T) {
	m := model{
		showPicker:     true,
		pickerItems:    []string{"Alpha", "Beta"},
		pickerValues:   []string{"a", "b"},
		width:          80,
		height:         24,
		hoverPickerIdx: -1,
		styles:         ApplyThemeColors("tokyonight"),
		input:          newTestTextarea(),
		scrollSpeed:    3,
	}
	m.layout()

	pickerTopY := 3
	mouse := mockMouseMsg{x: 10, y: pickerTopY, btn: 0}
	result, _, _ := m.handleMouseMotion(mouse.Mouse())
	resultModel := result.(model)
	if resultModel.hoverPickerIdx < 0 {
		t.Fatal("expected picker hover")
	}

	mouse2 := mockMouseMsg{x: 10, y: 0, btn: 0}
	result2, _, _ := resultModel.handleMouseMotion(mouse2.Mouse())
	resultModel2 := result2.(model)
	if resultModel2.hoverPickerIdx != -1 {
		t.Errorf("expected hoverPickerIdx -1 after moving away, got %d", resultModel2.hoverPickerIdx)
	}
}
