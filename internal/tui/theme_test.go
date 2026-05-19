package tui

import (
	"reflect"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestApplyThemeColorsUpdatesScrollbarStyles(t *testing.T) {
	ApplyThemeColors("opencode")

	if got := selectedStyle.GetForeground(); !reflect.DeepEqual(got, lipgloss.Color("#000000")) {
		t.Fatalf("expected selected foreground to use theme selected foreground, got %q", got)
	}
	if got := selectedStyle.GetBackground(); !reflect.DeepEqual(got, lipgloss.Color("#00ff00")) {
		t.Fatalf("expected selected background to use theme selected background, got %q", got)
	}
	if got := errorStyle.GetForeground(); !reflect.DeepEqual(got, lipgloss.Color("#ff0000")) {
		t.Fatalf("expected error style to use theme error color, got %q", got)
	}
	if got := scrollbarTrackStyle.GetForeground(); !reflect.DeepEqual(got, lipgloss.Color("#444444")) {
		t.Fatalf("expected scrollbar track to use theme dim color, got %q", got)
	}
	if got := scrollbarThumbStyle.GetForeground(); !reflect.DeepEqual(got, lipgloss.Color("#00ff00")) {
		t.Fatalf("expected scrollbar thumb to use theme selected background color, got %q", got)
	}
}
