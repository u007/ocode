package tui

import (
	"strings"
	"testing"
)

func TestExitResumeSummaryIncludesSessionAndCommand(t *testing.T) {
	got := exitResumeSummary("session-123")
	for _, want := range []string{"Session ID: session-123", "Resume with: ocode -session session-123"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected summary to include %q, got %q", want, got)
		}
	}
}

func TestExitResumeSummarySkipsEmptySession(t *testing.T) {
	if got := exitResumeSummary(""); got != "" {
		t.Fatalf("expected empty summary for empty session ID, got %q", got)
	}
}
