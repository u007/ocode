package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jamesmercstudio/ocode/internal/agent"

	tea "charm.land/bubbletea/v2"
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

func (m model) updateSlashPopupState() (model, tea.Cmd) {
	value := m.input.Value()
	if m.showPicker || m.showConnect || m.showPalette {
		m.closeSlashPopup()
		return m, nil
	}
	if token, ok := activeAtToken(value); ok {
		var cmd tea.Cmd
		if m.fileListCache == nil {
			cmd = buildFileListCache()
		}
		m.slashPopupItems = filterFileCache(m.fileListCache, strings.ToLower(token))
		m.showSlashPopup = true
		if m.slashPopupIndex >= len(m.slashPopupItems) {
			m.slashPopupIndex = 0
		}
		return m, cmd
	}
	if !strings.HasPrefix(value, "/") || strings.Contains(value, " ") {
		m.closeSlashPopup()
		return m, nil
	}

	m.slashPopupItems = slashSuggestions(value)
	m.showSlashPopup = true
	if m.slashPopupIndex >= len(m.slashPopupItems) {
		m.slashPopupIndex = 0
	}
	return m, nil
}

func buildFileListCache() tea.Cmd {
	return func() tea.Msg {
		var items []slashSuggestion
		filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error { //nolint:errcheck
			if err != nil {
				return nil
			}
			name := d.Name()
			if d.IsDir() && (name == ".git" || name == ".ocode") {
				return filepath.SkipDir
			}
			if d.IsDir() {
				return nil
			}
			clean := strings.TrimPrefix(filepath.ToSlash(path), "./")
			desc := "file"
			if agent.IsImageFile(clean) {
				desc = "image"
			}
			items = append(items, slashSuggestion{name: "@" + clean, display: "@" + clean, desc: desc})
			return nil
		})
		return fileListCacheMsg{items: items}
	}
}

func filterFileCache(cache []slashSuggestion, query string) []slashSuggestion {
	out := make([]slashSuggestion, 0, 32)
	for _, item := range cache {
		clean := strings.TrimPrefix(strings.ToLower(item.name), "@")
		if query == "" || fuzzyScore(clean, query) > 0 {
			out = append(out, item)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		li := fuzzyScore(strings.TrimPrefix(strings.ToLower(out[i].name), "@"), query)
		lj := fuzzyScore(strings.TrimPrefix(strings.ToLower(out[j].name), "@"), query)
		if li != lj {
			return li > lj
		}
		return strings.ToLower(out[i].name) < strings.ToLower(out[j].name)
	})
	return out
}

func activeAtToken(value string) (string, bool) {
	idx := strings.LastIndex(value, "@")
	if idx == -1 {
		return "", false
	}
	if idx > 0 {
		prev := value[idx-1]
		if prev != ' ' && prev != '\n' && prev != '\t' {
			return "", false
		}
	}
	token := value[idx+1:]
	if strings.ContainsAny(token, " \n\t") {
		return "", false
	}
	return token, true
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

func (m *model) acceptPopupSuggestion(selected slashSuggestion) {
	m.closeSlashPopup()
	if strings.HasPrefix(selected.name, "@") {
		value := m.input.Value()
		idx := strings.LastIndex(value, "@")
		if idx == -1 {
			m.input.SetValue(selected.name + " ")
			return
		}
		m.input.SetValue(value[:idx] + selected.name + " ")
		return
	}
	m.input.SetValue(selected.name + " ")
	if selected.name == "/models" {
		m.openModelPicker()
	} else if selected.name == "/session" {
		m.openSessionPicker()
	} else if selected.name == "/themes" {
		m.openThemePicker()
	}
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
