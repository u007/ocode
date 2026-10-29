package server

import (
	"fmt"
	"strings"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/ghosttyconfig"
)

// Terminal appearance resolution.
//
// The interactive terminal is rendered by ghostty-web in the browser, so its
// font and colours are a CLIENT concern — but their source of truth can be the
// user's Ghostty configuration, which only exists on the machine the server
// runs on. Resolving here (not in the client) keeps one answer for the API, the
// Settings UI and the panels, and makes "requested vs effective" impossible to
// disagree on.
//
// Remote projects: this must answer from the LOCAL Ghostty config. A request
// proxied to a remote host would read that host's (usually nonexistent) config
// and describe a terminal the viewer is not looking at.

// terminalAppearanceSettings are the PERSISTED ocode-source values. They are
// reported alongside the resolved block so the Settings UI can keep showing (and
// editing) its own values while the Ghostty source is the one in effect.
type terminalAppearanceSettings struct {
	FontFamily        string  `json:"font_family"`
	FontSize          float64 `json:"font_size"`
	CellWidthPercent  float64 `json:"cell_width_percent"`
	CellHeightPercent float64 `json:"cell_height_percent"`
	Theme             string  `json:"theme,omitempty"`
}

// terminalAppearanceResponse is the resolved terminal font/colour set.
type terminalAppearanceResponse struct {
	// Source is the source that actually applied: "ghostty" or "ocode".
	Source string `json:"source"`
	// Requested is the configured preference. When it is "ghostty" and no
	// config was found, Source is "ocode" and this still says "ghostty" so the
	// UI can explain the fallback instead of silently switching.
	Requested  string  `json:"requested"`
	FontFamily string  `json:"font_family"`
	FontSize   float64 `json:"font_size"`
	// The ADJUSTMENT (delta) forms, named so they cannot be confused with the
	// ABSOLUTE `settings.cell_*_percent` below. A Ghostty adjust-cell-* may be a
	// pixel delta, which has no absolute equivalent.
	CellWidth  ghosttyconfig.CellAdjust   `json:"cell_width_adjust"`
	CellHeight ghosttyconfig.CellAdjust   `json:"cell_height_adjust"`
	ThemeName  string                     `json:"theme_name,omitempty"`
	Light      *ghosttyconfig.ThemeColors `json:"light,omitempty"`
	Dark       *ghosttyconfig.ThemeColors `json:"dark,omitempty"`
	// Errors are non-fatal problems with the CONFIGURED appearance — today an
	// unresolvable `terminal_theme`. Separate from the ghostty block's errors,
	// which describe the Ghostty source itself.
	Errors []string `json:"errors,omitempty"`
	// Settings are the persisted ocode-source values (not the resolved ones).
	Settings terminalAppearanceSettings `json:"settings"`
}

// terminalGhosttyResponse is the report of what was read from Ghostty.
type terminalGhosttyResponse struct {
	Found      bool                     `json:"found"`
	Paths      []string                 `json:"paths,omitempty"`
	Errors     []string                 `json:"errors,omitempty"`
	FontFamily string                   `json:"font_family,omitempty"`
	FontSize   float64                  `json:"font_size,omitempty"`
	CellWidth  ghosttyconfig.CellAdjust `json:"cell_width"`
	CellHeight ghosttyconfig.CellAdjust `json:"cell_height"`
	ThemeName  string                   `json:"theme_name,omitempty"`
	Keybinds   []ghosttyconfig.Keybind  `json:"keybinds,omitempty"`
	Ignored    []ghosttyconfig.Ignored  `json:"ignored,omitempty"`
}

func newTerminalGhosttyResponse(cfg ghosttyconfig.Config) terminalGhosttyResponse {
	return terminalGhosttyResponse{
		Found:      cfg.Found,
		Paths:      cfg.Paths,
		Errors:     cfg.Errors,
		FontFamily: cfg.FontFamily,
		FontSize:   cfg.FontSize,
		CellWidth:  cfg.CellWidth,
		CellHeight: cfg.CellHeight,
		ThemeName:  cfg.ThemeName,
		Keybinds:   cfg.Keybinds,
		Ignored:    cfg.Ignored,
	}
}

// resolveTerminalAppearance applies the whole-block precedence:
//
//	"ocode"                       → the Terminal* settings below
//	"ghostty" with a config found → the Ghostty values
//	"ghostty" with nothing found  → the Terminal* settings, reported as "ocode"
//
// A value the chosen source does not define falls back to the built-in default
// for that field (never to the other source's value, which would mix two
// fonts/palettes into one terminal).
func resolveTerminalAppearance(ocode *config.OcodeConfig, ghostty ghosttyconfig.Config) terminalAppearanceResponse {
	requested := config.DefaultTerminalAppearanceSource
	var settings config.OcodeConfig
	if ocode != nil {
		settings = *ocode
		requested = config.NormalizeTerminalAppearanceSource(ocode.TerminalAppearanceSource)
	}

	// The persisted ocode-source values, always reported: while Ghostty is the
	// active source the UI still needs them, or switching to "ocode" would show
	// (and then save) the Ghostty values.
	storedFontSize := config.DefaultTerminalFontSize
	if settings.TerminalFontSize > 0 {
		storedFontSize = config.NormalizeTerminalFontSize(settings.TerminalFontSize)
	}
	storedFontFamily := strings.TrimSpace(settings.TerminalFontFamily)
	if storedFontFamily == "" {
		storedFontFamily = defaultTerminalFontFamily
	}
	stored := terminalAppearanceSettings{
		FontFamily:        storedFontFamily,
		FontSize:          float64(storedFontSize),
		CellWidthPercent:  config.NormalizeTerminalCellWidthPercent(settings.TerminalCellWidthPercent),
		CellHeightPercent: config.NormalizeTerminalCellHeightPercent(settings.TerminalCellHeightPercent),
		Theme:             strings.TrimSpace(settings.TerminalTheme),
	}

	if requested == "ghostty" && ghostty.Found {
		appearance := terminalAppearanceResponse{
			Source:     "ghostty",
			Requested:  requested,
			FontFamily: ghostty.FontFamily,
			FontSize:   ghostty.FontSize,
			CellWidth:  ghostty.CellWidth,
			CellHeight: ghostty.CellHeight,
			ThemeName:  ghostty.ThemeName,
			Light:      ghostty.Light,
			Dark:       ghostty.Dark,
		}
		if appearance.FontFamily == "" {
			appearance.FontFamily = defaultTerminalFontFamily
		}
		if appearance.FontSize == 0 {
			appearance.FontSize = float64(config.DefaultTerminalFontSize)
		}
		appearance.Settings = stored
		return appearance
	}

	fontSize := config.DefaultTerminalFontSize
	if settings.TerminalFontSize > 0 {
		fontSize = config.NormalizeTerminalFontSize(settings.TerminalFontSize)
	}
	appearance := terminalAppearanceResponse{
		Source:     "ocode",
		Requested:  requested,
		FontFamily: strings.TrimSpace(settings.TerminalFontFamily),
		FontSize:   float64(fontSize),
		// Stored percentages are ABSOLUTE (100 = the font's natural cell). The
		// wire form is a delta, matching Ghostty's adjust-cell-* semantics and
		// the ghostty block above, so the client has one formula.
		CellWidth:  ghosttyconfig.CellAdjust{Percent: config.NormalizeTerminalCellWidthPercent(settings.TerminalCellWidthPercent) - 100},
		CellHeight: ghosttyconfig.CellAdjust{Percent: config.NormalizeTerminalCellHeightPercent(settings.TerminalCellHeightPercent) - 100},
	}
	if appearance.FontFamily == "" {
		appearance.FontFamily = defaultTerminalFontFamily
	}
	if name := stored.Theme; name != "" {
		theme, err := ghosttyconfig.ResolveTheme(name)
		switch {
		case err != nil:
			// A typo or a theme that is not installed yet. Reported instead of
			// silently falling back to ocode's palette, and never a save error:
			// the user must still be able to store the rest of their settings.
			appearance.Errors = append(appearance.Errors, fmt.Sprintf("theme %q: %v", name, err))
		case theme != nil:
			appearance.ThemeName = name
			appearance.Light = theme
			appearance.Dark = theme
		}
	}
	appearance.Settings = stored
	return appearance
}

// defaultTerminalFontFamily mirrors the client's built-in stack so the API never
// reports an empty family the UI would have to special-case.
const defaultTerminalFontFamily = "ui-monospace, SFMono-Regular, Menlo, monospace"

// loadGhosttyConfig is a package var so tests can supply a config instead of
// depending on the machine's real Ghostty files.
var loadGhosttyConfig = ghosttyconfig.Load
