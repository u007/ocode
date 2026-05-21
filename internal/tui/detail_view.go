package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/viewport"
	"github.com/jamesmercstudio/ocode/internal/agent"
	"github.com/jamesmercstudio/ocode/internal/tool"
)

// detailViewKind enumerates the recursive drill-in screens.
type detailViewKind int

const (
	detailAgentRun     detailViewKind = iota // one async subagent's transcript
	detailProcessList                        // list of processes in a registry
	detailProcessLog                         // one process's streamed output
)

// detailView is one entry on the drill-in stack.
type detailView struct {
	kind   detailViewKind
	runID  string // set for detailAgentRun and process views scoped to a run
	procID string // set for detailProcessLog
	vp     viewport.Model
}

// detailStack is a LIFO stack of drill-in views. The empty stack means the
// normal tabbed UI is showing.
type detailStack []detailView

func (s detailStack) empty() bool { return len(s) == 0 }

func (s detailStack) top() (detailView, bool) {
	if len(s) == 0 {
		return detailView{}, false
	}
	return s[len(s)-1], true
}

func (s *detailStack) push(v detailView) { *s = append(*s, v) }

func (s *detailStack) pop() {
	if len(*s) == 0 {
		return
	}
	*s = (*s)[:len(*s)-1]
}

// agentStripBlock holds the screen rows occupied by one strip block, for
// mouse hit-testing.
type agentStripBlock struct {
	runID    string
	rowStart int // inclusive, screen-relative within the strip
	rowEnd   int // exclusive
}

// blockAtRow returns the run id of the strip block containing the given
// strip-relative row, or "" if none.
func blockAtRow(blocks []agentStripBlock, row int) string {
	for _, b := range blocks {
		if row >= b.rowStart && row < b.rowEnd {
			return b.runID
		}
	}
	return ""
}

// renderRunTranscript formats an AgentRun's transcript for the detail viewport.
func renderRunTranscript(run *agent.AgentRun) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Agent %s (%s) — %s\n\n", run.ID, run.Name, run.Status))
	b.WriteString("────────────────────────────────────────\n\n")
	for _, m := range run.TranscriptPublic() {
		switch m.Role {
		case "assistant":
			if m.Content != "" {
				b.WriteString(m.Content + "\n\n")
			}
			for _, tc := range m.ToolCalls {
				b.WriteString("  ⚙ " + tc.Function.Name + " " + tc.Function.Arguments + "\n")
			}
		case "tool":
			b.WriteString("  ↳ " + truncateToWidth(strings.ReplaceAll(m.Content, "\n", " "), 200) + "\n")
		case "user", "system":
			b.WriteString("» " + m.Content + "\n\n")
		}
	}
	if run.Status == agent.RunDone {
		b.WriteString("\n── result ──\n" + run.Result + "\n")
	} else if run.Status == agent.RunFailed {
		b.WriteString("\n── error ──\n" + run.Err + "\n")
	}
	return b.String()
}

// renderProcessList formats a registry's processes for the list view.
func renderProcessList(reg *tool.ProcessRegistry) string {
	var b strings.Builder
	b.WriteString("Background processes (enter/click to open):\n\n")
	for _, pi := range reg.Snapshot() {
		line := fmt.Sprintf("  %-8s %-9s %s", pi.ID, pi.Status, pi.Command)
		if pi.Status != tool.ProcRunning {
			line += fmt.Sprintf("  (exit %d)", pi.ExitCode)
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

// renderProcessLog formats one process's current output.
func renderProcessLog(reg *tool.ProcessRegistry, id string) string {
	text, status, code, err := reg.Dump(id)
	if err != nil {
		return "Error: " + err.Error()
	}
	header := fmt.Sprintf("Process %s — %s", id, status)
	if status != tool.ProcRunning {
		header += fmt.Sprintf(" (exit %d)", code)
	}
	return header + "\n\n────────────────────────────────────────\n\n" + text
}
