package tool

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

func readInTemp(t *testing.T, name, content string, args map[string]any) string {
	t.Helper()
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(origWd) }) //nolint:errcheck
	if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	args["path"] = name
	raw, _ := json.Marshal(args)
	res, err := (ReadTool{}).Execute(raw)
	if err != nil {
		t.Fatalf("ReadTool failed: %v", err)
	}
	return res
}

// A single 100 KB line (minified JS) must be reachable in full via byte
// windows, which line-based paging cannot address.
func TestReadToolByteWindow(t *testing.T) {
	content := strings.Repeat("0123456789", 10000) // 100,000 bytes, one line
	res := readInTemp(t, "min.js", content, map[string]any{"offset_bytes": 50000, "max_bytes": 100})

	if !strings.Contains(res, "[bytes 50000-50100 of 100000]") {
		t.Fatalf("expected byte window header, got:\n%.300s", res)
	}
	if !strings.Contains(res, "\n"+strings.Repeat("0123456789", 10)+"\n") {
		t.Fatalf("expected exactly 100 bytes of payload, got:\n%.300s", res)
	}
	if !strings.Contains(res, "offset_bytes=50100") {
		t.Fatalf("expected continuation hint, got:\n%.300s", res)
	}
}

// Line mode must clamp an over-long line and point at the byte offset where
// the clamp happened so the model can page inside the line.
func TestReadToolLineModeClampsLongLine(t *testing.T) {
	long := strings.Repeat("x", 70000) // > bufio.Scanner default 64 KB
	content := "short\n" + long + "\nafter\n"
	res := readInTemp(t, "wide.txt", content, map[string]any{})

	if strings.Contains(res, strings.Repeat("x", maxReadLineChars+1)) {
		t.Fatalf("line 2 must be clamped to %d chars", maxReadLineChars)
	}
	// Line 2 starts at byte 6 ("short\n"); clamp at maxReadLineChars into it.
	want := fmt.Sprintf("offset_bytes=%d", 6+maxReadLineChars)
	if !strings.Contains(res, want) {
		t.Fatalf("expected clamp hint %s, got:\n%.200s ... %.300s", want, res, res[len(res)-300:])
	}
	if !strings.Contains(res, "70000 chars") {
		t.Fatalf("expected total line length in clamp hint, got tail:\n%.300s", res[len(res)-300:])
	}
	if !strings.Contains(res, "3\tafter\n") {
		t.Fatalf("line 3 must still be returned after a clamped line, got tail:\n%.300s", res[len(res)-300:])
	}
}

// Line mode stops early once the output budget is spent and tells the model
// which line to resume from.
func TestReadToolLineModeStopsAtCharBudget(t *testing.T) {
	line := strings.Repeat("y", 1000)
	var b strings.Builder
	for i := 0; i < 100; i++ {
		b.WriteString(line + "\n")
	}
	res := readInTemp(t, "wide.txt", b.String(), map[string]any{"end_line": 100})

	if len(res) > maxReadOutputChars+512 {
		t.Fatalf("output %d chars exceeds budget %d", len(res), maxReadOutputChars)
	}
	expectLines := maxReadOutputChars / (len(line) + 4)
	if !strings.Contains(res, fmt.Sprintf("%d\t%s\n", expectLines, line)) {
		t.Fatalf("expected line %d present, got head:\n%.100s", expectLines, res)
	}
	if strings.Contains(res, fmt.Sprintf("%d\t%s\n", expectLines+2, line)) {
		t.Fatalf("expected output stopped near line %d", expectLines)
	}
	if !strings.Contains(res, "(stopped at line") || !strings.Contains(res, "start_line=") {
		t.Fatalf("expected budget-stop hint, got tail:\n%.300s", res[len(res)-300:])
	}
}
