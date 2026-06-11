package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// permDialogModal adapts the existing permission dialog state machine
// (permDialogInput, permConfirm, permButtonRegions) into a Modal interface
// so it can be pushed onto a ModalStack. This is a transitional adapter —
// it delegates to the model's existing methods during migration.
type permDialogModal struct {
	m *model
}

func (p *permDialogModal) Handle(msg tea.Msg) bool {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return false
	}

	keyStr := keyMsg.String()

	// Map key names to the choices permDialogInput expects.
	var choice string
	switch keyStr {
	case "y", "Y":
		choice = "y"
	case "n", "N", "esc", "esc+" :
		choice = "n"
	case "a", "A":
		choice = "a"
	case "t", "T":
		choice = "t"
	case "enter":
		choice = "y" // default to allow on enter
	case "up":
		p.m.permViewport.ScrollUp(p.m.scrollSpeed)
		return true
	case "down":
		p.m.permViewport.ScrollDown(p.m.scrollSpeed)
		return true
	case "pgup":
		p.m.permViewport.HalfPageUp()
		return true
	case "pgdown":
		p.m.permViewport.HalfPageDown()
		return true
	case "ctrl+u":
		p.m.permViewport.HalfPageUp()
		return true
	case "ctrl+d":
		p.m.permViewport.HalfPageDown()
		return true
	case "backspace":
		if p.m.permConfirm != "" {
			choice = "back"
		}
	default:
		return false
	}

	if choice != "" {
		_, _ = p.m.permDialogInput(choice)
		return true
	}
	return false
}

func (p *permDialogModal) Render() string {
	if !p.m.showPermDialog {
		return ""
	}
	return p.m.renderPermissionDialog(p.m.panelWidth())
}

func (p *permDialogModal) Bounds() Rect {
	w := p.m.panelWidth()
	contentWidth := max(0, w-2)
	body := renderPermissionRequestBody(p.m.pendingPermission)
	if p.m.permConfirm != "" {
		body = renderPermConfirmBody(p.m.pendingPermission, p.m.pendingToolName, p.m.permConfirm)
	}
	bodyLines := strings.Count(body, "\n") + 1
	if bodyLines > permissionDialogMaxBodyLines {
		bodyLines = permissionDialogMaxBodyLines
	}
	// header(1) + blank(1) + body + blank(1) + buttons(1) + borders(2)
	height := 4 + bodyLines
	return Rect{
		X:      0,
		Y:      0,
		Width:  contentWidth,
		Height: height,
	}
}

// pushPermissionModal pushes the permission dialog adapter onto the modal stack
// if not already present. No-op if modalStack is nil (e.g. in tests).
func (m *model) pushPermissionModal() {
	if m.modalStack == nil {
		return
	}
	if _, ok := m.modalStack.Top().(*permDialogModal); !ok {
		m.modalStack.Push(&permDialogModal{m: m})
	}
}

// popPermissionModal pops the permission dialog from the modal stack.
// No-op if modalStack is nil.
func (m *model) popPermissionModal() {
	if m.modalStack == nil {
		return
	}
	if m.modalStack.Top() != nil {
		if _, ok := m.modalStack.Top().(*permDialogModal); ok {
			m.modalStack.Pop()
		}
	}
}

// renderPermissionOverlay renders the permission dialog as a centered overlay
// with a dimmed backdrop, replacing the inline bottom-chrome placement.
func (m *model) renderPermissionOverlay(backdrop string) string {
	if m.modalStack.Top() == nil {
		return backdrop
	}

	top := m.modalStack.Top()
	dialogRendered := top.Render()
	bounds := top.Bounds()

	termW := m.width
	termH := m.height

	// Center the dialog.
	x := (termW - bounds.Width) / 2
	y := (termH - bounds.Height) / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}

	// Dim the backdrop.
	lines := strings.Split(backdrop, "\n")
	dimmed := dimLines(lines)
	dimmedStr := strings.Join(dimmed, "\n")

	return compositeOverlay(dimmedStr, dialogRendered, x, y)
}

// permDialogBtnStyles returns the button styles for the current permission
// dialog state, using the existing permBtnStyle/permBtnHoverStyle.
func permDialogBtnStyles(hoverChoice string, btns []permBtnDef) []ButtonConfig {
	configs := make([]ButtonConfig, len(btns))
	for i, b := range btns {
		variant := ButtonNormal
		if b.choice == "allow" || b.choice == "always-allow" {
			variant = ButtonPrimary
		} else if b.choice == "deny" {
			variant = ButtonDanger
		}
		configs[i] = ButtonConfig{
			Label:   b.label + " " + b.desc,
			Variant: variant,
		}
	}
	return configs
}

// Unused but kept for reference during migration.
var _ lipgloss.Style = permBtnStyle
