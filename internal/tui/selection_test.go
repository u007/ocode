package tui

import (
	"strings"
	"testing"
)

func TestStripANSI(t *testing.T) {
	cases := []struct{ in, want string }{
		{"\x1b[1mhello\x1b[m", "hello"},
		{"plain", "plain"},
		{"\x1b[38;2;59;66;97mcolour\x1b[m text", "colour text"},
	}
	for _, c := range cases {
		got := stripANSI(c.in)
		if got != c.want {
			t.Errorf("stripANSI(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestVisualColToRuneIdx(t *testing.T) {
	// ASCII: each char is width 1
	line := "hello"
	if got := visualColToRuneIdx(line, 0); got != 0 {
		t.Errorf("col 0 want 0, got %d", got)
	}
	if got := visualColToRuneIdx(line, 3); got != 3 {
		t.Errorf("col 3 want 3, got %d", got)
	}
	if got := visualColToRuneIdx(line, 10); got != 5 {
		t.Errorf("col beyond end want 5, got %d", got)
	}

	// Wide rune (CJK, width 2)
	wide := "AB中C"
	// A=0, B=1, 中 occupies cols 2-3 (width 2), C starts at col 4
	if got := visualColToRuneIdx(wide, 2); got != 2 {
		t.Errorf("wide col 2 want byte 2, got %d", got)
	}
	if got := visualColToRuneIdx(wide, 4); got != 5 {
		t.Errorf("wide col 4 (after 中) want byte 5, got %d", got)
	}
}

func TestExtractSelectionText(t *testing.T) {
	rawLines := []string{"hello world", "foo bar", "baz"}

	// Single line
	got := extractSelectionText(rawLines, 0, 6, 0, 11)
	if got != "world" {
		t.Errorf("single line: got %q, want %q", got, "world")
	}

	// Multi-line
	got = extractSelectionText(rawLines, 0, 6, 1, 3)
	if got != "world\nfoo" {
		t.Errorf("multi-line: got %q, want %q", got, "world\nfoo")
	}

	// Reverse direction normalised
	got = extractSelectionText(rawLines, 1, 3, 0, 6)
	if got != "world\nfoo" {
		t.Errorf("reversed: got %q, want %q", got, "world\nfoo")
	}
}

func TestApplySelectionHighlight_plainText(t *testing.T) {
	lines := []string{"hello world"}
	rawLines := []string{"hello world"}
	out := applySelectionHighlight(lines, rawLines, 0, 0, 0, 5)
	if len(out) != 1 {
		t.Fatalf("expected 1 line, got %d", len(out))
	}
	if !strings.Contains(out[0], "\x1b[7m") || !strings.Contains(out[0], "\x1b[27m") {
		t.Errorf("expected reverse-video codes in %q", out[0])
	}
	stripped := stripANSI(out[0])
	if stripped != "hello world" {
		t.Errorf("text after stripping ANSI should be unchanged, got %q", stripped)
	}
}

func TestApplySelectionHighlight_multiLine(t *testing.T) {
	lines := []string{"line one", "line two", "line three"}
	rawLines := lines
	out := applySelectionHighlight(lines, rawLines, 0, 5, 1, 4)
	if !strings.Contains(out[0], "\x1b[7m") {
		t.Errorf("first line should have highlight")
	}
	if !strings.Contains(out[1], "\x1b[7m") {
		t.Errorf("second line should have highlight")
	}
	if strings.Contains(out[2], "\x1b[7m") {
		t.Errorf("third line should not have highlight")
	}
}
