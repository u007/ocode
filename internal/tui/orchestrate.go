// internal/tui/orchestrate.go
package tui

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/u007/ocode/internal/orchestrator"
)

// orchestrateStatusMsg is sent to the TUI model each time the pipeline
// transitions to a new state. Live status streaming is a follow-up — the
// current TUI only surfaces the final result via orchestrateDoneMsg.
type orchestrateStatusMsg struct {
	state string
	msg   string
}

// orchestrateDoneMsg is sent when the pipeline completes (pass or halt).
type orchestrateDoneMsg struct {
	report string // FormatMarkdown() output
	err    string // non-empty if Run() returned an error
}

// runOrchestrateBackground launches the orchestrator pipeline as a Bubble Tea
// background command. Status updates stream to the TUI via orchestrateStatusMsg.
// noWorktree disables worktree isolation (--no-worktree mode).
func runOrchestrateBackground(m *model, goal string, noWorktree bool) tea.Cmd {
	parent := m.agent
	workDir := m.workDir

	return func() tea.Msg {
		opts := orchestrator.PipelineOptions{
			UseWorktree: !noWorktree,
			WorkDir:     workDir,
		}
		pipeline := orchestrator.New(parent, opts)
		report, err := pipeline.Run(context.Background(), goal)
		if err != nil {
			return orchestrateDoneMsg{err: fmt.Sprintf("orchestrator error: %v", err)}
		}
		return orchestrateDoneMsg{report: report.FormatMarkdown()}
	}
}
