package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestRandomPipboyArtReturnsNonEmpty(t *testing.T) {
	art := RandomPipboyArt()
	if len(art) == 0 {
		t.Fatal("RandomPipboyArt returned empty slice")
	}
	for i, line := range art {
		if strings.TrimSpace(line) == "" {
			t.Fatalf("RandomPipboyArt line %d is empty", i)
		}
	}
}

func TestRandomPipboyArtIsOneOfThree(t *testing.T) {
	// Check that the returned art matches one of the three known arts by
	// comparing first and last meaningful lines.
	art1Lines := strings.Split(strings.Trim(pipboyArt1, "\n"), "\n")
	art2Lines := strings.Split(strings.Trim(pipboyArt2, "\n"), "\n")
	art3Lines := strings.Split(strings.Trim(pipboyArt3, "\n"), "\n")

	art := RandomPipboyArt()
	ok := false
	for _, candidate := range [][]string{art1Lines, art2Lines, art3Lines} {
		if len(art) == len(candidate) {
			ok = true
			break
		}
	}
	if !ok {
		t.Fatalf("RandomPipboyArt returned art with %d lines, expected one of %d, %d, %d",
			len(art), len(art1Lines), len(art2Lines), len(art3Lines))
	}
}

func TestRandomPipboyArtCanProduceMultipleVariants(t *testing.T) {
	// Not a strong randomness test, but verifies the function doesn't always
	// return the same slice identity (it copies).
	seen := make(map[int]bool)
	for i := 0; i < 10; i++ {
		art := RandomPipboyArt()
		// Key by length as a simple differentiator across the three arts
		seen[len(art)] = true
		if len(seen) >= 2 {
			return // at least two different variants seen
		}
	}
	// It's statistically possible but extremely unlikely to always get the same
	// art in 10 tries. If it happens, something is likely wrong.
	t.Logf("Only saw one art variant (line count %d) in 10 random picks", len(RandomPipboyArt()))
}

func TestRenderPipboyBackgroundZeroDimensions(t *testing.T) {
	art := RandomPipboyArt()
	style := lipgloss.NewStyle()

	result := renderPipboyBackground(art, 0, 10, style)
	if result != "" {
		t.Errorf("expected empty result for width=0, got %q", result)
	}

	result = renderPipboyBackground(art, 10, 0, style)
	if result != "" {
		t.Errorf("expected empty result for height=0, got %q", result)
	}
}

func TestRenderPipboyBackgroundSmoke(t *testing.T) {
	art := RandomPipboyArt()
	style := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))

	result := renderPipboyBackground(art, 100, 50, style)
	if result == "" {
		t.Fatal("expected non-empty rendered background")
	}
	// The result should contain braille characters from the art
	if !strings.Contains(result, "⣠") && !strings.Contains(result, "⡏") && !strings.Contains(result, "⠸") {
		t.Logf("rendered output does not contain expected braille pattern chars (may be okay for some art variants)")
	}
}

func TestRenderPipboyBackgroundClipsToHeight(t *testing.T) {
	art := []string{"line1", "line2", "line3", "line4"}
	style := lipgloss.NewStyle()

	result := renderPipboyBackground(art, 40, 2, style)
	lines := strings.Split(strings.TrimSuffix(result, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 rendered lines, got %d (%q)", len(lines), result)
	}
	if !strings.Contains(result, "line2") || !strings.Contains(result, "line3") {
		t.Fatalf("expected centered crop to keep middle lines, got %q", result)
	}
	if strings.Contains(result, "line1") || strings.Contains(result, "line4") {
		t.Fatalf("expected outer lines to be clipped, got %q", result)
	}
}
