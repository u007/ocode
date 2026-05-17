package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
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

func (m *model) openThemePicker() {
	items := AvailableThemes()
	m.pickerKind = "theme"
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
		if !isRevertibleUserMessage(msg) {
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

func isRevertibleUserMessage(msg message) bool {
	if msg.role != roleUser {
		return false
	}
	if msg.raw != nil && msg.raw.Role != "user" {
		return false
	}
	return strings.TrimSpace(msg.text) != ""
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

func (m model) pickerVisibleRange() (int, int) {
	const maxRows = 15
	items, _ := m.pickerVisibleItems()
	start := 0
	if m.pickerIndex >= maxRows {
		start = m.pickerIndex - maxRows + 1
	}
	end := start + maxRows
	if end > len(items) {
		end = len(items)
	}
	return start, end
}

func (m model) pickerRowForY(y int) (int, bool) {
	if !m.showPicker {
		return 0, false
	}
	start, end := m.pickerVisibleRange()
	idx := y - 3 // border top + header + blank line
	if idx < 0 || start+idx >= end {
		return 0, false
	}
	return start + idx, true
}

func (m model) selectPickerIndex(index int) (tea.Model, tea.Cmd) {
	items, values := m.pickerVisibleItems()
	if len(items) == 0 || index < 0 || index >= len(items) {
		m.closePicker()
		return m, nil
	}

	selected := values[index]
	kind := m.pickerKind
	m.closePicker()
	m.input.Reset()
	if kind == "session" {
		return m.handleCommand("/session load " + selected)
	}
	if kind == "message" {
		idx, _ := strconv.Atoi(selected)
		input := m.messages[idx].text
		m.messages = m.messages[:idx]
		m.input.SetValue(input)
		m.renderTranscript()
		m.viewport.GotoBottom()
		if len(m.messages) == 0 {
			session.Save(m.sessionID, "", nil, m.sessionSidebarMetadata()) //nolint:errcheck
		} else {
			m.saveSession()
		}
		return m, nil
	}
	if kind == "theme" {
		return m.handleCommand("/themes " + selected)
	}
	return m.handleCommand("/models " + selected)
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
	if m.pickerKind == "theme" {
		title = "Select theme"
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
		if m.pickerKind == "theme" {
			empty = "(no themes)"
		}
		body.WriteString(hintStyle.Render(empty))
	} else {
		start, end := m.pickerVisibleRange()
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
		const maxRows = 15
		if len(items) > maxRows {
			body.WriteString(hintStyle.Render(fmt.Sprintf("  …%d of %d shown", end-start, len(items))))
		}
	}

	width := m.width - 4
	if width < 40 {
		width = 40
	}

	filteredItems, _ := m.pickerVisibleItems()
	start, end := m.pickerVisibleRange()
	visibleCount := end - start
	if visibleCount < 1 {
		visibleCount = 1
	}
	sb := renderListScrollbar(visibleCount, len(filteredItems), start, visibleCount)
	bodyStr := body.String()
	hintStr := hintLine
	sbLines := strings.Split(sb, "\n")
	bodyLines := strings.Split(strings.TrimRight(bodyStr, "\n"), "\n")
	for i, bLine := range bodyLines {
		sbCol := scrollbarTrackStyle.Render(scrollbarTrack)
		if i < len(sbLines) {
			sbCol = sbLines[i]
		}
		bodyLines[i] = bLine + sbCol
	}
	inner := header + "\n\n" + strings.Join(bodyLines, "\n") + "\n" + hintStr
	return borderStyle.Width(width).Render(inner)
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
}
