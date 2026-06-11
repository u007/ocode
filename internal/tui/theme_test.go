package tui

import (
	"reflect"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/u007/ocode/internal/theme"
)

// TestRegistryParityWithThemePackage guards against the tui registry and the
// shared internal/theme registry drifting apart — the exact gap that let
// /api/theme miss the 58 generated themes. Every name the tui exposes must
// resolve identically through theme.Get.
func TestRegistryParityWithThemePackage(t *testing.T) {
	tuiNames := AvailableThemes()
	themeNames := theme.AvailableThemes()
	if !reflect.DeepEqual(tuiNames, themeNames) {
		t.Fatalf("tui and theme registries disagree on names:\n  tui:   %d\n  theme: %d", len(tuiNames), len(themeNames))
	}
	for _, name := range tuiNames {
		tuiDef, ok := GetTheme(name)
		if !ok {
			t.Errorf("tui GetTheme(%q) returned false for a listed theme", name)
			continue
		}
		if got := theme.Get(name); !reflect.DeepEqual(got, tuiDef) {
			t.Errorf("theme %q differs between tui and theme.Get", name)
		}
	}
}

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

func TestGeneratedThemesHaveNonEmptyColors(t *testing.T) {
	for _, name := range AvailableThemes() {
		theme, ok := GetTheme(name)
		if !ok {
			t.Errorf("theme %q registered but GetTheme returns false", name)
			continue
		}
		c := theme.Colors
		if strings.TrimSpace(c.User) == "" {
			t.Errorf("theme %q has empty User color", name)
		}
		if strings.TrimSpace(c.Assistant) == "" {
			t.Errorf("theme %q has empty Assistant color", name)
		}
		if strings.TrimSpace(c.Background) == "" {
			t.Errorf("theme %q has empty Background color", name)
		}
		if strings.TrimSpace(c.Text) == "" {
			t.Errorf("theme %q has empty Text color", name)
		}
		if strings.TrimSpace(c.Error) == "" {
			t.Errorf("theme %q has empty Error color", name)
		}
	}
}

func TestLightVariantsPresent(t *testing.T) {
	// Spot-check a few key themes that should have light variants
	type pair struct{ dark, light string }
	for _, p := range []pair{
		{"amoled", "amoled-light"},
		{"aura", "aura-light"},
		{"catppuccin", "catppuccin-light"},
		{"rosepine", "rosepine-light"},
		{"synthwave84", "synthwave84-light"},
		{"zenburn", "zenburn-light"},
	} {
		if _, ok := GetTheme(p.dark); !ok {
			t.Errorf("expected dark theme %q to exist", p.dark)
		}
		if _, ok := GetTheme(p.light); !ok {
			t.Errorf("expected light theme %q to exist (dark exists)", p.light)
		}
		// Light should have different background than dark
		darkDef, _ := GetTheme(p.dark)
		lightDef, _ := GetTheme(p.light)
		dark := darkDef.Colors
		light := lightDef.Colors
		if dark.Background == light.Background {
			t.Errorf("%q and %q have same Background (%s), expected different for light/dark", p.dark, p.light, dark.Background)
		}
	}
}
