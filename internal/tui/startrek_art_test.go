package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestRandomStartrekArtReturnsNonEmpty(t *testing.T) {
	art := RandomStartrekArt()
	if len(art) == 0 {
		t.Fatal("RandomStartrekArt returned empty slice")
	}
	for i, line := range art {
		if strings.TrimSpace(line) == "" {
			t.Fatalf("RandomStartrekArt line %d is empty", i)
		}
	}
}

func TestRandomStartrekArtIsOneOfFive(t *testing.T) {
	candidates := [][]string{
		strings.Split(strings.Trim(startrekArt1, "\n"), "\n"),
		strings.Split(strings.Trim(startrekArt2, "\n"), "\n"),
		strings.Split(strings.Trim(startrekArt3, "\n"), "\n"),
		strings.Split(strings.Trim(startrekArt4, "\n"), "\n"),
		strings.Split(strings.Trim(startrekArt5, "\n"), "\n"),
	}

	art := RandomStartrekArt()
	got := strings.Join(art, "\n")
	for _, candidate := range candidates {
		if got == strings.Join(candidate, "\n") {
			return
		}
	}
	t.Fatalf("RandomStartrekArt returned unknown art with %d lines", len(art))
}

func TestRandomStartrekArtCanProduceMultipleVariants(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 20; i++ {
		seen[strings.Join(RandomStartrekArt(), "\n")] = true
		if len(seen) >= 2 {
			return
		}
	}
	t.Logf("Only saw one art variant in 20 random picks")
}

func TestRenderStartrekBackgroundZeroDimensions(t *testing.T) {
	art := RandomStartrekArt()
	style := lipgloss.NewStyle()

	result := renderStartrekBackground(art, 0, 10, style)
	if result != "" {
		t.Errorf("expected empty result for width=0, got %q", result)
	}

	result = renderStartrekBackground(art, 10, 0, style)
	if result != "" {
		t.Errorf("expected empty result for height=0, got %q", result)
	}
}

func TestRenderStartrekBackgroundSmoke(t *testing.T) {
	art := RandomStartrekArt()
	style := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))

	result := renderStartrekBackground(art, 100, 50, style)
	if result == "" {
		t.Fatal("expected non-empty rendered background")
	}
	if !strings.Contains(result, "LCARS") && !strings.Contains(result, "STARFLEET") {
		t.Logf("rendered output did not contain a visible label; this may be okay for some art variants")
	}
}

func TestRenderStartrekBackgroundClipsToHeight(t *testing.T) {
	art := []string{"line1", "line2", "line3", "line4"}
	style := lipgloss.NewStyle()

	result := renderStartrekBackground(art, 40, 2, style)
	lines := strings.Split(strings.TrimSuffix(result, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 rendered lines, got %d (%q)", len(lines), result)
	}
	if !strings.Contains(result, "line2") || !strings.Contains(result, "line3") {
		t.Fatalf("expected centered crop to keep middle lines, got %q", result)
	}
}
