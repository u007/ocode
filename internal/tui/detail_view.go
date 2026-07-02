package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/tool"
)

const agentRunPreviewLineCount = 4

// detailViewKind enumerates the recursive drill-in screens.
type detailViewKind int

const (
	detailAgentRun    detailViewKind = iota // one async subagent's transcript
	detailProcessList                       // list of processes in a registry
	detailProcessLog                        // one process's streamed output
	detailReview                            // code review overlay
)

// detailView is one entry on the drill-in stack.
type detailView struct {
	kind     detailViewKind
	runID    string // set for detailAgentRun and process views scoped to a run
	runPath  string // unique nested path for detailAgentRun/process views
	procID   string // set for detailProcessLog
	vp       viewport.Model
	content  string
	lines    []string
	rawLines []string
	sel      selectionState
	runs     []detailRunBlock
	procs    []detailProcBlock
	expanded map[string]bool
	regions  []detailExpandRegion
	// in-view find bar (ctrl+f while the detail card is open)
	searchActive  bool
	searchInput   textinput.Model
	searchQuery   string
	searchMatches []int // rawLines indices containing the query
	searchCursor  int   // index into searchMatches of the current jump, -1 = none
	searchNoMatch bool  // true when query is non-empty but has zero matches
}

type detailExpandRegion struct {
	id       string
	rowStart int
	rowEnd   int
}

type detailRunBlock struct {
	runID    string
	runPath  string
	rowStart int
	rowEnd   int
}

type detailProcBlock struct {
	procID   string
	rowStart int
	rowEnd   int
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
	content, _, _, _ := renderRunTranscriptDetail(run, run.ID, width, nil)
	return content
}

func renderRunTranscriptDetail(run *agent.AgentRun, runPath string, width int, expanded map[string]bool) (string, []detailRunBlock, []detailProcBlock, []detailExpandRegion) {
	if width < 24 {
		width = 24
	}
	return renderAgentRunCard(run, runPath, width, 0, expanded)
}

func renderAgentRunCard(run *agent.AgentRun, runPath string, width, depth int, expanded map[string]bool) (string, []detailRunBlock, []detailProcBlock, []detailExpandRegion) {
	if run == nil {
		return "", nil, nil, nil
	}
	indent := strings.Repeat("  ", min(depth, 2))
	cardWidth := max(20, width-lipglossWidth(indent)-4)
	children := agentRunChildren(run)
	childSummary := formatChildSummary(children)

	var body strings.Builder
	var runBlocks []detailRunBlock
	var procBlocks []detailProcBlock
	var expandRegions []detailExpandRegion
	currentRow := 0
	innerWidth := max(1, cardWidth-4)
	appendBlock := func(block string) {
		block = strings.TrimRight(block, "\n")
		if block == "" {
			body.WriteString("\n")
			currentRow++
			return
		}
		body.WriteString(block)
		body.WriteString("\n")
		currentRow += lineCount(block)
	}
	appendLine := func(line string) {
		appendBlock(wrapView(line, innerWidth))
	}
	appendExpandableRegion := func(id, block string) {
		start := currentRow
		appendBlock(block)
		expandRegions = append(expandRegions, detailExpandRegion{id: id, rowStart: start, rowEnd: currentRow})
	}
	header := fmt.Sprintf("▾ %s  %s %s · %s", run.Name, statusIcon(run.Status, "●"), run.Status, formatRunElapsed(run))
	if childSummary != "" {
		header += " · " + childSummary
	}
	cardStart := currentRow
	appendLine(header)
	appendLine(hintStyle.Render(run.ID))

	if run.Status == agent.RunFailed && strings.TrimSpace(run.Err) != "" {
		appendLine(errorStyle.Render("Error: " + strings.TrimSpace(run.Err)))
	}

	if messages := run.TranscriptPublic(); len(messages) > 0 {
		appendLine(headerStyle.Render("Messages"))
		toolNames := make(map[string]string)
		systemHidden := false
		for i, msg := range messages {
			switch msg.Role {
			case "system":
				systemHidden = true
			case "user":
				if text := strings.TrimSpace(msg.Content); text != "" {
					appendLine(headerStyle.Render("Task"))
					appendLine(renderMarkdown(text, textStyle))
				}
			case "assistant":
				if msg.ReasoningContent != "" {
					regionID := fmt.Sprintf("%s/thinking:%d", runPath, i)
					appendExpandableRegion(regionID, renderDetailThinkingBox(msg.ReasoningContent, innerWidth, expanded[regionID]))
				}
				if text := strings.TrimSpace(msg.Content); text != "" {
					appendLine(headerStyle.Render("LLM message"))
					appendLine(renderMarkdown(text, textStyle))
				}
				for j, tc := range msg.ToolCalls {
					toolNames[tc.ID] = tc.Function.Name
					regionID := fmt.Sprintf("%s/tool-request:%d:%d", runPath, i, j)
					appendExpandableRegion(regionID, renderDetailToolRequestBox(tc, innerWidth, expanded[regionID]))
				}
			case "tool":
				regionID := fmt.Sprintf("%s/tool-result:%d", runPath, i)
				appendExpandableRegion(regionID, renderDetailToolResultBox(toolNames[msg.ToolID], msg.Content, innerWidth, expanded[regionID]))
			}
		}
		if systemHidden {
			appendLine(hintStyle.Render("System prompt hidden"))
		}
	} else {
		appendLine(hintStyle.Render("No user-visible activity yet."))
	}

	if run.Status == agent.RunDone && strings.TrimSpace(run.Result) != "" {
		appendLine(headerStyle.Render("Result"))
		appendLine(renderMarkdown(strings.TrimSpace(run.Result), textStyle))
	}

	if procs := runProcesses(run); len(procs) > 0 {
		appendLine(headerStyle.Render("Background processes"))
		for _, pi := range procs {
			line := formatProcessListLine(pi)
			rowStart := currentRow
			appendLine(line)
			procBlocks = append(procBlocks, detailProcBlock{procID: pi.ID, rowStart: rowStart, rowEnd: currentRow})
		}
	}

	if len(children) > 0 {
		appendLine(headerStyle.Render("Sub-agents"))
		for _, child := range children {
			childStart := currentRow
			childPath := runPath + "/" + child.ID
			childText, childRuns, childProcs, childRegions := renderAgentRunCard(child, childPath, max(20, cardWidth-4), depth+1, expanded)
			body.WriteString(childText)
			if !strings.HasSuffix(childText, "\n") {
				body.WriteString("\n")
			}
			added := lineCount(childText)
			for _, b := range childRuns {
				b.rowStart += childStart
				b.rowEnd += childStart
				runBlocks = append(runBlocks, b)
			}
			for _, b := range childProcs {
				b.rowStart += childStart
				b.rowEnd += childStart
				procBlocks = append(procBlocks, b)
			}
			for _, b := range childRegions {
				b.rowStart += childStart
				b.rowEnd += childStart
				expandRegions = append(expandRegions, b)
			}
			currentRow += added
		}
	}

	// Render as plain content (no boxed background) so the transcript matches
	// the main chat view. Indentation provides the visual nesting cue for
	// sub-agent cards.
	card := strings.TrimRight(body.String(), "\n")
	runBlocks = append([]detailRunBlock{{runID: run.ID, runPath: runPath, rowStart: cardStart, rowEnd: currentRow}}, runBlocks...)
	for i := range runBlocks {
		runBlocks[i].rowStart++
		runBlocks[i].rowEnd++
	}
	for i := range procBlocks {
		procBlocks[i].rowStart++
		procBlocks[i].rowEnd++
	}
	for i := range expandRegions {
		expandRegions[i].rowStart++
		expandRegions[i].rowEnd++
	}
	if indent == "" {
		return card, runBlocks, procBlocks, expandRegions
	}
	return indentBlock(card, indent), runBlocks, procBlocks, expandRegions
}

func renderDetailThinkingBox(content string, width int, expanded bool) string {
	text := strings.TrimSpace(renderThinkingContent(content, currentStyles()))
	if text == "" {
		return ""
	}
	wrapped := wrapView(text, max(1, width))
	lines := strings.Split(wrapped, "\n")
	totalLines := len(lines)
	collapsed := !expanded && totalLines > 8
	header := "⟁ thinking"
	if collapsed {
		header = fmt.Sprintf("⟁ thinking · %d lines [▸ click to expand]", totalLines)
	} else if totalLines > 8 {
		header = fmt.Sprintf("⟁ thinking · %d lines [▾ click to collapse]", totalLines)
	}
	body := text
	if collapsed {
		body = strings.Join(lines[totalLines-8:], "\n")
	}
	return thinkingHeaderStyle.Render(header) + "\n" + toolBoxStyle.Width(max(1, width)).Render(textStyle.Render(body))
}

func renderDetailToolRequestBox(tc agent.ToolCall, width int, expanded bool) string {
	toolName := tc.Function.Name
	if toolName == "" {
		toolName = "tool"
	}
	var body string
	if toolName == "advisor" {
		// Extract the full prompt directly for a cleaner header in the detail
		// view instead of relying on formatToolCallHint's summary format.
		var args map[string]interface{}
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err == nil {
			if p, ok := args["prompt"].(string); ok && p != "" {
				body = "◆ advisor prompt:\n" + p
			}
		}
		if body == "" {
			body = formatToolCallHint(tc)
		}
	} else {
		body = formatToolCallHint(tc)
		if args := strings.TrimSpace(prettyToolArguments(tc.Function.Arguments)); args != "" && args != "{}" {
			body += "\n\n" + args
		}
	}
	visible, footer := collapsePreviewBlock(body, 8, expanded, false)
	box := toolBoxStyle.Width(max(1, width)).Render(textStyle.Render(visible))
	header := hintStyle.Render("tool request · " + toolName)
	if footer != "" {
		return header + "\n" + box + "\n" + hintStyle.Render(footer)
	}
	return header + "\n" + box
}

func renderDetailToolResultBox(toolName, content string, width int, expanded bool) string {
	if toolName == "" {
		toolName = "tool"
	}
	content = stripTruncationFooter(content)
	content = strings.TrimRight(content, "\n")
	previewLines := toolOutputPreviewLines
	tail := true
	if toolName == "advisor" {
		// Show more lines from the top so the key advice steps are visible.
		previewLines = 20
		tail = false
	}
	visible, footer := collapsePreviewBlock(content, previewLines, expanded, tail)
	box := toolBoxStyle.Width(max(1, width)).Render(renderToolResult(toolName, visible, currentStyles()))
	header := hintStyle.Render("tool result · " + toolName)
	if footer != "" {
		return header + "\n" + box + "\n" + hintStyle.Render(footer)
	}
	return header + "\n" + box
}

func collapsePreviewBlock(content string, previewLines int, expanded, tail bool) (string, string) {
	content = strings.TrimRight(content, "\n")
	if content == "" {
		return "", ""
	}
	lines := strings.Split(content, "\n")
	if expanded && len(lines) > previewLines {
		return content, "  ▲ click to collapse"
	}
	if len(lines) <= previewLines {
		return content, ""
	}
	if tail {
		return strings.Join(lines[len(lines)-previewLines:], "\n"), fmt.Sprintf("  … %d earlier lines · click to expand", len(lines)-previewLines)
	}
	return strings.Join(lines[:previewLines], "\n"), fmt.Sprintf("  … %d more lines · click to expand", len(lines)-previewLines)
}

func prettyToolArguments(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var v interface{}
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return raw
	}
	formatted, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return raw
	}
	return string(formatted)
}

func lineCount(s string) int {
	trimmed := strings.TrimSuffix(stripANSI(s), "\n")
	if trimmed == "" {
		return 0
	}
	return strings.Count(trimmed, "\n") + 1
}

func runProcesses(run *agent.AgentRun) []tool.ProcInfo {
	if run == nil || run.Procs == nil {
		return nil
	}
	return run.Procs.Snapshot()
}

func formatProcessListLine(pi tool.ProcInfo) string {
	line := fmt.Sprintf("  %-8s %-9s %s", pi.ID, pi.Status, pi.Command)
	if pi.Status != tool.ProcRunning {
		line += fmt.Sprintf("  (exit %d)", pi.ExitCode)
	}
	return line
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
		b.WriteString(formatProcessListLine(pi) + "\n")
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
