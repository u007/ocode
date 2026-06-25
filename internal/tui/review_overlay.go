package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/atotto/clipboard"
)

// reviewOverlay represents the review overlay state.
type reviewOverlay struct {
	active   bool
	result   reviewResult
	vp       viewport.Model
	scrollY  int
	selected int // index of selected finding (-1 for none)
}

// openReviewOverlay pushes a review result onto the detail stack for display.
func (m *model) openReviewOverlay(result reviewResult) {
	// Render the review content
	content := renderReviewContent(result, m.panelWidth())

	// Create a viewport for the review
	vp := viewport.New(
		viewport.WithWidth(m.panelWidth()),
		viewport.WithHeight(m.detailViewportHeight()),
	)
	vp.SetContent(content)

	// Create a detail view for the review
	dv := detailView{
		kind:    detailReview, // Use the review detail view kind
		vp:      vp,
		content: content,
	}

	// Push onto the detail stack
	m.detail.push(dv)
}

// renderReviewContent renders the review result as styled content.
func renderReviewContent(result reviewResult, width int) string {
	var b strings.Builder

	// Header
	header := fmt.Sprintf("⌾ Code Review — %s", result.Timestamp.Format("2006-01-02 15:04:05"))
	b.WriteString(headerStyle.Render(header))
	b.WriteString("\n\n")

	// Summary
	if result.Summary != "" {
		b.WriteString(headerStyle.Render("Summary"))
		b.WriteString("\n")
		b.WriteString(textStyle.Render(result.Summary))
		b.WriteString("\n\n")
	}

	// Findings
	if len(result.Findings) > 0 {
		b.WriteString(headerStyle.Render(fmt.Sprintf("Findings (%d)", len(result.Findings))))
		b.WriteString("\n\n")

		for i, finding := range result.Findings {
			// Finding header with severity icon
			icon := severityIcon(finding.Severity)
			label := severityLabel(finding.Severity)
			findingHeader := fmt.Sprintf("%s %s #%d", icon, label, i+1)

			// File and line info
			location := ""
			if finding.File != "" {
				location = finding.File
				if finding.Line > 0 {
					location += fmt.Sprintf(":%d", finding.Line)
				}
			}

			if location != "" {
				findingHeader += fmt.Sprintf(" — %s", location)
			}

			b.WriteString(headerStyle.Render(findingHeader))
			b.WriteString("\n")

			// Message
			b.WriteString(textStyle.Render(finding.Message))
			b.WriteString("\n")

			// Suggestion
			if finding.Suggestion != "" {
				b.WriteString(hintStyle.Render("Suggestion: " + finding.Suggestion))
				b.WriteString("\n")
			}

			b.WriteString("\n")
		}
	} else {
		b.WriteString(hintStyle.Render("No findings reported."))
		b.WriteString("\n")
	}

	// Hints
	b.WriteString("\n")
	b.WriteString(hintStyle.Render("esc: back · j/k: scroll · mouse: scroll · drag: select"))
	b.WriteString("\n")
	b.WriteString(hintStyle.Render("Press 'a' to accept all suggestions · 'e' to export · 'c' to copy"))

	return b.String()
}

// handleReviewKeys handles keyboard input when the review overlay is active.
func (m *model) handleReviewKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "a":
		// Accept all suggestions (placeholder for future implementation)
		m.messages = append(m.messages, message{role: roleAssistant, text: "Accept functionality will be implemented with patch generation."})
		return m, nil
	case "e":
		// Export review to file
		return m, m.exportReview()
	case "c":
		// Copy review to clipboard
		return m, m.copyReviewToClipboard()
	}
	return m, nil
}

// exportReview exports the current review to a file.
func (m *model) exportReview() tea.Cmd {
	return func() tea.Msg {
		if !m.review.active {
			return statusMsg{text: "No active review to export"}
		}

		result := m.review.result
		filename := fmt.Sprintf("review_%s.md", time.Now().Format("2006-01-02_150405"))

		var b strings.Builder
		b.WriteString(fmt.Sprintf("# Code Review — %s\n\n", result.Timestamp.Format("2006-01-02 15:04:05")))
		b.WriteString(fmt.Sprintf("## Target: %s\n\n", result.Context))

		if result.Summary != "" {
			b.WriteString("## Summary\n\n")
			b.WriteString(result.Summary)
			b.WriteString("\n\n")
		}

		if len(result.Findings) > 0 {
			b.WriteString("## Findings\n\n")
			for i, finding := range result.Findings {
				icon := severityIcon(finding.Severity)
				label := severityLabel(finding.Severity)
				b.WriteString(fmt.Sprintf("### %s %s #%d\n\n", icon, label, i+1))

				if finding.File != "" {
					b.WriteString(fmt.Sprintf("**File:** %s", finding.File))
					if finding.Line > 0 {
						b.WriteString(fmt.Sprintf(":%d", finding.Line))
					}
					b.WriteString("\n\n")
				}

				b.WriteString(fmt.Sprintf("%s\n\n", finding.Message))

				if finding.Suggestion != "" {
					b.WriteString(fmt.Sprintf("**Suggestion:** %s\n\n", finding.Suggestion))
				}
			}
		}

		err := os.WriteFile(filename, []byte(b.String()), 0644)
		if err != nil {
			return statusMsg{text: fmt.Sprintf("Error exporting review: %v", err)}
		}
		return statusMsg{text: fmt.Sprintf("Review exported to %s", filename)}
	}
}

// copyReviewToClipboard copies the review summary to clipboard.
func (m *model) copyReviewToClipboard() tea.Cmd {
	return func() tea.Msg {
		if !m.review.active {
			return statusMsg{text: "No active review to copy"}
		}

		result := m.review.result
		var b strings.Builder

		b.WriteString(fmt.Sprintf("Code Review — %s\n", result.Timestamp.Format("2006-01-02 15:04:05")))
		b.WriteString(fmt.Sprintf("Target: %s\n\n", result.Context))

		if result.Summary != "" {
			b.WriteString("Summary:\n")
			b.WriteString(result.Summary)
			b.WriteString("\n\n")
		}

		if len(result.Findings) > 0 {
			b.WriteString(fmt.Sprintf("Findings (%d):\n", len(result.Findings)))
			for i, finding := range result.Findings {
				label := severityLabel(finding.Severity)
				b.WriteString(fmt.Sprintf("%d. [%s] %s", i+1, label, finding.Message))
				if finding.File != "" {
					b.WriteString(fmt.Sprintf(" (%s", finding.File))
					if finding.Line > 0 {
						b.WriteString(fmt.Sprintf(":%d", finding.Line))
					}
					b.WriteString(")")
				}
				b.WriteString("\n")
			}
		}

		err := clipboard.WriteAll(b.String())
		if err != nil {
			return statusMsg{text: fmt.Sprintf("Error copying to clipboard: %v", err)}
		}
		return statusMsg{text: "Review copied to clipboard"}
	}
}

// renderReviewDetail renders the review overlay in the detail view.
func (m model) renderReviewDetail(d detailView) string {
	var title string
	switch d.kind {
	case detailReview:
		title = "Code Review"
	case detailProcessList:
		title = "Background processes"
	case detailProcessLog:
		title = "Process " + d.procID
	}

	hints := "esc: back · j/k: scroll · mouse: scroll · drag: select"
	if d.kind == detailReview {
		hints += " · a: accept all · e: export · c: copy"
	}

	header := wrapView(hintStyle.Render("◆ "+title)+hintStyle.Render("  "+hints), m.panelWidth())
	scrollbar := renderScrollbar(d.vp.Height(), d.vp.TotalLineCount(), d.vp.VisibleLineCount(), d.vp.YOffset())
	bodyContent := lipgloss.JoinHorizontal(lipgloss.Top,
		constrainView(d.vp.View(), d.vp.Width(), d.vp.Height()),
		scrollbar,
	)
	body := borderStyle.Width(m.panelWidth() - 2).Render(bodyContent)
	statusBar := m.renderDetailStatusBar(d)
	if statusBar == "" {
		return lipgloss.JoinVertical(lipgloss.Left, header, body)
	}
	return lipgloss.JoinVertical(lipgloss.Left, header, body, statusBar)
}
