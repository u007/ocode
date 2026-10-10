package tui

import (
	"testing"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"github.com/u007/ocode/internal/tui/fastviewport"
)

// The replacement-queue gate (model.go:6609) is checked BEFORE the shell gate
// (model.go:6616), so input typed while BOTH a `!` command is running and an
// agent replacement is pending lands in the replacement queue. This probes
// whether drainReplacementQueueIfReady — which guards on m.streaming only —
// then dispatches it to the LLM while the shell is still running.

// TestTypingDuringShellAndModelSwitchQueuesForReplacement documents the
// ordering: the message must go to the replacement queue, not be dispatched.
func TestTypingDuringShellAndModelSwitchQueuesForReplacement(t *testing.T) {
	a := newTestAgent(nil, nil, nil, nil)
	t.Cleanup(func() { a.Shutdown() })
	m := model{
		input:     textarea.New(),
		viewport:  fastviewport.New(80, 20),
		styles:    ApplyThemeColors("tokyonight"),
		sessionID: "test-shell-replacement",
		agent:     a,
		mcpReady:  true,
	}
	m.shellStreamCmd = func() tea.Msg { return nil }
	m.cmdRunningCount = 1
	m.replacementQueuePending = true

	m.input.SetValue("typed during shell + model switch")
	updated, _ := m.handleChatKeys(tea.KeyPressMsg{Code: tea.KeyEnter}, nil, nil)
	got := updated.(model)

	if len(got.queuedItems) != 1 {
		t.Fatalf("expected the message to be queued, got %d", len(got.queuedItems))
	}
	t.Logf("queued: %+v", got.queuedItems[0])
}

// TestReplacementDrainHonoursShell is the actual probe: once the replacement
// completes, does the drain fire while the shell is still running?
func TestReplacementDrainHonoursShell(t *testing.T) {
	a := newTestAgent(nil, nil, nil, nil)
	t.Cleanup(func() { a.Shutdown() })
	m := model{
		input:     textarea.New(),
		viewport:  fastviewport.New(80, 20),
		styles:    ApplyThemeColors("tokyonight"),
		sessionID: "test-replacement-drain-shell",
		agent:     a,
		mcpReady:  true,
	}
	// A `!sleep 600` is still running, but no LLM turn.
	m.shellStreamCmd = func() tea.Msg { return nil }
	m.cmdRunningCount = 1
	// The replacement finished: the new agent is installed and MCP is ready.
	m.replacementQueuePending = true
	m.queuedItems = []queuedItem{{kind: queueItemInput, text: "typed during shell + model switch"}}

	cmd := m.drainReplacementQueueIfReady()
	t.Logf("drain returned cmd=%v, queuedItems left=%d, replacementQueuePending=%v",
		cmd != nil, len(m.queuedItems), m.replacementQueuePending)

	if cmd != nil {
		t.Errorf("REPLACEMENT GAP: message dispatched to the LLM while `!sleep 600` is still running")
	}
}

// TestReplacementDrainLatchParityWithStreaming checks the EXISTING behaviour
// when m.streaming blocks the replacement drain, so a shell guard can match it.
func TestReplacementDrainLatchParityWithStreaming(t *testing.T) {
	a := newTestAgent(nil, nil, nil, nil)
	t.Cleanup(func() { a.Shutdown() })
	m := model{
		input:     textarea.New(),
		viewport:  fastviewport.New(80, 20),
		styles:    ApplyThemeColors("tokyonight"),
		sessionID: "test-replacement-latch",
		agent:     a,
		mcpReady:  true,
	}
	m.streaming = true
	m.replacementQueuePending = true
	m.queuedItems = []queuedItem{{kind: queueItemInput, text: "queued during a turn"}}

	if cmd := m.drainReplacementQueueIfReady(); cmd != nil {
		t.Fatalf("expected streaming to block the replacement drain")
	}
	// streamDoneMsg then releases it via the plain drain.
	after, cmd := m.Update(streamDoneMsg{})
	got := after.(model)
	t.Logf("after streamDone: queuedItems=%d cmd=%v replacementQueuePending=%v",
		len(got.queuedItems), cmd != nil, got.replacementQueuePending)
}

// TestReplacementQueueReleasedAfterShellFinishes closes the loop: the guard
// defers the drain, so shellFinishedMsg must be the thing that releases it.
func TestReplacementQueueReleasedAfterShellFinishes(t *testing.T) {
	a := newTestAgent(nil, nil, nil, nil)
	t.Cleanup(func() { a.Shutdown() })
	m := model{
		input:     textarea.New(),
		viewport:  fastviewport.New(80, 20),
		styles:    ApplyThemeColors("tokyonight"),
		sessionID: "test-replacement-release",
		agent:     a,
		mcpReady:  true,
	}
	m.shellStreamCmd = func() tea.Msg { return nil }
	m.cmdRunningCount = 1
	m.replacementQueuePending = true
	m.queuedItems = []queuedItem{{kind: queueItemInput, text: "typed during shell + model switch"}}

	if cmd := m.drainReplacementQueueIfReady(); cmd != nil {
		t.Fatalf("drain fired while the shell was still running")
	}
	if len(m.queuedItems) != 1 {
		t.Fatalf("expected the message to stay queued during the shell, got %d", len(m.queuedItems))
	}
	after, cmd := m.Update(shellFinishedMsg{command: "sleep 600", output: "", toolCallID: "shell-1"})
	got := after.(model)
	if len(got.queuedItems) != 0 {
		t.Errorf("shellFinishedMsg did not release the replacement queue: %+v", got.queuedItems)
	}
	if cmd == nil {
		t.Error("expected the released message to produce a command")
	}
}
