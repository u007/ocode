package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/jamesmercstudio/ocode/internal/agent"
)

func (m *model) openModelPicker() {
	provider := "openai"
	if m.agent != nil {
		provider = m.agent.GetProvider()
	}
	items := agent.ProviderModels(provider)
	m.pickerItems = items
	m.pickerIndex = 0
	m.pickerFilter = ""
	m.showPicker = true
}

func (m *model) closePicker() {
	m.showPicker = false
	m.pickerItems = nil
	m.pickerIndex = 0
	m.pickerFilter = ""
}

func (m model) pickerVisibleItems() []string {
	if m.pickerFilter == "" {
		return m.pickerItems
	}
	q := strings.ToLower(m.pickerFilter)
	out := make([]string, 0, len(m.pickerItems))
	for _, item := range m.pickerItems {
		if strings.Contains(strings.ToLower(item), q) {
			out = append(out, item)
		}
	}
	return out
}

func (m model) renderPicker() string {
	headerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#7DCFFF")).Bold(true)
	hintLine := hintStyle.Render("↑/↓ select · Enter confirm · Esc cancel · type to filter")

	header := headerStyle.Render("Select model") + "  " + hintStyle.Render("filter: "+m.pickerFilter+"_")

	items := m.pickerVisibleItems()
	var body strings.Builder
	if len(items) == 0 {
		body.WriteString(hintStyle.Render("(no models — check provider auth or network)"))
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
				line = lipgloss.NewStyle().Foreground(lipgloss.Color("#1A1B26")).Background(lipgloss.Color("#7AA2F7")).Render(" " + line + " ")
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
