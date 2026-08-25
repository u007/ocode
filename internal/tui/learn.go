package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/u007/ocode/internal/commandctx"
)

// buildLearnPrompt delegates to the shared commandctx.Learn builder so the
// server's /api/command-context/learn endpoint produces byte-identical prompts.
func buildLearnPrompt(workDir string, args []string) (string, error) {
	return commandctx.Learn(workDir, args)
}

func (m *model) handleLearnCmd(args []string) tea.Cmd {
	prompt, err := buildLearnPrompt(m.workDir, args)
	if err != nil {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("/learn: %v", err)})
		return nil
	}
	if m.agent != nil {
		m.agent.ResetSubagentDispatch()
	}
	m.rerenderTranscriptAndMaybeScroll()
	return m.sendCustomCommandPrompt(prompt)
}
