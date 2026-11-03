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

// uiMappings is spec §8.2 verbatim. A foreground paired with a surface is
// authored-first: --primary-foreground, --secondary-foreground and
// --accent-foreground each name their VS Code partner key here, and
// synthesiseForeground fills only a gap the theme leaves.
var uiMappings = []uiMapping{
	{variable: "--background", keys: []string{"editor.background", "sideBar.background"}},
	{variable: "--foreground", keys: []string{"editor.foreground", "sideBar.foreground"}},
	{variable: "--card", keys: []string{"sideBar.background", "editor.background"}},
	{variable: "--card-foreground", keys: []string{"sideBar.foreground", "editor.foreground"}},
	{variable: "--popover", keys: []string{"menu.background", "editorWidget.background", "editor.background"}},
	{variable: "--popover-foreground", keys: []string{"menu.foreground", "editorWidget.foreground", "editor.foreground"}},
	{variable: "--primary", keys: []string{"button.background", "focusBorder", "textLink.foreground"}},
	{variable: "--primary-foreground", keys: []string{"button.foreground"}},
	{variable: "--secondary", keys: []string{"input.background", "editorWidget.background"}},
	{variable: "--secondary-foreground", keys: []string{"input.foreground", "editor.foreground"}},
	{variable: "--muted", keys: []string{"list.hoverBackground", "editorWidget.background", "sideBar.background"}},
	{variable: "--muted-foreground", keys: []string{"descriptionForeground", "editorLineNumber.foreground"}},
	{variable: "--accent", keys: []string{"list.activeSelectionBackground", "list.hoverBackground"}},
	{variable: "--accent-foreground", keys: []string{"list.activeSelectionForeground"}},
	{variable: "--destructive", keys: []string{"errorForeground", "terminal.ansiRed"}},
	{variable: "--border", keys: []string{"panel.border", "sideBar.border", "editorGroup.border", "contrastBorder"}},
	{variable: "--input", keys: []string{"input.border", "panel.border"}},
	{variable: "--ring", keys: []string{"focusBorder", "textLink.foreground"}},
	{variable: "--link", keys: []string{"textLink.foreground"}},
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
	{prefix: "comment", token: "comment"},
	{prefix: "string", token: "string"},
	{prefix: "constant.numeric", token: "number"},
	{prefix: "regexp", token: "regexp"},
	{prefix: "keyword.operator", token: "operator"},
	{prefix: "keyword", token: "keyword"},
	{prefix: "storage", token: "keyword"},
	{prefix: "entity.name.function", token: "function"},
	{prefix: "support.function", token: "function"},
	{prefix: "entity.name.type", token: "type"},
	{prefix: "entity.name.class", token: "type"},
	{prefix: "support.class", token: "type"},
	{prefix: "support.type", token: "type"},
	{prefix: "variable.parameter", token: "parameter"},
	{prefix: "variable", token: "variable"},
	{prefix: "support.variable", token: "variable"},
	{prefix: "meta.object-literal.key", token: "property"},
	{prefix: "entity.other.attribute-name", token: "attribute"},
	{prefix: "entity.name.tag", token: "tag"},
	{prefix: "constant", token: "constant"},
	{prefix: "punctuation", token: "delimiter"},
	{prefix: "invalid", token: "invalid"},
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

// partnerForegroundVars are the ui foregrounds whose authored partner key wins
// over synthesis. They are the three variables synthesiseForeground fills.
var partnerForegroundVars = map[string]struct{}{
	"--primary-foreground":   {},
	"--secondary-foreground": {},
	"--accent-foreground":    {},
}

// partnerForegroundContributes reports whether an authored partner foreground
// actually carries a colour of its own once flattened onto the page background.
//
// A fully transparent value contributes nothing, so it is treated as absent and
// synthesiseForeground computes a readable one instead of the theme painting
// background-coloured text on a coloured surface. An OPAQUE value equal to the
// page background is still authoritative: Gruvbox Light authors
// button.foreground #fbf1c7 — the page background — for text on its blue
// button, and dropping it would replace a deliberate choice with a synthesized
// one.
func partnerForegroundContributes(value, background string) bool {
	_, alpha, ok := parseHexColourAlpha(value)
	if !ok || alpha == 0 {
		return false
	}
	if alpha == 255 {
		return true
	}
	composited, ok := compositeOver(value, background)
	// A semi-transparent value whose RGB is the background adds nothing either.
	return ok && composited != background
}

// buildUI maps spec §8.2's web variables and applies the permitted computed
// colours.
//
// Every value is composited over the resolved background before it is emitted.
// The CSS variables carry no alpha (they are consumed as HSL triplets), and the
// web converter accepts six hex digits only, so an authored #RRGGBBAA value
// would be dropped and the stylesheet default would survive. Compositing keeps
// the author's intent: a fully transparent value becomes the background itself,
// never black.
func buildUI(colors map[string]string, kind Polarity, palette theme.ThemeColors) map[string]string {
	background := uiBackground(colors, kind)
	ui := make(map[string]string, len(uiMappings)+3)
	for _, mapping := range uiMappings {
		value, ok := firstPresent(colors, mapping.keys...)
		if !ok {
			continue
		}
		if _, partner := partnerForegroundVars[mapping.variable]; partner && !partnerForegroundContributes(value, background) {
			// The partner key exists but contributes no colour; leave the gap for
			// synthesiseForeground below.
			continue
		}
		composited, ok := compositeOver(value, background)
		if !ok {
			// A value the web converter could not read is omitted rather than
			// emitted, matching the "no source key, no variable" rule.
			continue
		}
		ui[mapping.variable] = composited
	}
	// The background is the surface everything else is flattened onto, so it is
	// always emitted even when the author value was missing or unparseable.
	ui["--background"] = background

	// Authored partner foregrounds win (uiMappings copied them above); only a
	// gap the theme leaves is synthesized, from the surface it sits on.
	synthesiseForeground(ui, "--primary", "--primary-foreground", palette.Text)
	synthesiseForeground(ui, "--destructive", "--destructive-foreground", palette.Text)
	synthesiseForeground(ui, "--secondary", "--secondary-foreground", palette.Text)
	// --accent-foreground must never be omitted: leaving it out falls back to a
	// near-white stylesheet default that can be unreadable on a light accent.
	// When the theme authors neither list.activeSelectionForeground nor an
	// accent surface, compute the foreground from the page background.
	if _, ok := ui["--accent-foreground"]; !ok {
		surface := ui["--accent"]
		if !validColour(surface) {
			surface = background
		}
		ui["--accent-foreground"] = pickReadableFg(surface, []string{palette.Text})
	}

	repairLink(ui, kind)
	normaliseUI(ui, background)
	return ui
}

// normaliseUI is the single choke point enforcing the web palette's #RRGGBB
// contract. ui has three writers — the uiMappings loop, synthesiseForeground /
// pickReadableFg, and deriveWithoutColors — and only the first composites its
// values, so an authored #RRGGBBAA colour or an alpha-carrying token foreground
// copied verbatim by pickReadableFg could otherwise reach the stylesheet, where
// the converter drops it. Each value is composited over background and any value
// that is not a colour at all is deleted.
//
// The pass is idempotent: #RRGGBB carries alpha 255 and composites to itself, so
// running it over already-normalised output changes nothing.
func normaliseUI(ui map[string]string, background string) {
	backdrop := background
	if rgb, _, ok := parseHexColourAlpha(background); ok {
		backdrop = formatHexRGB(rgb)
	}
	for variable, value := range ui {
		composited, ok := compositeOver(value, backdrop)
		if !ok {
			delete(ui, variable)
			continue
		}
		ui[variable] = composited
	}
}

// uiBackground resolves the background every ui value is composited over. It
// follows the --background mapping's key order and reduces the value to its RGB
// channels; when the theme supplies no parseable background it uses the
// polarity neutral.
func uiBackground(colors map[string]string, kind Polarity) string {
	if value, ok := firstPresent(colors, "editor.background", "sideBar.background"); ok {
		if rgb, _, ok := parseHexColourAlpha(value); ok {
			return formatHexRGB(rgb)
		}
	}
	if kind == PolarityLight {
		return "#ffffff"
	}
	return "#000000"
}

// compositeOver flattens a theme colour onto background and returns #RRGGBB.
// Alpha is composited, never truncated: #00000000 becomes the background and
// #31324480 becomes a blend of the author colour and the background. A value
// that does not parse is reported as not ok so the caller can omit it.
func compositeOver(value, background string) (string, bool) {
	rgb, alpha, ok := parseHexColourAlpha(value)
	if !ok {
		return "", false
	}
	if alpha == 255 {
		return formatHexRGB(rgb), true
	}
	backdrop, ok := parseHexColour(background)
	if !ok {
		backdrop = [3]uint8{}
	}
	strength := int(alpha)
	var out [3]uint8
	for index := 0; index < 3; index++ {
		out[index] = uint8((int(rgb[index])*strength + int(backdrop[index])*(255-strength) + 127) / 255)
	}
	return formatHexRGB(out), true
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
	if foreground, ok := firstCommentForeground(raw.TokenColors.Rules); ok {
		muted = foreground
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
	// The token rules are raw author values, so they may be #RGB/#RRGGBBAA just
	// like a colors block; the same choke point flattens them to #RRGGBB.
	normaliseUI(ui, background)
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

// parseHexColour parses #RGB, #RGBA, #RRGGBB or #RRGGBBAA. Alpha is ignored
// here: only the RGB channels matter for contrast and mixing.
func parseHexColour(value string) ([3]uint8, bool) {
	rgb, _, ok := parseHexColourAlpha(value)
	return rgb, ok
}

// parseHexColourAlpha parses #RGB, #RGBA, #RRGGBB or #RRGGBBAA and returns the
// RGB channels plus the alpha channel, which is 255 when the value carries
// none.
func parseHexColourAlpha(value string) ([3]uint8, uint8, bool) {
	var rgb [3]uint8
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "#") {
		return rgb, 0, false
	}
	digits := value[1:]
	switch len(digits) {
	case 3, 4:
		for index := 0; index < 3; index++ {
			nibble, ok := parseHexNibble(digits[index])
			if !ok {
				return rgb, 0, false
			}
			rgb[index] = nibble*16 + nibble
		}
		alpha := uint8(255)
		if len(digits) == 4 {
			nibble, ok := parseHexNibble(digits[3])
			if !ok {
				return rgb, 0, false
			}
			alpha = nibble*16 + nibble
		}
		return rgb, alpha, true
	case 6, 8:
		for index := 0; index < 3; index++ {
			b, ok := parseHexByte(digits[index*2 : index*2+2])
			if !ok {
				return rgb, 0, false
			}
			rgb[index] = b
		}
		alpha := uint8(255)
		if len(digits) == 8 {
			b, ok := parseHexByte(digits[6:8])
			if !ok {
				return rgb, 0, false
			}
			alpha = b
		}
		return rgb, alpha, true
	default:
		return rgb, 0, false
	}
}

// formatHexRGB renders RGB channels as a lower-case #RRGGBB, the only shape the
// web converter accepts.
func formatHexRGB(rgb [3]uint8) string {
	const hexDigits = "0123456789abcdef"
	var buf [7]byte
	buf[0] = '#'
	for index := 0; index < 3; index++ {
		buf[1+index*2] = hexDigits[rgb[index]>>4]
		buf[2+index*2] = hexDigits[rgb[index]&0x0f]
	}
	return string(buf[:])
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
