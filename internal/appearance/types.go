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
// Conversion preserves the author's colours, with these deliberate exceptions.
// Every value emitted to the web palette is composited over the theme
// background and flattened to #RRGGBB, because the CSS variables cannot carry
// alpha and the web converter ignores an 8-digit value. That flattening is one
// shared pass (normaliseUI) applied to every producer of the palette: the
// mapping loop, the synthesized foregrounds and the colors-less derivation.
// The TUI palette (Colors) and the emitted terminal block are flattened the
// same way at the end of ConvertTheme — normaliseColours over the theme
// background and normaliseTerminal over each block's own background — because
// lipgloss and ghostty-web also render #RRGGBB only. A field or palette slot
// that cannot be parsed as a colour is cleared (Colors) or replaced by the
// xterm default (Palette), never passed through.
// A foreground the theme does not author (--primary-foreground,
// --destructive-foreground, --secondary-foreground, and --accent-foreground when
// neither the partner key nor an accent surface exists) is synthesized for
// readability; when the theme does author the partner key (button.foreground,
// input.foreground, list.activeSelectionForeground) that value wins, unless the
// authored value is fully transparent — it then counts as absent and is
// synthesized. --link is repaired only when it fails AA on both the page
// background and the muted surface. A variable with no source key is omitted
// rather than substituted.
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
	Foreground          string   `json:"foreground,omitempty"`
	Background          string   `json:"background,omitempty"`
	Cursor              string   `json:"cursor,omitempty"`
	CursorText          string   `json:"cursor_text,omitempty"`
	SelectionBackground string   `json:"selection_background,omitempty"`
	SelectionForeground string   `json:"selection_foreground,omitempty"`
	Palette             []string `json:"palette,omitempty"`
}
