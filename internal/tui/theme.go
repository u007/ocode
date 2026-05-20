package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/jamesmercstudio/ocode/internal/config"
)

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

type ThemeDefinition struct {
	Colors ThemeColors `json:"colors"`
}

type Styles struct {
	User, Assistant, Header, Hint, Border lipgloss.Style
	Text, Thinking                        lipgloss.Style
	Selected, Status, Success, Error      lipgloss.Style
	Dim, ToolBox                          lipgloss.Style
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
			Assistant:  "#4385BE",
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
}

var themeRegistry = map[string]ThemeDefinition{}

func init() {
	for k, v := range builtinThemes {
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
			var theme ThemeDefinition
			if err := json.Unmarshal(data, &theme); err != nil {
				continue
			}
			name := strings.TrimSuffix(entry.Name(), ".json")
			registry[name] = theme
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

func GetTheme(name string) (ThemeDefinition, bool) {
	t, ok := themeRegistry[name]
	return t, ok
}

func AvailableThemes() []string {
	names := make([]string, 0, len(themeRegistry))
	for k := range themeRegistry {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

func ApplyThemeColors(name string) Styles {
	theme, ok := GetTheme(name)
	if !ok {
		theme = builtinThemes["tokyonight"]
	}
	c := theme.Colors
	s := Styles{
		User:      lipgloss.NewStyle().Foreground(lipgloss.Color(c.User)).Bold(true),
		Assistant: lipgloss.NewStyle().Foreground(lipgloss.Color(c.Assistant)).Bold(true),
		Header:    lipgloss.NewStyle().Foreground(lipgloss.Color(c.Header)).Bold(true),
		Hint:      lipgloss.NewStyle().Foreground(lipgloss.Color(c.Hint)).Italic(true),
		Border:    lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(c.Border)).Padding(0, 1),
		Selected:  lipgloss.NewStyle().Foreground(lipgloss.Color(c.SelectedFg)).Background(lipgloss.Color(c.SelectedBg)),
		Status:    lipgloss.NewStyle().Foreground(lipgloss.Color(c.StatusFg)).Background(lipgloss.Color(c.StatusBg)).Padding(0, 1).Bold(true),
		Success:   lipgloss.NewStyle().Foreground(lipgloss.Color(c.Success)),
		Error:     lipgloss.NewStyle().Foreground(lipgloss.Color(c.Error)),
		Text:      lipgloss.NewStyle().Foreground(lipgloss.Color(themeColor(c.Text, "#ffffff"))),
		Thinking:  lipgloss.NewStyle().Foreground(lipgloss.Color(themeColor(c.Thinking, c.Dim))).Italic(true),
		Dim:       lipgloss.NewStyle().Foreground(lipgloss.Color(c.Dim)),
		ToolBox:   lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(c.Dim)).Padding(0, 1),
	}
	setUserStyle(s.User)
	setAssistantStyle(s.Assistant)
	setHeaderStyle(s.Header)
	setBorderStyle(s.Border)
	setHintStyle(s.Hint)
	setSelectedStyle(s.Selected)
	setStatusStyle(s.Status)
	setSuccessStyle(s.Success)
	setErrorStyle(s.Error)
	setTextStyle(s.Text)
	setThinkingStyle(s.Thinking)
	setDimStyle(s.Dim)
	setToolBoxStyle(s.ToolBox)
	setTodoStyles(s.Dim, s.Header, s.Text)
	setScrollbarStyles(s.Dim, s.Selected)
	return s
}

func themeColor(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func setUserStyle(s lipgloss.Style) {
	userStyle = s
}

func setAssistantStyle(s lipgloss.Style) {
	assistantStyle = s
}

func setHeaderStyle(s lipgloss.Style) {
	headerStyle = s
}

func setBorderStyle(s lipgloss.Style) {
	borderStyle = s
}

func setHintStyle(s lipgloss.Style) {
	hintStyle = s
}

func setSelectedStyle(s lipgloss.Style) {
	selectedStyle = s
}

func setStatusStyle(s lipgloss.Style) {
	statusStyle = s
}

func setSuccessStyle(s lipgloss.Style) {
	successStyle = s
}

func setErrorStyle(s lipgloss.Style) {
	errorStyle = s
}

func setTextStyle(s lipgloss.Style) {
	textStyle = s
}

func setThinkingStyle(s lipgloss.Style) {
	thinkingStyle = s
}

func setDimStyle(s lipgloss.Style) {
	dimStyle = s
}

func setToolBoxStyle(s lipgloss.Style) {
	toolBoxStyle = s
}

func currentStyles() Styles {
	return Styles{
		User:      userStyle,
		Assistant: assistantStyle,
		Header:    headerStyle,
		Hint:      hintStyle,
		Border:    borderStyle,
		Selected:  selectedStyle,
		Status:    statusStyle,
		Success:   successStyle,
		Error:     errorStyle,
		Text:      textStyle,
		Thinking:  thinkingStyle,
		Dim:       dimStyle,
		ToolBox:   toolBoxStyle,
	}
}

func setScrollbarStyles(track, thumb lipgloss.Style) {
	scrollbarTrackStyle = lipgloss.NewStyle().Foreground(track.GetForeground())
	scrollbarThumbStyle = lipgloss.NewStyle().Foreground(thumb.GetBackground())
}

func setTodoStyles(done, inProgress, pending lipgloss.Style) {
	todoDoneStyle = lipgloss.NewStyle().Foreground(done.GetForeground()).Strikethrough(true)
	todoInProgressStyle = lipgloss.NewStyle().Foreground(inProgress.GetForeground()).Bold(true)
	todoPendingStyle = lipgloss.NewStyle().Foreground(pending.GetForeground())
}
