package tui

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/u007/ocode/internal/agent"

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

	// Add agents from the registry as slash-command suggestions so users
	// can tab-complete /build, /git-commit-push, etc.
	for _, def := range agent.DefaultAgentRegistry.All() {
		if def.Hidden {
			continue
		}
		name := "/" + def.Name
		consider(name, name, def.Description, []string{name})
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
	m.popSlashPopupModal()
	m.slashPopupIndex = 0
	m.slashPopupItems = nil
}

// looksLikeFilePath returns true when the input is most likely a file path
// (e.g. "/path/to/file.png") rather than a slash command like "/models".
func looksLikeFilePath(s string) bool {
	if !strings.HasPrefix(s, "/") {
		return false
	}
	rest := s[1:] // strip leading slash
	if strings.Contains(rest, "/") {
		return true // contains more path segments: /home/user/file.png
	}
	// Single-segment absolute path with an image extension: /file.png
	return agent.IsImageFile(s)
}

// shortcodeForPastedFiles converts terminal file drags (which arrive as pasted
// absolute paths or file:// URIs) into compact, Claude Code-style file tokens.
// It only converts when the entire paste is one or more existing files, so
// ordinary pasted prose is left untouched.
func shortcodeForPastedFiles(content, workDir string) (string, bool) {
	paths := pastedExistingFilePaths(content)
	if len(paths) == 0 {
		return content, false
	}

	shortcodes := make([]string, 0, len(paths))
	for _, p := range paths {
		shortcodes = append(shortcodes, compactFileShortcode(p))
	}
	return strings.Join(shortcodes, " ") + " ", true
}

func (m *model) shortcodePastedFiles(content string) (string, bool) {
	paths := pastedExistingFilePaths(content)
	if len(paths) == 0 {
		return content, false
	}
	if m.fileShortcodePaths == nil {
		m.fileShortcodePaths = make(map[string]string)
	}

	shortcodes := make([]string, 0, len(paths))
	for _, p := range paths {
		token := m.uniqueFileShortcode(p)
		m.fileShortcodePaths[token] = p
		shortcodes = append(shortcodes, token)
	}
	return strings.Join(shortcodes, " ") + " ", true
}

func (m *model) uniqueFileShortcode(path string) string {
	base := safeFileShortcodeLabel(filepath.Base(path))
	for i := 1; ; i++ {
		label := base
		if i > 1 {
			label = fmt.Sprintf("%s %d", base, i)
		}
		token := "[file: " + label + "]"
		if existing, ok := m.fileShortcodePaths[token]; !ok || existing == path {
			return token
		}
	}
}

func compactFileShortcode(path string) string {
	return "[file: " + safeFileShortcodeLabel(filepath.Base(path)) + "]"
}

func safeFileShortcodeLabel(label string) string {
	label = strings.ReplaceAll(label, "]", "）")
	label = strings.TrimSpace(label)
	if label == "" {
		return "file"
	}
	return label
}

func pastedExistingFilePaths(content string) []string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return nil
	}

	if p, ok := normalizePastedPath(trimmed); ok {
		return []string{p}
	}

	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return nil
	}
	paths := make([]string, 0, len(fields))
	for _, f := range fields {
		p, ok := normalizePastedPath(f)
		if !ok {
			return nil
		}
		paths = append(paths, p)
	}
	return paths
}

func normalizePastedPath(s string) (string, bool) {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "\r\n")
	if len(s) >= 2 {
		if (s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '"' && s[len(s)-1] == '"') {
			s = s[1 : len(s)-1]
		}
	}
	if strings.HasPrefix(strings.ToLower(s), "file://") {
		if u, err := url.Parse(s); err == nil {
			s = u.Path
			if s == "" && u.Host != "" {
				s = u.Host
			}
		}
	}
	s = unescapeDraggedPath(s)
	if info, err := os.Stat(s); err == nil && !info.IsDir() {
		if abs, err := filepath.Abs(s); err == nil {
			return abs, true
		}
		return s, true
	}
	return "", false
}

func unescapeDraggedPath(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	escaped := false
	for _, r := range s {
		if escaped {
			b.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		b.WriteRune(r)
	}
	if escaped {
		b.WriteRune('\\')
	}
	return b.String()
}

func displayPathForShortcode(path, workDir string) string {
	absPath, err := filepath.Abs(path)
	if err != nil {
		absPath = path
	}
	if workDir != "" {
		if absWorkDir, err := filepath.Abs(workDir); err == nil {
			if rel, err := filepath.Rel(absWorkDir, absPath); err == nil && rel != "." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".." {
				return filepath.ToSlash(rel)
			}
		}
	}
	return filepath.ToSlash(absPath)
}

func escapeAtPath(path string) string {
	var b strings.Builder
	b.Grow(len(path))
	for _, r := range path {
		switch r {
		case ' ', '\t', '\n', '\r', '\\':
			b.WriteRune('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func unescapeAtPath(path string) string { return unescapeDraggedPath(path) }

func (m model) updateSlashPopupState() (model, tea.Cmd) {
	value := m.input.Value()
	if m.showPicker || m.showConnect || m.showFileSearch {
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
		m.pushSlashPopupModal()
		if m.slashPopupIndex >= len(m.slashPopupItems) {
			m.slashPopupIndex = 0
		}
		return m, cmd
	}
	if !strings.HasPrefix(value, "/") || strings.Contains(value, " ") {
		m.closeSlashPopup()
		return m, nil
	}

	// Avoid triggering slash-command popup when a file path is dragged into
	// the input (e.g. "/path/to/file.png" pasted into an empty field).
	if looksLikeFilePath(value) {
		m.closeSlashPopup()
		return m, nil
	}

	m.slashPopupItems = slashSuggestions(value)
	m.showSlashPopup = true
	m.pushSlashPopupModal()
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
			items = append(items, slashSuggestion{name: "@" + escapeAtPath(clean), display: "@" + clean, desc: desc})
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

func (m *model) acceptPopupSuggestion(selected slashSuggestion) tea.Cmd {
	m.closeSlashPopup()
	if strings.HasPrefix(selected.name, "@") {
		value := m.input.Value()
		idx := strings.LastIndex(value, "@")
		if idx == -1 {
			m.input.SetValue(selected.name + " ")
			return nil
		}
		m.input.SetValue(value[:idx] + selected.name + " ")
		return nil
	}
	m.input.SetValue(selected.name + " ")
	if selected.name == "/models" {
		m.openModelPicker()
	} else if selected.name == "/session" {
		return m.openSessionPicker()
	} else if selected.name == "/themes" {
		m.openThemePicker()
	} else if selected.name == "/small-model" {
		m.openSmallModelPicker()
	} else if selected.name == "/advisor" {
		m.openAdvisorPicker()
	}
	return nil
}

func (m model) renderSlashPopup() string {
	nameStyle := lipgloss.NewStyle().Bold(true)
	descStyle := m.styles.Hint

	items := m.slashPopupItems
	start, end := m.slashPopupVisibleRange()

	var body strings.Builder

	// Show the active @query so the user can see what they're filtering by.
	if token, ok := activeAtToken(m.input.Value()); ok {
		query := token
		if query == "" {
			query = "…"
		}
		body.WriteString(m.styles.Hint.Render("@ filter: ") + nameStyle.Render(query))
		body.WriteByte('\n')
	}

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

	visibleCount := end - start
	if visibleCount < 1 {
		visibleCount = 1
	}
	sb := renderListScrollbar(visibleCount, len(items), start, visibleCount)
	bodyLines := strings.Split(strings.TrimRight(body.String(), "\n"), "\n")
	sbLines := strings.Split(sb, "\n")
	for i, bLine := range bodyLines {
		sbCol := scrollbarTrackStyle.Render(scrollbarTrack)
		if i < len(sbLines) {
			sbCol = sbLines[i]
		}
		bodyLines[i] = bLine + sbCol
	}
	return borderStyle.Width(width).Render(strings.Join(bodyLines, "\n"))
}
