package appearance

import (
	"errors"
	"math"
	"sort"
	"strings"

	"github.com/u007/ocode/internal/theme"
)

// aaNormal is the WCAG AA contrast ratio for normal-size text. Only
// synthesized foregrounds and an unreadable --link are adjusted; author
// colours are never repaired.
const aaNormal = 4.5

// defaultANSI is the xterm default palette in ANSI order. It is the fallback
// for any missing terminal.ansi* slot, so the renderer always receives 16
// colours. Kept as a local table rather than imported from internal/theme: a
// builtin TUI theme is a curated palette, not the terminal's default, and the
// conversion must not depend on whichever theme happens to be current.
var defaultANSI = [16]string{
	"#000000", // black
	"#cd0000", // red
	"#00cd00", // green
	"#cdcd00", // yellow
	"#0000ee", // blue
	"#cd00cd", // magenta
	"#00cdcd", // cyan
	"#e5e5e5", // white
	"#7f7f7f", // bright black
	"#ff0000", // bright red
	"#00ff00", // bright green
	"#ffff00", // bright yellow
	"#5c5cff", // bright blue
	"#ff00ff", // bright magenta
	"#00ffff", // bright cyan
	"#ffffff", // bright white
}

// terminalANSIKeys lists the VS Code keys for palette slots 0-15.
var terminalANSIKeys = [16]string{
	"terminal.ansiBlack",
	"terminal.ansiRed",
	"terminal.ansiGreen",
	"terminal.ansiYellow",
	"terminal.ansiBlue",
	"terminal.ansiMagenta",
	"terminal.ansiCyan",
	"terminal.ansiWhite",
	"terminal.ansiBrightBlack",
	"terminal.ansiBrightRed",
	"terminal.ansiBrightGreen",
	"terminal.ansiBrightYellow",
	"terminal.ansiBrightBlue",
	"terminal.ansiBrightMagenta",
	"terminal.ansiBrightCyan",
	"terminal.ansiBrightWhite",
}

// monacoColourKeys is the set of VS Code colour keys Monaco understands.
// They are copied verbatim when present and skipped when absent.
var monacoColourKeys = []string{
	"editor.background",
	"editor.foreground",
	"editorCursor.foreground",
	"editor.lineHighlightBackground",
	"editor.lineHighlightBorder",
	"editorLineNumber.foreground",
	"editorLineNumber.activeForeground",
	"editor.selectionBackground",
	"editor.inactiveSelectionBackground",
	"editor.selectionHighlightBackground",
	"editorBracketMatch.background",
	"editorBracketMatch.border",
	"editorGutter.background",
	"editorWidget.background",
	"editorWidget.border",
	"editorHoverWidget.background",
	"editorHoverWidget.border",
	"editorSuggestWidget.background",
	"editorSuggestWidget.border",
	"editorSuggestWidget.selectedBackground",
	"input.background",
	"input.border",
	"input.foreground",
	"list.activeSelectionBackground",
	"list.hoverBackground",
	"scrollbar.shadow",
	"scrollbarSlider.background",
	"scrollbarSlider.hoverBackground",
	"scrollbarSlider.activeBackground",
	"minimap.background",
	"editorGroupHeader.tabsBackground",
	"tab.activeBackground",
	"tab.activeForeground",
	"tab.inactiveBackground",
	"tab.inactiveForeground",
}

// uiMapping maps one web CSS variable to the ordered VS Code keys that can
// supply it; the first present key wins.
type uiMapping struct {
	variable string
	keys     []string
}

// uiMappings is spec §8.2 verbatim. --primary-foreground and
// --destructive-foreground have no author source and are always synthesized.
var uiMappings = []uiMapping{
	{"--background", []string{"editor.background", "sideBar.background"}},
	{"--foreground", []string{"editor.foreground", "sideBar.foreground"}},
	{"--card", []string{"sideBar.background", "editor.background"}},
	{"--card-foreground", []string{"sideBar.foreground", "editor.foreground"}},
	{"--popover", []string{"menu.background", "editorWidget.background", "editor.background"}},
	{"--popover-foreground", []string{"menu.foreground", "editorWidget.foreground", "editor.foreground"}},
	{"--primary", []string{"button.background", "focusBorder", "textLink.foreground"}},
	{"--secondary", []string{"input.background", "editorWidget.background"}},
	{"--secondary-foreground", []string{"input.foreground", "editor.foreground"}},
	{"--muted", []string{"list.hoverBackground", "editorWidget.background", "sideBar.background"}},
	{"--muted-foreground", []string{"descriptionForeground", "editorLineNumber.foreground"}},
	{"--accent", []string{"list.activeSelectionBackground", "list.hoverBackground"}},
	{"--accent-foreground", []string{"list.activeSelectionForeground"}},
	{"--destructive", []string{"errorForeground", "terminal.ansiRed"}},
	{"--border", []string{"panel.border", "sideBar.border", "editorGroup.border", "contrastBorder"}},
	{"--input", []string{"input.border", "panel.border"}},
	{"--ring", []string{"focusBorder", "textLink.foreground"}},
	{"--link", []string{"textLink.foreground"}},
}

// tokenScopeMapping maps a TextMate scope prefix to a Monaco token.
type tokenScopeMapping struct {
	prefix string
	token  string
}

// tokenScopeMappings is spec §8.3's ordered table. Order matters: the first
// matching prefix wins, so "constant.numeric" must be checked before
// "constant" and "keyword.operator" before "keyword".
var tokenScopeMappings = []tokenScopeMapping{
	{"comment", "comment"},
	{"string", "string"},
	{"constant.numeric", "number"},
	{"regexp", "regexp"},
	{"keyword.operator", "operator"},
	{"keyword", "keyword"},
	{"storage", "keyword"},
	{"entity.name.function", "function"},
	{"support.function", "function"},
	{"entity.name.type", "type"},
	{"entity.name.class", "type"},
	{"support.class", "type"},
	{"support.type", "type"},
	{"variable.parameter", "parameter"},
	{"variable", "variable"},
	{"support.variable", "variable"},
	{"meta.object-literal.key", "property"},
	{"entity.other.attribute-name", "attribute"},
	{"entity.name.tag", "tag"},
	{"constant", "constant"},
	{"punctuation", "delimiter"},
	{"invalid", "invalid"},
}

// ConvertTheme converts a parsed VS Code colour theme into ocode's four
// palettes. It never mutates raw. A nil theme is an error; everything else is
// tolerated, with non-fatal problems reported in ConvertedTheme.Warnings.
//
// A theme with no colors block still converts (see deriveWithoutColors): the
// web palette and the TUI text/background are derived from tokenColors and one
// warning records the fallback.
func ConvertTheme(id, label string, raw *RawTheme) (ConvertedTheme, error) {
	if raw == nil {
		return ConvertedTheme{}, errors.New("convert theme: nil theme")
	}

	kind := PolarityFromType(raw.Type, raw.Colors["editor.background"])

	var ui map[string]string
	var colors theme.ThemeColors
	var warnings []string
	if len(raw.Colors) == 0 {
		ui, colors = deriveWithoutColors(raw, kind)
		warnings = append(warnings, "theme has no colors block; the web palette was derived from token rules")
	} else {
		colors = buildColours(raw)
		ui = buildUI(raw.Colors, kind, colors)
	}

	return ConvertedTheme{
		ID:       id,
		Label:    label,
		Kind:     kind,
		Colors:   colors,
		UI:       ui,
		Monaco:   buildMonaco(raw, kind),
		Terminal: buildTerminal(raw.Colors, kind),
		Warnings: warnings,
	}, nil
}

// PolarityFromType classifies a theme from its declared type, falling back to
// the editor background's luminance when the type is missing or unknown.
// Anything unreadable is treated as dark, the common case for themes that omit
// both.
func PolarityFromType(kind, background string) Polarity {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case string(PolarityLight):
		return PolarityLight
	case string(PolarityDark):
		return PolarityDark
	}
	if isLightBackground(background) {
		return PolarityLight
	}
	return PolarityDark
}

// ResolveTokenScopes builds Monaco token rules from TextMate token rules.
//
// Later entries win for a token's foreground (TextMate's later-wins), while
// fontStyle accumulates: an entry that only adds italic to an already bold
// token leaves it "bold italic". An empty fontStyle means "not set" and never
// clears an earlier style. Scopes matching no prefix are skipped. The result
// is sorted by token so the emitted rules are deterministic.
func ResolveTokenScopes(rules []TokenRule) []MonacoRule {
	type tokenState struct {
		foreground string
		fontStyle  string
	}
	states := make(map[string]*tokenState)

	for _, rule := range rules {
		foreground := strings.TrimSpace(rule.Settings.Foreground)
		fontStyle := normaliseFontStyle(rule.Settings.FontStyle)
		if foreground == "" && fontStyle == "" {
			continue
		}
		seen := make(map[string]bool, len(rule.Scope))
		for _, scope := range rule.Scope {
			token, ok := monacoTokenForScope(scope)
			if !ok || seen[token] {
				continue
			}
			seen[token] = true
			state, ok := states[token]
			if !ok {
				state = &tokenState{}
				states[token] = state
			}
			if foreground != "" {
				state.foreground = foreground
			}
			state.fontStyle = mergeFontStyle(state.fontStyle, fontStyle)
		}
	}

	tokens := make([]string, 0, len(states))
	for token := range states {
		tokens = append(tokens, token)
	}
	sort.Strings(tokens)

	out := make([]MonacoRule, 0, len(tokens))
	for _, token := range tokens {
		state := states[token]
		out = append(out, MonacoRule{
			Token:      token,
			Foreground: state.foreground,
			FontStyle:  state.fontStyle,
		})
	}
	return out
}

// buildColours maps spec §8.5's TUI fields. Every value is the first present
// source key; a field with no source stays empty, except SelectedFg and
// Thinking which have explicit fallbacks.
func buildColours(raw *RawTheme) theme.ThemeColors {
	colors := raw.Colors
	out := theme.ThemeColors{}
	assign := func(dst *string, keys ...string) {
		if value, ok := firstPresent(colors, keys...); ok {
			*dst = value
		}
	}

	assign(&out.User, "terminal.ansiBlue", "textLink.foreground")
	assign(&out.Assistant, "terminal.ansiMagenta", "editor.foreground")
	assign(&out.Header, "terminal.ansiCyan", "textLink.foreground")
	assign(&out.Border, "panel.border", "sideBar.border")
	assign(&out.Hint, "descriptionForeground", "editorLineNumber.foreground")
	assign(&out.Text, "editor.foreground")
	assign(&out.Background, "editor.background")
	assign(&out.StatusBg, "statusBar.background", "sideBar.background")
	assign(&out.StatusFg, "statusBar.foreground", "descriptionForeground")
	assign(&out.SelectedFg, "list.activeSelectionForeground")
	assign(&out.SelectedBg, "list.activeSelectionBackground")
	assign(&out.Success, "terminal.ansiGreen", "gitDecoration.addedResourceForeground")
	assign(&out.Error, "errorForeground", "terminal.ansiRed")
	assign(&out.Accent, "focusBorder", "textLink.foreground")
	assign(&out.Dim, "editorLineNumber.foreground", "descriptionForeground")

	// SelectedFg is the one TUI field with a computed fallback: the pair must
	// stay legible when the theme supplies only a selection background.
	if out.SelectedFg == "" && validColour(out.SelectedBg) {
		out.SelectedFg = pickReadableFg(out.SelectedBg, []string{out.Text})
	}
	// Thinking follows the theme's comment colour, matching how the builtin
	// palettes are authored, and falls back to Dim.
	if foreground, ok := firstCommentForeground(raw.TokenColors.Rules); ok {
		out.Thinking = foreground
	}
	if out.Thinking == "" {
		out.Thinking = out.Dim
	}
	return out
}

// buildUI maps spec §8.2's web variables and applies the two permitted
// computed colours.
func buildUI(colors map[string]string, kind Polarity, palette theme.ThemeColors) map[string]string {
	ui := make(map[string]string, len(uiMappings)+3)
	for _, mapping := range uiMappings {
		if value, ok := firstPresent(colors, mapping.keys...); ok {
			ui[mapping.variable] = value
		}
	}

	// A VS Code theme never authors these surfaces' foregrounds, so they are
	// always derived from the surface they sit on.
	synthesiseForeground(ui, "--primary", "--primary-foreground", palette.Text)
	synthesiseForeground(ui, "--destructive", "--destructive-foreground", palette.Text)
	// --secondary-foreground is authored when the theme supplies
	// input.foreground/editor.foreground; otherwise derive it.
	if _, ok := ui["--secondary-foreground"]; !ok {
		synthesiseForeground(ui, "--secondary", "--secondary-foreground", palette.Text)
	}

	repairLink(ui, kind)
	return ui
}

// synthesiseForeground writes foregroundVar as the most readable of the
// palette's text colour and neutral black/white on surfaceVar. It does nothing
// when the surface is absent or unparseable.
func synthesiseForeground(ui map[string]string, surfaceVar, foregroundVar, text string) {
	if _, ok := ui[foregroundVar]; ok {
		return
	}
	surface, ok := ui[surfaceVar]
	if !ok || !validColour(surface) {
		return
	}
	ui[foregroundVar] = pickReadableFg(surface, []string{text})
}

// repairLink blends --link toward the page's neutral only when it fails AA on
// BOTH the page background and the muted surface. Failing one of them is the
// theme author's choice and is left alone.
func repairLink(ui map[string]string, kind Polarity) {
	link, ok := ui["--link"]
	if !ok || !validColour(link) {
		return
	}
	background, ok := ui["--background"]
	if !ok || !validColour(background) {
		return
	}

	surfaces := []string{background}
	unreadable := contrastRatio(link, background) < aaNormal
	if muted, ok := ui["--muted"]; ok && validColour(muted) {
		surfaces = append(surfaces, muted)
		unreadable = unreadable && contrastRatio(link, muted) < aaNormal
	}
	if !unreadable {
		return
	}

	fallback := "#ffffff"
	if kind == PolarityLight {
		fallback = "#000000"
	}
	ui["--link"] = ensureContrast(link, surfaces, fallback)
}

// deriveWithoutColors builds the minimal palette a theme without a colors
// block can still provide (ruling R2). Text and background come from the
// token rules; only --background, --foreground, --border and
// --muted-foreground are emitted.
func deriveWithoutColors(raw *RawTheme, kind Polarity) (map[string]string, theme.ThemeColors) {
	text := ""
	for _, rule := range raw.TokenColors.Rules {
		foreground := strings.TrimSpace(rule.Settings.Foreground)
		if foreground == "" {
			continue
		}
		if scopeHasComponent(rule.Scope, "text") || scopeHasComponent(rule.Scope, "source") {
			text = foreground
			break
		}
	}
	if text == "" {
		for _, rule := range raw.TokenColors.Rules {
			if foreground := strings.TrimSpace(rule.Settings.Foreground); foreground != "" {
				text = foreground
				break
			}
		}
	}

	background := ""
	for _, rule := range raw.TokenColors.Rules {
		if value := strings.TrimSpace(rule.Settings.Background); value != "" {
			background = value
			break
		}
	}
	if background == "" {
		if kind == PolarityLight {
			background = "#ffffff"
		} else {
			background = "#000000"
		}
	}
	if text == "" {
		// Last resort: the neutral that reads on the fallback background.
		if kind == PolarityLight {
			text = "#000000"
		} else {
			text = "#ffffff"
		}
	}

	muted := text
	for _, rule := range raw.TokenColors.Rules {
		foreground := strings.TrimSpace(rule.Settings.Foreground)
		if foreground == "" {
			continue
		}
		if scopeHasComponent(rule.Scope, "comment") {
			muted = foreground
			break
		}
	}

	// A border has no source in a colors-less theme. Blend it toward the muted
	// foreground so it stays visible without competing with body text.
	border := mixHex(background, muted, 0.35)

	ui := map[string]string{
		"--background":       background,
		"--foreground":       text,
		"--border":           border,
		"--muted-foreground": muted,
	}
	colors := theme.ThemeColors{
		Text:       text,
		Background: background,
		Border:     border,
		Hint:       muted,
		Dim:        muted,
		Thinking:   muted,
	}
	return ui, colors
}

// buildMonaco assembles the Monaco theme: a polarity base, the passthrough
// colour keys and the token rules.
func buildMonaco(raw *RawTheme, kind Polarity) MonacoTheme {
	base := "vs-dark"
	if kind == PolarityLight {
		base = "vs"
	}
	monaco := MonacoTheme{
		Base:   base,
		Colors: make(map[string]string, len(monacoColourKeys)),
	}
	for _, key := range monacoColourKeys {
		if value, ok := firstPresent(raw.Colors, key); ok {
			monaco.Colors[key] = value
		}
	}
	monaco.Rules = ResolveTokenScopes(raw.TokenColors.Rules)
	return monaco
}

// buildTerminal assembles only the block matching kind. Every palette slot is
// filled: a missing VS Code key takes the xterm default at the same index.
func buildTerminal(colors map[string]string, kind Polarity) TerminalPalette {
	foreground, _ := firstPresent(colors, "terminal.foreground", "editor.foreground")
	background, _ := firstPresent(colors, "terminal.background", "editor.background")
	cursor, _ := firstPresent(colors, "terminalCursor.foreground", "editorCursor.foreground")
	cursorText, _ := firstPresent(colors, "terminalCursor.background")
	if cursorText == "" && validColour(background) {
		cursorText = pickReadableFg(background, []string{foreground})
	}
	selectionBackground, _ := firstPresent(colors, "terminal.selectionBackground")
	selectionForeground, _ := firstPresent(colors, "terminal.selectionForeground")

	palette := make([]string, len(terminalANSIKeys))
	for index, key := range terminalANSIKeys {
		if value, ok := firstPresent(colors, key); ok {
			palette[index] = value
		} else {
			palette[index] = defaultANSI[index]
		}
	}

	block := &TerminalColors{
		Foreground:          foreground,
		Background:          background,
		Cursor:              cursor,
		CursorText:          cursorText,
		SelectionBackground: selectionBackground,
		SelectionForeground: selectionForeground,
		Palette:             palette,
	}
	if kind == PolarityLight {
		return TerminalPalette{Light: block}
	}
	return TerminalPalette{Dark: block}
}

// firstPresent returns the first non-empty value among keys, trimmed.
func firstPresent(colors map[string]string, keys ...string) (string, bool) {
	for _, key := range keys {
		if value := strings.TrimSpace(colors[key]); value != "" {
			return value, true
		}
	}
	return "", false
}

// monacoTokenForScope resolves a TextMate scope to a Monaco token using the
// ordered prefix table.
func monacoTokenForScope(scope string) (string, bool) {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return "", false
	}
	for _, mapping := range tokenScopeMappings {
		if scopeMatchesPrefix(scope, mapping.prefix) {
			return mapping.token, true
		}
	}
	return "", false
}

// scopeMatchesPrefix reports whether scope is the prefix or a more specific
// scope beneath it. "comment" matches but "commentary" does not.
func scopeMatchesPrefix(scope, prefix string) bool {
	return scope == prefix || strings.HasPrefix(scope, prefix+".")
}

// scopeHasComponent reports whether any scope in scopes has want as one dotted
// component ("source.js" has "source"; "context.foo" does not have "text").
func scopeHasComponent(scopes []string, want string) bool {
	for _, scope := range scopes {
		for _, component := range strings.Split(scope, ".") {
			if strings.TrimSpace(component) == want {
				return true
			}
		}
	}
	return false
}

// normaliseFontStyle reduces a VS Code fontStyle to its recognised pieces in a
// canonical order so accumulation is deterministic.
func normaliseFontStyle(style string) string {
	seen := make(map[string]bool)
	for _, piece := range strings.Fields(strings.ToLower(style)) {
		switch piece {
		case "bold", "italic", "underline", "strikethrough":
			seen[piece] = true
		}
	}
	ordered := make([]string, 0, len(seen))
	for _, piece := range []string{"bold", "italic", "underline", "strikethrough"} {
		if seen[piece] {
			ordered = append(ordered, piece)
		}
	}
	return strings.Join(ordered, " ")
}

// mergeFontStyle unions two normalised font styles.
func mergeFontStyle(existing, added string) string {
	if added == "" {
		return existing
	}
	return normaliseFontStyle(existing + " " + added)
}

// firstCommentForeground returns the first foreground colour whose rule
// carries a comment scope, matching the Thinking rule in spec §8.5.
func firstCommentForeground(rules []TokenRule) (string, bool) {
	for _, rule := range rules {
		foreground := strings.TrimSpace(rule.Settings.Foreground)
		if foreground == "" {
			continue
		}
		if scopeHasComponent(rule.Scope, "comment") {
			return foreground, true
		}
	}
	return "", false
}

// validColour reports whether value parses as a hex colour.
func validColour(value string) bool {
	_, ok := parseHexColour(value)
	return ok
}

// parseHexColour parses #RGB, #RGBA, #RRGGBB or #RRGGBBAA. Alpha is ignored:
// only the RGB channels matter for contrast and mixing.
func parseHexColour(value string) ([3]uint8, bool) {
	var rgb [3]uint8
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "#") {
		return rgb, false
	}
	digits := value[1:]
	switch len(digits) {
	case 3, 4:
		for index := 0; index < 3; index++ {
			nibble, ok := parseHexNibble(digits[index])
			if !ok {
				return rgb, false
			}
			rgb[index] = nibble*16 + nibble
		}
		return rgb, true
	case 6, 8:
		for index := 0; index < 3; index++ {
			b, ok := parseHexByte(digits[index*2 : index*2+2])
			if !ok {
				return rgb, false
			}
			rgb[index] = b
		}
		return rgb, true
	default:
		return rgb, false
	}
}

func parseHexNibble(c byte) (uint8, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	default:
		return 0, false
	}
}

func parseHexByte(pair string) (uint8, bool) {
	high, ok := parseHexNibble(pair[0])
	if !ok {
		return 0, false
	}
	low, ok := parseHexNibble(pair[1])
	if !ok {
		return 0, false
	}
	return high*16 + low, true
}

// relativeLuminance implements the WCAG 2.x luminance definition.
func relativeLuminance(rgb [3]uint8) float64 {
	linear := func(channel uint8) float64 {
		value := float64(channel) / 255
		if value <= 0.04045 {
			return value / 12.92
		}
		return math.Pow((value+0.055)/1.055, 2.4)
	}
	return 0.2126*linear(rgb[0]) + 0.7152*linear(rgb[1]) + 0.0722*linear(rgb[2])
}

// contrastRatio returns the WCAG contrast ratio between two hex colours. An
// unparseable colour yields 21 (the maximum) so callers treat unknown values
// as already legible; pickReadableFg filters invalid candidates before this
// can accept one.
func contrastRatio(a, b string) float64 {
	first, ok := parseHexColour(a)
	if !ok {
		return 21
	}
	second, ok := parseHexColour(b)
	if !ok {
		return 21
	}
	firstLum := relativeLuminance(first)
	secondLum := relativeLuminance(second)
	high, low := firstLum, secondLum
	if firstLum < secondLum {
		high, low = secondLum, firstLum
	}
	return (high + 0.05) / (low + 0.05)
}

// mixHex linearly interpolates from a (t=0) to b (t=1) in RGB.
func mixHex(a, b string, t float64) string {
	first, ok := parseHexColour(a)
	if !ok {
		return a
	}
	second, ok := parseHexColour(b)
	if !ok {
		return b
	}
	if t <= 0 {
		return a
	}
	if t >= 1 {
		return b
	}
	var buf [7]byte
	buf[0] = '#'
	const hexDigits = "0123456789abcdef"
	for index := 0; index < 3; index++ {
		value := int(math.Round(float64(first[index]) + float64(int(second[index])-int(first[index]))*t))
		if value < 0 {
			value = 0
		}
		if value > 255 {
			value = 255
		}
		buf[1+index*2] = hexDigits[value>>4]
		buf[2+index*2] = hexDigits[value&0x0f]
	}
	return string(buf[:])
}

// pickReadableFg returns the first candidate that clears AA on background,
// preserving the theme's character. When none does it falls back to whichever
// of pure black/white contrasts more.
func pickReadableFg(background string, candidates []string) string {
	for _, candidate := range candidates {
		if !validColour(candidate) {
			continue
		}
		if contrastRatio(background, candidate) >= aaNormal {
			return candidate
		}
	}
	if contrastRatio(background, "#ffffff") >= contrastRatio(background, "#000000") {
		return "#ffffff"
	}
	return "#000000"
}

// ensureContrast blends foreground toward fallback until it clears AA on every
// background. Contrast is re-measured on each blended colour because RGB
// interpolation is not monotonic in luminance.
func ensureContrast(foreground string, backgrounds []string, fallback string) string {
	readable := func(candidate string) bool {
		for _, background := range backgrounds {
			if contrastRatio(candidate, background) < aaNormal {
				return false
			}
		}
		return true
	}
	if readable(foreground) {
		return foreground
	}
	for t := 0.1; t < 1; t += 0.1 {
		candidate := mixHex(foreground, fallback, t)
		if readable(candidate) {
			return candidate
		}
	}
	return mixHex(foreground, fallback, 1)
}

// isLightBackground reports whether a background colour sits closer to white
// than to black.
func isLightBackground(background string) bool {
	if !validColour(background) {
		return false
	}
	return contrastRatio(background, "#000000") > contrastRatio(background, "#ffffff")
}
