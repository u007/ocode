package tui

import (
	"testing"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/tui/fastviewport"
)

// A `!` shell command runs a completely separate process loop from an LLM
// turn, and does NOT set m.streaming (that flag is set only on
// streamStartedMsg). These tests pin that plain chat input typed during a
// running `!` command is queued and released when the command finishes,
// mirroring the web composer's `busy = isStreaming || shellInFlight || ...`
// gate in web/src/components/Chat/ChatInput.tsx.

func newShellBusyModel(t *testing.T) model {
	t.Helper()
	a := agent.NewAgent(nil, nil, nil, nil)
	t.Cleanup(func() { a.Shutdown() })
	m := model{
		input:     textarea.New(),
		viewport:  fastviewport.New(80, 20),
		styles:    ApplyThemeColors("tokyonight"),
		sessionID: "test-shell-input-queue",
		agent:     a,
		mcpReady:  true,
	}
	// What startShellExecution/runStreamingShell set for `!sleep 600`.
	m.shellStreamCmd = func() tea.Msg { return nil }
	m.cmdRunningCount = 1
	return m
}

// TestChatInputQueuesWhileShellRunning is the direct regression for the
// reported bug: `!sleep 600`, then type a message and press enter.
func TestChatInputQueuesWhileShellRunning(t *testing.T) {
	m := newShellBusyModel(t)

	m.input.SetValue("what did the sleep do?")
	updated, _ := m.handleChatKeys(tea.KeyPressMsg{Code: tea.KeyEnter}, nil, nil)
	got := updated.(model)

	if len(got.queuedItems) != 1 {
		t.Fatalf("expected the message to queue during an in-flight `!`, got %d queued (delayed=%d)",
			len(got.queuedItems), len(got.delayedChatInputs))
	}
	if got.queuedItems[0].kind != queueItemInput || got.queuedItems[0].text != "what did the sleep do?" {
		t.Fatalf("unexpected queued item: %+v", got.queuedItems[0])
	}
	// A `!` command is not an agent Step loop, so injecting would leave an
	// entry nothing ever consumes. The agent must have no pending injection.
	if got.agent.HasPendingInjections() {
		t.Error("message was injected during a `!` shell command; there is no Step loop to consume it")
	}
}

// TestChatInputNotDelayedWhileShellRunning guards the debounce specifically:
// a queued message must go to the queue, not into the 1.5s delayed-input
// buffer, so it cannot race ahead of the shell.
func TestChatInputNotDelayedWhileShellRunning(t *testing.T) {
	m := newShellBusyModel(t)

	m.input.SetValue("hello")
	updated, _ := m.handleChatKeys(tea.KeyPressMsg{Code: tea.KeyEnter}, nil, nil)
	got := updated.(model)

	if got.hasDelayedChatInput() {
		t.Errorf("message went to the delayed-input buffer instead of the queue")
	}
}

// TestDebouncedInputRequeuesIfShellStartsMidWindow covers the second,
// independent hole: the 1.5s tick can fire AFTER a `!` command has started,
// even though the text was typed when nothing was busy. The flush handler
// must re-check the shell, not just streaming/compacting.
func TestDebouncedInputRequeuesIfShellStartsMidWindow(t *testing.T) {
	m := newShellBusyModel(t)
	// Simulate: typed while idle (goes to the debounce buffer)...
	m.delayedChatInputs = []string{"typed while idle"}
	gen := m.delayedChatGeneration
	// ...then a `!` command starts before the tick fires.
	m.shellStreamCmd = func() tea.Msg { return nil }
	m.cmdRunningCount = 1

	updated, cmd := m.Update(delayedChatInputMsg{generation: gen})
	got := updated.(model)

	if len(got.queuedItems) != 1 {
		t.Fatalf("debounce flush did not re-queue during an in-flight `!`: queued=%d", len(got.queuedItems))
	}
	if cmd != nil {
		t.Error("debounce flush dispatched to the agent while a `!` command was still running")
	}
	if got.agent.HasPendingInjections() {
		t.Error("debounce flush injected during a `!` command; nothing would consume it")
	}
}

// TestShellFinishDrainsQueue is the other half: queued work must actually be
// released when the command finishes. Before this, drainQueuedItems was
// called only on streamDoneMsg/compactFinishedMsg, so anything queued behind
// a `!` command sat in the queue row forever.
func TestShellFinishDrainsQueue(t *testing.T) {
	m := newShellBusyModel(t)
	m.queuedItems = []queuedItem{{kind: queueItemInput, text: "queued behind the shell"}}

	updated, cmd := m.Update(shellFinishedMsg{command: "sleep 600", output: "", toolCallID: "shell-1"})
	got := updated.(model)

	if got.shellStreamCmd != nil {
		t.Error("shellFinishedMsg must clear the in-flight shell")
	}
	if len(got.queuedItems) != 0 {
		t.Errorf("shellFinishedMsg left %d queued item(s) undrained: %+v",
			len(got.queuedItems), got.queuedItems)
	}
	if cmd == nil {
		t.Error("expected the drained message to produce a command to send it")
	}
}

// TestQueuedBangCommandRunsAfterFirstFinishes exercises the `!`-behind-`!`
// case, which has always queued at the input gate. It goes through the real
// key handler rather than a hand-built queue, so it proves the whole
// serialize-then-release path completes.
func TestQueuedBangCommandRunsAfterFirstFinishes(t *testing.T) {
	m := model{
		input:     textarea.New(),
		viewport:  fastviewport.New(80, 20),
		styles:    ApplyThemeColors("tokyonight"),
		sessionID: "test-bang-behind-bang",
		mcpReady:  true,
	}
	// `!sleep 600` is in flight.
	m.shellStreamCmd = func() tea.Msg { return nil }
	m.cmdRunningCount = 1

	m.input.SetValue("!echo second")
	updated, _ := m.handleChatKeys(tea.KeyPressMsg{Code: tea.KeyEnter}, nil, nil)
	got := updated.(model)
	if len(got.queuedItems) != 1 {
		t.Fatalf("expected the second ! to queue, got %d queued", len(got.queuedItems))
	}

	// The first command finishes — the queued one must start.
	finished, cmd := got.Update(shellFinishedMsg{command: "sleep 600", output: "", toolCallID: "shell-1"})
	after := finished.(model)

	if len(after.queuedItems) != 0 {
		t.Errorf("queued %q is stranded: shellFinishedMsg did not drain it", after.queuedItems[0].text)
	}
	if cmd == nil {
		t.Error("expected the queued ! command to start after the first finished")
	}
	// The replacement command must now own the shell slot.
	if after.shellStreamCmd == nil {
		t.Error("expected the drained ! command to become the in-flight shell")
	}
}

// TestShellFinishDoesNotDrainBehindDialog pins the dialog guard: a queued
// message must not be dispatched into a pending question/permission dialog.
func TestShellFinishDoesNotDrainBehindDialog(t *testing.T) {
	m := newShellBusyModel(t)
	m.queuedItems = []queuedItem{{kind: queueItemInput, text: "queued behind the shell"}}
	m.showQuestionDialog = true

	updated, cmd := m.Update(shellFinishedMsg{command: "sleep 600", output: "", toolCallID: "shell-1"})
	got := updated.(model)

	if len(got.queuedItems) != 1 {
		t.Errorf("queue drained into an open question dialog: %d left", len(got.queuedItems))
	}
	if cmd != nil {
		t.Error("expected no dispatch while a question dialog owns the turn")
	}
}
