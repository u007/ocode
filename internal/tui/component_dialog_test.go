package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestDialogRenderBasic(t *testing.T) {
	d := NewDialog("Confirm", "Are you sure?", []ButtonConfig{
		{Label: "Yes", Variant: ButtonPrimary},
		{Label: "No", Variant: ButtonNormal},
	}, 40, 10)

	rendered := d.Render()
	if rendered == "" {
		t.Fatal("expected non-empty render")
	}

	// Should contain the title
	if !strings.Contains(rendered, "Confirm") {
		t.Error("rendered output missing title")
	}

	// Should contain the body
	if !strings.Contains(rendered, "Are you sure?") {
		t.Error("rendered output missing body")
	}

	// Should contain button labels
	if !strings.Contains(rendered, "Yes") {
		t.Error("rendered output missing Yes button")
	}
	if !strings.Contains(rendered, "No") {
		t.Error("rendered output missing No button")
	}
}

func TestDialogRenderBordered(t *testing.T) {
	d := NewDialog("Title", "Body", nil, 30, 8)
	rendered := d.Render()

	// Should contain rounded border characters
	if !strings.Contains(rendered, "╭") || !strings.Contains(rendered, "╰") {
		t.Error("expected rounded border characters")
	}
}

func TestDialogClampsToTerminalSize(t *testing.T) {
	// Dialog wider than terminal
	d := NewDialog("Title", "Body", nil, 200, 50)
	d.TermWidth = 40
	d.TermHeight = 10

	rendered := d.Render()
	lines := strings.Split(rendered, "\n")

	// Should not exceed terminal width
	for i, line := range lines {
		w := ansi.StringWidth(stripANSI(line))
		if w > d.TermWidth {
			t.Errorf("line %d width %d exceeds terminal width %d: %q", i, w, d.TermWidth, line)
		}
	}

	// Should not exceed terminal height
	if len(lines) > d.TermHeight {
		t.Errorf("render height %d exceeds terminal height %d", len(lines), d.TermHeight)
	}
}

func TestDialogScrollIndicators(t *testing.T) {
	// Short body — no scroll indicators
	d1 := NewDialog("Title", "Short body", nil, 40, 10)
	d1.TermWidth = 40
	d1.TermHeight = 10
	rendered1 := d1.Render()
	if strings.Contains(rendered1, "▲") || strings.Contains(rendered1, "▼") {
		t.Error("short body should not have scroll indicators")
	}

	// Long body that overflows — should have scroll indicators
	longBody := strings.Repeat("Line of text that should overflow.\n", 20)
	d2 := NewDialog("Title", longBody, nil, 40, 10)
	d2.TermWidth = 40
	d2.TermHeight = 10
	d2.SetMaxBodyHeight(3)
	rendered2 := d2.Render()
	if !strings.Contains(rendered2, "▼") {
		t.Error("overflowing body should have down scroll indicator")
	}
}

func TestDialogBounds(t *testing.T) {
	d := NewDialog("Title", "Body", []ButtonConfig{
		{Label: "OK", Variant: ButtonNormal},
	}, 40, 10)

	bounds := d.Bounds()
	if bounds.Width <= 0 || bounds.Height <= 0 {
		t.Errorf("expected positive bounds, got %dx%d", bounds.Width, bounds.Height)
	}
}

func TestDialogButtonBounds(t *testing.T) {
	d := NewDialog("Title", "Body", []ButtonConfig{
		{Label: "Yes", Variant: ButtonPrimary},
		{Label: "No", Variant: ButtonNormal},
	}, 40, 10)

	bounds := d.ButtonBounds()
	if len(bounds) != 2 {
		t.Fatalf("expected 2 button bounds, got %d", len(bounds))
	}

	for i, b := range bounds {
		if b.Width <= 0 || b.Height <= 0 {
			t.Errorf("button %d has non-positive bounds: %dx%d", i, b.Width, b.Height)
		}
	}

	// Buttons should not overlap
	if bounds[0].X+bounds[0].Width > bounds[1].X {
		t.Error("buttons overlap")
	}
}

func TestDialogScroll(t *testing.T) {
	longBody := strings.Repeat("Line of text\n", 50)
	d := NewDialog("Title", longBody, nil, 40, 10)
	d.TermWidth = 40
	d.TermHeight = 10
	d.SetMaxBodyHeight(5)

	// Initially at top
	if d.ScrollOffset() != 0 {
		t.Errorf("expected initial scroll offset 0, got %d", d.ScrollOffset())
	}

	// Scroll down
	d.ScrollDown(3)
	if d.ScrollOffset() != 3 {
		t.Errorf("expected scroll offset 3 after ScrollDown, got %d", d.ScrollOffset())
	}

	// Scroll up
	d.ScrollUp(1)
	if d.ScrollOffset() != 2 {
		t.Errorf("expected scroll offset 2 after ScrollUp, got %d", d.ScrollOffset())
	}

	// Scroll to top
	d.ScrollUp(100)
	if d.ScrollOffset() != 0 {
		t.Errorf("expected scroll offset 0 at top, got %d", d.ScrollOffset())
	}
}

func TestDialogEmptyButtons(t *testing.T) {
	d := NewDialog("Title", "Body", nil, 40, 10)
	rendered := d.Render()
	if rendered == "" {
		t.Fatal("dialog with no buttons should still render")
	}

	bounds := d.ButtonBounds()
	if len(bounds) != 0 {
		t.Errorf("expected 0 button bounds, got %d", len(bounds))
	}
}

func TestDialogOverflowsMaxHeight(t *testing.T) {
	d := NewDialog("Title", strings.Repeat("Line\n", 100), nil, 40, 10)
	d.TermWidth = 40
	d.TermHeight = 10
	d.SetMaxBodyHeight(3)

	rendered := d.Render()
	lines := strings.Split(rendered, "\n")

	// Should not exceed max body height + chrome (title + borders + buttons)
	maxExpected := 3 + 4            // body + title row + top border + bottom border + button row
	if len(lines) > maxExpected+2 { // +2 for some slack
		t.Errorf("render height %d exceeds expected max %d", len(lines), maxExpected+2)
	}
}
