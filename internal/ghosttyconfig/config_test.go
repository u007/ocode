package ghosttyconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// writeFile writes one file under dir, creating parents, and returns its path.
func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// testOptions builds Options rooted at a temp dir; never touches the real HOME.
func testOptions(t *testing.T) (Options, string) {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	xdg := filepath.Join(home, ".config")
	resources := filepath.Join(root, "ghostty-resources")
	if err := os.MkdirAll(xdg, 0o755); err != nil {
		t.Fatalf("mkdir xdg: %v", err)
	}
	return Options{
		Home:          home,
		GOOS:          "darwin",
		XDGConfigHome: xdg,
		ResourcesDirs: []string{resources},
	}, root
}

func TestLoadNoConfig(t *testing.T) {
	opts, _ := testOptions(t)
	cfg := LoadWith(opts)
	if cfg.Found {
		t.Fatalf("Found = true with no config files; paths = %v", cfg.Paths)
	}
	if cfg.FontFamily != "" || cfg.FontSize != 0 {
		t.Fatalf("unexpected appearance from empty config: %+v", cfg)
	}
}

// Ghostty loads XDG files first and the macOS app-support files AFTER them,
// so a value in the app-support file wins.
func TestLoadOrderMacOSAppSupportOverridesXDG(t *testing.T) {
	opts, root := testOptions(t)
	writeFile(t, filepath.Join(opts.Home, ".config", "ghostty"), "config", "font-size = 11\nfont-family = Fira Code\n")
	writeFile(t, root, "home/Library/Application Support/com.mitchellh.ghostty/config", "font-size = 13\ntheme = Bright Lights\n")
	writeFile(t, filepath.Join(root, "ghostty-resources", "themes"), "Bright Lights", "background = #191919\nforeground = #b3c9d7\npalette = 1=#ff355b\n")

	cfg := LoadWith(opts)
	if !cfg.Found {
		t.Fatal("Found = false")
	}
	if cfg.FontSize != 13 {
		t.Errorf("FontSize = %v, want 13 (app-support wins)", cfg.FontSize)
	}
	if cfg.FontFamily != `"Fira Code", ui-monospace, SFMono-Regular, Menlo, monospace` {
		t.Errorf("FontFamily = %q", cfg.FontFamily)
	}
	if cfg.ThemeName != "Bright Lights" {
		t.Errorf("ThemeName = %q", cfg.ThemeName)
	}
	if cfg.Dark == nil || cfg.Dark.Background != "#191919" {
		t.Fatalf("Dark theme = %+v", cfg.Dark)
	}
	if cfg.Dark.Palette[1] != "#ff355b" {
		t.Errorf("palette[1] = %q", cfg.Dark.Palette[1])
	}
	// Single (non-split) theme applies to both appearances.
	if cfg.Light == nil || cfg.Light.Background != "#191919" {
		t.Errorf("Light theme = %+v", cfg.Light)
	}
}

func TestParseSyntaxQuotesCommentsAndReset(t *testing.T) {
	opts, _ := testOptions(t)
	writeFile(t, filepath.Join(opts.XDGConfigHome, "ghostty"), "config", strings.Join([]string{
		"# a comment line",
		"",
		`font-family = "JetBrains Mono"`,
		"font-size = 15",
		"background = #112233",
		"# font-size = 99   (commented out, must not apply)",
		"font-size =",
		"background =",
	}, "\n"))

	cfg := LoadWith(opts)
	if cfg.FontFamily != `"JetBrains Mono", ui-monospace, SFMono-Regular, Menlo, monospace` {
		t.Errorf("FontFamily = %q", cfg.FontFamily)
	}
	if cfg.FontSize != 0 {
		t.Errorf("FontSize = %v, want 0 after empty value reset", cfg.FontSize)
	}
	if cfg.Dark != nil {
		t.Errorf("Dark = %+v, want nil after background reset and no theme", cfg.Dark)
	}
}

func TestCellAdjustParsesPercentAndPixels(t *testing.T) {
	opts, _ := testOptions(t)
	writeFile(t, filepath.Join(opts.XDGConfigHome, "ghostty"), "config", "adjust-cell-height = 10%\nadjust-cell-width = 2\n")
	cfg := LoadWith(opts)
	if cfg.CellHeight != (CellAdjust{Percent: 10}) {
		t.Errorf("CellHeight = %+v", cfg.CellHeight)
	}
	if cfg.CellWidth != (CellAdjust{Px: 2}) {
		t.Errorf("CellWidth = %+v", cfg.CellWidth)
	}
}

func TestPaletteAboveFifteenIsReportedNotApplied(t *testing.T) {
	opts, _ := testOptions(t)
	writeFile(t, filepath.Join(opts.XDGConfigHome, "ghostty"), "config", "palette = 3=#AABBCC\npalette = 200=#ff0000\n")
	cfg := LoadWith(opts)
	if cfg.Dark == nil || cfg.Dark.Palette[3] != "#aabbcc" {
		t.Fatalf("palette[3] not normalised: %+v", cfg.Dark)
	}
	if len(cfg.Ignored) == 0 || cfg.Ignored[0].Key != "palette" || !strings.Contains(cfg.Ignored[0].Reason, "0-15") {
		t.Fatalf("Ignored = %+v, want a 0-15 note", cfg.Ignored)
	}
}

func TestExplicitColoursOverrideTheme(t *testing.T) {
	opts, root := testOptions(t)
	writeFile(t, filepath.Join(root, "ghostty-resources", "themes"), "Base", "background = #101010\nforeground = #eeeeee\npalette = 2=#00ff00\n")
	writeFile(t, filepath.Join(opts.XDGConfigHome, "ghostty"), "config", "theme = Base\nbackground = #000000\npalette = 4=#0000ff\n")

	cfg := LoadWith(opts)
	dark := cfg.Dark
	if dark == nil {
		t.Fatal("Dark = nil")
	}
	if dark.Background != "#000000" {
		t.Errorf("Background = %q, want the explicit #000000", dark.Background)
	}
	if dark.Foreground != "#eeeeee" {
		t.Errorf("Foreground = %q, want the theme value", dark.Foreground)
	}
	if dark.Palette[2] != "#00ff00" || dark.Palette[4] != "#0000ff" {
		t.Errorf("Palette = %v", dark.Palette)
	}
	// Derived from fg/bg so ghostty-web's VS Code defaults never leak through.
	if dark.Cursor != "#eeeeee" || dark.CursorText != "#000000" {
		t.Errorf("cursor = %q/%q", dark.Cursor, dark.CursorText)
	}
	if dark.SelectionBackground != "#eeeeee" || dark.SelectionForeground != "#000000" {
		t.Errorf("selection = %q/%q", dark.SelectionBackground, dark.SelectionForeground)
	}
}

func TestSplitThemeSpec(t *testing.T) {
	light, dark, split, ok := splitThemeSpec("light:Rose Pine Dawn,dark:Rose Pine")
	if !ok || !split || light != "Rose Pine Dawn" || dark != "Rose Pine" {
		t.Fatalf("got %q/%q split=%v ok=%v", light, dark, split, ok)
	}
	light, dark, split, ok = splitThemeSpec("Bright Lights")
	if !ok || split || light != "Bright Lights" || dark != "Bright Lights" {
		t.Fatalf("single theme: %q/%q split=%v ok=%v", light, dark, split, ok)
	}
	if _, _, _, ok := splitThemeSpec("dark:Only Dark"); ok {
		t.Fatal("a half-specified split theme must be rejected")
	}
}

func TestThemeNameWithPathSeparatorRejected(t *testing.T) {
	opts, _ := testOptions(t)
	writeFile(t, filepath.Join(opts.XDGConfigHome, "ghostty"), "config", "theme = ../secret\n")
	cfg := LoadWith(opts)
	if len(cfg.Errors) == 0 || !strings.Contains(cfg.Errors[0], "path separators") {
		t.Fatalf("Errors = %v, want a path-separator rejection", cfg.Errors)
	}
	if cfg.Dark != nil {
		t.Fatalf("Dark = %+v, want nil", cfg.Dark)
	}
}

func TestConfigFileIncludes(t *testing.T) {
	opts, _ := testOptions(t)
	dir := filepath.Join(opts.XDGConfigHome, "ghostty")
	writeFile(t, dir, "sub/colors", "background = #222222\n")
	writeFile(t, dir, "config", "font-size = 14\nconfig-file = sub/colors\nconfig-file = ?does-not-exist\nfont-size = 10\n")

	cfg := LoadWith(opts)
	if cfg.FontSize != 10 {
		t.Errorf("FontSize = %v, want 10", cfg.FontSize)
	}
	// The include is processed AFTER the file body, so its colour is the only
	// background and the missing optional include is not an error.
	if cfg.Dark == nil || cfg.Dark.Background != "#222222" {
		t.Fatalf("Dark = %+v", cfg.Dark)
	}
	if len(cfg.Errors) != 0 {
		t.Errorf("Errors = %v, want none", cfg.Errors)
	}
}

func TestConfigFileIncludeCycleIsReported(t *testing.T) {
	opts, _ := testOptions(t)
	dir := filepath.Join(opts.XDGConfigHome, "ghostty")
	writeFile(t, dir, "a", "config-file = b\n")
	writeFile(t, dir, "b", "config-file = a\n")
	writeFile(t, dir, "config", "config-file = a\n")
	cfg := LoadWith(opts)
	if len(cfg.Errors) == 0 || !strings.Contains(strings.Join(cfg.Errors, " "), "cycle") {
		t.Fatalf("Errors = %v, want a cycle report", cfg.Errors)
	}
}

func TestUnsupportedKeysAreReportedWithReasons(t *testing.T) {
	opts, _ := testOptions(t)
	writeFile(t, filepath.Join(opts.XDGConfigHome, "ghostty"), "config", strings.Join([]string{
		"background-opacity = 0.9",
		"window-padding-x = 8",
		"font-feature = calt",
		"shell-integration-features = ssh-terminfo",
		"custom-shader = shader.glsl",
		"font-family-bold = Fira Code Bold",
		"window-theme = dark",
	}, "\n"))

	cfg := LoadWith(opts)
	reasons := map[string]string{}
	for _, item := range cfg.Ignored {
		reasons[item.Key] = item.Reason
	}
	for key, want := range map[string]string{
		"background-opacity":         "opaque",
		"window-padding-x":           "panel layout",
		"font-feature":               "OpenType",
		"shell-integration-features": "server-side",
		"custom-shader":              "GLSL",
		"font-family-bold":           "one family",
	} {
		if !strings.Contains(reasons[key], want) {
			t.Errorf("reason for %q = %q, want it to contain %q", key, reasons[key], want)
		}
	}
}

func TestKeybindParsing(t *testing.T) {
	opts, _ := testOptions(t)
	writeFile(t, filepath.Join(opts.XDGConfigHome, "ghostty"), "config", strings.Join([]string{
		"keybind = cmd+shift+t=new_tab",
		"keybind = ctrl+plus=increase_font_size:2",
		"keybind = cmd+k=unbind",
		"keybind = global:ctrl+grave_accent=toggle_quick_terminal",
		"keybind = cmd+shift+t=close_surface",
	}, "\n"))

	cfg := LoadWith(opts)
	if len(cfg.Keybinds) != 4 {
		t.Fatalf("Keybinds = %+v, want 4 (later binding for one chord replaces it)", cfg.Keybinds)
	}
	byChord := map[string]Keybind{}
	for _, kb := range cfg.Keybinds {
		byChord[kb.Chord] = kb
	}
	if got := byChord["cmd+shift+t"]; got.Action != "close_surface" || got.Unbound {
		t.Errorf("cmd+shift+t = %+v, want the later close_surface", got)
	}
	if got := byChord["ctrl+plus"]; got.Action != "increase_font_size" || got.Arg != "2" {
		t.Errorf("ctrl+plus = %+v", got)
	}
	if got := byChord["cmd+k"]; !got.Unbound {
		t.Errorf("cmd+k = %+v, want unbound", got)
	}
	if got := byChord["ctrl+grave_accent"]; !got.Global {
		t.Errorf("global bind lost its Global flag: %+v", got)
	}
}

func TestNormalizeFontFamily(t *testing.T) {
	cases := map[string]string{
		"":                          "",
		"Fira Code":                 `"Fira Code", ui-monospace, SFMono-Regular, Menlo, monospace`,
		"JetBrains Mono, monospace": `"JetBrains Mono", monospace`,
		`"Fira Code"`:               `"Fira Code", ui-monospace, SFMono-Regular, Menlo, monospace`,
		"Menlo":                     `Menlo, ui-monospace, SFMono-Regular, Menlo, monospace`,
	}
	for in, want := range cases {
		if got := normalizeFontFamily(in); got != want {
			t.Errorf("normalizeFontFamily(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFontSizeClamped(t *testing.T) {
	if got := clampFontSize(2); got != MinFontSize {
		t.Errorf("clampFontSize(2) = %v, want %v", got, MinFontSize)
	}
	if got := clampFontSize(80); got != MaxFontSize {
		t.Errorf("clampFontSize(80) = %v, want %v", got, MaxFontSize)
	}
	if got := clampFontSize(13.5); got != 13.5 {
		t.Errorf("clampFontSize(13.5) = %v", got)
	}
}

// Sizes between the old floor (6) and the new one must reach the appearance
// block unchanged; a floor above them silently re-sized the user's Ghostty font.
func TestGhosttyFontSizeFloorIsHonoured(t *testing.T) {
	opts, _ := testOptions(t)
	writeFile(t, filepath.Join(opts.XDGConfigHome, "ghostty"), "config", "font-size = 7\n")

	if cfg := LoadWith(opts); cfg.FontSize != 7 {
		t.Errorf("font-size = 7 resolved to %v, want 7", cfg.FontSize)
	}
	writeFile(t, filepath.Join(opts.XDGConfigHome, "ghostty"), "config", "font-size = 6\n")
	if cfg := LoadWith(opts); cfg.FontSize != 6 {
		t.Errorf("font-size = 6 resolved to %v, want 6", cfg.FontSize)
	}
	writeFile(t, filepath.Join(opts.XDGConfigHome, "ghostty"), "config", "font-size = 2\n")
	if cfg := LoadWith(opts); cfg.FontSize != MinFontSize {
		t.Errorf("font-size = 2 resolved to %v, want the floor %v", cfg.FontSize, MinFontSize)
	}
}

// Ghostty defines an empty value as "reset to default". A theme's colour must
// not survive an explicit reset, or the user cannot undo a theme colour.
func TestEmptyColourResetsTheThemeValue(t *testing.T) {
	opts, _ := testOptions(t)
	writeFile(t, filepath.Join(opts.XDGConfigHome, "ghostty", "themes"), "reset-me",
		"background = #123456\nforeground = #abcdef\n")
	writeFile(t, opts.XDGConfigHome, "ghostty/config",
		"theme = reset-me\nbackground =\n")

	cfg := LoadWith(opts)
	if cfg.Dark == nil {
		t.Fatalf("expected a resolved dark palette, errors=%v", cfg.Errors)
	}
	if cfg.Dark.Background != "" {
		t.Errorf("an explicit empty value must clear the theme's background, got %q", cfg.Dark.Background)
	}
	if cfg.Dark.Foreground != "#abcdef" {
		t.Errorf("an unreset colour must keep the theme's value, got %q", cfg.Dark.Foreground)
	}
}

// A line without "=" is reported rather than skipped in silence: a typo like
// `font-size 12` is otherwise indistinguishable from an unknown key.
func TestMalformedLineIsReported(t *testing.T) {
	opts, _ := testOptions(t)
	writeFile(t, opts.XDGConfigHome, "ghostty/config", "font-size 12\n")

	cfg := LoadWith(opts)
	for _, entry := range cfg.Errors {
		if strings.Contains(entry, "malformed line") {
			return
		}
	}
	t.Fatalf("a malformed line must be reported, errors=%v", cfg.Errors)
}

// Found means "a config was read", not "a path existed". An unreadable config
// otherwise wins the whole appearance block with every field at its built-in
// default, silently discarding the user's ocode font/size/theme.
func TestLoadRequiresAReadableConfigForFound(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not honoured")
	}
	if os.Geteuid() == 0 {
		t.Skip("root can read a 0o000 file, so the read cannot fail")
	}
	opts, _ := testOptions(t)
	dir := filepath.Join(opts.XDGConfigHome, "ghostty")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config")
	if err := os.WriteFile(path, []byte("font-size = 15\n"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

	cfg := LoadWith(opts)
	if cfg.Found {
		t.Fatalf("Found must require a successful read; got paths=%v errors=%v", cfg.Paths, cfg.Errors)
	}
	if len(cfg.Errors) == 0 {
		t.Fatal("expected the read failure to be reported in Errors")
	}
	if cfg.FontSize != 0 {
		t.Fatalf("an unreadable config must not supply values, got font size %v", cfg.FontSize)
	}
}

// NaN/Inf parse as valid float64s. Left alone they reach the API response,
// json.Marshal fails, and writeJSON has already committed a 200 — so the client
// receives an empty body instead of the terminal settings.
func TestLoadIgnoresNonFiniteNumbers(t *testing.T) {
	opts, _ := testOptions(t)
	writeFile(t, opts.XDGConfigHome, "ghostty/config",
		"font-size = NaN\n"+
			"adjust-cell-width = inf%\n"+
			"adjust-cell-height = +Inf\n")

	cfg := LoadWith(opts)
	if !cfg.Found {
		t.Fatalf("expected the config to be found, errors=%v", cfg.Errors)
	}
	if cfg.FontSize != 0 {
		t.Errorf("font-size = NaN must be ignored, got %v", cfg.FontSize)
	}
	if cfg.CellWidth.Percent != 0 || cfg.CellWidth.Px != 0 {
		t.Errorf("adjust-cell-width = inf%% must be ignored, got %+v", cfg.CellWidth)
	}
	if cfg.CellHeight.Percent != 0 || cfg.CellHeight.Px != 0 {
		t.Errorf("adjust-cell-height = +Inf must be ignored, got %+v", cfg.CellHeight)
	}

	ignored := map[string]bool{}
	for _, entry := range cfg.Ignored {
		ignored[entry.Key] = true
	}
	for _, key := range []string{"font-size", "adjust-cell-width", "adjust-cell-height"} {
		if !ignored[key] {
			t.Errorf("%s = a non-finite number must be reported in Ignored, got %+v", key, cfg.Ignored)
		}
	}

	// The user-visible invariant: the resolved config must be serialisable.
	if _, err := json.Marshal(cfg); err != nil {
		t.Fatalf("resolved config must marshal (a NaN/Inf makes the API answer 200 with an empty body): %v", err)
	}
}
