package appearance

import "github.com/u007/ocode/internal/theme"

// Polarity is the light/dark classification of a colour theme.
type Polarity string

const (
	// PolarityDark is a theme designed for a dark background.
	PolarityDark Polarity = "dark"
	// PolarityLight is a theme designed for a light background.
	PolarityLight Polarity = "light"
)

// ConvertedTheme is a parsed VS Code colour theme expressed in the four
// palettes ocode renders with: the 16-field TUI palette, the web chrome CSS
// variables, the Monaco editor theme and the terminal palette.
//
// Conversion never rewrites an author's colour. Only the synthesized
// foregrounds (--primary-foreground, --destructive-foreground and
// --secondary-foreground when the theme supplies no value) and a repaired
// --link are computed; every other value is copied verbatim from the theme,
// and a variable with no source key is omitted rather than substituted.
type ConvertedTheme struct {
	ID       string            `json:"id"`
	Label    string            `json:"label"`
	Kind     Polarity          `json:"kind"`
	Colors   theme.ThemeColors `json:"colors"`
	UI       map[string]string `json:"ui"`
	Monaco   MonacoTheme       `json:"monaco"`
	Terminal TerminalPalette   `json:"terminal"`
	Warnings []string          `json:"warnings,omitempty"`
}

// MonacoTheme is the subset of a VS Code theme Monaco can express. Base is
// "vs-dark" or "vs"; Colors is a verbatim passthrough of the keys Monaco
// understands, and Rules are derived from tokenColors.
type MonacoTheme struct {
	Base   string            `json:"base"`
	Colors map[string]string `json:"colors"`
	Rules  []MonacoRule      `json:"rules"`
}

// MonacoRule is one Monaco token rule. FontStyle is a space-separated
// combination of italic, bold, underline and strikethrough.
type MonacoRule struct {
	Token      string `json:"token"`
	Foreground string `json:"foreground,omitempty"`
	FontStyle  string `json:"fontStyle,omitempty"`
}

// TerminalPalette carries only the block matching the theme's polarity. The
// other side stays nil so a renderer never mistakes a dark palette for a
// light one.
type TerminalPalette struct {
	Dark  *TerminalColors `json:"dark,omitempty"`
	Light *TerminalColors `json:"light,omitempty"`
}

// TerminalColors matches the wire shape the web terminal already consumes
// (TerminalThemeColors in web/src/components/Terminal/terminalAppearance.ts).
// Palette always holds 16 entries in ANSI order (black, red, ..., bright
// white); missing slots fall back to the default xterm table.
type TerminalColors struct {
	Foreground          string   `json:"foreground"`
	Background          string   `json:"background"`
	Cursor              string   `json:"cursor"`
	CursorText          string   `json:"cursor_text"`
	SelectionBackground string   `json:"selection_background"`
	SelectionForeground string   `json:"selection_foreground"`
	Palette             []string `json:"palette"`
}
