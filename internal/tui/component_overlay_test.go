package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// --- compositeOverlay tests ---

func TestCompositeOverlayPlainBackdrop(t *testing.T) {
	backdrop := "AAAAAAAAAA\nBBBBBBBBBB\nCCCCCCCCCC"
	box := "XX\nYY"
	got := compositeOverlay(backdrop, box, 2, 1)
	// Box line 0 "XX" spliced into backdrop line 1 at col 2
	// Box line 1 "YY" spliced into backdrop line 2 at col 2
	want := "AAAAAAAAAA\nBBXXBBBBBB\nCCYYCCCCCC"
	if got != want {
		t.Errorf("plain overlay:\n got %q\nwant %q", got, want)
	}
}

func TestCompositeOverlayANSIBackdrop(t *testing.T) {
	// Left of box: bold text. Right of box: italic text.
	// The overlay must preserve styles on both sides.
	left := "\x1b[1mHELLO\x1b[22m"  // "HELLO" bold
	right := "\x1b[3mWORLD\x1b[23m" // "WORLD" italic
	backdrop := left + " " + right  // visual width 11

	box := "XX"
	got := compositeOverlay(backdrop, box, 6, 0)
	// Splice at col 6: left="HELLO ", box="XX", right truncated to 3 chars="WOR"
	stripped := stripANSI(got)
	if stripped != "HELLO XXWOR" {
		t.Errorf("ANSI backdrop stripped: got %q, want %q", stripped, "HELLO XXWOR")
	}
	// Left side should still contain bold code
	if !strings.Contains(got, "\x1b[1m") {
		t.Error("left side lost bold style")
	}
	// Right side should still contain italic code
	if !strings.Contains(got, "\x1b[3m") {
		t.Error("right side lost italic style")
	}
}

func TestCompositeOverlayCJKWidth(t *testing.T) {
	// CJK chars are double-width. Backdrop: 5 CJK chars = 10 cells.
	backdrop := "ＡＢＣＤＥ" // each is 2 cells wide = 10 total
	box := "X"
	got := compositeOverlay(backdrop, box, 4, 0)
	stripped := stripANSI(got)
	// Splice at col 4: left 4 CJK (8 cells), box 1 cell, right 1 CJK (2 cells) = 11 cells
	visualWidth := ansi.StringWidth(stripped)
	if visualWidth != 11 {
		t.Errorf("CJK splice visual width: got %d, want 11 (line: %q)", visualWidth, stripped)
	}
}

func TestCompositeOverlayBoxTallerThanBackdrop(t *testing.T) {
	backdrop := "AA\nBB"
	box := "X\nY\nZ"
	// Box is taller — should clamp, not panic
	got := compositeOverlay(backdrop, box, 0, 0)
	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 lines from clamped taller box, got %d", len(lines))
	}
}

func TestCompositeOverlayBoxWiderThanBackdrop(t *testing.T) {
	backdrop := "AB"
	box := "XXXXXX"
	// Box wider than backdrop — clamp to backdrop width
	got := compositeOverlay(backdrop, box, 0, 0)
	stripped := stripANSI(got)
	visualWidth := ansi.StringWidth(stripped)
	if visualWidth != 2 {
		t.Errorf("clamped wider box visual width: got %d, want 2 (line: %q)", visualWidth, stripped)
	}
}

func TestCompositeOverlayEmptyBackdrop(t *testing.T) {
	got := compositeOverlay("", "XX", 0, 0)
	if got != "" {
		t.Errorf("empty backdrop should return empty, got %q", got)
	}
}

func TestCompositeOverlayEmptyBox(t *testing.T) {
	backdrop := "AA\nBB"
	got := compositeOverlay(backdrop, "", 0, 0)
	if got != backdrop {
		t.Errorf("empty box should return backdrop unchanged, got %q", got)
	}
}

func TestCompositeOverlayClampX(t *testing.T) {
	backdrop := "AAAA\nBBBB"
	box := "XX"
	// x=100 is way beyond backdrop width — should produce backdrop unchanged
	got := compositeOverlay(backdrop, box, 100, 0)
	if got != backdrop {
		t.Errorf("out-of-bounds x should return backdrop, got %q", got)
	}
}

func TestCompositeOverlayClampY(t *testing.T) {
	backdrop := "AA\nBB"
	box := "XX"
	// y=100 is beyond backdrop height
	got := compositeOverlay(backdrop, box, 0, 100)
	if got != backdrop {
		t.Errorf("out-of-bounds y should return backdrop, got %q", got)
	}
}

func TestCompositeOverlayMultiLineBox(t *testing.T) {
	backdrop := "AAAAAAAAAA\nBBBBBBBBBB\nCCCCCCCCCC\nDDDDDDDDDD"
	box := "XX\nYY\nZZ"
	got := compositeOverlay(backdrop, box, 3, 1)
	// Each box line spliced into its corresponding backdrop line at col 3.
	want := "AAAAAAAAAA\nBBBXXBBBBB\nCCCYYCCCCC\nDDDZZDDDDD"
	if got != want {
		t.Errorf("multi-line box:\n got %q\nwant %q", got, want)
	}
}

// --- dimLines tests ---

func TestDimLinesPlain(t *testing.T) {
	lines := []string{"Hello", "World"}
	got := dimLines(lines)
	if len(got) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(got))
	}
	// Each line should be wrapped with faint SGR code (\x1b[2m ... \x1b[22m)
	for i, l := range got {
		if !strings.Contains(l, "\x1b[2m") {
			t.Errorf("line %d missing faint code: %q", i, l)
		}
		if !strings.Contains(l, "\x1b[22m") {
			t.Errorf("line %d missing faint reset: %q", i, l)
		}
	}
}

func TestDimLinesANSI(t *testing.T) {
	lines := []string{"\x1b[1mBOLD\x1b[22m", "\x1b[3mitalic\x1b[23m"}
	got := dimLines(lines)
	for i, l := range got {
		stripped := stripANSI(l)
		origStripped := stripANSI(lines[i])
		if stripped != origStripped {
			t.Errorf("dimmed line %d stripped: got %q, want %q", i, stripped, origStripped)
		}
	}
}

func TestDimLinesEmpty(t *testing.T) {
	got := dimLines([]string{})
	if len(got) != 0 {
		t.Errorf("expected empty result, got %v", got)
	}
}

func TestDimLinesWidthPreserved(t *testing.T) {
	lines := []string{"AAAA", "BBBB"}
	got := dimLines(lines)
	for i, l := range got {
		origWidth := ansi.StringWidth(lines[i])
		dimWidth := ansi.StringWidth(l)
		if origWidth != dimWidth {
			t.Errorf("line %d width changed: %d -> %d", i, origWidth, dimWidth)
		}
	}
}

// --- dimCache tests ---

func TestDimCacheSameInput(t *testing.T) {
	lines := []string{"Hello", "World"}
	cache := newDimCache()
	got1 := cache.DimIfChanged(lines, 1)
	got2 := cache.DimIfChanged(lines, 1)

	// Same version — second call should return same pointer (cached)
	if len(got1) != len(got2) {
		t.Fatal("cached result has different length")
	}
	for i := range got1 {
		if got1[i] != got2[i] {
			t.Errorf("line %d differs between calls: %q vs %q", i, got1[i], got2[i])
		}
	}
	// Verify it's actually the same slice (pointer equality means cache hit)
	if &got1[0] != &got2[0] {
		t.Error("expected same underlying slice for cache hit")
	}
}

func TestDimCacheDifferentVersion(t *testing.T) {
	lines := []string{"Hello", "World"}
	cache := newDimCache()
	got1 := cache.DimIfChanged(lines, 1)
	got2 := cache.DimIfChanged(lines, 2)

	// Different version — should recompute
	if &got1[0] == &got2[0] {
		t.Error("expected different slice for different version")
	}
}

func TestDimCacheDifferentContent(t *testing.T) {
	cache := newDimCache()
	got1 := cache.DimIfChanged([]string{"Hello"}, 1)
	got2 := cache.DimIfChanged([]string{"World"}, 1)

	// Same version but different content — should recompute
	if &got1[0] == &got2[0] {
		t.Error("expected different slice for different content at same version")
	}
}

func TestDimCacheNilInput(t *testing.T) {
	cache := newDimCache()
	got := cache.DimIfChanged(nil, 1)
	if got != nil {
		t.Errorf("nil input should return nil, got %v", got)
	}
}
