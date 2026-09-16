package tool

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

// Parity: numbered output must match the old ReadFile+Split rendering, and
// the continuation hint must appear when unserved lines follow.
func TestReadParityWithOldPath(t *testing.T) {
	var b strings.Builder
	for i := 1; i <= 300; i++ {
		fmt.Fprintf(&b, "line-%d content\n", i)
	}
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origWd)
	os.WriteFile("p.txt", []byte(b.String()), 0o644)

	// Default 50-line window.
	raw, _ := json.Marshal(map[string]any{"path": "p.txt"})
	res, err := (ReadTool{}).Execute(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res, "1\tline-1 content\n") || !strings.Contains(res, "50\tline-50 content\n") {
		t.Fatalf("expected lines 1..50, got head %.200s / tail %.200s", res[:200], res[len(res)-200:])
	}
	if strings.Contains(res, "51\t") {
		t.Fatalf("must not render line 51: %.200s", res[len(res)-200:])
	}
	if !strings.Contains(res, "…(use start_line=51, end_line=100 to continue)\n") {
		t.Fatalf("expected continuation hint, tail: %.200s", res[len(res)-200:])
	}

	// Deep window with offset alias.
	raw2, _ := json.Marshal(map[string]any{"path": "p.txt", "offset": 296})
	res2, err := (ReadTool{}).Execute(raw2)
	if err != nil {
		t.Fatal(err)
	}
	for i := 296; i <= 300; i++ {
		if !strings.Contains(res2, fmt.Sprintf("%d\tline-%d content\n", i, i)) {
			t.Fatalf("deep window missing line %d: %.200s", i, res2)
		}
	}
	if strings.Contains(res2, "…(use start_line=") {
		t.Fatalf("no hint at EOF, tail: %.200s", res2[len(res2)-200:])
	}

	// Out-of-range.
	raw3, _ := json.Marshal(map[string]any{"path": "p.txt", "start_line": 301})
	res3, err := (ReadTool{}).Execute(raw3)
	if err != nil {
		t.Fatal(err)
	}
	// Old-path parity: strings.Split leaves a trailing "" segment, so a
	// 300-line file reports total=301 and start_line=301 renders an empty
	// line 301 rather than an out-of-range message.
	if !strings.Contains(res3, "301\t\n") {
		t.Fatalf("expected empty trailing line 301 (old-path parity), got %q", res3)
	}
}

// File without a trailing newline: last line must still render.
func TestReadNoTrailingNewline(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origWd)
	os.WriteFile("n.txt", []byte("alpha\nbeta"), 0o644)
	raw, _ := json.Marshal(map[string]any{"path": "n.txt"})
	res, err := (ReadTool{}).Execute(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res, "2\tbeta\n") {
		t.Fatalf("missing final unterminated line: %q", res)
	}
}

// Empty file.
func TestReadEmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origWd)
	os.WriteFile("e.txt", []byte(""), 0o644)
	raw, _ := json.Marshal(map[string]any{"path": "e.txt"})
	res, err := (ReadTool{}).Execute(raw)
	if err != nil {
		t.Fatal(err)
	}
	// Old-path parity: strings.Split("", "\n") == [""] renders "1\t\n".
	if res != "1\t\n" {
		t.Fatalf("unexpected empty-file result: %q", res)
	}
}
