package tui

import (
	"fmt"
	"strings"

	"github.com/jamesmercstudio/ocode/internal/agent"
	"github.com/jamesmercstudio/ocode/internal/session"
)

func (m *model) openModelPicker() {
	items := agent.AllProviderModels()
	m.pickerKind = "model"
	m.pickerItems = items
	m.pickerValues = items
	m.pickerIndex = 0
	m.pickerFilter = ""
	m.showPicker = true
}

func (m *model) openSessionPicker() {
	sessions, err := session.ListAll()
	if err != nil {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Error listing sessions: %v", err)})
		return
	}
	items := make([]string, 0, len(sessions))
	values := make([]string, 0, len(sessions))
	for _, s := range sessions {
		title := s.Title
		if title == "" {
			title = "(no title)"
		}
		marker := "[ocode]"
		if s.Source == session.SourceClaude {
			marker = "[claude]"
		}
		items = append(items, fmt.Sprintf("%s %s  %s", marker, s.ID, title))
		values = append(values, s.ID)
	}
	m.pickerKind = "session"
	m.pickerItems = items
	m.pickerValues = values
	m.pickerIndex = 0
	m.pickerFilter = ""
	m.showPicker = true
}

func (m *model) openMessagePicker() {
	items := make([]string, 0, len(m.messages))
	values := make([]string, 0, len(m.messages))
	for i, msg := range m.messages {
		if msg.role != roleUser {
			continue
		}
		preview := strings.TrimSpace(msg.text)
		if len(preview) > 80 {
			preview = preview[:77] + "..."
		}
		items = append(items, fmt.Sprintf("[%d] %s", i, preview))
		values = append(values, fmt.Sprintf("%d", i))
	}
	m.pickerKind = "message"
	m.pickerItems = items
	m.pickerValues = values
	m.pickerIndex = 0
	m.pickerFilter = ""
	m.showPicker = true
}

func (m *model) closePicker() {
	m.showPicker = false
	m.pickerKind = ""
	m.pickerItems = nil
	m.pickerValues = nil
	m.pickerIndex = 0
	m.pickerFilter = ""
	m.input.Focus()
}

func (m model) pickerVisibleItems() ([]string, []string) {
	valuesFor := func(items []string) []string {
		if len(m.pickerValues) != len(m.pickerItems) {
			return items
		}
		values := make([]string, 0, len(items))
		used := make(map[int]struct{})
		for _, item := range items {
			for i, original := range m.pickerItems {
				if item != original {
					continue
				}
				if _, ok := used[i]; ok {
					continue
				}
				used[i] = struct{}{}
				values = append(values, m.pickerValues[i])
				break
			}
		}
		return values
	}

	if m.pickerFilter == "" {
		return m.pickerItems, valuesFor(m.pickerItems)
	}

	items := fuzzyFilter(m.pickerItems, m.pickerFilter)
	return items, valuesFor(items)
}

func (m model) renderPicker() string {
	hintLine := hintStyle.Render("↑/↓ select · Enter confirm · Esc cancel · type to filter")

	title := "Select model"
	if m.pickerKind == "session" {
		title = "Resume session"
	}
	if m.pickerKind == "message" {
		title = "Revert to message"
	}
	header := m.styles.Header.Render(title) + "  " + hintStyle.Render("filter: "+m.pickerFilter+"_")

	items, _ := m.pickerVisibleItems()
	var body strings.Builder
	if len(items) == 0 {
		empty := "(no models — check provider auth or network)"
		if m.pickerKind == "session" {
			empty = "(no sessions)"
		}
		if m.pickerKind == "message" {
			empty = "(no user messages)"
		}
		body.WriteString(hintStyle.Render(empty))
	} else {
		maxRows := 15
		start := 0
		if m.pickerIndex >= maxRows {
			start = m.pickerIndex - maxRows + 1
		}
		end := start + maxRows
		if end > len(items) {
			end = len(items)
		}
		for i := start; i < end; i++ {
			line := items[i]
			if i == m.pickerIndex {
				line = m.styles.Selected.Render(" " + line + " ")
			} else {
				line = "  " + line
			}
			body.WriteString(line)
			body.WriteString("\n")
		}
		if len(items) > maxRows {
			body.WriteString(hintStyle.Render(fmt.Sprintf("  …%d of %d shown", end-start, len(items))))
		}
	}

	width := m.width - 4
	if width < 40 {
		width = 40
	}
	return borderStyle.Width(width).Render(header + "\n\n" + body.String() + "\n" + hintLine)
}

func (m *model) cycleAgentMode() {
	specs := agent.DefaultAgents
	if len(specs) == 0 {
		return
	}
	m.currentAgentIdx = (m.currentAgentIdx + 1) % len(specs)
	spec := specs[m.currentAgentIdx]
	if m.agent != nil {
		m.agent.SetSpec(&spec)
	}
	m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Agent → %s (%s)", spec.Name, spec.Description)})
	m.renderTranscript()
	m.viewport.GotoBottom()
}

func (m model) agentModeLabel() string {
	if m.agent == nil {
		return string(agent.ModeBuild)
	}
	return string(m.agent.Mode())
}
