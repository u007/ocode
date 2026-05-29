package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTruncateToolResultByLines(t *testing.T) {
	input := strings.Repeat("line\n", maxToolResultLines+20)
	got := TruncateToolResult("tool-lines", input)

	if !strings.Contains(got, "[output truncated: showing 100/120 lines") {
		t.Fatalf("expected line truncation notice, got: %q", got)
	}
	if !strings.Contains(got, `"start_line": 101, "end_line": <n>`) {
		t.Fatalf("expected read pagination hint, got: %q", got)
	}
	if strings.Count(got, "line\n") > maxToolResultLines {
		t.Fatalf("expected at most %d visible lines before footer", maxToolResultLines)
	}
}

func TestTruncateToolResultByChars(t *testing.T) {
	input := strings.Repeat("x", maxToolResultChars+500)
	got := TruncateToolResult("tool-chars", input)

	if !strings.Contains(got, fmt.Sprintf("1/1 lines, %d/%d chars", maxToolResultChars, maxToolResultChars+500)) {
		t.Fatalf("expected char truncation notice, got: %q", got)
	}
	if !strings.Contains(got, strings.Repeat("x", 200)) {
		t.Fatal("expected visible prefix content to be preserved")
	}
	if strings.Contains(got, strings.Repeat("x", maxToolResultChars+100)) {
		t.Fatal("expected oversized single-line output to be truncated")
	}
}

func TestCleanupToolResults(t *testing.T) {
	// We can't override toolResultCacheDir, so write files into the real
	// cache dir and clean them up after.
	if err := CleanupToolResults(time.Hour); err != nil {
		t.Fatalf("CleanupToolResults on empty/missing dir: %v", err)
	}

	// Write a fake tool-result file into the real cache dir so we can
	// exercise the age check. We backdate its mtime to 3 days ago.
	cacheDir, err := toolResultCacheDir()
	if err != nil {
		t.Skipf("cannot determine cache dir: %v", err)
	}
	_ = os.MkdirAll(cacheDir, 0o755)

	stale := filepath.Join(cacheDir, "call_cleanup_test_stale.txt")
	fresh := filepath.Join(cacheDir, "call_cleanup_test_fresh.txt")

	if err := os.WriteFile(stale, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fresh, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Backdate stale file to 3 days ago.
	oldTime := time.Now().Add(-72 * time.Hour)
	if err := os.Chtimes(stale, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(stale)
	defer os.Remove(fresh)

	if err := CleanupToolResults(48 * time.Hour); err != nil {
		t.Fatalf("CleanupToolResults: %v", err)
	}

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale file should have been removed, err=%v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("fresh file should still exist: %v", err)
	}
}
