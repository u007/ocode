package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/u007/ocode/internal/commandctx"
)

// buildDocSyncPrompt delegates to the shared commandctx.DocSync builder so the
// server's /api/command-context/doc-sync endpoint produces byte-identical
// prompts.
func buildDocSyncPrompt(workDir string, mode string) (string, error) {
	return commandctx.DocSync(workDir, mode)
}

func (m *model) handleDocSyncCmd(args []string) tea.Cmd {
	mode := "session"
	if len(args) > 0 {
		switch strings.ToLower(args[0]) {
		case "all":
			mode = "all"
		case "session":
			mode = "session"
		default:
			m.messages = append(m.messages, message{
				role: roleAssistant,
				text: fmt.Sprintf("/doc-sync: unknown mode %q — use 'session' or 'all'", args[0]),
			})
			return nil
		}
	}

	prompt, err := buildDocSyncPrompt(m.workDir, mode)
	if err != nil {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("/doc-sync: %v", err)})
		return nil
	}

	if m.agent != nil {
		m.agent.ResetSubagentDispatch()
	}
	m.rerenderTranscriptAndMaybeScroll()
	return m.sendCustomCommandPrompt(prompt)
}
