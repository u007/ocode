package tui

import (
	"strings"
	"testing"
)

// TestHandleExportEmptySessionReportsError: /export on a session with no input
// yet must report that there is nothing to export (parity with
// /export-claude) instead of writing an empty markdown file and claiming
// success.
func TestHandleExportEmptySessionReportsError(t *testing.T) {
	m := &model{}
	m.handleExportCmd(nil)
	if len(m.messages) != 1 {
		t.Fatalf("expected exactly one message, got %d", len(m.messages))
	}
	if got := m.messages[0].text; !strings.Contains(got, "No messages available to export") {
		t.Fatalf("message %q does not report the empty session", got)
	}

	// /export-claude keeps the same wording — the two must not drift.
	c := &model{}
	c.handleExportClaudeCmd(nil)
	if len(c.messages) != 1 {
		t.Fatalf("export-claude: expected exactly one message, got %d", len(c.messages))
	}
	if got := c.messages[0].text; !strings.Contains(got, "No messages available to export") {
		t.Fatalf("export-claude message %q does not report the empty session", got)
	}
}
