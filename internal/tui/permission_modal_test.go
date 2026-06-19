package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// --- PermissionModal tests ---
// These test the PermissionModal as a Modal that wraps a Dialog,
// before full integration with model.go.

func TestPermissionModalImplementsModal(t *testing.T) {
	var _ Modal = (*PermissionModal)(nil)
}

func TestPermissionModalRender(t *testing.T) {
	pm := NewPermissionModal(PermissionModalConfig{
		Title:      "Permission required",
		Body:       "bash wants to run: rm -rf /tmp/test",
		Buttons:    []ButtonConfig{{Label: "Allow", Variant: ButtonPrimary}, {Label: "Deny", Variant: ButtonDanger}},
		TermWidth:  80,
		TermHeight: 24,
	})

	rendered := pm.Render()
	if rendered == "" {
		t.Fatal("expected non-empty render")
	}
	if !strings.Contains(rendered, "Permission required") {
		t.Error("rendered output missing title")
	}
	if !strings.Contains(rendered, "rm -rf /tmp/test") {
		t.Error("rendered output missing body")
	}
}

func TestPermissionModalBounds(t *testing.T) {
	pm := NewPermissionModal(PermissionModalConfig{
		Title:      "Test",
		Body:       "Body",
		Buttons:    []ButtonConfig{{Label: "OK", Variant: ButtonNormal}},
		TermWidth:  80,
		TermHeight: 24,
	})

	bounds := pm.Bounds()
	if bounds.Width <= 0 || bounds.Height <= 0 {
		t.Errorf("expected positive bounds, got %dx%d", bounds.Width, bounds.Height)
	}
}

func TestPermissionModalKeyYAllow(t *testing.T) {
	var callbackChoice string
	pm := NewPermissionModal(PermissionModalConfig{
		Title: "Test",
		Body:  "Body",
		Buttons: []ButtonConfig{
			{Label: "Allow", Variant: ButtonPrimary},
			{Label: "Deny", Variant: ButtonDanger},
		},
		TermWidth:  80,
		TermHeight: 24,
		OnChoice: func(choice string) {
			callbackChoice = choice
		},
	})

	keyMsg := mockKeyMsg{code: 'y'}
	consumed := pm.Handle(keyMsg)
	if !consumed {
		t.Error("y key should be consumed")
	}
	if callbackChoice != "allow" {
		t.Errorf("expected callback choice 'allow', got %q", callbackChoice)
	}
}

func TestPermissionModalKeyNEscDeny(t *testing.T) {
	var callbackChoice string
	pm := NewPermissionModal(PermissionModalConfig{
		Title: "Test",
		Body:  "Body",
		Buttons: []ButtonConfig{
			{Label: "Allow", Variant: ButtonPrimary},
			{Label: "Deny", Variant: ButtonDanger},
		},
		TermWidth:  80,
		TermHeight: 24,
		OnChoice: func(choice string) {
			callbackChoice = choice
		},
	})

	keyMsg := mockKeyMsg{code: tea.KeyEsc}
	consumed := pm.Handle(keyMsg)
	if !consumed {
		t.Error("esc key should be consumed")
	}
	if callbackChoice != "deny" {
		t.Errorf("expected callback choice 'deny', got %q", callbackChoice)
	}
}

func TestPermissionModalScroll(t *testing.T) {
	longBody := strings.Repeat("Line of permission text\n", 20)
	pm := NewPermissionModal(PermissionModalConfig{
		Title:         "Test",
		Body:          longBody,
		Buttons:       []ButtonConfig{{Label: "OK", Variant: ButtonNormal}},
		TermWidth:     80,
		TermHeight:    24,
		MaxBodyHeight: 5,
	})

	// Initially at top
	if pm.ScrollOffset() != 0 {
		t.Errorf("expected initial scroll 0, got %d", pm.ScrollOffset())
	}

	// Scroll down
	pm.ScrollDown(3)
	if pm.ScrollOffset() != 3 {
		t.Errorf("expected scroll 3, got %d", pm.ScrollOffset())
	}
}

func TestPermissionModalCenteredOverlay(t *testing.T) {
	pm := NewPermissionModal(PermissionModalConfig{
		Title:      "Test",
		Body:       "Body text",
		Buttons:    []ButtonConfig{{Label: "OK", Variant: ButtonNormal}},
		TermWidth:  80,
		TermHeight: 20,
	})

	// Render the dialog
	dialogRendered := pm.Dialog.Render()

	// Create a dimmed backdrop matching term width
	backdropLine := strings.Repeat("A", 80)
	backdrop := strings.Repeat(backdropLine+"\n", 20)
	dimmed := dimLines(strings.Split(strings.TrimRight(backdrop, "\n"), "\n"))
	dimmedStr := strings.Join(dimmed, "\n")

	// Center the dialog
	bounds := pm.Dialog.Bounds()
	x := (80 - bounds.Width) / 2
	y := (20 - bounds.Height) / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}

	// Composite
	result := compositeOverlay(dimmedStr, dialogRendered, x, y)

	// Should contain the dialog content
	if !strings.Contains(stripANSI(result), "Body text") {
		t.Error("composite result missing dialog body")
	}

	// Should contain dimmed backdrop around it
	stripped := stripANSI(result)
	if !strings.Contains(stripped, "AAAAAAAAAA") {
		t.Error("composite result missing backdrop content")
	}
}

func TestPermissionModalEmptyCallback(t *testing.T) {
	pm := NewPermissionModal(PermissionModalConfig{
		Title:      "Test",
		Body:       "Body",
		Buttons:    []ButtonConfig{{Label: "OK", Variant: ButtonNormal}},
		TermWidth:  80,
		TermHeight: 24,
		// No OnChoice set
	})

	// Should not panic
	keyMsg := mockKeyMsg{code: 'y'}
	pm.Handle(keyMsg)
}
