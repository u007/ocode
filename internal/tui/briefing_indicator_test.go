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

// The root briefing ("CLAUDE.md loaded" / "AGENTS.md loaded") must also be
// surfaced as a transient, non-persisted transcript notice once per
// (session, briefing file), and stay silent when no briefing exists.
func TestBriefingAnnouncementIsTransientAndOnce(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("RULES\n"), 0644); err != nil {
		t.Fatal(err)
	}
	m := permClickModel(t)
	m.workDir = root
	m.announceBriefing()
	if len(m.messages) != 1 {
		t.Fatalf("expected one briefing notice, got %d", len(m.messages))
	}
	got := m.messages[0]
	if !got.transient || !got.skipLLM {
		t.Fatalf("briefing notice must be transient+skipLLM, got %+v", got)
	}
	if !strings.Contains(stripANSI(got.text), "CLAUDE.md loaded") {
		t.Fatalf("expected 'CLAUDE.md loaded', got %q", got.text)
	}

	// second announce is a no-op
	m.announceBriefing()
	if len(m.messages) != 1 {
		t.Fatalf("expected briefing notice to fire once, got %d", len(m.messages))
	}

	// empty workdir: no notice
	empty := permClickModel(t)
	empty.workDir = t.TempDir()
	empty.announceBriefing()
	if len(empty.messages) != 0 {
		t.Fatalf("expected no briefing notice without a briefing file, got %d", len(empty.messages))
	}
}
