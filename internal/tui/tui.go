package tui

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
)

func Run(sessionID string, cont bool, yolo bool) error {
	m := newModel(sessionID, cont, yolo)

	if m.config != nil {
		if err := validateStartupEditorConfig(&m.config.Ocode); err != nil {
			fmt.Fprintf(os.Stderr, "ocode: %v\n", err)
			return err
		}
	}

	p := tea.NewProgram(m)
	finalModel, err := p.Run()
	if err != nil {
		return err
	}
	switch m := finalModel.(type) {
	case model:
		fmt.Fprint(os.Stdout, exitResumeSummary(m.sessionID))
	case *model:
		fmt.Fprint(os.Stdout, exitResumeSummary(m.sessionID))
	}
	return nil
}

func exitResumeSummary(sessionID string) string {
	if sessionID == "" {
		return ""
	}
	return fmt.Sprintf("Session ID: %s\nResume with: ocode -session %s\n", sessionID, sessionID)
}
