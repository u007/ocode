package tui

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	tea "charm.land/bubbletea/v2"
)

func Run(sessionID string, cont bool, yolo bool) error {
	// Redirect the standard library logger into the debug panel before anything
	// runs. Once bubbletea enters the alt-screen, any stray log/os.Stderr write
	// paints over the frame and corrupts it; routing log here keeps those
	// messages visible without bleeding onto the screen.
	log.SetFlags(0)
	log.SetOutput(debugLogWriter{})

	m := newModel(sessionID, cont, yolo)

	if m.config != nil {
		if err := validateStartupEditorConfig(&m.config.Ocode); err != nil {
			fmt.Fprintf(os.Stderr, "ocode: %v\n", err)
			return err
		}
	}

	p := tea.NewProgram(m)
	var finalModel tea.Model
	defer func() {
		if finalModel == nil {
			return
		}
		cleanupProgramModel(finalModel)
	}()
	stopSignals := watchProgramSignals(p)
	defer stopSignals()
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

func watchProgramSignals(p *tea.Program) func() {
	sigCh := make(chan os.Signal, 1)
	done := make(chan struct{})
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		for {
			select {
			case <-done:
				return
			case <-sigCh:
				p.Send(cleanupRequestMsg{})
			}
		}
	}()
	return func() {
		signal.Stop(sigCh)
		close(done)
	}
}

func cleanupProgramModel(m tea.Model) {
	switch m := m.(type) {
	case model:
		m.cleanupCurrentSession()
	case *model:
		m.cleanupCurrentSession()
	}
}

func exitResumeSummary(sessionID string) string {
	if sessionID == "" {
		return ""
	}
	return fmt.Sprintf("Session ID: %s\nResume with: ocode -session %s\n", sessionID, sessionID)
}
