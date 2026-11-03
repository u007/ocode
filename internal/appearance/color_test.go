package appearance

import (
	"os"
	"path/filepath"
	"testing"
)

// loadFixture decodes one of the testdata themes with the same JSONC decoder
// the loader uses, so the fixtures exercise the real parsing path.
func loadFixture(t *testing.T, name string) *RawTheme {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	var raw RawTheme
	if err := DecodeJSONC(data, &raw); err != nil {
		t.Fatalf("decode fixture %s: %v", name, err)
	}
	return &raw
}

func assertEq[T comparable](t *testing.T, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func assertRule(t *testing.T, rules []MonacoRule, token, foreground, fontStyle string) {
	t.Helper()
	for _, rule := range rules {
		if rule.Token != token {
			continue
		}
		if rule.Foreground != foreground {
			t.Errorf("rule %q foreground = %q, want %q", token, rule.Foreground, foreground)
		}
		if rule.FontStyle != fontStyle {
			t.Errorf("rule %q fontStyle = %q, want %q", token, rule.FontStyle, fontStyle)
		}
		return
	}
	t.Errorf("no Monaco rule for token %q", token)
}

func hasRule(rules []MonacoRule, token string) bool {
	for _, rule := range rules {
		if rule.Token == token {
			return true
		}
	}
	return false
}

func TestConvertCatppuccinMocha(t *testing.T) {
	got, err := ConvertTheme("vscode:test/mocha", "Catppuccin Mocha", loadFixture(t, "catppuccin-mocha.json"))
	if err != nil {
		t.Fatal(err)
	}
	assertEq(t, got.ID, "vscode:test/mocha")
	assertEq(t, got.Label, "Catppuccin Mocha")
	assertEq(t, got.Kind, Polarity("dark"))
	assertEq(t, got.UI["--background"], "#1e1e2e")
	assertEq(t, got.UI["--card"], "#181825")
	assertEq(t, got.UI["--ring"], "#cba6f7")
	assertEq(t, got.Monaco.Base, "vs-dark")
	assertEq(t, got.Monaco.Colors["editor.background"], "#1e1e2e")
	if got.Terminal.Dark == nil {
		t.Fatal("Terminal.Dark is nil for a dark theme")
	}
	assertEq(t, got.Terminal.Dark.Palette[1], "#f38ba8") // ansiRed
	assertEq(t, got.Terminal.Light, (*TerminalColors)(nil))
	assertEq(t, got.Colors.Accent, "#cba6f7")
	assertEq(t, got.Colors.Text, "#cdd6f4")
	assertEq(t, got.Colors.User, "#89b4fa")
	assertEq(t, got.Colors.SelectedFg, "#cdd6f4")
	assertEq(t, got.Colors.Thinking, "#9399b2")
	assertRule(t, got.Monaco.Rules, "comment", "#9399b2", "italic")
	assertRule(t, got.Monaco.Rules, "string", "#a6e3a1", "")
	if len(got.Warnings) != 0 {
		t.Errorf("a complete theme should convert without warnings, got %v", got.Warnings)
	}
}

func TestConvertLightThemePutsPaletteInLight(t *testing.T) {
	got, err := ConvertTheme("vscode:test/gruvbox-light", "Gruvbox Light", loadFixture(t, "gruvbox-light.json"))
	if err != nil {
		t.Fatal(err)
	}
	assertEq(t, got.Kind, Polarity("light"))
	assertEq(t, got.Monaco.Base, "vs")
	if got.Terminal.Light == nil {
		t.Fatal("Terminal.Light is nil for a light theme")
	}
	assertEq(t, got.Terminal.Dark, (*TerminalColors)(nil))
	assertEq(t, got.Terminal.Light.Palette[1], "#9d0006")
	assertEq(t, got.Terminal.Light.Background, "#fbf1c7")
	assertEq(t, got.UI["--background"], "#fbf1c7")
	assertEq(t, got.Colors.Accent, "#b57614")
	assertRule(t, got.Monaco.Rules, "comment", "#928374", "italic")
	assertRule(t, got.Monaco.Rules, "keyword", "#9d0006", "bold")
}

func TestConvertThemeWithoutColorsDerivesFromTokenColors(t *testing.T) {
	got, err := ConvertTheme("vscode:test/no-colors", "Token Rules Only", loadFixture(t, "no-colors.json"))
	if err != nil {
		t.Fatal(err)
	}
	assertEq(t, got.Kind, Polarity("dark"))
	assertEq(t, got.UI["--background"], "#000000")
	assertEq(t, got.UI["--foreground"], "#d4d4d4")
	assertEq(t, got.UI["--muted-foreground"], "#6a9955")
	if got.UI["--border"] == "" {
		t.Error("UI[--border] must be derived for a colors-less theme")
	}
	if len(got.UI) != 4 {
		t.Errorf("a colors-less theme emits exactly the four fallback variables, got %d: %v", len(got.UI), got.UI)
	}
	if len(got.Warnings) != 1 {
		t.Errorf("want exactly one warning, got %d: %v", len(got.Warnings), got.Warnings)
	}
	assertEq(t, got.Colors.Text, "#d4d4d4")
	assertEq(t, got.Colors.Background, "#000000")
	assertEq(t, got.Colors.Thinking, "#6a9955")
	if got.Terminal.Dark == nil || got.Terminal.Light != nil {
		t.Error("a dark colors-less theme must emit only the dark terminal block")
	}
	if !hasRule(got.Monaco.Rules, "comment") {
		t.Error("token rules must still convert without a colors block")
	}
}

func TestUIHasNoRepairedSourceColours(t *testing.T) {
	got, err := ConvertTheme("vscode:test/mocha", "Catppuccin Mocha", loadFixture(t, "catppuccin-mocha.json"))
	if err != nil {
		t.Fatal(err)
	}
	// Author values are verbatim, even when a role's pair would be repaired by
	// a synthesised-foreground rule. --muted-foreground keeps the theme's
	// descriptionForeground rather than being dimmed for contrast.
	assertEq(t, got.UI["--foreground"], "#cdd6f4")
	assertEq(t, got.UI["--background"], "#1e1e2e")
	assertEq(t, got.UI["--muted-foreground"], "#cdd6f4")
	assertEq(t, got.UI["--accent-foreground"], "#cdd6f4")
}

func TestPolarityFromType(t *testing.T) {
	cases := []struct {
		name       string
		kind       string
		background string
		want       Polarity
	}{
		{"explicit dark", "dark", "#ffffff", PolarityDark},
		{"explicit light", "light", "#000000", PolarityLight},
		{"case and space", " Dark ", "#ffffff", PolarityDark},
		{"infer light", "", "#ffffff", PolarityLight},
		{"infer dark", "", "#000000", PolarityDark},
		{"infer light hex case", "", "#FBF1C7", PolarityLight},
		{"unknown type infers", "system", "#fbf1c7", PolarityLight},
		{"missing background defaults dark", "", "", PolarityDark},
		{"invalid background defaults dark", "", "not-a-colour", PolarityDark},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertEq(t, PolarityFromType(tc.kind, tc.background), tc.want)
		})
	}
}

func TestResolveTokenScopes(t *testing.T) {
	rules := []TokenRule{
		{Scope: scopeList{"comment.line.double-slash"}, Settings: TokenSettings{Foreground: "#111111"}},
		{Scope: scopeList{"comment"}, Settings: TokenSettings{Foreground: "#222222", FontStyle: "italic"}},
		{Scope: scopeList{"constant.numeric"}, Settings: TokenSettings{Foreground: "#333333"}},
		{Scope: scopeList{"keyword.operator"}, Settings: TokenSettings{Foreground: "#444444"}},
		{Scope: scopeList{"keyword"}, Settings: TokenSettings{Foreground: "#555555", FontStyle: "bold"}},
		{Scope: scopeList{"punctuation.definition.comment"}, Settings: TokenSettings{Foreground: "#666666"}},
		{Scope: scopeList{"unmatched.scope"}, Settings: TokenSettings{Foreground: "#777777"}},
		{Scope: scopeList{""}, Settings: TokenSettings{Foreground: "#888888"}},
	}

	got := ResolveTokenScopes(rules)

	assertRule(t, got, "comment", "#222222", "italic")
	assertRule(t, got, "number", "#333333", "")
	assertRule(t, got, "operator", "#444444", "")
	assertRule(t, got, "keyword", "#555555", "bold")
	assertRule(t, got, "delimiter", "#666666", "")
	for _, unexpected := range []string{"constant", "text", "variable"} {
		if hasRule(got, unexpected) {
			t.Errorf("unexpected rule for token %q", unexpected)
		}
	}
	if len(got) != 5 {
		t.Fatalf("want 5 rules, got %d: %v", len(got), got)
	}
	for i := 1; i < len(got); i++ {
		if got[i-1].Token > got[i].Token {
			t.Errorf("rules must be sorted by token, got %q before %q", got[i-1].Token, got[i].Token)
		}
	}
}

func TestResolveTokenScopesAccumulatesFontStyle(t *testing.T) {
	rules := []TokenRule{
		{Scope: scopeList{"entity.name.function"}, Settings: TokenSettings{FontStyle: "bold"}},
		{Scope: scopeList{"support.function"}, Settings: TokenSettings{Foreground: "#123456", FontStyle: "italic"}},
		{Scope: scopeList{"entity.name.function"}, Settings: TokenSettings{Foreground: "#654321"}},
	}
	got := ResolveTokenScopes(rules)
	// Both scopes map to "function"; the later foreground wins and the two
	// font styles accumulate.
	assertRule(t, got, "function", "#654321", "bold italic")
}

func TestResolveTokenScopesEmptyFontStyleDoesNotClear(t *testing.T) {
	rules := []TokenRule{
		{Scope: scopeList{"keyword"}, Settings: TokenSettings{Foreground: "#111111", FontStyle: "italic"}},
		{Scope: scopeList{"storage.modifier"}, Settings: TokenSettings{Foreground: "#222222", FontStyle: ""}},
	}
	got := ResolveTokenScopes(rules)
	assertRule(t, got, "keyword", "#222222", "italic")
}

func TestConvertThemeNilRaw(t *testing.T) {
	if _, err := ConvertTheme("vscode:test/nil", "Nil", nil); err == nil {
		t.Fatal("want an error for a nil theme")
	}
}

func TestConvertThemeOmitsUnmappedUIVariables(t *testing.T) {
	raw := &RawTheme{
		Type: "dark",
		Colors: map[string]string{
			"editor.background": "#101010",
			"editor.foreground": "#f0f0f0",
		},
	}
	got, err := ConvertTheme("vscode:test/minimal", "Minimal", raw)
	if err != nil {
		t.Fatal(err)
	}
	assertEq(t, got.UI["--background"], "#101010")
	assertEq(t, got.UI["--foreground"], "#f0f0f0")
	if _, ok := got.UI["--accent-foreground"]; ok {
		t.Error("--accent-foreground has no source key in this theme and must be omitted")
	}
	if _, ok := got.UI["--secondary"]; ok {
		t.Error("--secondary has no source key in this theme and must be omitted")
	}
}

func TestConvertThemeSynthesizesReadableForegrounds(t *testing.T) {
	raw := &RawTheme{
		Type: "dark",
		Colors: map[string]string{
			"editor.background": "#000000",
			"button.background": "#000000",
			"errorForeground":   "#00ff00",
			"input.background":  "#222222",
		},
	}
	got, err := ConvertTheme("vscode:test/synth", "Synth", raw)
	if err != nil {
		t.Fatal(err)
	}
	assertEq(t, got.UI["--primary-foreground"], "#ffffff")
	assertEq(t, got.UI["--destructive-foreground"], "#000000")
	if ratio := contrastRatio(got.UI["--secondary-foreground"], got.UI["--secondary"]); ratio < aaNormal {
		t.Errorf("secondary-foreground contrast = %.2f, want >= %.1f", ratio, aaNormal)
	}
}

func TestConvertThemePrefersAuthoredSecondaryForeground(t *testing.T) {
	raw := &RawTheme{
		Type: "dark",
		Colors: map[string]string{
			"editor.background": "#101010",
			"editor.foreground": "#f0f0f0",
			"input.background":  "#202020",
			"input.foreground":  "#abcdef",
		},
	}
	got, err := ConvertTheme("vscode:test/secondary", "Secondary", raw)
	if err != nil {
		t.Fatal(err)
	}
	assertEq(t, got.UI["--secondary-foreground"], "#abcdef")
}

func TestConvertThemeRepairsOnlyUnreadableLink(t *testing.T) {
	raw := &RawTheme{
		Type: "light",
		Colors: map[string]string{
			"editor.background":    "#ffffff",
			"editor.foreground":    "#111111",
			"list.hoverBackground": "#ffffff",
			"textLink.foreground":  "#dddddd",
		},
	}
	got, err := ConvertTheme("vscode:test/link", "Link", raw)
	if err != nil {
		t.Fatal(err)
	}
	link := got.UI["--link"]
	if link == "#dddddd" {
		t.Fatal("an unreadable link must be repaired")
	}
	if ratio := contrastRatio(link, got.UI["--background"]); ratio < aaNormal {
		t.Errorf("repaired link contrast on background = %.2f, want >= %.1f", ratio, aaNormal)
	}
	if ratio := contrastRatio(link, got.UI["--muted"]); ratio < aaNormal {
		t.Errorf("repaired link contrast on muted = %.2f, want >= %.1f", ratio, aaNormal)
	}

	// Failing only one of the two surfaces leaves the author value alone.
	raw.Colors["list.hoverBackground"] = "#000000"
	got, err = ConvertTheme("vscode:test/link2", "Link 2", raw)
	if err != nil {
		t.Fatal(err)
	}
	assertEq(t, got.UI["--link"], "#dddddd")
}

func TestConvertThemeFallsBackToXtermANSI(t *testing.T) {
	raw := &RawTheme{
		Type: "dark",
		Colors: map[string]string{
			"editor.background": "#101010",
			"editor.foreground": "#f0f0f0",
		},
	}
	got, err := ConvertTheme("vscode:test/ansi", "ANSI", raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Terminal.Dark == nil {
		t.Fatal("Terminal.Dark is nil")
	}
	assertEq(t, len(got.Terminal.Dark.Palette), 16)
	for i, want := range defaultANSI {
		assertEq(t, got.Terminal.Dark.Palette[i], want)
	}

	// A supplied slot is used verbatim and only the missing ones fall back.
	raw.Colors["terminal.ansiRed"] = "#ff1122"
	got, err = ConvertTheme("vscode:test/ansi2", "ANSI 2", raw)
	if err != nil {
		t.Fatal(err)
	}
	assertEq(t, got.Terminal.Dark.Palette[1], "#ff1122")
	assertEq(t, got.Terminal.Dark.Palette[2], defaultANSI[2])
}

func TestConvertThemeMonacoPassesThroughListedKeysOnly(t *testing.T) {
	raw := &RawTheme{
		Type: "dark",
		Colors: map[string]string{
			"editor.background":       "#101010",
			"editor.foreground":       "#f0f0f0",
			"editorCursor.foreground": "#ff0000",
			"badge.background":        "#00ff00", // not in the Monaco passthrough list
		},
	}
	got, err := ConvertTheme("vscode:test/monaco", "Monaco", raw)
	if err != nil {
		t.Fatal(err)
	}
	assertEq(t, got.Monaco.Colors["editor.background"], "#101010")
	assertEq(t, got.Monaco.Colors["editorCursor.foreground"], "#ff0000")
	if _, ok := got.Monaco.Colors["badge.background"]; ok {
		t.Error("unlisted VS Code key leaked into Monaco colors")
	}
	assertEq(t, got.Monaco.Base, "vs-dark")
}

func TestConvertThemeNoColorsUsesFirstRuleBackground(t *testing.T) {
	raw := &RawTheme{
		Type: "dark",
		TokenColors: tokenColors{Rules: []TokenRule{
			{Scope: scopeList{"text"}, Settings: TokenSettings{Foreground: "#cccccc"}},
			{Scope: scopeList{"string"}, Settings: TokenSettings{Background: "#101010"}},
		}},
	}
	got, err := ConvertTheme("vscode:test/rules-bg", "Rules BG", raw)
	if err != nil {
		t.Fatal(err)
	}
	assertEq(t, got.UI["--background"], "#101010")
	assertEq(t, got.UI["--foreground"], "#cccccc")

	// A light theme with no rule background falls back to white.
	raw.Type = "light"
	raw.TokenColors = tokenColors{Rules: []TokenRule{
		{Scope: scopeList{"text"}, Settings: TokenSettings{Foreground: "#111111"}},
	}}
	got, err = ConvertTheme("vscode:test/rules-bg-light", "Rules BG Light", raw)
	if err != nil {
		t.Fatal(err)
	}
	assertEq(t, got.Kind, PolarityLight)
	assertEq(t, got.UI["--background"], "#ffffff")
}
