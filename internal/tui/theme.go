package tui

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/u007/ocode/internal/theme"
)

// ThemeColors and ThemeDefinition are re-exported from internal/theme, the
// single source of truth for theme data and resolution. tui owns everything
// style-related (Styles, ApplyThemeColors, etc.).
type ThemeColors = theme.ThemeColors

type ThemeDefinition = theme.ThemeDefinition

type Styles struct {
	User, Assistant, Header, Hint, Border lipgloss.Style
	Text, Thinking, ThinkingHeader        lipgloss.Style
	Selected, Status, Success, Error      lipgloss.Style
	Dim, ToolBox, UserMessageBox          lipgloss.Style
	SidebarText                           lipgloss.Style
	// SearchHit is the accent style used to flash a transcript message whose
	// contents matched an in-chat /search query (and is now jumped to). It
	// stays foreground-only and Bold so the highlight reads on every theme
	// without dragging a background that would clash with the user/assistant
	// bubbles it overlays.
	SearchHit lipgloss.Style
}

// GetTheme resolves a theme by name against the shared registry.
func GetTheme(name string) (ThemeDefinition, bool) {
	return theme.GetTheme(name)
}

// AvailableThemes returns all registered theme names, sorted alphabetically.
func AvailableThemes() []string {
	return theme.AvailableThemes()
}

func ApplyThemeColors(name string) Styles {
	def, ok := GetTheme(name)
	if !ok {
		def = theme.Get("tokyonight")
	}
	c := def.Colors
	s := Styles{
		User:           lipgloss.NewStyle().Foreground(lipgloss.Color(c.User)).Bold(true),
		Assistant:      lipgloss.NewStyle().Foreground(lipgloss.Color(c.Assistant)).Bold(true),
		Header:         lipgloss.NewStyle().Foreground(lipgloss.Color(c.Header)).Bold(true),
		Hint:           lipgloss.NewStyle().Foreground(lipgloss.Color(c.Hint)).Italic(true),
		Border:         lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(c.Border)).Padding(0, 1),
		Selected:       lipgloss.NewStyle().Foreground(lipgloss.Color(c.SelectedFg)).Background(lipgloss.Color(c.SelectedBg)),
		Status:         lipgloss.NewStyle().Foreground(lipgloss.Color(c.StatusFg)).Background(lipgloss.Color(c.StatusBg)).Padding(0, 1).Bold(true),
		Success:        lipgloss.NewStyle().Foreground(lipgloss.Color(c.Success)),
		Error:          lipgloss.NewStyle().Foreground(lipgloss.Color(c.Error)),
		Text:           lipgloss.NewStyle().Foreground(lipgloss.Color(c.Text)),
		Thinking:       lipgloss.NewStyle().Foreground(lipgloss.Color(c.Hint)),
		ThinkingHeader: lipgloss.NewStyle().Foreground(lipgloss.Color(c.Accent)).Bold(true),
		Dim:            lipgloss.NewStyle().Foreground(lipgloss.Color(c.Dim)),
		SidebarText:    lipgloss.NewStyle().Foreground(lipgloss.Color(c.Hint)),
		ToolBox:        lipgloss.NewStyle().Foreground(lipgloss.Color(c.Text)).Background(lipgloss.Color(c.Background)).Padding(0, 1).Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(c.Border)),
		UserMessageBox: lipgloss.NewStyle().
			Foreground(lipgloss.Color(c.Text)).
			BorderStyle(lipgloss.ThickBorder()).
			BorderLeft(true).
			BorderTop(false).
			BorderRight(false).
			BorderBottom(false).
			BorderForeground(lipgloss.Color(c.Header)).
			Padding(0, 1),
		SearchHit: lipgloss.NewStyle().Foreground(lipgloss.Color(c.Accent)).Bold(true),
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
	setThinkingHeaderStyle(s.ThinkingHeader)
	setSidebarTextStyle(s.SidebarText)
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

func setThinkingHeaderStyle(s lipgloss.Style) {
	thinkingHeaderStyle = s
}

func setSidebarTextStyle(s lipgloss.Style) {
	sidebarTextStyle = s
}

func setDimStyle(s lipgloss.Style) {
	dimStyle = s
}

func setToolBoxStyle(s lipgloss.Style) {
	toolBoxStyle = s
}

func currentStyles() Styles {
	return Styles{
		User:           userStyle,
		Assistant:      assistantStyle,
		Header:         headerStyle,
		Hint:           hintStyle,
		Border:         borderStyle,
		Selected:       selectedStyle,
		Status:         statusStyle,
		Success:        successStyle,
		Error:          errorStyle,
		Text:           textStyle,
		Thinking:       thinkingStyle,
		ThinkingHeader: thinkingHeaderStyle,
		Dim:            dimStyle,
		SidebarText:    sidebarTextStyle,
		ToolBox:        toolBoxStyle,
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
