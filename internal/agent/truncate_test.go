package agent

import (
	"fmt"
	"strings"
	"testing"
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
