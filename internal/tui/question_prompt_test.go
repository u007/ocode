package tui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/tui/fastviewport"
)

func testQuestionContent() string {
	return `QUESTION_PROMPT:
[{"header":"Continue with what?","question":"There's no active task in this session yet — what would you like to work on?","options":[{"label":"Read AGENTS.md","description":"Review agent instructions and project setup"},{"label":"Explore codebase","description":"Browse the internal/ directory structure"},{"label":"Something else","description":"Tell me what you need"}],"multiple":false}]

WAITING_FOR_USER_RESPONSE`
}

func TestQuestionPromptStartsInteractiveDialog(t *testing.T) {
	m := model{input: textarea.New(), questionInput: newTestTextarea(), viewport: fastviewport.New(90, 20), styles: ApplyThemeColors("tokyonight")}
	m.appendAgentMessage(agent.Message{Role: "tool", ToolID: "call-1", Content: testQuestionContent()})

	if !m.showQuestionDialog {
		t.Fatal("expected question dialog to open")
	}
	if len(m.questionPrompts) != 1 {
		t.Fatalf("expected one prompt, got %d", len(m.questionPrompts))
	}
	view := m.renderQuestionDialog(88)
	for _, want := range []string{"Continue with what?", "Read AGENTS.md", "Something else"} {
		if !strings.Contains(stripANSI(view), want) {
			t.Fatalf("expected dialog to contain %q, got %q", want, stripANSI(view))
		}
	}
}

func TestQuestionPromptArrowSelectionSubmitsToolResult(t *testing.T) {
	m := model{input: newTestTextarea(), questionInput: newTestTextarea(), viewport: fastviewport.New(90, 20), styles: ApplyThemeColors("tokyonight")}
	m.appendAgentMessage(agent.Message{Role: "tool", ToolID: "call-1", Content: testQuestionContent()})

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	got := derefTestModel(t, updated)
	if got.questionCursor[0] != 1 {
		t.Fatalf("expected cursor on second option, got %d", got.questionCursor[0])
	}
	updated, _ = got.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	got = derefTestModel(t, updated)

	if got.showQuestionDialog {
		t.Fatal("expected dialog to close after answer")
	}
	last := got.messages[len(got.messages)-1]
	if last.raw == nil || last.raw.Role != "tool" || last.raw.ToolID != "call-1" {
		t.Fatalf("expected final message to be tool result, got %#v", last.raw)
	}
	if !strings.Contains(last.raw.Content, "Explore codebase") {
		t.Fatalf("expected submitted answer in tool result, got %q", last.raw.Content)
	}
}

func TestQuestionPromptOtherOpensTextInput(t *testing.T) {
	m := model{input: newTestTextarea(), questionInput: newTestTextarea(), viewport: fastviewport.New(90, 20), styles: ApplyThemeColors("tokyonight")}
	m.appendAgentMessage(agent.Message{Role: "tool", ToolID: "call-1", Content: testQuestionContent()})
	m.questionCursor[0] = 2

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	got := derefTestModel(t, updated)
	if !got.questionTextActive {
		t.Fatal("expected other option to open text input")
	}
	got.questionInput.SetValue("Fix process tool question handling")
	updated, _ = got.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	got = derefTestModel(t, updated)

	if got.showQuestionDialog || got.questionTextActive {
		t.Fatal("expected custom answer to submit and close dialog")
	}
	last := got.messages[len(got.messages)-1]
	for _, want := range []string{"Something else", "Fix process tool question handling", "custom"} {
		if !strings.Contains(last.raw.Content, want) {
			t.Fatalf("expected custom response to contain %q, got %q", want, last.raw.Content)
		}
	}
}

// TestQueueDrainBlockedWhileQuestionActive locks in the contract that the
// post-stream queue (queued inputs, queued commands, background-job resume)
// is NOT processed while a question dialog is active. The handler gates on
// !m.queueDrainBlocked(), so this test fails if the guard is removed.
func TestQueueDrainBlockedWhileQuestionActive(t *testing.T) {
	m := model{}
	if m.queueDrainBlocked() {
		t.Fatal("queue must not be blocked when no question dialog is active")
	}
	m.showQuestionDialog = true
	if !m.queueDrainBlocked() {
		t.Fatal("queue must be blocked while a question dialog is active")
	}
}

func TestQuestionPromptLeftRightTabs(t *testing.T) {
	content := `QUESTION_PROMPT:
[{"header":"First","question":"Pick first","options":[{"label":"A","description":"Alpha"}],"multiple":false},{"header":"Second","question":"Pick second","options":[{"label":"B","description":"Beta"}],"multiple":false}]

WAITING_FOR_USER_RESPONSE`
	m := model{input: newTestTextarea(), questionInput: newTestTextarea(), viewport: fastviewport.New(90, 20), styles: ApplyThemeColors("tokyonight")}
	m.appendAgentMessage(agent.Message{Role: "tool", ToolID: "call-1", Content: content})

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	got := derefTestModel(t, updated)
	if got.questionTab != 1 {
		t.Fatalf("expected right arrow to move to second question tab, got %d", got.questionTab)
	}
	updated, _ = got.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	got = derefTestModel(t, updated)
	if got.questionTab != 0 {
		t.Fatalf("expected left arrow to move to first question tab, got %d", got.questionTab)
	}
}
