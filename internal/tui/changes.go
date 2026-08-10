package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/u007/ocode/internal/commandctx"
	"github.com/u007/ocode/internal/lsp"
)

// getChangesContext gathers all context for the /changes command. Thin
// wrapper over the shared commandctx package so the web/desktop server can
// produce the identical prompt via GET /api/command-context/changes.
func getChangesContext(workDir string, lspMgr *lsp.Manager) (string, error) {
	return commandctx.ChangesContext(workDir, lspMgr)
}

// buildChangesPrompt creates the LLM prompt for the /changes command. Thin
// wrapper over commandctx.BuildChangesPrompt.
func buildChangesPrompt(context string) string {
	return commandctx.BuildChangesPrompt(context)
}

// runChangesCmd is the command wrapper for /changes.
func runChangesCmd(m *model, args []string) tea.Cmd {
	return m.handleChangesCmd(args)
}

// handleChangesCmd gathers all change context and sends it to the agent.
func (m *model) handleChangesCmd(args []string) tea.Cmd {
	prompt, err := commandctx.Changes(m.workDir, m.lspMgr)
	if err != nil {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("/changes: %v", err)})
		return nil
	}
	return m.sendCustomCommandPrompt(prompt)
}
