package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/viewport"
	"github.com/jamesmercstudio/ocode/internal/agent"
	"github.com/jamesmercstudio/ocode/internal/tool"
)

const agentRunPreviewLineCount = 3

// detailViewKind enumerates the recursive drill-in screens.
type detailViewKind int

const (
	detailAgentRun    detailViewKind = iota // one async subagent's transcript
	detailProcessList                       // list of processes in a registry
	detailProcessLog                        // one process's streamed output
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

// renderRunTranscript formats an AgentRun as a structured, nested trace for the
// detail viewport. It intentionally hides raw system prompts; the default user
// view should explain agent activity, not dump internal prompts.
func renderRunTranscript(run *agent.AgentRun, width int) string {
	if width < 24 {
		width = 24
	}
	return renderAgentRunCard(run, width, 0)
}

func renderAgentRunCard(run *agent.AgentRun, width, depth int) string {
	if run == nil {
		return ""
	}
	indent := strings.Repeat("  ", min(depth, 2))
	cardWidth := max(20, width-lipglossWidth(indent)-2)
	children := agentRunChildren(run)
	childSummary := formatChildSummary(children)

	var body strings.Builder
	header := fmt.Sprintf("▾ %s  %s %s · %s", run.Name, statusIcon(run.Status, "●"), run.Status, formatRunElapsed(run))
	if childSummary != "" {
		header += " · " + childSummary
	}
	body.WriteString(header)
	body.WriteString("\n")
	body.WriteString(hintStyle.Render(run.ID))

	if run.Status == agent.RunFailed && strings.TrimSpace(run.Err) != "" {
		body.WriteString("\n\n")
		body.WriteString(errorStyle.Render("Error: " + strings.TrimSpace(run.Err)))
	}

	if events := agentRunEvents(run, 0); len(events) > 0 {
		body.WriteString("\n\n")
		body.WriteString(headerStyle.Render("Timeline"))
		for _, event := range events {
			body.WriteString("\n")
			body.WriteString("• " + renderMarkdownBold(event, textStyle))
		}
	} else {
		body.WriteString("\n\n")
		body.WriteString(hintStyle.Render("No user-visible activity yet."))
	}

	if run.Status == agent.RunDone && strings.TrimSpace(run.Result) != "" {
		body.WriteString("\n\n")
		body.WriteString(headerStyle.Render("Result"))
		body.WriteString("\n")
		body.WriteString(renderMarkdownBold(strings.TrimSpace(run.Result), textStyle))
	}

	if len(children) > 0 {
		body.WriteString("\n\n")
		body.WriteString(headerStyle.Render("Sub-agents"))
		for _, child := range children {
			body.WriteString("\n")
			body.WriteString(renderAgentRunCard(child, max(20, cardWidth-4), depth+1))
		}
	}

	card := toolBoxStyle.Width(cardWidth).Render(body.String())
	if indent == "" {
		return card
	}
	return indentBlock(card, indent)
}

func agentRunEvents(run *agent.AgentRun, limit int) []string {
	var events []string
	systemHidden := false
	for _, m := range run.TranscriptPublic() {
		switch m.Role {
		case "system":
			systemHidden = true
		case "user":
			if summary := firstMeaningfulLine(m.Content); summary != "" {
				events = append(events, "Task: "+summary)
			}
		case "assistant":
			if summary := firstMeaningfulLine(m.Content); summary != "" {
				events = append(events, "Agent: "+summary)
			}
			for _, tc := range m.ToolCalls {
				if tc.Function.Name != "" {
					events = append(events, "Tool call: "+tc.Function.Name)
				}
			}
		case "tool":
			if summary := summarizeToolEvent(m.Content); summary != "" {
				events = append(events, "Tool result: "+summary)
			}
		}
	}
	if systemHidden {
		events = append([]string{"System prompt hidden"}, events...)
	}
	if limit > 0 && len(events) > limit {
		return events[len(events)-limit:]
	}
	return events
}

func agentRunChildren(run *agent.AgentRun) []*agent.AgentRun {
	if run == nil || run.Sub == nil || run.Sub.Runs() == nil {
		return nil
	}
	return run.Sub.Runs().Snapshot()
}

func formatChildSummary(children []*agent.AgentRun) string {
	if len(children) == 0 {
		return ""
	}
	running, done, failed := 0, 0, 0
	for _, child := range children {
		switch child.Status {
		case agent.RunRunning:
			running++
		case agent.RunDone:
			done++
		case agent.RunFailed:
			failed++
		}
	}
	parts := []string{fmt.Sprintf("%d sub", len(children))}
	if running > 0 {
		parts = append(parts, fmt.Sprintf("%d running", running))
	}
	if done > 0 {
		parts = append(parts, fmt.Sprintf("%d done", done))
	}
	if failed > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", failed))
	}
	return strings.Join(parts, " · ")
}

func firstMeaningfulLine(content string) string {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "```") {
			continue
		}
		return truncateToWidth(trimmed, 160)
	}
	return ""
}

func summarizeToolEvent(content string) string {
	content = strings.TrimSpace(stripTruncationFooter(content))
	if content == "" {
		return "completed"
	}
	if strings.HasPrefix(content, "Error:") || strings.Contains(content, "\nError:") {
		return truncateToWidth(firstMeaningfulLine(content), 160)
	}
	if strings.Contains(content, "state: running") && strings.Contains(content, "task_id:") {
		return "sub-agent started"
	}
	return truncateToWidth(firstMeaningfulLine(content), 160)
}

func formatRunElapsed(run *agent.AgentRun) string {
	if run == nil || run.StartedAt.IsZero() {
		return "0s"
	}
	end := run.EndedAt
	if end.IsZero() {
		end = time.Now()
	}
	d := end.Sub(run.StartedAt).Round(time.Second)
	if d < time.Second {
		return "<1s"
	}
	return d.String()
}

func statusIcon(status agent.RunStatus, running string) string {
	switch status {
	case agent.RunDone:
		return successStyle.Render("✓")
	case agent.RunFailed:
		return errorStyle.Render("✗")
	default:
		return running
	}
}

func indentBlock(s, indent string) string {
	lines := strings.Split(s, "\n")
	for i := range lines {
		if lines[i] != "" {
			lines[i] = indent + lines[i]
		}
	}
	return strings.Join(lines, "\n")
}

func lipglossWidth(s string) int {
	return len([]rune(s))
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
