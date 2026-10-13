package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The status bar's second line must name the always-on briefing file that the
// prompt loaded, root-relative, and must stay silent when none exists.
func TestStatusLineShowsLoadedBriefingFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("RULES\n"), 0644); err != nil {
		t.Fatal(err)
	}
	m := permClickModel(t)
	m.workDir = root
	m.renderStatus()
	if got := m.statusRawLines[1]; !strings.Contains(got, "CLAUDE.md loaded") {
		t.Fatalf("status line 2 = %q, want it to show CLAUDE.md loaded", got)
	}

	empty := permClickModel(t)
	empty.workDir = t.TempDir()
	empty.renderStatus()
	if got := empty.statusRawLines[1]; strings.Contains(got, "loaded") {
		t.Fatalf("status line 2 = %q, want no briefing indicator without a briefing file", got)
	}
}
