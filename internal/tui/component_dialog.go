package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// ButtonConfig describes a button in a Dialog's button row.
type ButtonConfig struct {
	Label   string
	Variant ButtonVariant
}

// Dialog is a bordered dialog box with a title, scrollable body, and button row.
// The caller composites it onto a backdrop via compositeOverlay.
type Dialog struct {
	title   string
	body    string
	buttons []*Button

	// Layout dimensions (set by the caller).
	TermWidth  int
	TermHeight int

	// Body scroll state.
	scrollOffset  int
	maxBodyHeight int

	// Computed layout (populated by layout()).
	width       int
	height      int
	bodyTop     int
	bodyHeight  int
	buttonRowY  int
	contentTopY int // first row of the dialog (after border)
}

// NewDialog creates a new Dialog with the given title, body text, and buttons.
// preferredWidth/preferredHeight are the desired dimensions before clamping.
func NewDialog(title, body string, btnConfigs []ButtonConfig, preferredWidth, preferredHeight int) *Dialog {
	buttons := make([]*Button, len(btnConfigs))
	for i, cfg := range btnConfigs {
		buttons[i] = NewButton(cfg.Label, cfg.Variant)
	}

	d := &Dialog{
		title:         title,
		body:          body,
		buttons:       buttons,
		TermWidth:     80,
		TermHeight:    24,
		maxBodyHeight: 10,
		width:         preferredWidth,
		height:        preferredHeight,
	}
	d.layout()
	return d
}

// SetMaxBodyHeight sets the maximum height of the scrollable body area.
func (d *Dialog) SetMaxBodyHeight(h int) {
	d.maxBodyHeight = h
	d.layout()
}

// ScrollOffset returns the current scroll offset.
func (d *Dialog) ScrollOffset() int {
	return d.scrollOffset
}

// ScrollDown scrolls the body down by n lines.
func (d *Dialog) ScrollDown(n int) {
	d.scrollOffset += n
	d.clampScroll()
}

// ScrollUp scrolls the body up by n lines.
func (d *Dialog) ScrollUp(n int) {
	d.scrollOffset -= n
	d.clampScroll()
}

func (d *Dialog) clampScroll() {
	if d.scrollOffset < 0 {
		d.scrollOffset = 0
	}
	maxScroll := d.bodyLineCount() - d.bodyHeight
	if maxScroll < 0 {
		maxScroll = 0
	}
	if d.scrollOffset > maxScroll {
		d.scrollOffset = maxScroll
	}
}

// bodyLineCount returns the number of visual lines in the body text.
func (d *Dialog) bodyLineCount() int {
	if d.body == "" {
		return 0
	}
	return len(strings.Split(d.body, "\n"))
}

// Bounds returns the overall dialog bounds.
func (d *Dialog) Bounds() Rect {
	return Rect{X: 0, Y: 0, Width: d.width, Height: d.height}
}

// ButtonBounds returns the bounds of each button relative to the dialog origin.
func (d *Dialog) ButtonBounds() []Rect {
	if len(d.buttons) == 0 {
		return nil
	}

	// Render buttons to get their positions.
	innerWidth := d.width - 2 // minus borders
	ox := (innerWidth - buttonRowVisualWidth(d.buttons)) / 2
	if ox < 1 {
		ox = 1
	}

	bounds := make([]Rect, len(d.buttons))
	cursor := ox
	for i, btn := range d.buttons {
		rendered := btn.Render()
		w := lipgloss.Width(rendered)
		bounds[i] = Rect{
			X:      cursor,
			Y:      d.buttonRowY,
			Width:  w,
			Height: 1,
		}
		cursor += w + 2
	}
	return bounds
}

// layout computes the dialog's internal layout, clamping to terminal size.
func (d *Dialog) layout() {
	// Clamp width to terminal.
	if d.width > d.TermWidth-2 {
		d.width = d.TermWidth - 2
	}
	if d.width < 10 {
		d.width = 10
	}

	// Clamp height to terminal.
	if d.height > d.TermHeight-2 {
		d.height = d.TermHeight - 2
	}
	if d.height < 5 {
		d.height = 5
	}

	innerHeight := d.height - 2 // minus top+bottom borders

	// Title row takes 1 line.
	titleRow := 1

	// Button row takes 1 line (if buttons exist).
	buttonRows := 0
	if len(d.buttons) > 0 {
		buttonRows = 1
	}

	// Body gets the remaining space.
	d.bodyHeight = innerHeight - titleRow - buttonRows
	if d.bodyHeight < 1 {
		d.bodyHeight = 1
	}
	if d.bodyHeight > d.maxBodyHeight {
		d.bodyHeight = d.maxBodyHeight
	}

	// Recalculate total height to fit the actual content.
	d.height = titleRow + d.bodyHeight + buttonRows + 2 // +2 for borders
	if d.height > d.TermHeight-2 {
		d.height = d.TermHeight - 2
	}

	d.contentTopY = 0 // border top
	d.bodyTop = 1     // after top border + title
	d.buttonRowY = d.bodyTop + d.bodyHeight

	d.clampScroll()
}

// Render renders the dialog as a bordered box with title, body, and buttons.
func (d *Dialog) Render() string {
	d.layout()

	innerWidth := d.width - 2
	var lines []string

	// Top border with title.
	titleStr := d.title
	if ansi.StringWidth(titleStr) > innerWidth-2 {
		titleStr = ansi.Truncate(titleStr, innerWidth-2, "…")
	}
	titleLine := "╭─ " + titleStr + " " + strings.Repeat("─", innerWidth-ansi.StringWidth(titleStr)-3) + "╮"
	lines = append(lines, titleLine)

	// Body lines.
	bodyLines := strings.Split(d.body, "\n")
	bodyContentHeight := d.bodyHeight
	overflowTop := d.scrollOffset > 0
	overflowBottom := d.scrollOffset+bodyContentHeight < len(bodyLines)

	// Top scroll indicator.
	if overflowTop {
		lines = append(lines, "│ "+padRight("▲", innerWidth-1)+"│")
		bodyContentHeight--
	}

	// Body content.
	for i := 0; i < bodyContentHeight; i++ {
		lineIdx := d.scrollOffset + i
		var line string
		if lineIdx < len(bodyLines) {
			line = bodyLines[lineIdx]
		}
		// Truncate to inner width.
		lineWidth := ansi.StringWidth(line)
		if lineWidth > innerWidth {
			line = ansi.Truncate(line, innerWidth, "…")
		} else {
			line = line + strings.Repeat(" ", innerWidth-lineWidth)
		}
		lines = append(lines, "│ "+line+"│")
	}

	// Bottom scroll indicator.
	if overflowBottom {
		lines = append(lines, "│ "+padRight("▼", innerWidth-1)+"│")
	}

	// Fill remaining body space if body is short.
	remaining := d.bodyHeight - (len(lines) - 1) // minus title line
	if overflowTop {
		remaining--
	}
	if overflowBottom {
		remaining--
	}
	for i := 0; i < remaining; i++ {
		lines = append(lines, "│ "+strings.Repeat(" ", innerWidth)+"│")
	}

	// Button row.
	if len(d.buttons) > 0 {
		// Render buttons centered.
		buttonStr := renderDialogButtons(d.buttons, innerWidth)
		lines = append(lines, "│ "+buttonStr+"│")
	}

	// Bottom border.
	lines = append(lines, "╰"+strings.Repeat("─", innerWidth)+"╯")

	// Clamp total height.
	if len(lines) > d.height {
		lines = lines[:d.height]
	}

	return strings.Join(lines, "\n")
}

// renderDialogButtons renders the button row centered within the given width.
func renderDialogButtons(buttons []*Button, width int) string {
	totalWidth := buttonRowVisualWidth(buttons)
	padding := (width - totalWidth) / 2
	if padding < 0 {
		padding = 0
	}

	var b strings.Builder
	b.WriteString(strings.Repeat(" ", padding))
	for i, btn := range buttons {
		if i > 0 {
			b.WriteString("  ")
		}
		b.WriteString(btn.Render())
	}
	remaining := width - padding - totalWidth
	if remaining > 0 {
		b.WriteString(strings.Repeat(" ", remaining))
	}
	return b.String()
}

// buttonRowVisualWidth returns the total visual width of the button row.
func buttonRowVisualWidth(buttons []*Button) int {
	total := 0
	for i, btn := range buttons {
		total += lipgloss.Width(btn.Render())
		if i > 0 {
			total += 2 // spacing between buttons
		}
	}
	return total
}

// padRight pads s to the right with spaces to fill width.
func padRight(s string, width int) string {
	w := ansi.StringWidth(s)
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}

// Rect represents a rectangular region for hit-testing.
type Rect struct {
	X, Y          int
	Width, Height int
}

// Contains reports whether the point (px, py) is inside the rect.
func (r Rect) Contains(px, py int) bool {
	return px >= r.X && px < r.X+r.Width && py >= r.Y && py < r.Y+r.Height
}
