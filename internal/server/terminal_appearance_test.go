package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/ghosttyconfig"
)

// stubGhosttyConfig replaces the Ghostty read for the duration of a test.
func stubGhosttyConfig(t *testing.T, cfg ghosttyconfig.Config) {
	t.Helper()
	prev := loadGhosttyConfig
	loadGhosttyConfig = func() ghosttyconfig.Config { return cfg }
	t.Cleanup(func() { loadGhosttyConfig = prev })
}

// ghosttyFixture is a minimal but realistic Ghostty config: a family, a size, a
// theme and one unmappable key.
func ghosttyFixture() ghosttyconfig.Config {
	return ghosttyconfig.Config{
		Found:      true,
		Paths:      []string{"/home/u/.config/ghostty/config"},
		FontFamily: `"Fira Code", ui-monospace, monospace`,
		FontSize:   12,
		CellHeight: ghosttyconfig.CellAdjust{Percent: 5},
		ThemeName:  "Bright Lights",
		Dark: &ghosttyconfig.ThemeColors{
			Foreground: "#b3c9d7",
			Background: "#191919",
		},
		Light: &ghosttyconfig.ThemeColors{
			Foreground: "#b3c9d7",
			Background: "#191919",
		},
		Keybinds: []ghosttyconfig.Keybind{{Chord: "cmd+shift+t", Action: "new_tab"}},
		Ignored:  []ghosttyconfig.Ignored{{Key: "background-opacity", Value: "0.9", Reason: "opaque"}},
	}
}

func TestResolveTerminalAppearanceGhostty(t *testing.T) {
	ocode := &config.OcodeConfig{TerminalAppearanceSource: "ghostty"}
	app := resolveTerminalAppearance(ocode, ghosttyFixture())
	if app.Source != "ghostty" || app.Requested != "ghostty" {
		t.Fatalf("source/requested = %q/%q", app.Source, app.Requested)
	}
	if app.FontFamily != `"Fira Code", ui-monospace, monospace` {
		t.Errorf("font family = %q", app.FontFamily)
	}
	if app.FontSize != 12 {
		t.Errorf("font size = %v", app.FontSize)
	}
	if app.CellHeight != (ghosttyconfig.CellAdjust{Percent: 5}) {
		t.Errorf("cell height = %+v", app.CellHeight)
	}
	if app.ThemeName != "Bright Lights" || app.Dark == nil || app.Dark.Background != "#191919" {
		t.Errorf("theme = %q / %+v", app.ThemeName, app.Dark)
	}
}

// A Ghostty config that sets only a theme must not inherit ocode's font
// settings: the chosen source owns every field, so unset ones use defaults.
func TestResolveTerminalAppearanceGhosttyDoesNotMixOcodeSettings(t *testing.T) {
	ocode := &config.OcodeConfig{
		TerminalAppearanceSource: "ghostty",
		TerminalFontFamily:       "Comic Sans",
		TerminalFontSize:         28,
	}
	app := resolveTerminalAppearance(ocode, ghosttyconfig.Config{Found: true})
	if app.Source != "ghostty" {
		t.Fatalf("source = %q", app.Source)
	}
	if app.FontFamily == "Comic Sans" || app.FontSize == 28 {
		t.Fatalf("ghostty source leaked ocode settings: %q %v", app.FontFamily, app.FontSize)
	}
	if app.FontSize != float64(config.DefaultTerminalFontSize) {
		t.Errorf("font size = %v, want the default", app.FontSize)
	}
	if app.FontFamily == "" {
		t.Error("font family is empty; the client would fall back silently")
	}
}

func TestResolveTerminalAppearanceOcode(t *testing.T) {
	ocode := &config.OcodeConfig{
		TerminalAppearanceSource:  "ocode",
		TerminalFontFamily:        "Menlo",
		TerminalFontSize:          15,
		TerminalCellWidthPercent:  100,
		TerminalCellHeightPercent: 120,
	}
	app := resolveTerminalAppearance(ocode, ghosttyFixture())
	if app.Source != "ocode" || app.Requested != "ocode" {
		t.Fatalf("source/requested = %q/%q", app.Source, app.Requested)
	}
	if app.FontFamily != "Menlo" || app.FontSize != 15 {
		t.Fatalf("font = %q %v", app.FontFamily, app.FontSize)
	}
	if app.CellWidth.Percent != 0 {
		t.Errorf("cell width = %+v, want 0 (100%% stored, 0 delta)", app.CellWidth)
	}
	if app.CellHeight.Percent != 20 {
		t.Errorf("cell height = %+v, want a 20%% delta", app.CellHeight)
	}
	if app.ThemeName != "" || app.Dark != nil {
		t.Errorf("ocode source must not borrow the Ghostty theme: %q %+v", app.ThemeName, app.Dark)
	}
}

// Requested "ghostty" with nothing installed must SAY it fell back, not pretend
// Ghostty answered.
func TestResolveTerminalAppearanceFallbackIsVisible(t *testing.T) {
	ocode := &config.OcodeConfig{
		TerminalAppearanceSource:  "ghostty",
		TerminalFontSize:          14,
		TerminalCellHeightPercent: 110,
	}
	app := resolveTerminalAppearance(ocode, ghosttyconfig.Config{Found: false})
	if app.Source != "ocode" {
		t.Fatalf("source = %q, want ocode", app.Source)
	}
	if app.Requested != "ghostty" {
		t.Fatalf("requested = %q, want the configured ghostty so the UI can explain", app.Requested)
	}
	if app.FontSize != 14 {
		t.Errorf("font size = %v, want the ocode setting", app.FontSize)
	}
	if app.CellHeight.Percent != 10 {
		t.Errorf("cell height = %+v, want a 10%% delta", app.CellHeight)
	}
}

func TestTerminalConfigLocalReportsAppearance(t *testing.T) {
	stubGhosttyConfig(t, ghosttyFixture())
	h := testConfigHandler(t)
	h.SetWorkDir(t.TempDir())
	h.mu.Lock()
	h.cfg.Ocode.TerminalAppearanceSource = "ghostty"
	h.mu.Unlock()

	w := httptest.NewRecorder()
	h.HandleGetTerminalConfig(w, httptest.NewRequest("GET", "/api/config/terminal", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var out struct {
		FontFamily string          `json:"font_family"`
		FontSize   float64         `json:"font_size"`
		Scope      string          `json:"appearance_scope"`
		Appearance json.RawMessage `json:"appearance"`
		Ghostty    *struct {
			Found   bool     `json:"found"`
			Paths   []string `json:"paths"`
			Ignored []struct {
				Key string `json:"key"`
			} `json:"ignored"`
			Keybinds []struct {
				Chord string `json:"chord"`
			} `json:"keybinds"`
		} `json:"ghostty"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Scope != "local" {
		t.Errorf("appearance_scope = %q", out.Scope)
	}
	if out.FontFamily != `"Fira Code", ui-monospace, monospace` || out.FontSize != 12 {
		t.Errorf("resolved font = %q %v", out.FontFamily, out.FontSize)
	}
	if out.Ghostty == nil || !out.Ghostty.Found {
		t.Fatalf("ghostty report = %+v", out.Ghostty)
	}
	if len(out.Ghostty.Paths) != 1 || len(out.Ghostty.Keybinds) != 1 || len(out.Ghostty.Ignored) != 1 {
		t.Errorf("ghostty report incomplete: %+v", out.Ghostty)
	}
	if len(out.Appearance) == 0 {
		t.Error("appearance block missing")
	}
}

func TestTerminalConfigRemoteDoesNotReportGhostty(t *testing.T) {
	restore := stubRemoteShellTransport(t, "shell=/bin/bash\navailable=/bin/bash\n")
	defer restore()
	stubGhosttyConfig(t, ghosttyFixture())

	project := t.TempDir()
	h := newTestHandlerWithRemote(t, "ci.local", project)
	code, out := getTerminalConfigHost(t, h, "ci.local", project)
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if out["appearance_scope"] != "remote" {
		t.Errorf("appearance_scope = %v", out["appearance_scope"])
	}
	if _, ok := out["ghostty"]; ok {
		t.Error("a remote response must not carry this machine's Ghostty report")
	}
	if _, ok := out["appearance"]; ok {
		t.Error("a remote response must not claim a resolved appearance")
	}
}

func TestHandleSetTerminalAppearancePersists(t *testing.T) {
	stubGhosttyConfig(t, ghosttyconfig.Config{Found: false})
	h := testConfigHandler(t)

	body := `{"appearance_source":"ocode","cell_height_percent":125,"cell_width_percent":105,"theme":"","font_family":"Menlo","font_size":15}`
	w := httptest.NewRecorder()
	h.HandleSetTerminalConfig(w, httptest.NewRequest("PUT", "/api/config/terminal", strings.NewReader(body)))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", w.Code, w.Body.String())
	}

	var cfg config.Config
	if err := config.LoadOcodeConfig(&cfg); err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if cfg.Ocode.TerminalAppearanceSource != "ocode" {
		t.Errorf("source = %q", cfg.Ocode.TerminalAppearanceSource)
	}
	if cfg.Ocode.TerminalCellHeightPercent != 125 || cfg.Ocode.TerminalCellWidthPercent != 105 {
		t.Errorf("cell percentages = %v/%v", cfg.Ocode.TerminalCellHeightPercent, cfg.Ocode.TerminalCellWidthPercent)
	}
	if cfg.Ocode.TerminalFontFamily != "Menlo" || cfg.Ocode.TerminalFontSize != 15 {
		t.Errorf("font = %q %v", cfg.Ocode.TerminalFontFamily, cfg.Ocode.TerminalFontSize)
	}

	var out struct {
		Appearance struct {
			Source     string  `json:"source"`
			FontFamily string  `json:"font_family"`
			FontSize   float64 `json:"font_size"`
			CellHeight struct {
				Percent float64 `json:"percent"`
			} `json:"cell_height_adjust"`
		} `json:"appearance"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Appearance.Source != "ocode" || out.Appearance.FontFamily != "Menlo" || out.Appearance.FontSize != 15 {
		t.Errorf("response appearance = %+v", out.Appearance)
	}
	if out.Appearance.CellHeight.Percent != 25 {
		t.Errorf("response cell height = %v, want a 25%% delta", out.Appearance.CellHeight.Percent)
	}
}

// A theme that does not resolve is reported, not silently replaced by ocode's
// palette — and it must not stop the rest of the appearance from resolving.
func TestResolveTerminalAppearanceReportsUnresolvableTheme(t *testing.T) {
	cfg := config.OcodeConfig{
		TerminalAppearanceSource: "ocode",
		TerminalFontFamily:       "Menlo",
		TerminalFontSize:         14,
		TerminalTheme:            "ocode-test-theme-that-does-not-exist-7f3a",
	}
	got := resolveTerminalAppearance(&cfg, ghosttyconfig.Config{Found: false})
	if len(got.Errors) == 0 {
		t.Fatal("an unresolved theme must be reported in appearance.errors")
	}
	if !strings.Contains(got.Errors[0], "ocode-test-theme-that-does-not-exist-7f3a") {
		t.Errorf("error must name the theme, got %q", got.Errors[0])
	}
	if got.Source != "ocode" || got.FontFamily != "Menlo" || got.FontSize != 14 {
		t.Errorf("the rest of the appearance must still resolve, got %+v", got)
	}
	if got.Settings.Theme != "ocode-test-theme-that-does-not-exist-7f3a" {
		t.Errorf("the configured theme must still be reported as stored, got %q", got.Settings.Theme)
	}
}

// A successful PUT must leave the handler's in-memory config equal to the file.
// The per-field path normalized the request a second time and left h.cfg
// untouched on any failure, so the two could disagree until restart.
func TestHandleSetTerminalConfigKeepsMemoryAndDiskInSync(t *testing.T) {
	stubGhosttyConfig(t, ghosttyconfig.Config{Found: false})
	h := testConfigHandler(t)

	fired := 0
	remove := config.OnConfigSaved.Add(func() { fired++ })
	defer remove()

	body := `{"appearance_source":"ocode","cell_height_percent":125,"font_family":"Menlo","font_size":15}`
	w := httptest.NewRecorder()
	h.HandleSetTerminalConfig(w, httptest.NewRequest("PUT", "/api/config/terminal", strings.NewReader(body)))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", w.Code, w.Body.String())
	}
	if fired != 1 {
		t.Errorf("OnConfigSaved fired %d times for one PUT, want 1", fired)
	}

	var disk config.Config
	if err := config.LoadOcodeConfig(&disk); err != nil {
		t.Fatalf("reload config: %v", err)
	}

	h.mu.Lock()
	live := h.cfg.Ocode
	h.mu.Unlock()

	fields := []struct {
		name     string
		mem, onD any
	}{
		{"scrollback", live.TerminalScrollbackLines, disk.Ocode.TerminalScrollbackLines},
		{"font family", live.TerminalFontFamily, disk.Ocode.TerminalFontFamily},
		{"font size", live.TerminalFontSize, disk.Ocode.TerminalFontSize},
		{"shell", live.TerminalShell, disk.Ocode.TerminalShell},
		{"source", live.TerminalAppearanceSource, disk.Ocode.TerminalAppearanceSource},
		{"cell width", live.TerminalCellWidthPercent, disk.Ocode.TerminalCellWidthPercent},
		{"cell height", live.TerminalCellHeightPercent, disk.Ocode.TerminalCellHeightPercent},
		{"theme", live.TerminalTheme, disk.Ocode.TerminalTheme},
	}
	for _, f := range fields {
		if f.mem != f.onD {
			t.Errorf("%s: memory %v != disk %v", f.name, f.mem, f.onD)
		}
	}
}

// One rendered property, one range: a size must not be accepted from one source
// and rejected by the other.
func TestFontSizeRangeAgreesWithGhosttyParser(t *testing.T) {
	if ghosttyconfig.MinFontSize != config.MinTerminalFontSize {
		t.Errorf("min font size drifted: ghosttyconfig %d, config %d", ghosttyconfig.MinFontSize, config.MinTerminalFontSize)
	}
	if ghosttyconfig.MaxFontSize != config.MaxTerminalFontSize {
		t.Errorf("max font size drifted: ghosttyconfig %d, config %d", ghosttyconfig.MaxFontSize, config.MaxTerminalFontSize)
	}
}

func TestHandleSetTerminalConfigRejectsEmptyBody(t *testing.T) {
	h := testConfigHandler(t)
	w := httptest.NewRecorder()
	h.HandleSetTerminalConfig(w, httptest.NewRequest("PUT", "/api/config/terminal", strings.NewReader(`{}`)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}
