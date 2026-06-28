package theme

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/u007/ocode/internal/config"
)

// ThemeColors holds the color values for a theme.
type ThemeColors struct {
	User       string `json:"user"`
	Assistant  string `json:"assistant"`
	Header     string `json:"header"`
	Border     string `json:"border"`
	Hint       string `json:"hint"`
	Text       string `json:"text"`
	Background string `json:"background"`
	StatusBg   string `json:"status_bg"`
	StatusFg   string `json:"status_fg"`
	SelectedFg string `json:"selected_fg"`
	SelectedBg string `json:"selected_bg"`
	Success    string `json:"success"`
	Error      string `json:"error"`
	Accent     string `json:"accent"`
	Dim        string `json:"dim"`
	Thinking   string `json:"thinking"`
}

// ThemeDefinition wraps a theme's color palette.
type ThemeDefinition struct {
	Colors ThemeColors `json:"colors"`
	Label  string      `json:"label,omitempty"` // optional display label (e.g. "lcars (Star Trek OS)")
}

var builtinThemes = map[string]ThemeDefinition{
	"tokyonight": {
		Colors: ThemeColors{
			User:       "#7aa2f7",
			Assistant:  "#bb9af7",
			Header:     "#7dcfff",
			Border:     "#3b4261",
			Hint:       "#565f89",
			Text:       "#c0caf5",
			Background: "#1a1b26",
			StatusBg:   "#1a1b26",
			StatusFg:   "#787c99",
			SelectedFg: "#1a1b26",
			SelectedBg: "#7aa2f7",
			Success:    "#9ece6a",
			Error:      "#f7768e",
			Accent:     "#7dcfff",
			Dim:        "#3b4261",
		},
	},
	"tokyonight-storm": {
		Colors: ThemeColors{
			User:       "#7aa2f7",
			Assistant:  "#bb9af7",
			Header:     "#7dcfff",
			Border:     "#414868",
			Hint:       "#545c7e",
			Text:       "#c0caf5",
			Background: "#24283b",
			StatusBg:   "#24283b",
			StatusFg:   "#9aa5ce",
			SelectedFg: "#24283b",
			SelectedBg: "#7aa2f7",
			Success:    "#9ece6a",
			Error:      "#f7768e",
			Accent:     "#7dcfff",
			Dim:        "#414868",
		},
	},
	"opencode": {
		Colors: ThemeColors{
			User:       "#FAB283",
			Assistant:  "#F5A97F",
			Header:     "#FAB283",
			Border:     "#3C3C3C",
			Hint:       "#888888",
			Text:       "#EEEEEE",
			Background: "#0A0A0A",
			StatusBg:   "#0A0A0A",
			StatusFg:   "#808080",
			SelectedFg: "#0A0A0A",
			SelectedBg: "#FAB283",
			Success:    "#A6E3A1",
			Error:      "#E06C75",
			Accent:     "#FAB283",
			Dim:        "#3C3C3C",
		},
	},
	"opencode-light": {
		Colors: ThemeColors{
			User:       "#008800",
			Assistant:  "#008888",
			Header:     "#008800",
			Border:     "#aaaaaa",
			Hint:       "#666666",
			Text:       "#333333",
			Background: "#ffffff",
			StatusBg:   "#ffffff",
			StatusFg:   "#333333",
			SelectedFg: "#ffffff",
			SelectedBg: "#008800",
			Success:    "#008800",
			Error:      "#cc0000",
			Accent:     "#008888",
			Dim:        "#aaaaaa",
		},
	},

	"flexoki": {
		Colors: ThemeColors{
			User:       "#CE5D97",
			Assistant:  "#DA702C",
			Header:     "#D14D41",
			Border:     "#403E3C",
			Hint:       "#878580",
			Text:       "#CECDC3",
			Background: "#100F0F",
			StatusBg:   "#1C1B1A",
			StatusFg:   "#878580",
			SelectedFg: "#100F0F",
			SelectedBg: "#FFFCF0",
			Success:    "#879A39",
			Error:      "#D14D41",
			Accent:     "#DA702C",
			Dim:        "#403E3C",
		},
	},
	"one-dark": {
		Colors: ThemeColors{
			User:       "#61AFEF",
			Assistant:  "#C678DD",
			Header:     "#E5C07B",
			Border:     "#3E4451",
			Hint:       "#5C6370",
			Text:       "#ABB2BF",
			Background: "#282C34",
			StatusBg:   "#21252B",
			StatusFg:   "#5C6370",
			SelectedFg: "#282C34",
			SelectedBg: "#61AFEF",
			Success:    "#98C379",
			Error:      "#E06C75",
			Accent:     "#56B6C2",
			Dim:        "#3E4451",
		},
	},
	"gruvbox": {
		Colors: ThemeColors{
			User:       "#b8bb26",
			Assistant:  "#83a598",
			Header:     "#fabd2f",
			Border:     "#504945",
			Hint:       "#665c54",
			Text:       "#ebdbb2",
			Background: "#282828",
			StatusBg:   "#282828",
			StatusFg:   "#bdae93",
			SelectedFg: "#282828",
			SelectedBg: "#fabd2f",
			Success:    "#b8bb26",
			Error:      "#fb4934",
			Accent:     "#83a598",
			Dim:        "#504945",
		},
	},
	"gruvbox-light": {
		Colors: ThemeColors{
			User:       "#b8bb26",
			Assistant:  "#076678",
			Header:     "#d79921",
			Border:     "#a89984",
			Hint:       "#928374",
			Text:       "#3c3836",
			Background: "#fbf1c7",
			StatusBg:   "#fbf1c7",
			StatusFg:   "#7c6f64",
			SelectedFg: "#fbf1c7",
			SelectedBg: "#d79921",
			Success:    "#b8bb26",
			Error:      "#cc241d",
			Accent:     "#076678",
			Dim:        "#a89984",
		},
	},
	"onedark": {
		Colors: ThemeColors{
			User:       "#61afef",
			Assistant:  "#c678dd",
			Header:     "#56b6c2",
			Border:     "#4b5263",
			Hint:       "#5c6370",
			Text:       "#abb2bf",
			Background: "#282c34",
			StatusBg:   "#282c34",
			StatusFg:   "#9da5b4",
			SelectedFg: "#282c34",
			SelectedBg: "#61afef",
			Success:    "#98c379",
			Error:      "#e06c75",
			Accent:     "#56b6c2",
			Dim:        "#4b5263",
		},
	},
	"nord": {
		Colors: ThemeColors{
			User:       "#88c0d0",
			Assistant:  "#b48ead",
			Header:     "#88c0d0",
			Border:     "#4c566a",
			Hint:       "#4c566a",
			Text:       "#d8dee9",
			Background: "#2e3440",
			StatusBg:   "#2e3440",
			StatusFg:   "#88c0d0",
			SelectedFg: "#2e3440",
			SelectedBg: "#88c0d0",
			Success:    "#a3be8c",
			Error:      "#bf616a",
			Accent:     "#8fbcbb",
			Dim:        "#4c566a",
		},
	},
	"nord-light": {
		Colors: ThemeColors{
			User:       "#5e81ac",
			Assistant:  "#b48ead",
			Header:     "#5e81ac",
			Border:     "#d8dee9",
			Hint:       "#7b88a1",
			Text:       "#2e3440",
			Background: "#eceff4",
			StatusBg:   "#eceff4",
			StatusFg:   "#4c566a",
			SelectedFg: "#eceff4",
			SelectedBg: "#5e81ac",
			Success:    "#a3be8c",
			Error:      "#bf616a",
			Accent:     "#88c0d0",
			Dim:        "#d8dee9",
		},
	},
	"kanagawa": {
		Colors: ThemeColors{
			User:       "#7e9cd8",
			Assistant:  "#957fb8",
			Header:     "#7e9cd8",
			Border:     "#363b44",
			Hint:       "#545464",
			Text:       "#dcd7ba",
			Background: "#1f1f28",
			StatusBg:   "#1f1f28",
			StatusFg:   "#72767e",
			SelectedFg: "#1f1f28",
			SelectedBg: "#7e9cd8",
			Success:    "#98bb6c",
			Error:      "#c34043",
			Accent:     "#7a9c6e",
			Dim:        "#363b44",
		},
	},
	"catppuccin-mocha": {
		Colors: ThemeColors{
			User:       "#89b4fa",
			Assistant:  "#cba6f7",
			Header:     "#89dceb",
			Border:     "#45475a",
			Hint:       "#585b70",
			Text:       "#cdd6f4",
			Background: "#1e1e2e",
			StatusBg:   "#1e1e2e",
			StatusFg:   "#6c7086",
			SelectedFg: "#1e1e2e",
			SelectedBg: "#89b4fa",
			Success:    "#a6e3a1",
			Error:      "#f38ba8",
			Accent:     "#89dceb",
			Dim:        "#45475a",
		},
	},
	"catppuccin-latte": {
		Colors: ThemeColors{
			User:       "#1e66f5",
			Assistant:  "#8839ef",
			Header:     "#04a5e5",
			Border:     "#bcc0cc",
			Hint:       "#9ca0b0",
			Text:       "#4c4f69",
			Background: "#eff1f5",
			StatusBg:   "#eff1f5",
			StatusFg:   "#9ca0b0",
			SelectedFg: "#eff1f5",
			SelectedBg: "#1e66f5",
			Success:    "#40a02b",
			Error:      "#d20f39",
			Accent:     "#04a5e5",
			Dim:        "#ccd0da",
		},
	},
	"dracula": {
		Colors: ThemeColors{
			User:       "#bd93f9",
			Assistant:  "#ff79c6",
			Header:     "#8be9fd",
			Border:     "#44475a",
			Hint:       "#6272a4",
			Text:       "#f8f8f2",
			Background: "#282a36",
			StatusBg:   "#282a36",
			StatusFg:   "#6272a4",
			SelectedFg: "#282a36",
			SelectedBg: "#bd93f9",
			Success:    "#50fa7b",
			Error:      "#ff5555",
			Accent:     "#8be9fd",
			Dim:        "#44475a",
		},
	},
	"solarized": {
		Colors: ThemeColors{
			User:       "#268bd2",
			Assistant:  "#6c71c4",
			Header:     "#2aa198",
			Border:     "#586e75",
			Hint:       "#657b83",
			Text:       "#839496",
			Background: "#002b36",
			StatusBg:   "#002b36",
			StatusFg:   "#657b83",
			SelectedFg: "#002b36",
			SelectedBg: "#268bd2",
			Success:    "#859900",
			Error:      "#dc322f",
			Accent:     "#b58900",
			Dim:        "#586e75",
		},
	},
	"solarized-light": {
		Colors: ThemeColors{
			User:       "#268bd2",
			Assistant:  "#6c71c4",
			Header:     "#2aa198",
			Border:     "#93a1a1",
			Hint:       "#93a1a1",
			Text:       "#657b83",
			Background: "#fdf6e3",
			StatusBg:   "#fdf6e3",
			StatusFg:   "#93a1a1",
			SelectedFg: "#fdf6e3",
			SelectedBg: "#268bd2",
			Success:    "#859900",
			Error:      "#dc322f",
			Accent:     "#b58900",
			Dim:        "#93a1a1",
		},
	},
	"github-dark": {
		Colors: ThemeColors{
			User:       "#58a6ff",
			Assistant:  "#bc8cff",
			Header:     "#58a6ff",
			Border:     "#30363d",
			Hint:       "#6e7681",
			Text:       "#c9d1d9",
			Background: "#0d1117",
			StatusBg:   "#0d1117",
			StatusFg:   "#6e7681",
			SelectedFg: "#0d1117",
			SelectedBg: "#58a6ff",
			Success:    "#3fb950",
			Error:      "#f85149",
			Accent:     "#58a6ff",
			Dim:        "#30363d",
		},
	},
	"github-light": {
		Colors: ThemeColors{
			User:       "#0969da",
			Assistant:  "#8250df",
			Header:     "#0969da",
			Border:     "#d0d7de",
			Hint:       "#6e7781",
			Text:       "#1f2328",
			Background: "#ffffff",
			StatusBg:   "#ffffff",
			StatusFg:   "#6e7781",
			SelectedFg: "#ffffff",
			SelectedBg: "#0969da",
			Success:    "#1a7f37",
			Error:      "#cf222e",
			Accent:     "#0969da",
			Dim:        "#d0d7de",
		},
	},
	"everforest": {
		Colors: ThemeColors{
			User:       "#7fbbb3",
			Assistant:  "#a37acc",
			Header:     "#d699b6",
			Border:     "#5c6a72",
			Hint:       "#859289",
			Text:       "#d3c6ab",
			Background: "#2d353b",
			StatusBg:   "#2d353b",
			StatusFg:   "#859289",
			SelectedFg: "#2d353b",
			SelectedBg: "#7fbbb3",
			Success:    "#a7c080",
			Error:      "#e67e80",
			Accent:     "#e69875",
			Dim:        "#5c6a72",
		},
	},
	"everforest-light": {
		Colors: ThemeColors{
			User:       "#3a94c5",
			Assistant:  "#7a3e9d",
			Header:     "#c3807a",
			Border:     "#c8c0aa",
			Hint:       "#7a8582",
			Text:       "#5c6a72",
			Background: "#fdf6e3",
			StatusBg:   "#fdf6e3",
			StatusFg:   "#7a8582",
			SelectedFg: "#fdf6e3",
			SelectedBg: "#3a94c5",
			Success:    "#8da101",
			Error:      "#f85552",
			Accent:     "#dfa000",
			Dim:        "#c8c0aa",
		},
	},
	"pipboy": {
		Colors: ThemeColors{
			User:       "#00ff9f",
			Assistant:  "#50fa7b",
			Header:     "#39ff14",
			Border:     "#2d5a2d",
			Hint:       "#5aaa5a",
			Text:       "#75fb4c",
			Background: "#0a110a",
			StatusBg:   "#0a110a",
			StatusFg:   "#66b366",
			SelectedFg: "#0a110a",
			SelectedBg: "#75fb4c",
			Success:    "#39ff14",
			Error:      "#ff5555",
			Accent:     "#00e5ff",
			Dim:        "#2d5a2d",
			Thinking:   "#00ff9f",
		},
	},
	"lcars": {
		Label:  "lcars (Star Trek OS)",
		Colors: ThemeColors{
			User:       "#FF9F1C",
			Assistant:  "#77C8FF",
			Header:     "#D98CFF",
			Border:     "#FF7A1A",
			Hint:       "#8E6E55",
			Text:       "#F7D28B",
			Background: "#070912",
			StatusBg:   "#0E1020",
			StatusFg:   "#FFB84D",
			SelectedFg: "#070912",
			SelectedBg: "#FFB84D",
			Success:    "#B7FF7A",
			Error:      "#FF4D4D",
			Accent:     "#77C8FF",
			Dim:        "#3A2B1A",
			Thinking:   "#77C8FF",
		},
	},
}

// ── Opencode-format JSON support ──
// opencodeThemeFile matches the schema at
// https://opencode.ai/desktop-theme.json
type opencodeVariant struct {
	Palette struct {
		Neutral     string `json:"neutral"`
		Ink         string `json:"ink"`
		Primary     string `json:"primary"`
		Accent      string `json:"accent,omitempty"`
		Success     string `json:"success"`
		Warning     string `json:"warning"`
		Error       string `json:"error"`
		Info        string `json:"info"`
		Interactive string `json:"interactive,omitempty"`
		DiffAdd     string `json:"diffAdd,omitempty"`
		DiffDelete  string `json:"diffDelete,omitempty"`
	} `json:"palette"`
	Overrides map[string]string `json:"overrides"`
}

type opencodeThemeFile struct {
	Name  string          `json:"name"`
	ID    string          `json:"id"`
	Light opencodeVariant `json:"light"`
	Dark  opencodeVariant `json:"dark"`
}

// convertOpencodeVariant maps an opencode variant (dark or light) to ocode's
// ThemeColors.  The variant parameter is "dark" or "light" for logging only.
func convertOpencodeVariant(v opencodeVariant, variant string) ThemeColors {
	p := v.Palette
	o := v.Overrides

	// Helper: pick first non-empty string, with fallback.
	first := func(vals ...string) string {
		for _, s := range vals {
			if s != "" {
				return s
			}
		}
		return "#000000"
	}

	textWeak := first(o["text-weak"], o["syntax-comment"], p.Ink)
	syntaxComment := first(o["syntax-comment"], o["text-weak"], p.Ink)
	borderColor := first(o["syntax-comment"], o["text-weak"])
	if borderColor == "" || borderColor == p.Neutral {
		// Derive a visible border: use ink at low opacity by blending logic.
		// Simplest: use syntaxComment if available.
		borderColor = syntaxComment
	}

	return ThemeColors{
		User:       p.Primary,
		Assistant:  first(p.Accent, o["syntax-keyword"], p.Info),
		Header:     first(p.Info, p.Primary, p.Warning),
		Border:     borderColor,
		Hint:       textWeak,
		Text:       p.Ink,
		Background: p.Neutral,
		StatusBg:   p.Neutral,
		StatusFg:   textWeak,
		SelectedFg: p.Neutral,
		SelectedBg: p.Primary,
		Success:    p.Success,
		Error:      p.Error,
		Accent:     first(p.Accent, o["syntax-constant"], p.Info),
		Dim:        borderColor,
		Thinking:   textWeak,
	}
}

// detectOpencodeJSON returns true if the JSON data looks like an opencode
// desktop-theme file (has "light" and "dark" keys at the top level).
func detectOpencodeJSON(data []byte) bool {
	var probe struct {
		Light json.RawMessage `json:"light"`
		Dark  json.RawMessage `json:"dark"`
	}
	return json.Unmarshal(data, &probe) == nil && len(probe.Light) > 0 && len(probe.Dark) > 0
}

var themeRegistry = map[string]ThemeDefinition{}

func init() {
	for k, v := range builtinThemes {
		themeRegistry[k] = v
	}
	for k, v := range generatedThemes {
		themeRegistry[k] = v
	}
	loadThemesFromDir(themeRegistry)
}

func loadThemesFromDir(registry map[string]ThemeDefinition) {
	dirs := themeSearchPaths()
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			// Try ocode-native format first.
			var theme ThemeDefinition
			if err := json.Unmarshal(data, &theme); err == nil && theme.Colors.User != "" {
				name := strings.TrimSuffix(entry.Name(), ".json")
				registry[name] = theme
				continue
			}
			// Fall back to opencode desktop-theme format.
			if detectOpencodeJSON(data) {
				var oc opencodeThemeFile
				if err := json.Unmarshal(data, &oc); err != nil {
					continue
				}
				// Register dark variant with the file's ID.
				if oc.Dark.Palette.Neutral != "" {
					registry[oc.ID] = ThemeDefinition{Colors: convertOpencodeVariant(oc.Dark, "dark")}
				}
				// Register light variant with "-light" suffix.
				if oc.Light.Palette.Neutral != "" {
					registry[oc.ID+"-light"] = ThemeDefinition{Colors: convertOpencodeVariant(oc.Light, "light")}
				}
			}
		}
	}
}

func themeSearchPaths() []string {
	home, _ := os.UserHomeDir()
	var paths []string

	globalDir := filepath.Join(home, ".config", "opencode", "themes")
	if runtime.GOOS == "windows" {
		if appdata := os.Getenv("APPDATA"); appdata != "" {
			globalDir = filepath.Join(appdata, "opencode", "themes")
		}
	}
	paths = append(paths, globalDir)

	projectDir := config.FindProjectRoot()
	if projectDir != "" {
		paths = append(paths, filepath.Join(projectDir, ".opencode", "themes"))
		paths = append(paths, filepath.Join(projectDir, "themes"))
	}

	return paths
}

// GetTheme returns the ThemeDefinition for name and whether it was found in the
// registry (builtin + generated + disk-loaded themes).
func GetTheme(name string) (ThemeDefinition, bool) {
	t, ok := themeRegistry[name]
	return t, ok
}

// Get returns the ThemeDefinition for the given name, resolving against the
// full registry (builtin + generated + disk-loaded themes). Unknown names fall
// back to the default theme (tokyonight) — this is deliberate config
// resolution, not a silent error swallow.
func Get(name string) ThemeDefinition {
	if t, ok := themeRegistry[name]; ok {
		return t
	}
	return themeRegistry["tokyonight"]
}

// AvailableThemes returns all registered theme names, sorted alphabetically.
func AvailableThemes() []string {
	names := make([]string, 0, len(themeRegistry))
	for k := range themeRegistry {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// DisplayName returns the human-readable label for a theme, falling back to
// the internal name if no label is set.
func DisplayName(name string) string {
	if t, ok := themeRegistry[name]; ok && t.Label != "" {
		return t.Label
	}
	return name
}
