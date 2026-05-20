package tui

import (
	"reflect"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestApplyThemeColorsUpdatesScrollbarStyles(t *testing.T) {
	ApplyThemeColors("opencode")

	if got := selectedStyle.GetForeground(); !reflect.DeepEqual(got, lipgloss.Color("#0A0A0A")) {
		t.Fatalf("expected selected foreground to use theme selected foreground, got %q", got)
	}
	if got := selectedStyle.GetBackground(); !reflect.DeepEqual(got, lipgloss.Color("#FAB283")) {
		t.Fatalf("expected selected background to use theme selected background, got %q", got)
	}
	if got := errorStyle.GetForeground(); !reflect.DeepEqual(got, lipgloss.Color("#E06C75")) {
		t.Fatalf("expected error style to use theme error color, got %q", got)
	}
	if got := scrollbarTrackStyle.GetForeground(); !reflect.DeepEqual(got, lipgloss.Color("#3C3C3C")) {
		t.Fatalf("expected scrollbar track to use theme dim color, got %q", got)
	}
	if got := scrollbarThumbStyle.GetForeground(); !reflect.DeepEqual(got, lipgloss.Color("#FAB283")) {
		t.Fatalf("expected scrollbar thumb to use theme selected background color, got %q", got)
	}
}

func TestAvailableThemesIncludesOpencodeFlexoki(t *testing.T) {
	found := false
	for _, name := range AvailableThemes() {
		if name == "flexoki" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected flexoki in ocode theme list")
	}

	if _, ok := GetTheme("flexoki"); !ok {
		t.Fatalf("expected flexoki theme to resolve")
	}
	if _, ok := GetTheme("one-dark"); !ok {
		t.Fatalf("expected opencode one-dark theme to resolve")
	}
	if _, ok := GetTheme("onedark"); !ok {
		t.Fatalf("expected legacy onedark alias to resolve")
	}
}
