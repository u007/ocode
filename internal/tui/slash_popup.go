package tui

import (
	"fmt"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"
)

type slashSuggestion struct {
	name    string
	display string
	desc    string
}

func slashSuggestions(prefix string) []slashSuggestion {
	// Strip the leading slash for scoring so "/mod" and "mod" behave the
	// same and rank by the command name itself.
	q := strings.TrimPrefix(strings.ToLower(prefix), "/")

	type scoredSuggestion struct {
		score int
		idx   int
		item  slashSuggestion
	}
	scored := make([]scoredSuggestion, 0, len(commandSpecs)+len(loadedCustomCommands))
	seen := make(map[string]struct{})
	order := 0

	consider := func(name, display, desc string, searchKeys []string) {
		if _, ok := seen[name]; ok {
			return
		}
		best := 0
		for _, k := range searchKeys {
			if s := fuzzyScore(strings.TrimPrefix(strings.ToLower(k), "/"), q); s > best {
				best = s
			}
		}
		if best == 0 {
			return
		}
		seen[name] = struct{}{}
		scored = append(scored, scoredSuggestion{
			score: best,
			idx:   order,
			item:  slashSuggestion{name: name, display: display, desc: desc},
		})
		order++
	}

	for _, spec := range commandSpecs {
		keys := append([]string{spec.name}, spec.aliases...)
		display := spec.name
		// If an alias scores higher than the canonical name, show it in the label.
		bestAlias, bestAliasScore := "", 0
		for _, a := range spec.aliases {
			if s := fuzzyScore(strings.TrimPrefix(strings.ToLower(a), "/"), q); s > bestAliasScore {
				bestAlias, bestAliasScore = a, s
			}
		}
		nameScore := fuzzyScore(strings.TrimPrefix(strings.ToLower(spec.name), "/"), q)
		if bestAliasScore > nameScore && bestAlias != "" {
			display = bestAlias + " → " + spec.name
		}
		consider(spec.name, display, spec.help, keys)
	}

	for _, cmd := range loadedCustomCommands {
		name := "/" + cmd.Name
		consider(name, name, cmd.Description, []string{name})
	}

	sort.SliceStable(scored, func(a, b int) bool {
		if scored[a].score != scored[b].score {
			return scored[a].score > scored[b].score
		}
		return scored[a].idx < scored[b].idx
	})
	out := make([]slashSuggestion, len(scored))
	for i, s := range scored {
		out[i] = s.item
	}
	return out
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
	idx := y - (m.viewport.Height() + 4) // 1 (header) + 1 (transcript border top) + viewport.Height() (content) + 1 (transcript border bottom) + 1 (popup border top)
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
	descStyle := m.styles.Hint

	items := m.slashPopupItems
	start, end := m.slashPopupVisibleRange()

	var body strings.Builder
	if len(items) == 0 {
		body.WriteString(hintStyle.Render("(no matching commands)"))
		body.WriteByte('\n')
	} else {
		maxNameLen := 0
		for _, item := range items[start:end] {
			if len(item.display) > maxNameLen {
				maxNameLen = len(item.display)
			}
		}
		for i := start; i < end; i++ {
			item := items[i]
			paddedName := fmt.Sprintf("%-*s", maxNameLen, item.display)
			line := nameStyle.Render(paddedName) + "  " + descStyle.Render(item.desc)
			if i == m.slashPopupIndex {
				line = m.styles.Selected.Render(" " + paddedName + "  " + item.desc + " ")
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
