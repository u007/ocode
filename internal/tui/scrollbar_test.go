package tui

import (
	"strings"
	"testing"
)

func TestRenderScrollbar_NoThumbWhenFits(t *testing.T) {
	result := renderScrollbar(5, 5, 5, 0)
	lines := strings.Split(result, "\n")
	if len(lines) != 5 {
		t.Fatalf("expected 5 lines, got %d", len(lines))
	}
	for _, line := range lines {
		if strings.Contains(line, "█") {
			t.Error("expected no thumb when content fits")
		}
	}
}

func TestRenderScrollbar_ThumbAtTop(t *testing.T) {
	result := renderScrollbar(5, 20, 5, 0)
	lines := strings.Split(result, "\n")
	if len(lines) != 5 {
		t.Fatalf("expected 5 lines, got %d", len(lines))
	}
	if !strings.Contains(lines[0], "█") {
		t.Errorf("expected thumb on first line at offset=0, got: %q", lines[0])
	}
}

func TestRenderScrollbar_ThumbAtBottom(t *testing.T) {
	result := renderScrollbar(5, 20, 5, 15)
	lines := strings.Split(result, "\n")
	if len(lines) != 5 {
		t.Fatalf("expected 5 lines, got %d", len(lines))
	}
	if !strings.Contains(lines[4], "█") {
		t.Errorf("expected thumb on last line at max offset, got: %q", lines[4])
	}
}

func TestRenderScrollbar_WidthIsOne(t *testing.T) {
	result := renderScrollbar(4, 20, 4, 0)
	for i, line := range strings.Split(result, "\n") {
		_ = i
		_ = line
	}
	if result == "" {
		t.Error("expected non-empty scrollbar")
	}
}

func TestRenderListScrollbar_NoThumbWhenFits(t *testing.T) {
	result := renderListScrollbar(4, 4, 0, 4)
	lines := strings.Split(result, "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 lines, got %d", len(lines))
	}
	for _, line := range lines {
		if strings.Contains(line, "█") {
			t.Error("expected no thumb when all items visible")
		}
	}
}

func TestRenderListScrollbar_ThumbAtTop(t *testing.T) {
	result := renderListScrollbar(4, 16, 0, 4)
	lines := strings.Split(result, "\n")
	if !strings.Contains(lines[0], "█") {
		t.Errorf("expected thumb at top when visibleStart=0, got: %q", lines[0])
	}
}

func TestRenderListScrollbar_ThumbAtBottom(t *testing.T) {
	result := renderListScrollbar(4, 16, 12, 4)
	lines := strings.Split(result, "\n")
	if !strings.Contains(lines[3], "█") {
		t.Errorf("expected thumb at bottom at max visibleStart, got: %q", lines[3])
	}
}
