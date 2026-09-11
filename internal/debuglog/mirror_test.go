package debuglog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMirrorKindToFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "compact.log")

	l := newLog()
	l.MirrorKindToFile(EntryKind("COMPACT"), path)

	l.Append(Entry{Kind: "COMPACT", Message: "triggered: ~180000 tokens"})
	l.Append(Entry{Kind: "LLM", Message: "stream done OK"})
	l.Append(Entry{Kind: "COMPACT", Message: "done: replaced [0:391]"})

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("mirror file not written: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 mirrored lines, got %d: %q", len(lines), lines)
	}
	if !strings.Contains(lines[0], "[COMPACT] triggered: ~180000 tokens") {
		t.Errorf("line 0 missing entry text: %q", lines[0])
	}
	if !strings.Contains(lines[1], "[COMPACT] done: replaced [0:391]") {
		t.Errorf("line 1 missing entry text: %q", lines[1])
	}
	// Each line must carry a timestamp prefix before the [KIND] tag.
	if strings.HasPrefix(lines[0], "[COMPACT]") {
		t.Errorf("line 0 has no timestamp prefix: %q", lines[0])
	}
}

func TestMirrorRotatesWhenOversized(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "compact.log")
	// Pre-create an oversized file so the next mirrored write rotates it.
	if err := os.WriteFile(path, make([]byte, mirrorMaxBytes+1), 0644); err != nil {
		t.Fatal(err)
	}

	l := newLog()
	l.MirrorKindToFile(EntryKind("COMPACT"), path)
	l.Append(Entry{Kind: "COMPACT", Message: "after rotation"})

	rotated, err := os.Stat(path + ".1")
	if err != nil {
		t.Fatalf("rotated file missing: %v", err)
	}
	if rotated.Size() <= mirrorMaxBytes {
		t.Errorf("rotated file should hold the oversized content, got %d bytes", rotated.Size())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("fresh mirror file missing: %v", err)
	}
	if !strings.Contains(string(data), "after rotation") {
		t.Errorf("fresh mirror file missing new entry: %q", string(data))
	}
}

// TestMirrorMultipleKinds guards the regression where a second
// MirrorKindToFile call silently replaced the first: the TUI registers
// COMPACT then TOKENS, and compact.log stopped receiving entries.
func TestMirrorMultipleKinds(t *testing.T) {
	dir := t.TempDir()
	compactPath := filepath.Join(dir, "compact.log")
	tokensPath := filepath.Join(dir, "tokens.log")

	l := newLog()
	l.MirrorKindToFile(EntryKind("COMPACT"), compactPath)
	l.MirrorKindToFile(EntryKind("TOKENS"), tokensPath)

	l.Append(Entry{Kind: "COMPACT", Message: "triggered"})
	l.Append(Entry{Kind: "TOKENS", Message: "input=1"})

	compact, err := os.ReadFile(compactPath)
	if err != nil {
		t.Fatalf("compact mirror not written: %v", err)
	}
	if !strings.Contains(string(compact), "[COMPACT] triggered") || strings.Contains(string(compact), "TOKENS") {
		t.Errorf("compact mirror content wrong: %q", compact)
	}
	tokens, err := os.ReadFile(tokensPath)
	if err != nil {
		t.Fatalf("tokens mirror not written: %v", err)
	}
	if !strings.Contains(string(tokens), "[TOKENS] input=1") || strings.Contains(string(tokens), "COMPACT") {
		t.Errorf("tokens mirror content wrong: %q", tokens)
	}

	// Empty path disables that kind only.
	l.MirrorKindToFile(EntryKind("TOKENS"), "")
	l.Append(Entry{Kind: "TOKENS", Message: "input=2"})
	l.Append(Entry{Kind: "COMPACT", Message: "done"})
	tokens, _ = os.ReadFile(tokensPath)
	if strings.Contains(string(tokens), "input=2") {
		t.Errorf("tokens mirror should be disabled: %q", tokens)
	}
	compact, _ = os.ReadFile(compactPath)
	if !strings.Contains(string(compact), "[COMPACT] done") {
		t.Errorf("compact mirror should still be active: %q", compact)
	}
}
