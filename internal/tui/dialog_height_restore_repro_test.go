package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/tool"
)

// Repro: after answering a permission or ask-tool (question) dialog, the
// transcript viewport must scale back to its pre-dialog height.
func TestPermDialogRestoresViewportHeightAfterAnswer(t *testing.T) {
	m := newModel()
	m.ready = true
	upd, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = upd.(model)

	baseline := m.viewport.Height()

	req := agent.PermissionRequest{ToolName: "bash", Command: "ls -la", Rule: "bash(ls*)"}
	m.showPermDialog = true
	m.pendingPermission = req
	m.pendingToolName = req.ToolName
	m.layout()
	open := m.viewport.Height()
	if open >= baseline {
		t.Fatalf("dialog open did not shrink viewport: open=%d baseline=%d", open, baseline)
	}

	// Answer "y" via the real keyboard path.
	next, _ := m.handleChatKeys(tea.KeyPressMsg{Code: 'y', Text: "y"}, nil, nil)
	m = next.(model)
	if got := m.viewport.Height(); got != baseline {
		t.Errorf("perm: viewport height %d != baseline %d after answer (open=%d)", got, baseline, open)
	}
}

func TestQuestionDialogRestoresViewportHeightAfterAnswer(t *testing.T) {
	m := newModel()
	m.ready = true
	upd, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = upd.(model)

	baseline := m.viewport.Height()

	prompts := []tool.QuestionPrompt{{
		Header:   "Approach",
		Question: "Which approach?",
		Options:  []tool.QuestionOption{{Label: "A"}, {Label: "B"}},
	}}
	m.startQuestionPrompt("tc-1", prompts)
	m.layout() // any layout while the dialog is open shrinks the transcript
	open := m.viewport.Height()
	if open >= baseline {
		t.Fatalf("question dialog open did not shrink viewport: open=%d baseline=%d", open, baseline)
	}

	// Answer: enter on first option (single question -> auto-submit).
	next, _ := m.handleQuestionKeys(tea.KeyPressMsg{Code: tea.KeyEnter}, nil, nil)
	m = next.(model)
	if m.showQuestionDialog {
		t.Fatalf("question dialog still open after answer")
	}
	if got := m.viewport.Height(); got != baseline {
		t.Errorf("question: viewport height %d != baseline %d after answer (open=%d)", got, baseline, open)
	}
}
