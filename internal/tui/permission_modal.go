package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// PermissionModalConfig configures a PermissionModal.
type PermissionModalConfig struct {
	Title         string
	Body          string
	Buttons       []ButtonConfig
	TermWidth     int
	TermHeight    int
	MaxBodyHeight int
	OnChoice      func(choice string) // called with "allow"/"deny"/"always-allow"/"always-tool"
}

// PermissionModal is a Modal that wraps a Dialog for permission requests.
// It handles keyboard shortcuts (y/n/a/esc/enter) and delegates to OnChoice.
type PermissionModal struct {
	Dialog   *Dialog
	config   PermissionModalConfig
	callback func(string)
	done     bool // set to true after a choice is made
}

// NewPermissionModal creates a new PermissionModal.
func NewPermissionModal(cfg PermissionModalConfig) *PermissionModal {
	if cfg.MaxBodyHeight == 0 {
		cfg.MaxBodyHeight = 10
	}

	pm := &PermissionModal{
		config:   cfg,
		callback: cfg.OnChoice,
	}

	pm.Dialog = NewDialog(cfg.Title, cfg.Body, cfg.Buttons, cfg.TermWidth-4, cfg.TermHeight-2)
	pm.Dialog.TermWidth = cfg.TermWidth
	pm.Dialog.TermHeight = cfg.TermHeight
	pm.Dialog.SetMaxBodyHeight(cfg.MaxBodyHeight)

	return pm
}

// Handle processes keyboard messages. Returns true if consumed.
func (pm *PermissionModal) Handle(msg tea.Msg) bool {
	if pm.done {
		return false
	}

	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return false
	}

	// Map keyboard shortcuts to choices.
	key := keyMsg.Key()
	var choice string

	switch key.Code {
	case 'y', 'Y':
		choice = "allow"
	case 'n', 'N', tea.KeyEsc:
		choice = "deny"
	case 'a', 'A':
		choice = "always-allow"
	case 't', 'T':
		choice = "always-tool"
	case tea.KeyEnter:
		// Enter confirms the currently focused button (default: first = allow)
		choice = "allow"
	case tea.KeyUp:
		pm.Dialog.ScrollUp(1)
		return true
	case tea.KeyDown:
		pm.Dialog.ScrollDown(1)
		return true
	default:
		return false
	}

	if choice != "" {
		pm.done = true
		if pm.callback != nil {
			pm.callback(choice)
		}
		return true
	}

	return false
}

// Render renders the permission dialog.
func (pm *PermissionModal) Render() string {
	return pm.Dialog.Render()
}

// Bounds returns the dialog bounds.
func (pm *PermissionModal) Bounds() Rect {
	return pm.Dialog.Bounds()
}

// ScrollOffset returns the body scroll offset.
func (pm *PermissionModal) ScrollOffset() int {
	return pm.Dialog.ScrollOffset()
}

// ScrollDown scrolls the body down.
func (pm *PermissionModal) ScrollDown(n int) {
	pm.Dialog.ScrollDown(n)
}

// ScrollUp scrolls the body up.
func (pm *PermissionModal) ScrollUp(n int) {
	pm.Dialog.ScrollUp(n)
}

// IsDone returns whether a choice has been made.
func (pm *PermissionModal) IsDone() bool {
	return pm.done
}

// permissionKeyMap maps permission button choices to keyboard shortcuts.
// This is used by the model to display key hints next to buttons.
var permissionKeyMap = map[string]string{
	"allow":        "y",
	"deny":         "n",
	"always-allow": "a",
	"always-tool":  "t",
}

// FormatPermissionButtons formats button configs with key hints.
func FormatPermissionButtons(defs []permBtnDef) []ButtonConfig {
	configs := make([]ButtonConfig, len(defs))
	for i, d := range defs {
		variant := ButtonNormal
		if d.choice == "allow" || d.choice == "always-allow" {
			variant = ButtonPrimary
		} else if d.choice == "deny" {
			variant = ButtonDanger
		}

		// Add key hint to label.
		keyHint := permissionKeyMap[d.choice]
		label := d.label
		if keyHint != "" {
			label = label + " (" + keyHint + ")"
		}

		configs[i] = ButtonConfig{
			Label:   label,
			Variant: variant,
		}
	}
	return configs
}

// renderPermissionOverlay renders the permission dialog centered over a
// dimmed backdrop. This is the new rendering path that replaces the inline
// bottom-chrome placement.
func renderPermissionOverlay(pm *PermissionModal, backdrop string, termWidth, termHeight int) string {
	if pm == nil {
		return backdrop
	}

	dialogRendered := pm.Render()
	bounds := pm.Bounds()

	// Center the dialog.
	x := (termWidth - bounds.Width) / 2
	y := (termHeight - bounds.Height) / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}

	// Dim the backdrop.
	dimmed := dimLines(strings.Split(backdrop, "\n"))
	dimmedStr := strings.Join(dimmed, "\n")

	return compositeOverlay(dimmedStr, dialogRendered, x, y)
}
