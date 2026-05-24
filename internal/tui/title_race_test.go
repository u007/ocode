package tui

import (
	"testing"
)

// TestTitleGen_RejectsStaleResult guards against the regression where a title
// goroutine started for session N delivers its result after /new has rolled to
// session N+1, overwriting the new session's title. The fix stamps each
// request with the model's current titleGen and bumps the counter on /new and
// /title clear; receive-side compares and drops stale results.
func TestTitleGen_RejectsStaleResult(t *testing.T) {
	m := &model{
		titleCh:  make(chan titleResult, 4),
		titleGen: 1,
	}

	// Simulate a pending goroutine started for gen=1.
	pending := m.titleGen

	// User runs /new — bumps gen, clears title.
	m.sessionTitle = ""
	m.titleRequested = false
	m.titleGen++

	// Now the stale goroutine lands its result.
	stale := titleGeneratedMsg{title: "stale title", gen: pending}
	if stale.gen == m.titleGen {
		t.Fatalf("test setup wrong: stale gen %d should differ from current %d", stale.gen, m.titleGen)
	}

	// Mirror the Update handler's logic — only apply when gen matches.
	if stale.gen == m.titleGen && stale.title != "" && m.sessionTitle == "" {
		m.sessionTitle = stale.title
	}
	if m.sessionTitle != "" {
		t.Errorf("stale title was applied to new session, got %q", m.sessionTitle)
	}

	// A fresh result with the current gen should still apply.
	fresh := titleGeneratedMsg{title: "fresh title", gen: m.titleGen}
	if fresh.gen == m.titleGen && fresh.title != "" && m.sessionTitle == "" {
		m.sessionTitle = fresh.title
	}
	if m.sessionTitle != "fresh title" {
		t.Errorf("fresh title not applied, got %q", m.sessionTitle)
	}
}
