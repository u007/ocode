// internal/tui/goal.go
package tui

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/u007/ocode/internal/orchestrator"
)

// goalStatusMsg is sent to the TUI model each time the pipeline
// transitions to a new state. Live status streaming is a follow-up — the
// current TUI only surfaces the final result via goalDoneMsg.
type goalStatusMsg struct {
	state string
	msg   string
}

// goalDoneMsg is sent when the pipeline completes (pass or halt).
type goalDoneMsg struct {
	report string // FormatMarkdown() output
	err    string // non-empty if Run() returned an error
}

// runGoalBackground launches the orchestrator pipeline as a Bubble Tea
// background command. Status updates stream to the TUI via goalStatusMsg.
// noWorktree disables worktree isolation (--no-worktree mode).
func runGoalBackground(m *model, goal string, noWorktree bool) tea.Cmd {
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
			return goalDoneMsg{err: fmt.Sprintf("orchestrator error: %v", err)}
		}
		return goalDoneMsg{report: report.FormatMarkdown()}
	}
}
