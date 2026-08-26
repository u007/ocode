package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/tui/fastviewport"
)

func replacementTestModel() model {
	return model{
		ready:                true,
		width:                120,
		height:               40,
		activeTab:            tabChat,
		styles:               ApplyThemeColors("tokyonight"),
		input:                newTestTextarea(),
		viewport:             fastviewport.New(120, 20),
		streamingThinkingIdx: -1,
	}
}

func TestInputIsQueuedDuringAgentReplacement(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*model)
		text  string
		kind  queueItemKind
	}{
		{
			name:  "model prompt",
			setup: func(m *model) { m.modelSwitchPending = true },
			text:  "prompt during model switch",
			kind:  queueItemInput,
		},
		{
			name:  "model shell",
			setup: func(m *model) { m.modelSwitchPending = true },
			text:  "!echo during model switch",
			kind:  queueItemCommand,
		},
		{
			name:  "model slash",
			setup: func(m *model) { m.modelSwitchPending = true },
			text:  "/help",
			kind:  queueItemCommand,
		},
		{
			name:  "new prompt",
			setup: func(m *model) { m.sessionResetPending = true },
			text:  "prompt during new session",
			kind:  queueItemInput,
		},
		{
			name:  "new shell",
			setup: func(m *model) { m.sessionResetPending = true },
			text:  "!echo during new session",
			kind:  queueItemCommand,
		},
		{
			name:  "new slash",
			setup: func(m *model) { m.sessionResetPending = true },
			text:  "/help",
			kind:  queueItemCommand,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := replacementTestModel()
			tc.setup(&m)
			m.input.SetValue(tc.text)

			updated, _ := m.handleChatKeys(tea.KeyPressMsg{Code: tea.KeyEnter}, nil, nil)
			got := asReplacementModel(t, updated)
			if len(got.queuedItems) != 1 || got.queuedItems[0].kind != tc.kind || got.queuedItems[0].text != tc.text {
				t.Fatalf("queued items = %#v, want one %v item %q", got.queuedItems, tc.kind, tc.text)
			}
			if !got.replacementQueuePending {
				t.Fatal("replacement queue was not held")
			}
			if got.input.Value() != "" {
				t.Fatalf("input = %q, want cleared after queueing", got.input.Value())
			}
			if got.replacementNoticeShown != true || countText(got.messages, "Queued until the model/session replacement completes.") != 1 {
				t.Fatalf("expected one visible replacement notice, messages = %#v", got.messages)
			}
		})
	}
}

func TestReplacementQueueNoticeIsCoalesced(t *testing.T) {
	m := replacementTestModel()
	m.modelSwitchPending = true
	m.input.SetValue("first")
	updated, _ := m.handleChatKeys(tea.KeyPressMsg{Code: tea.KeyEnter}, nil, nil)
	m = asReplacementModel(t, updated)
	m.input.SetValue("second")
	updated, _ = m.handleChatKeys(tea.KeyPressMsg{Code: tea.KeyEnter}, nil, nil)
	m = asReplacementModel(t, updated)

	if len(m.queuedItems) != 2 {
		t.Fatalf("queued items = %d, want 2", len(m.queuedItems))
	}
	if got := countText(m.messages, "Queued until the model/session replacement completes."); got != 1 {
		t.Fatalf("replacement notice count = %d, want 1", got)
	}
}

func TestReplacementQueueWaitsForMCPAndUsesCurrentAgent(t *testing.T) {
	m := replacementTestModel()
	m.agent = agent.NewAgent(retryTestClient{}, nil, nil, nil)
	m.mcpReady = false
	m.replacementQueuePending = true
	m.replacementNoticeShown = true
	m.queuedItems = []queuedItem{{kind: queueItemCommand, text: "/help"}}

	if cmd := m.drainReplacementQueueIfReady(); cmd != nil {
		t.Fatal("replacement queue dispatched before MCP was ready")
	}
	if len(m.queuedItems) != 1 {
		t.Fatal("queued work was removed before MCP was ready")
	}

	m.mcpReady = true
	if cmd := m.drainReplacementQueueIfReady(); cmd == nil {
		// /help is synchronous, so no command is expected; the queue state is
		// the assertion that it was dispatched.
	}
	if len(m.queuedItems) != 0 {
		t.Fatalf("queued items after drain = %#v", m.queuedItems)
	}
	if m.replacementQueuePending || m.replacementNoticeShown {
		t.Fatalf("replacement queue remained held: pending=%v notice=%v", m.replacementQueuePending, m.replacementNoticeShown)
	}
	if !strings.Contains(strings.Join(messageTexts(m.messages), "\n"), "Commands") {
		t.Fatal("queued slash command did not execute after MCP became ready")
	}
}

func TestStaleStreamEventsDoNotMutateCurrentSession(t *testing.T) {
	m := replacementTestModel()
	m.agentEpoch = 2
	m.streaming = true
	m.messages = []message{{role: roleUser, text: "new session"}}

	updated, _ := m.Update(streamMsgEvent{
		epoch: 1,
		msg:   agent.Message{Role: "assistant", Content: "stale assistant"},
	})
	got := updated.(model)
	if len(got.messages) != 1 || got.messages[0].text != "new session" {
		t.Fatalf("stale assistant event mutated transcript: %#v", got.messages)
	}
	updated, _ = got.Update(deltaMsg{epoch: 1, delta: deltaEvent{kind: "reasoning", text: "stale reasoning"}})
	got = updated.(model)
	if got.streamingThinkingIdx != -1 {
		t.Fatalf("stale delta mutated thinking state: %d", got.streamingThinkingIdx)
	}
	updated, _ = got.Update(streamDoneMsg{epoch: 1})
	got = updated.(model)
	if !got.streaming {
		t.Fatal("stale stream completion stopped the current stream")
	}
}

func TestHandleNewInvalidatesOldStreamEvents(t *testing.T) {
	oldAgent := agent.NewAgent(nil, nil, nil, nil)
	t.Cleanup(func() { oldAgent.Shutdown() })
	m := replacementTestModel()
	m.agent = oldAgent
	m.sessionID = "old-session"
	m.agentEpoch = 1
	m.streaming = true
	m.cancelStream = make(chan struct{})
	m.messages = []message{{role: roleUser, text: "old session"}}

	oldEpoch := m.agentEpoch
	cmd := m.handleNewCmd(nil)
	if cmd == nil {
		t.Fatal("/new did not return its asynchronous replacement command")
	}
	if m.sessionID == "old-session" || !m.sessionResetPending {
		t.Fatalf("/new did not enter a new pending session: id=%q pending=%v", m.sessionID, m.sessionResetPending)
	}
	messageCount := len(m.messages)

	updated, _ := m.Update(streamMsgEvent{
		epoch: oldEpoch,
		msg:   agent.Message{Role: "assistant", Content: "stale response"},
	})
	got := updated.(model)
	updated, _ = got.Update(deltaMsg{epoch: oldEpoch, delta: deltaEvent{kind: "reasoning", text: "stale reasoning"}})
	got = updated.(model)
	updated, _ = got.Update(streamDoneMsg{epoch: oldEpoch})
	got = updated.(model)

	if len(got.messages) != messageCount {
		t.Fatalf("old stream events changed the new transcript: before=%d after=%d", messageCount, len(got.messages))
	}
	if got.streaming {
		t.Fatal("old stream completion changed the new session's streaming state")
	}
	if got.sessionResetPending != true {
		t.Fatal("old stream events cleared the pending session reset")
	}
	if got.messages[len(got.messages)-1].text != "Started new session." {
		t.Fatalf("new session transcript was overwritten: %#v", got.messages)
	}
}

func TestReplacementTrackerShutdownCleansUnclaimedAgent(t *testing.T) {
	tracker := &replacementTracker{}
	run, _ := tracker.start()
	next := agent.NewAgent(nil, nil, nil, nil)
	cleaned := false
	tracker.setCleanup(run, func() { cleaned = true })
	tracker.setResources(run, next, nil)
	tracker.complete(run)
	tracker.shutdown(time.Second)

	select {
	case <-next.Done():
	default:
		t.Fatal("unclaimed replacement agent was not shut down")
	}
	if !cleaned {
		t.Fatal("replacement prerequisites were not cleaned")
	}
}

func TestReplacementTrackerShutdownWaitsForConstruction(t *testing.T) {
	tracker := &replacementTracker{}
	run, ctx := tracker.start()
	started := make(chan struct{})
	release := make(chan struct{})
	go func() {
		close(started)
		<-ctx.Done()
		<-release
		tracker.complete(run)
	}()
	<-started

	shutdownDone := make(chan struct{})
	go func() {
		tracker.shutdown(time.Second)
		close(shutdownDone)
	}()
	select {
	case <-shutdownDone:
		t.Fatal("shutdown returned before replacement construction completed")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	select {
	case <-shutdownDone:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not wait for replacement construction")
	}
}

func countText(messages []message, want string) int {
	count := 0
	for _, msg := range messages {
		if strings.Contains(msg.text, want) {
			count++
		}
	}
	return count
}

func messageTexts(messages []message) []string {
	texts := make([]string, 0, len(messages))
	for _, msg := range messages {
		texts = append(texts, msg.text)
	}
	return texts
}

func asReplacementModel(t *testing.T, value tea.Model) model {
	t.Helper()
	switch got := value.(type) {
	case model:
		return got
	case *model:
		return *got
	default:
		t.Fatalf("unexpected model type %T", value)
		return model{}
	}
}
