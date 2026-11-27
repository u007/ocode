package tui

import (
	"context"
	"github.com/u007/ocode/internal/tool"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/u007/ocode/internal/agent"
)

// These tests pin the per-ask stop channel: the asker must read the agent's
// CURRENT Done channel, so a turn that started after an Esc (Cancel then
// ResetCancellation) still gets its dialog and its answer.

func stopChannelTestPermRequest() agent.PermissionRequest {
	return agent.PermissionRequest{ToolName: "bash", Scope: agent.PermissionScopeBashPrefix, Rule: "bash(git*)", Command: "git push"}
}

// nextSubAgentAsk receives a permission ask through the real listener command.
func nextSubAgentAsk(t *testing.T, m model) subAgentPermAskMsg {
	t.Helper()
	msg := listenSubAgentPerm(m.subAgentPermCh, context.Background())()
	ask, ok := msg.(subAgentPermAskMsg)
	if !ok {
		t.Fatalf("listener returned %T, want subAgentPermAskMsg", msg)
	}
	return ask
}

func awaitPermResponse(t *testing.T, ch <-chan agent.PermissionResponse) agent.PermissionResponse {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(2 * time.Second):
		t.Fatal("permission asker did not return")
		return agent.PermissionResponse{}
	}
}

func TestSubAgentPermAskAfterCancelAndResetIsShownAndAnswered(t *testing.T) {
	m := newPermQueueTestModel(t)
	ag := m.agent
	// The asker is built BEFORE the Esc, as wireCompactCallbacks does in a real
	// session: a stop channel captured at build time is closed by Cancel.
	asker := newSubAgentPermAsker(ag, m.subAgentPermCh, m.subAgentPermMu)
	ag.Cancel()
	ag.ResetCancellation()

	out := make(chan agent.PermissionResponse, 1)
	go func() { out <- asker(stopChannelTestPermRequest()) }()

	upd, _ := m.Update(nextSubAgentAsk(t, m))
	m = derefTestModel(t, upd)
	if !m.showPermDialog || m.pendingSubAgentResp == nil {
		t.Fatalf("permission dialog not open after cancel+reset (show=%v resp=%v)", m.showPermDialog, m.pendingSubAgentResp != nil)
	}

	m.permDialogInput("y")
	if got := awaitPermResponse(t, out); got.Level != agent.PermissionAllow {
		t.Fatalf("level = %v, want Allow: the ask was denied by a stale stop channel", got.Level)
	}
}

func TestQuestionParkedAfterCancelAndResetIsShownAndAnswered(t *testing.T) {
	m := newPermQueueTestModel(t)
	ag := m.agent
	// Built BEFORE the Esc, as in a real session (see the permission test above).
	asker := newParkedQuestionAsker(m.parkedQuestionCh, m.parkedQuestionMu, ag.Done)
	ag.Cancel()
	ag.ResetCancellation()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan parkedAskResult, 1)
	go func() {
		answers, dismissed := asker(ctx, []tool.QuestionPrompt{askOne("Scope", "Which package?")})
		result <- parkedAskResult{answers: answers, dismissed: dismissed}
	}()

	upd, _ := m.Update(nextParkedAsk(t, m))
	m = derefTestModel(t, upd)
	if !m.showQuestionDialog {
		t.Fatal("question dialog not open after cancel+reset: the ask was dismissed by a stale stop channel")
	}
	upd, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = derefTestModel(t, upd)
	upd, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = derefTestModel(t, upd)

	got := awaitParkedResult(t, result)
	if got.dismissed {
		t.Fatal("dismissed = true, want the answer delivered after cancel+reset")
	}
	if len(got.answers) != 1 || len(got.answers[0].Answers) != 1 || got.answers[0].Answers[0].Label != "no" {
		t.Fatalf("answers = %+v, want the single 'no' selection", got.answers)
	}
}

func TestBridgePermAskCancelWhileShownDeniesAndClosesDialog(t *testing.T) {
	m := newPermQueueTestModel(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	asker := newBridgePermAsker(m.agent, m.subAgentPermCh, m.subAgentPermMu)
	out := make(chan agent.PermissionResponse, 1)
	go func() { out <- asker(ctx, stopChannelTestPermRequest()) }()

	upd, watch := m.Update(nextSubAgentAsk(t, m))
	m = derefTestModel(t, upd)
	if !m.showPermDialog || m.pendingSubAgentResp == nil {
		t.Fatalf("bridge dialog not open (show=%v resp=%v)", m.showPermDialog, m.pendingSubAgentResp != nil)
	}
	if watch == nil {
		t.Fatal("bridge ask returned no end watcher, so its dialog could not close on cancel")
	}

	cancel()
	if got := awaitPermResponse(t, out); got.Level != agent.PermissionDeny {
		t.Fatalf("level = %v, want Deny after ctx cancel", got.Level)
	}
	ended, ok := watch().(permAskEndedMsg)
	if !ok {
		t.Fatalf("watcher returned %T, want permAskEndedMsg", ended)
	}
	upd, _ = m.Update(ended)
	m = derefTestModel(t, upd)
	if m.showPermDialog || m.pendingSubAgentResp != nil {
		t.Fatalf("permission dialog still open after the bridge ask was cancelled (show=%v resp=%v)", m.showPermDialog, m.pendingSubAgentResp != nil)
	}
}

func TestBridgePermAskCancelWhileQueuedIsDropped(t *testing.T) {
	m := newPermQueueTestModel(t)

	// A sub-agent ask holds the dialog, so the bridge ask below queues behind it.
	holderResp := make(chan agent.PermissionResponse, 1)
	upd, _ := m.Update(subAgentPermAskMsg{req: stopChannelTestPermRequest(), respCh: holderResp})
	m = derefTestModel(t, upd)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	asker := newBridgePermAsker(m.agent, m.subAgentPermCh, m.subAgentPermMu)
	out := make(chan agent.PermissionResponse, 1)
	go func() { out <- asker(ctx, stopChannelTestPermRequest()) }()

	upd, watch := m.Update(nextSubAgentAsk(t, m))
	m = derefTestModel(t, upd)
	if len(m.permAskQueue) != 1 {
		t.Fatalf("permAskQueue len = %d, want the bridge ask queued behind the open dialog", len(m.permAskQueue))
	}

	cancel()
	awaitPermResponse(t, out)
	ended, ok := watch().(permAskEndedMsg)
	if !ok {
		t.Fatalf("watcher returned %T, want permAskEndedMsg", ended)
	}
	upd, _ = m.Update(ended)
	m = derefTestModel(t, upd)

	if len(m.permAskQueue) != 0 {
		t.Fatalf("permAskQueue len = %d after the queued bridge ask was cancelled, want 0", len(m.permAskQueue))
	}
	if !m.showPermDialog || m.pendingSubAgentResp != holderResp {
		t.Fatal("the sub-agent dialog that was already open must stay open")
	}
}
