package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// ButtonVariant controls the visual style of a Button.
type ButtonVariant int

const (
	ButtonNormal ButtonVariant = iota
	ButtonPrimary
	ButtonDanger
)

// Button is a reusable button component with label, variant, and hover/focus states.
type Button struct {
	Label   string
	Variant ButtonVariant
	Hovered bool
	Focused bool

	// Position and size for hit-testing (set by the caller after rendering).
	X, Y, Width, Height int
}

// NewButton creates a new Button with the given label and variant.
func NewButton(label string, variant ButtonVariant) *Button {
	return &Button{
		Label:   label,
		Variant: variant,
	}
}

// Render returns the styled button string based on variant and state.
func (b *Button) Render() string {
	style := buttonBaseStyle(b.Variant)

	if b.Hovered {
		style = buttonHoverStyle(b.Variant)
	} else if b.Focused {
		style = buttonFocusStyle(b.Variant)
	}

	rendered := style.Render(b.Label)
	b.Width = lipgloss.Width(rendered)
	b.Height = 1
	return rendered
}

// Contains reports whether the screen coordinate (x, y) falls within the button bounds.
func (b *Button) Contains(x, y int) bool {
	return x >= b.X && x < b.X+b.Width && y >= b.Y && y < b.Y+b.Height
}

// RenderRow renders a row of buttons with spacing between them,
// recording each button's position relative to the given origin.
func RenderRow(buttons []*Button, ox, oy int) string {
	parts := make([]string, len(buttons))
	cursor := ox
	for i, btn := range buttons {
		rendered := btn.Render()
		btn.X = cursor
		btn.Y = oy
		parts[i] = rendered
		cursor += btn.Width + 2
	}
	return joinRow(parts)
}

func joinRow(parts []string) string {
	var b strings.Builder
	for i, p := range parts {
		if i > 0 {
			b.WriteString("  ")
		}
		b.WriteString(p)
	}
	return b.String()
}

// buttonBaseStyle returns the base style for a button variant.
func buttonBaseStyle(v ButtonVariant) lipgloss.Style {
	base := lipgloss.NewStyle().
		Bold(true).
		Padding(0, 1).
		Border(lipgloss.RoundedBorder())
	switch v {
	case ButtonPrimary:
		return base.Foreground(lipgloss.Color("#7AA2F7"))
	case ButtonDanger:
		return base.Foreground(lipgloss.Color("#F7768E"))
	default:
		return base
	}
}

// buttonHoverStyle returns the hover style for a button variant.
func buttonHoverStyle(v ButtonVariant) lipgloss.Style {
	base := lipgloss.NewStyle().
		Bold(true).
		Padding(0, 1).
		Border(lipgloss.RoundedBorder())
	switch v {
	case ButtonPrimary:
		return base.Foreground(lipgloss.Color("#1a1b26")).Background(lipgloss.Color("#7AA2F7"))
	case ButtonDanger:
		return base.Foreground(lipgloss.Color("#1a1b26")).Background(lipgloss.Color("#F7768E"))
	default:
		return base.Foreground(lipgloss.Color("#1a1b26")).Background(lipgloss.Color("#7AA2F7"))
	}
}

// buttonFocusStyle returns the focus style for a button variant.
func buttonFocusStyle(v ButtonVariant) lipgloss.Style {
	base := lipgloss.NewStyle().
		Bold(true).
		Padding(0, 1).
		Border(lipgloss.RoundedBorder())
	switch v {
	case ButtonPrimary:
		return base.Foreground(lipgloss.Color("#7AA2F7")).BorderForeground(lipgloss.Color("#7AA2F7"))
	case ButtonDanger:
		return base.Foreground(lipgloss.Color("#F7768E")).BorderForeground(lipgloss.Color("#F7768E"))
	default:
		return base.Foreground(lipgloss.Color("#7AA2F7")).BorderForeground(lipgloss.Color("#7AA2F7"))
	}
}