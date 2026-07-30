package agent

import (
	"strings"
	"testing"

	"github.com/u007/ocode/internal/config"
)

// Auto-compaction declining to run must leave a debug trace: without it,
// "auto compact never fired" is indistinguishable from "auto compact never
// evaluated" when diagnosing a live session from the log.
func TestMaybeCompactAsyncLogsDeclineReason(t *testing.T) {
	var lines []string
	prev := DebugAppend
	DebugAppend = func(kind, msg string) {
		if kind == "COMPACT" {
			lines = append(lines, msg)
		}
	}
	defer func() { DebugAppend = prev }()

	cfg := &config.Config{}
	cfg.Ocode.Compact.Enabled = true
	cfg.Ocode.Compact.TokenThreshold = 0.85
	cfg.Ocode.Compact.MinMessages = 1
	a := NewAgent(fakeCompactClient{}, nil, cfg, nil)

	msgs := []Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello"},
	}
	if a.MaybeCompactAsync(msgs) {
		t.Fatal("tiny conversation must not start compaction")
	}
	found := false
	for _, l := range lines {
		if strings.Contains(l, "below threshold") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a 'below threshold' COMPACT debug line, got %q", lines)
	}

	// Disabled config must also leave a trace.
	lines = nil
	cfg.Ocode.Compact.Enabled = false
	if a.MaybeCompactAsync(msgs) {
		t.Fatal("disabled compaction must not start")
	}
	found = false
	for _, l := range lines {
		if strings.Contains(l, "disabled") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a 'disabled' COMPACT debug line, got %q", lines)
	}
}
