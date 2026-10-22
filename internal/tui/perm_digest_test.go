package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
)

// permDigestStubClient answers the digest request with canned prose and records
// the user payload, so the TUI path never reaches a provider.
type permDigestStubClient struct {
	reply string
	err   error
	user  string
	calls int
}

func (c *permDigestStubClient) Chat(messages []agent.Message, _ []map[string]interface{}) (*agent.Message, error) {
	c.calls++
	for _, m := range messages {
		if m.Role == "user" {
			c.user = m.Content
		}
	}
	if c.err != nil {
		return nil, c.err
	}
	return &agent.Message{Role: "assistant", Content: c.reply}, nil
}

func (c *permDigestStubClient) GetProvider() string { return "testprov" }
func (c *permDigestStubClient) GetModel() string    { return "test-model" }

// newPermDigestTestModel lays out a chat view with one pending bash ask whose
// session agent is the stub (small model disabled, so the digest falls back to
// the session client exactly like production's no-small-model arm).
func newPermDigestTestModel(t *testing.T, stub agent.LLMClient) model {
	t.Helper()
	m := newTestModel()
	m.agent = newTestAgent(stub, nil, &config.Config{}, nil)
	m.ready = true
	upd, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 45})
	m = upd.(model)
	return deliverAsks(t, m, permAskToolMsg(t, "call_d", "bash", "rm -rf build"))
}

// runDigestCmd executes the tea.Cmd synchronously and folds its message back
// through Update, which is exactly what Bubble Tea does with it.
func runDigestCmd(t *testing.T, m model, cmd tea.Cmd) model {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected an explanation command, got nil")
	}
	msg, ok := cmd().(permDigestMsg)
	if !ok {
		t.Fatalf("command produced %T, want permDigestMsg", cmd())
	}
	upd, _ := m.Update(msg)
	return upd.(model)
}

// TestPermDialogExplainShowsTheModelsDigest is the happy path: `e` starts the
// side call WITHOUT answering the ask, and the reply renders inside the body.
func TestPermDialogExplainShowsTheModelsDigest(t *testing.T) {
	stub := &permDigestStubClient{reply: "Deletes the build output directory."}
	m := newPermDigestTestModel(t, stub)

	upd, cmd := m.handleChatKeys(tea.KeyPressMsg{Code: 'e', Text: "e"}, nil, nil)
	m = upd.(model)

	if !m.showPermDialog {
		t.Fatal("explain must not close the ask dialog")
	}
	if m.pendingToolCallID != "call_d" {
		t.Fatalf("pendingToolCallID = %q, want call_d (explain must not answer the ask)", m.pendingToolCallID)
	}
	if !m.permDigestBusy {
		t.Fatal("permDigestBusy = false, want true while the model runs")
	}
	m = runDigestCmd(t, m, cmd)

	if m.permDigest != "Deletes the build output directory." {
		t.Fatalf("permDigest = %q", m.permDigest)
	}
	if m.permDigestModel != "testprov/test-model" {
		t.Errorf("permDigestModel = %q, want the provider-qualified id", m.permDigestModel)
	}
	if stub.calls != 1 {
		t.Errorf("model calls = %d, want 1", stub.calls)
	}
	if !strings.Contains(stub.user, "rm -rf build") {
		t.Errorf("the pending command did not reach the model: %q", stub.user)
	}

	body := stripANSI(m.permissionBodyText(100))
	if !strings.Contains(body, "Deletes the build output directory.") {
		t.Errorf("digest missing from the dialog body:\n%s", body)
	}
	if !strings.Contains(body, "AI summary") || !strings.Contains(body, "not a safety verdict") {
		t.Errorf("digest must be labelled as an AI summary, not a verdict:\n%s", body)
	}
}

// TestPermDialogExplainShowsALoadingState proves the user gets immediate
// feedback between the keypress and the model's reply.
func TestPermDialogExplainShowsALoadingState(t *testing.T) {
	m := newPermDigestTestModel(t, &permDigestStubClient{reply: "x"})
	upd, cmd := m.handleChatKeys(tea.KeyPressMsg{Code: 'e', Text: "e"}, nil, nil)
	m = upd.(model)
	if cmd == nil {
		t.Fatal("expected a command")
	}
	if body := stripANSI(m.permissionBodyText(100)); !strings.Contains(body, "Asking the small model…") {
		t.Errorf("loading state missing from the body:\n%s", body)
	}
	_ = cmd // do not run it: the assertion is about the in-flight state
}

// TestSummarizeCancellationIsNotLabelledATimeout: closing the dialog (or
// navigating away) cancels the request, and the message must say so rather than
// claiming a 25 s deadline elapsed. The blocking client keeps the select parked
// on ctx.Done(), so the branch is deterministic.
func TestSummarizeCancellationIsNotLabelledATimeout(t *testing.T) {
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	a := newTestAgent(blockingPermDigestClient{release: block}, nil, &config.Config{}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := a.SummarizeToolCallCtx(ctx, agent.ToolCallDigestInput{Tool: "bash", Command: "ls"})
	if err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("err = %v, want a cancellation (not a timeout)", err)
	}
}

type blockingPermDigestClient struct{ release chan struct{} }

func (c blockingPermDigestClient) GetProvider() string { return "testprov" }
func (c blockingPermDigestClient) GetModel() string    { return "blocking" }
func (c blockingPermDigestClient) Chat([]agent.Message, []map[string]interface{}) (*agent.Message, error) {
	<-c.release
	return &agent.Message{Role: "assistant", Content: "late"}, nil
}

// TestPermDialogExplainShowsFailures covers the no-model/provider-error path:
// the dialog states why instead of silently doing nothing.
func TestPermDialogExplainShowsFailures(t *testing.T) {
	stub := &permDigestStubClient{err: errors.New("upstream exploded")}
	m := newPermDigestTestModel(t, stub)

	upd, cmd := m.handleChatKeys(tea.KeyPressMsg{Code: 'e', Text: "e"}, nil, nil)
	m = upd.(model)
	m = runDigestCmd(t, m, cmd)

	if !strings.Contains(m.permDigestErr, "upstream exploded") {
		t.Fatalf("permDigestErr = %q", m.permDigestErr)
	}
	if body := stripANSI(m.permissionBodyText(100)); !strings.Contains(body, "Could not explain this call") {
		t.Errorf("failure not surfaced in the body:\n%s", body)
	}
	if !m.showPermDialog {
		t.Error("a failed explanation must leave the ask answerable")
	}
}

// TestPermDialogExplainDropsAStaleReply: a reply for an ask that is no longer
// current must never be shown under the new command.
func TestPermDialogExplainDropsAStaleReply(t *testing.T) {
	m := newPermDigestTestModel(t, &permDigestStubClient{reply: "old answer"})
	upd, cmd := m.handleChatKeys(tea.KeyPressMsg{Code: 'e', Text: "e"}, nil, nil)
	m = upd.(model)

	// The next ask is PROMOTED (the previous one was answered) before the old
	// reply returns. A merely queued ask does not change the displayed command,
	// so it must not clear the digest — promotion is the reset point.
	m.showPermAsk(queuedPermAsk{
		req:        agent.PermissionRequest{ToolName: "bash", Command: "ls", Scope: agent.PermissionScopeTool},
		toolCallID: "call_new",
	})
	msg := cmd().(permDigestMsg)
	upd, _ = m.Update(msg)
	m = upd.(model)

	if m.permDigest != "" {
		t.Fatalf("stale explanation leaked into the new ask: %q", m.permDigest)
	}
}

// TestPermDialogExplainResetsWhenTheAskChanges pins the reset on promotion.
func TestPermDialogExplainResetsWhenTheAskChanges(t *testing.T) {
	m := newPermDigestTestModel(t, &permDigestStubClient{reply: "list files"})
	upd, cmd := m.handleChatKeys(tea.KeyPressMsg{Code: 'e', Text: "e"}, nil, nil)
	m = upd.(model)
	m = runDigestCmd(t, m, cmd)
	if m.permDigest == "" {
		t.Fatal("setup: expected a digest")
	}

	m.showPermAsk(queuedPermAsk{
		req:        agent.PermissionRequest{ToolName: "bash", Command: "ls -la", Scope: agent.PermissionScopeTool},
		toolCallID: "call_next",
	})
	if m.permDigest != "" || m.permDigestModel != "" {
		t.Fatalf("digest survived a new ask: %q / %q", m.permDigest, m.permDigestModel)
	}
}

// TestPermDialogExplainIsUnavailableForContentAsks: a content ask is about a
// result that already ran, so there is no call to explain.
func TestPermDialogExplainIsUnavailableForContentAsks(t *testing.T) {
	m := newPermDigestTestModel(t, &permDigestStubClient{reply: "x"})
	m.pendingPermission.Scope = agent.PermissionScopeContent

	if m.permDigestActionAvailable() {
		t.Fatal("content ask must not offer the explain action")
	}
	if body := stripANSI(m.permissionBodyText(100)); strings.Contains(body, "Asking the small model") {
		t.Errorf("content ask rendered a digest section:\n%s", body)
	}
}

// TestPermDialogExplainHintAndClickRegion pins the affordance: the hint row
// names it, and its clickable region sits one line under the button row.
func TestPermDialogExplainHintAndClickRegion(t *testing.T) {
	m := newPermDigestTestModel(t, &permDigestStubClient{reply: "x"})

	if hint := stripANSI(m.renderPermHintRow()); !strings.Contains(hint, "[e] explain") {
		t.Fatalf("hint row does not offer the action: %q", hint)
	}

	m.updatePermButtonRegions()
	var explain, allow *permButtonRegion
	for i := range m.permButtonRegions {
		switch m.permButtonRegions[i].choice {
		case "e":
			explain = &m.permButtonRegions[i]
		case "y":
			allow = &m.permButtonRegions[i]
		}
	}
	if explain == nil {
		t.Fatal("no click region registered for the explain chip")
	}
	if allow == nil {
		t.Fatal("setup: no allow button region")
	}
	if explain.y1 != allow.y1+3 {
		t.Errorf("explain region y1 = %d, want %d (one line under the 3-line button row)", explain.y1, allow.y1+3)
	}
	if explain.x1 != allow.x1 {
		t.Errorf("explain region x1 = %d, want %d (same x origin as the buttons)", explain.x1, allow.x1)
	}
}

// TestPermDialogExplainCommandDoesNotBlockUpdate proves the model call is
// deferred into the command (the Update call returns before the model answers).
func TestPermDialogExplainCommandDoesNotBlockUpdate(t *testing.T) {
	stub := &permDigestStubClient{reply: "x"}
	m := newPermDigestTestModel(t, stub)
	upd, cmd := m.handleChatKeys(tea.KeyPressMsg{Code: 'e', Text: "e"}, nil, nil)
	if cmd == nil {
		t.Fatal("expected a command")
	}
	if stub.calls != 0 {
		t.Fatalf("model called during Update (%d calls); the call must live in the tea.Cmd", stub.calls)
	}
	_ = upd
}
