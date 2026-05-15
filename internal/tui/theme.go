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
	Border     string `json:"border"`
	Hint       string `json:"hint"`
	Background string `json:"background"`
	StatusBg   string `json:"status_bg"`
	StatusFg   string `json:"status_fg"`
}

type ThemeDefinition struct {
	Colors ThemeColors `json:"colors"`
}

var builtinThemes = map[string]ThemeDefinition{
	"tokyonight": {
		Colors: ThemeColors{
			User:       "#7aa2f7",
			Assistant:  "#bb9af7",
			Border:     "#3b4261",
			Hint:       "#565f89",
			Background: "#1a1b26",
			StatusBg:   "#1a1b26",
			StatusFg:   "#565f89",
		},
	},
	"opencode": {
		Colors: ThemeColors{
			User:       "#00ff00",
			Assistant:  "#00ffff",
			Border:     "#444444",
			Hint:       "#888888",
			Background: "#000000",
			StatusBg:   "#000000",
			StatusFg:   "#00ff00",
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

func ApplyThemeColors(name string) {
	theme, ok := GetTheme(name)
	if !ok {
		return
	}

	setUserStyle(lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Colors.User)).Bold(true))
	setAssistantStyle(lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Colors.Assistant)).Bold(true))
	setBorderStyle(lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(theme.Colors.Border)).
		Padding(0, 1))
	setHintStyle(lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Colors.Hint)).Italic(true))
}

func setUserStyle(s lipgloss.Style) {
	userStyle = s
}

func setAssistantStyle(s lipgloss.Style) {
	assistantStyle = s
}

func setBorderStyle(s lipgloss.Style) {
	borderStyle = s
}

func setHintStyle(s lipgloss.Style) {
	hintStyle = s
}
