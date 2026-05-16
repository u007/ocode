package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

type slashSuggestion struct {
	name string
	desc string
}

func slashSuggestions(prefix string) []slashSuggestion {
	lower := strings.ToLower(prefix)
	seen := make(map[string]struct{})
	items := make([]slashSuggestion, 0, len(commandSpecs)+len(loadedCustomCommands))

	for _, spec := range commandSpecs {
		if !strings.HasPrefix(spec.name, lower) {
			continue
		}
		if _, ok := seen[spec.name]; ok {
			continue
		}
		items = append(items, slashSuggestion{name: spec.name, desc: spec.help})
		seen[spec.name] = struct{}{}
	}

	for _, cmd := range loadedCustomCommands {
		name := "/" + cmd.Name
		if !strings.HasPrefix(strings.ToLower(name), lower) {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		items = append(items, slashSuggestion{name: name, desc: cmd.Description})
		seen[name] = struct{}{}
	}

	return items
}

func (m *model) closeSlashPopup() {
	m.showSlashPopup = false
	m.slashPopupIndex = 0
	m.slashPopupItems = nil
}

func (m model) updateSlashPopupState() model {
	value := m.input.Value()
	if !strings.HasPrefix(value, "/") || strings.Contains(value, " ") || m.showPicker || m.showConnect || m.showPalette {
		m.closeSlashPopup()
		return m
	}

	m.slashPopupItems = slashSuggestions(value)
	m.showSlashPopup = true
	if m.slashPopupIndex >= len(m.slashPopupItems) {
		m.slashPopupIndex = 0
	}
	return m
}

func (m model) slashPopupRowForY(y int) (int, bool) {
	if !m.showSlashPopup || len(m.slashPopupItems) == 0 {
		return 0, false
	}
	start, end := m.slashPopupVisibleRange()
	idx := y - (m.viewport.Height() + 4) // 2 (header) + 1 (transcript border) + 1 (popup border)
	if idx < 0 || start+idx >= end {
		return 0, false
	}
	return start + idx, true
}

func (m model) slashPopupVisibleRange() (int, int) {
	const maxRows = 8
	start := 0
	if m.slashPopupIndex >= maxRows {
		start = m.slashPopupIndex - maxRows + 1
	}
	end := start + maxRows
	if end > len(m.slashPopupItems) {
		end = len(m.slashPopupItems)
	}
	return start, end
}

func (m model) inputIsExactSlashCommand() bool {
	text := strings.TrimSpace(m.input.Value())
	if text == "" || strings.Contains(text, " ") {
		return false
	}
	if lookupCommand(text) != nil {
		return true
	}
	_, ok := customCommandLookup[text]
	return ok
}

func (m model) renderSlashPopup() string {
	nameStyle := lipgloss.NewStyle().Bold(true)
	descStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#565F89"))
	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#1A1B26")).
		Background(lipgloss.Color("#7AA2F7"))

	items := m.slashPopupItems
	start, end := m.slashPopupVisibleRange()

	var body strings.Builder
	if len(items) == 0 {
		body.WriteString(hintStyle.Render("(no matching commands)"))
		body.WriteByte('\n')
	} else {
		maxNameLen := 0
		for _, item := range items[start:end] {
			if len(item.name) > maxNameLen {
				maxNameLen = len(item.name)
			}
		}
		for i := start; i < end; i++ {
			item := items[i]
			paddedName := fmt.Sprintf("%-*s", maxNameLen, item.name)
			line := nameStyle.Render(paddedName) + "  " + descStyle.Render(item.desc)
			if i == m.slashPopupIndex {
				line = selectedStyle.Render(" " + paddedName + "  " + item.desc + " ")
			}
			body.WriteString(line)
			body.WriteByte('\n')
		}
	}

	body.WriteString(hintStyle.Render("↑/↓/Tab select · Enter confirm · Esc cancel"))
	width := m.panelWidth() - 2
	if width < 40 {
		width = 40
	}
	return borderStyle.Width(width).Render(body.String())
}
