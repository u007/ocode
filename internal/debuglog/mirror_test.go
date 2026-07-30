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
