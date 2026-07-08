package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestURLDialogClickToConfirm verifies the mouse-driven escape hatch: while
// the URL confirmation dialog is open, a click inside the input area confirms
// and opens the URL (so a user who clicked the link can click again to open
// it), while a click elsewhere is swallowed and leaves the dialog open. This
// is the fix for "click URL -> input frozen, browser never opens".
func TestURLDialogClickToConfirm(t *testing.T) {
	m := model{
		showURLDialog: true,
		pendingURL:    "https://example.com",
		width:         120,
		height:        40,
	}

	// Click inside the input area (bottom region) confirms and opens.
	clickY := m.inputAreaTopY() + 2
	out, cmd, handled := m.handleMouseAction(tea.Mouse{X: 5, Y: clickY}, false)
	mm := out.(model)
	if !handled {
		t.Errorf("click inside input area while URL dialog is open should be handled")
	}
	if mm.showURLDialog {
		t.Errorf("click inside input area should confirm and close the URL dialog")
	}
	if mm.pendingURL != "" {
		t.Errorf("pendingURL should be cleared after confirm, got %q", mm.pendingURL)
	}
	if cmd == nil {
		t.Errorf("confirming via click should return a browser-open command")
	}

	// A click outside the input area must NOT confirm (dialog stays open).
	m2 := model{
		showURLDialog: true,
		pendingURL:    "https://example.com",
		width:         120,
		height:        40,
	}
	out2, _, handled2 := m2.handleMouseAction(tea.Mouse{X: 5, Y: 1}, false) // top of screen, above input area
	mm2 := out2.(model)
	if !handled2 {
		t.Errorf("click while URL dialog is open should be swallowed (handled=true)")
	}
	if !mm2.showURLDialog {
		t.Errorf("click outside input area must NOT close the URL dialog")
	}
}

// TestURLDialogClosePath drives the URL confirmation dialog through
// handleChatKeys the way the live Update loop does, to verify that confirming
// (Y/Enter) closes the dialog and returns a command, and cancelling (N/Esc)
// closes it without a command. This pins down whether the "click URL -> frozen
// input, browser never opens" report is a close-path defect or a UX issue.
func TestURLDialogClosePath(t *testing.T) {
	m := model{showURLDialog: true, pendingURL: "https://example.com"}

	// Confirm with 'y' — closes the dialog and returns a browser-open command.
	out, cmd := m.handleChatKeys(tea.KeyPressMsg{Code: 'y', Text: "y"}, nil, nil)
	mm := out.(model)
	if mm.showURLDialog {
		t.Errorf("'y' should close the URL dialog")
	}
	if cmd == nil {
		t.Errorf("'y' should return a command that opens the browser")
	}

	// Fresh model, confirm with Enter.
	m2 := model{showURLDialog: true, pendingURL: "https://example.com"}
	out2, cmd2 := m2.handleChatKeys(tea.KeyPressMsg{Code: tea.KeyEnter}, nil, nil)
	mm2 := out2.(model)
	if mm2.showURLDialog {
		t.Errorf("Enter should close the URL dialog")
	}
	if mm2.pendingURL != "" {
		t.Errorf("pendingURL should be cleared, got %q", mm2.pendingURL)
	}
	if cmd2 == nil {
		t.Errorf("confirming the URL dialog should return a command that opens the browser")
	}

	// Cancel with Esc.
	m3 := model{showURLDialog: true, pendingURL: "https://example.com"}
	out3, cmd3 := m3.handleChatKeys(tea.KeyPressMsg{Code: tea.KeyEscape}, nil, nil)
	mm3 := out3.(model)
	if mm3.showURLDialog {
		t.Errorf("Esc should close the URL dialog")
	}
	if cmd3 != nil {
		t.Errorf("cancelling the URL dialog should NOT return a command")
	}

	// An unrelated key must be swallowed (dialog stays open, no command).
	m4 := model{showURLDialog: true, pendingURL: "https://example.com"}
	out4, cmd4 := m4.handleChatKeys(tea.KeyPressMsg{Code: 'a', Text: "a"}, nil, nil)
	mm4 := out4.(model)
	if !mm4.showURLDialog {
		t.Errorf("unrelated key should NOT close the URL dialog")
	}
	if cmd4 != nil {
		t.Errorf("unrelated key should not return a command")
	}
}
