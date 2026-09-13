package computer

import (
	"runtime"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
)

func TestStatusLinesReportsEnabledStateAndPlatform(t *testing.T) {
	lines := StatusLines(config.ComputerUseConfig{Enabled: true})
	if len(lines) < 2 {
		t.Fatalf("got %d status lines, want at least 2", len(lines))
	}
	if lines[0] != "Computer use: enabled" {
		t.Fatalf("state line = %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "Backend: ") {
		t.Fatalf("backend line = %q", lines[1])
	}
	if lines[1] != "Backend: "+backendName() {
		t.Fatalf("backend line = %q, want %q", lines[1], "Backend: "+backendName())
	}
	if runtime.GOOS == "darwin" && len(lines) != 3 {
		t.Fatalf("darwin status has %d lines, want 3", len(lines))
	}
	if runtime.GOOS == "darwin" && !strings.Contains(lines[2], "Screen Recording and Accessibility") {
		t.Fatalf("darwin permission reminder = %q", lines[2])
	}
}

func TestStatusLinesReportsDisabledState(t *testing.T) {
	lines := StatusLines(config.ComputerUseConfig{})
	if lines[0] != "Computer use: disabled" {
		t.Fatalf("state line = %q", lines[0])
	}
}
