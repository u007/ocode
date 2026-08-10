package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/u007/ocode/internal/commandctx"
	"github.com/u007/ocode/internal/lsp"
)

// getStandupContext gathers context for the /standup command. Thin wrapper
// over the shared commandctx package so the web/desktop server can produce
// the identical prompt via GET /api/command-context/standup.
func getStandupContext(workDir string, lspMgr *lsp.Manager) (string, error) {
	return commandctx.StandupContext(workDir, lspMgr)
}

// buildStandupPrompt creates the LLM prompt for the /standup command. Thin
// wrapper over commandctx.BuildStandupPrompt.
func buildStandupPrompt(context string) string {
	return commandctx.BuildStandupPrompt(context)
}

// runStandupCmd is the command wrapper for /standup.
func runStandupCmd(m *model, args []string) tea.Cmd {
	return m.handleStandupCmd(args)
}

// handleStandupCmd gathers commit + change context and sends it to the agent.
func (m *model) handleStandupCmd(args []string) tea.Cmd {
	prompt, err := commandctx.Standup(m.workDir, m.lspMgr)
	if err != nil {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("/standup: %v", err)})
		return nil
	}
	return m.sendCustomCommandPrompt(prompt)
}
