package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/tool"
)

// clickStatusLine clicks (press+release) at the given status-bar-relative row.
func clickStatusLine(t *testing.T, m *model, relRow int) *model {
	t.Helper()
	m.renderStatus()
	statusTop := m.statusBarTopY()
	x := 10
	upd, _ := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: x, Y: statusTop + relRow})
	m = modelPtr(upd)
	upd, _ = m.Update(tea.MouseReleaseMsg{Button: tea.MouseNone, X: x, Y: statusTop + relRow})
	return modelPtr(upd)
}

func pendingTestModel(t *testing.T) *model {
	t.Helper()
	m := newModel()
	m.ready = true
	upd, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	mm := upd.(model)
	return &mm
}

// A pending permission ask opened on a non-Chat tab must offer a way back:
// clicking the status-bar pending hint jumps to Chat where the dialog renders.
func TestStatusPendingClickJumpsToChat_Perm(t *testing.T) {
	for _, tab := range []int{tabFiles, tabGit, tabChanges, tabLog, tabAgents} {
		m := pendingTestModel(t)
		m.activeTab = tab
		m.showPermDialog = true
		m.pendingPermission = agent.PermissionRequest{ToolName: "bash", Command: "rm -rf build", Rule: "bash(rm*)"}
		m.renderStatus()
		if got := m.statusRawLines[1]; !strings.Contains(got, "permission pending") || !strings.Contains(got, "click Chat to answer") {
			t.Fatalf("tab %d: status line missing pending hint: %q", tab, got)
		}
		m = clickStatusLine(t, m, 1)
		if m.activeTab != tabChat {
			t.Fatalf("tab %d: status pending click left activeTab=%d, want Chat(%d)", tab, m.activeTab, tabChat)
		}
	}
}

// Same for a pending question ask, which previously had no status-bar hint at all.
func TestStatusPendingClickJumpsToChat_Question(t *testing.T) {
	for _, tab := range []int{tabFiles, tabGit, tabAgents} {
		m := pendingTestModel(t)
		m.activeTab = tab
		m.showQuestionDialog = true
		m.questionPrompts = []tool.QuestionPrompt{{Header: "Approach", Question: "Which approach?", Options: []tool.QuestionOption{{Label: "A"}, {Label: "B"}}}}
		m.renderStatus()
		if got := m.statusRawLines[1]; !strings.Contains(got, "question pending") || !strings.Contains(got, "click Chat to answer") {
			t.Fatalf("tab %d: status line missing question hint: %q", tab, got)
		}
		m = clickStatusLine(t, m, 1)
		if m.activeTab != tabChat {
			t.Fatalf("tab %d: status question click left activeTab=%d, want Chat(%d)", tab, m.activeTab, tabChat)
		}
	}
}

// No pending ask: a status-bar click must not yank the user to Chat.
func TestStatusClickStaysWithoutPending(t *testing.T) {
	m := pendingTestModel(t)
	m.activeTab = tabFiles
	m.renderStatus()
	m = clickStatusLine(t, m, 1)
	if m.activeTab != tabFiles {
		t.Fatalf("no pending: status click moved to tab %d, want Files(%d)", m.activeTab, tabFiles)
	}
}

// Keyboard answer still works when the dialog is open off-Chat (existing
// behavior the click fix must not break): y/n keys are handled globally while
// showPermDialog, with no Chat-tab requirement.
func TestPermKeysWorkOffChat(t *testing.T) {
	m := pendingTestModel(t)
	m.activeTab = tabGit
	m.showPermDialog = true
	req := agent.PermissionRequest{ToolName: "bash", Command: "ls", Rule: "bash(ls*)"}
	m.pendingPermission = req
	m.pendingToolName = req.ToolName
	// handleChatKeys without an agent returns an error cmd, but must still
	// consume the key as a dialog answer (close the dialog) rather than
	// passing it through to the Git tab.
	next, _ := m.handleChatKeys(tea.KeyPressMsg{Code: 'y', Text: "y"}, nil, nil)
	mm := next.(model)
	if mm.showPermDialog {
		t.Fatal("y key off-Chat did not close the permission dialog")
	}
}
