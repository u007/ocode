package tui

import (
	"strings"
	"testing"
)

func TestScrollbarThumbMetrics(t *testing.T) {
	tests := []struct {
		name         string
		height       int
		totalLines   int
		visibleLines int
		offsetLines  int
		wantTop      int
		wantSize     int
		wantOK       bool
	}{
		{
			name:         "content fits viewport - hidden",
			height:       10,
			totalLines:   5,
			visibleLines: 10,
			offsetLines:  0,
			wantTop:      0,
			wantSize:     0,
			wantOK:       false,
		},
		{
			name:         "zero total lines - hidden",
			height:       10,
			totalLines:   0,
			visibleLines: 10,
			offsetLines:  0,
			wantTop:      0,
			wantSize:     0,
			wantOK:       false,
		},
		{
			name:         "zero height - hidden",
			height:       0,
			totalLines:   100,
			visibleLines: 10,
			offsetLines:  0,
			wantTop:      0,
			wantSize:     0,
			wantOK:       false,
		},
		{
			name:         "at top position",
			height:       10,
			totalLines:   100,
			visibleLines: 10,
			offsetLines:  0,
			wantTop:      0,
			wantSize:     1,
			wantOK:       true,
		},
		{
			name:         "at middle position",
			height:       10,
			totalLines:   100,
			visibleLines: 10,
			offsetLines:  45,
			wantTop:      4, // roughly middle
			wantSize:     1,
			wantOK:       true,
		},
		{
			name:         "at bottom position",
			height:       10,
			totalLines:   100,
			visibleLines: 10,
			offsetLines:  90,
			wantTop:      9,
			wantSize:     1,
			wantOK:       true,
		},
		{
			name:         "large content - bigger thumb",
			height:       20,
			totalLines:   40,
			visibleLines: 10,
			offsetLines:  0,
			wantTop:      0,
			wantSize:     5, // 10/40 * 20 = 5
			wantOK:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			top, size, ok := scrollbarThumbMetrics(tt.height, tt.totalLines, tt.visibleLines, tt.offsetLines)
			if ok != tt.wantOK {
				t.Errorf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok {
				if top != tt.wantTop {
					t.Errorf("top = %d, want %d", top, tt.wantTop)
				}
				if size != tt.wantSize {
					t.Errorf("size = %d, want %d", size, tt.wantSize)
				}
			}
		})
	}
}

func TestScrollbarRendering(t *testing.T) {
	tests := []struct {
		name         string
		height       int
		totalLines   int
		visibleLines int
		offsetLines  int
		wantEmpty    bool // expect empty string
		wantAllTrack bool // expect all track (content fits)
		wantThumb    bool // expect at least one thumb character
	}{
		{
			name:         "zero height returns empty",
			height:       0,
			totalLines:   100,
			visibleLines: 10,
			offsetLines:  0,
			wantEmpty:    true,
		},
		{
			name:         "content fits viewport - all track",
			height:       10,
			totalLines:   5,
			visibleLines: 10,
			offsetLines:  0,
			wantAllTrack: true,
		},
		{
			name:         "scrollable content shows thumb",
			height:       10,
			totalLines:   100,
			visibleLines: 10,
			offsetLines:  0,
			wantThumb:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sb := NewScrollbar()
			result := sb.Render(tt.height, tt.totalLines, tt.visibleLines, tt.offsetLines)

			if tt.wantEmpty {
				if result != "" {
					t.Errorf("expected empty string, got %q", result)
				}
				return
			}
			if result == "" {
				t.Error("expected non-empty string")
				return
			}

			lines := strings.Split(result, "\n")
			if len(lines) != tt.height {
				t.Errorf("output line count = %d, want %d", len(lines), tt.height)
			}

			if tt.wantAllTrack {
				thumbChar := scrollbarThumbStyle.Render(scrollbarThumb)
				for i, line := range lines {
					if line == thumbChar {
						t.Errorf("line %d has thumb but expected all track", i)
					}
				}
			}

			if tt.wantThumb {
				found := false
				thumbStr := scrollbarThumbStyle.Render(scrollbarThumb)
				for _, line := range lines {
					if line == thumbStr {
						found = true
						break
					}
				}
				if !found {
					t.Error("expected at least one thumb line, found none")
				}
			}
		})
	}
}

func TestScrollbarRenderHeight(t *testing.T) {
	sb := NewScrollbar()
	result := sb.Render(15, 200, 20, 50)
	lines := strings.Split(result, "\n")
	if len(lines) != 15 {
		t.Errorf("rendered height = %d, want 15", len(lines))
	}
}

func TestScrollbarListRender(t *testing.T) {
	sb := NewScrollbar()
	// list-style: totalItems=50, visibleStart=10, visibleCount=5
	result := sb.RenderList(10, 50, 10, 5)
	if result == "" {
		t.Fatal("expected non-empty string")
	}
	lines := strings.Split(result, "\n")
	if len(lines) != 10 {
		t.Errorf("rendered height = %d, want 10", len(lines))
	}
}

func TestScrollbarDragHitTest(t *testing.T) {
	sb := NewScrollbar()
	// 100 items, viewport of 10, total height 20
	// Drag from row 0 at offset 0
	offset, ok := sb.DragHitTest(0, 0, 20, 100, 10, 0)
	if !ok {
		t.Fatal("expected drag to be valid")
	}
	if offset != 0 {
		t.Errorf("drag offset = %d, want 0", offset)
	}

	// Click outside track
	_, ok = sb.DragHitTest(25, 0, 20, 100, 10, 0)
	if ok {
		t.Error("expected drag outside track to be invalid")
	}
}

func TestScrollbarConsistency(t *testing.T) {
	// The old renderScrollbar and renderListScrollbar should produce
	// the same output as the new Scrollbar methods for equivalent inputs.
	sb := NewScrollbar()

	// renderScrollbar(height=10, totalLines=50, visibleLines=10, offsetLines=20)
	oldResult := renderScrollbar(10, 50, 10, 20)
	newResult := sb.Render(10, 50, 10, 20)
	if oldResult != newResult {
		t.Errorf("Scrollbar.Render differs from old renderScrollbar:\nold: %q\nnew: %q", oldResult, newResult)
	}

	// renderListScrollbar(height=10, totalItems=50, visibleStart=10, visibleCount=5)
	oldListResult := renderListScrollbar(10, 50, 10, 5)
	newListResult := sb.RenderList(10, 50, 10, 5)
	if oldListResult != newListResult {
		t.Errorf("Scrollbar.RenderList differs from old renderListScrollbar:\nold: %q\nnew: %q", oldListResult, newListResult)
	}
}
