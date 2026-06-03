package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/jamesmercstudio/ocode/internal/agent"
	"github.com/jamesmercstudio/ocode/internal/config"
	"github.com/jamesmercstudio/ocode/internal/session"
)

func (m *model) openAdvisorPicker() {
	// Reuse the model picker listing with kind="advisor" so picker selection
	// saves the advisor model instead of switching the active model.
	m.openModelPicker()
	m.pickerKind = "advisor"
}

func (m *model) openPermissionModelPicker() {
	// Reuse the model picker listing with kind="permission-model" so picker
	// selection saves the auto-permission model instead of switching the
	// active model.
	m.openModelPicker()
	m.pickerKind = "permission-model"
	m.prependPermissionModelClearOption()
}

func (m *model) prependPermissionModelClearOption() {
	if m.pickerKind != "permission-model" {
		return
	}
	m.pickerItems = append([]string{"(not set)"}, m.pickerItems...)
	m.pickerValues = append([]string{"auto"}, m.pickerValues...)
	m.pickerIsHeader = append([]bool{false}, m.pickerIsHeader...)
}

func (m *model) openModelPicker() {
	m.input.Blur()
	lmsResult := agent.FetchLMStudioModels()
	allModels := agent.AllProviderModels()
	favorites := config.LoadFavorites()
	recents := config.LoadRecentModels()

	shown := make(map[string]bool)
	var items, values []string
	var isHeader []bool

	appendHeader := func(label string) {
		items = append(items, label)
		values = append(values, "")
		isHeader = append(isHeader, true)
	}
	appendModel := func(label, value string) {
		items = append(items, label)
		values = append(values, value)
		isHeader = append(isHeader, false)
		shown[value] = true
	}

	if len(favorites) > 0 {
		appendHeader("★ Favorites")
		for _, f := range favorites {
			appendModel("  ★ "+f, f)
		}
		appendHeader("")
	}

	var recentModels []string
	for _, r := range recents {
		if !shown[r] {
			recentModels = append(recentModels, r)
		}
	}
	if len(recentModels) > 0 {
		appendHeader("Recently Used")
		for _, r := range recentModels {
			appendModel("  "+r, r)
		}
		appendHeader("")
	}

	providerMap := make(map[string][]string)
	for _, modelID := range allModels {
		if shown[modelID] {
			continue
		}
		provider, model := splitPickerModel(modelID)
		providerMap[provider] = append(providerMap[provider], model)
	}
	providers := make([]string, 0, len(providerMap))
	for provider := range providerMap {
		providers = append(providers, provider)
	}
	sort.Strings(providers)
	for _, provider := range providers {
		appendHeader(provider)
		models := providerMap[provider]
		sort.Strings(models)
		for _, model := range models {
			appendModel("  "+provider+"/"+model, provider+"/"+model)
		}
	}
	if lmsResult.NeedsAPIKey && len(providerMap["lmstudio"]) == 0 {
		appendHeader("lmstudio")
		items = append(items, "  ⚠ API key required — set LMSTUDIO_API_KEY")
		values = append(values, "")
		isHeader = append(isHeader, true)
	}

	m.pickerKind = "model"
	m.pickerItems = items
	m.pickerValues = values
	m.pickerIsHeader = isHeader
	m.pickerIndex = 0
	m.pickerFilter = ""
	m.pickerFilterPending = ""
	m.pickerFilterSeq++
	m.showPicker = true
}


func splitPickerModel(s string) (string, string) {
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			return s[:i], s[i+1:]
		}
	}
	return "", s
}

func (m *model) openThemePicker() {
	m.input.Blur()
	items := AvailableThemes()
	m.pickerKind = "theme"
	m.pickerItems = items
	m.pickerValues = items
	m.pickerIsHeader = nil
	m.pickerIndex = 0
	m.pickerFilter = ""
	m.showPicker = true
}

const sessionPickerPageSize = 50

// formatPickerSession builds the display string for a session ref.
func formatPickerSession(s session.Ref) string {
	title := s.Title
	if title == "" {
		title = "(no title)"
	}
	marker := "[ocode]"
	if s.Source == session.SourceClaude {
		marker = "[claude]"
	}
	return fmt.Sprintf("%s %s  %s", marker, s.ID, title)
}

// rebuildSessionPickerItems rebuilds pickerItems/pickerValues from pickerSessionRefs
// up to the current page.
func (m *model) rebuildSessionPickerItems() {
	total := len(m.pickerSessionRefs)
	pageEnd := m.pickerSessionPage * sessionPickerPageSize
	if pageEnd > total {
		pageEnd = total
	}
	items := make([]string, 0, pageEnd)
	values := make([]string, 0, pageEnd)
	for i := 0; i < pageEnd; i++ {
		items = append(items, formatPickerSession(m.pickerSessionRefs[i]))
		values = append(values, m.pickerSessionRefs[i].ID)
	}
	m.pickerItems = items
	m.pickerValues = values
}

func loadSessionRefsCmd(seq int) tea.Cmd {
	return func() tea.Msg {
		refs, err := session.ListRefs()
		return sessionRefsLoadedMsg{seq: seq, refs: refs, err: err}
	}
}

func (m *model) openSessionPicker() tea.Cmd {
	m.input.Blur()
	m.pickerSessionLoadSeq++
	seq := m.pickerSessionLoadSeq
	m.pickerSessionLoading = true
	m.pickerSessionLoadErr = ""
	m.pickerSessionRefs = nil
	m.pickerSessionPage = 0
	m.pickerSessionTotal = 0
	m.pickerSessionMore = false
	m.pickerItems = nil
	m.pickerValues = nil
	m.pickerIsHeader = nil
	m.pickerIndex = 0

	m.pickerKind = "session"
	m.pickerFilter = ""
	m.pickerFilterPending = ""
	m.showPicker = true

	return loadSessionRefsCmd(seq)
}

// loadMoreSessions loads the next page of sessions into the picker items.
func (m *model) loadMoreSessions() {
	if !m.pickerSessionMore {
		return
	}
	m.pickerSessionPage++
	m.rebuildSessionPickerItems()
	m.pickerSessionMore = m.pickerSessionPage*sessionPickerPageSize < len(m.pickerSessionRefs)
}

// loadAllSessions loads all sessions into the picker (used when filtering).
func (m *model) loadAllSessions() {
	if !m.pickerSessionMore {
		return
	}
	m.pickerSessionPage = (len(m.pickerSessionRefs) + sessionPickerPageSize - 1) / sessionPickerPageSize
	m.rebuildSessionPickerItems()
	m.pickerSessionMore = false
}

// pickerScrollPercent returns how far through the items the cursor is (0.0 - 1.0).
func (m model) pickerScrollPercent() float64 {
	items, _ := m.pickerVisibleItems()
	if len(items) <= 1 {
		return 1.0
	}
	return float64(m.pickerIndex) / float64(len(items)-1)
}

func (m *model) openEditorPicker() {
	m.input.Blur()
	items := []string{"nvim", "vim", "nano", "code --wait", "cursor --wait"}
	m.pickerKind = "editor"
	m.pickerItems = items
	m.pickerValues = items
	m.pickerIsHeader = nil
	m.pickerIndex = 0
	m.pickerFilter = ""
	m.showPicker = true
}

func (m *model) openEditorModePicker() {
	m.input.Blur()
	items := []string{config.EditorModeExternal, config.EditorModeTmuxSplit, config.EditorModeTmuxWindow}
	m.pickerKind = "editor-mode"
	m.pickerItems = items
	m.pickerValues = items
	m.pickerIsHeader = nil
	m.pickerIndex = 0
	m.pickerFilter = ""
	m.showPicker = true
}

func (m *model) openMessagePicker() {
	m.input.Blur()
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
	m.pickerIsHeader = nil
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
	m.pickerIsHeader = nil
	m.pickerIndex = 0
	m.pickerFilter = ""
	m.pickerFilterPending = ""
	m.pickerFilterSeq++
	m.pickerSessionRefs = nil
	m.pickerSessionPage = 0
	m.pickerSessionTotal = 0
	m.pickerSessionMore = false
	m.pickerSessionLoading = false
	m.pickerSessionLoadErr = ""
	m.pickerSessionLoadSeq++
	m.input.Focus()
}

// modelPickerKeywords splits a model picker query into keywords, treating
// whitespace and dashes as separators so e.g. "gpt 4o" and "gpt-4o" both
// produce ["gpt", "4o"].
func modelPickerKeywords(query string) []string {
	return strings.FieldsFunc(query, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '-' || r == '_'
	})
}

// modelPickerMatches reports whether `candidate` matches the user's filter
// query using keyword-based AND-fuzzy matching: every whitespace/dash
// separated keyword in the query must fuzzy-match the candidate.
// `candidate` must already be lower-cased; the helper does not normalize it.
func modelPickerMatches(lower, filter string) bool {
	keywords := modelPickerKeywords(filter)
	if len(keywords) == 0 {
		return true
	}
	for _, kw := range keywords {
		if fuzzyScore(lower, strings.ToLower(kw)) == 0 {
			return false
		}
	}
	return true
}

func (m model) pickerVisibleItems() ([]string, []string) {
	if m.pickerKind == "model" && m.pickerFilter != "" {
		items := make([]string, 0, len(m.pickerItems))
		values := make([]string, 0, len(m.pickerValues))
		for i, item := range m.pickerItems {
			if i < len(m.pickerIsHeader) && m.pickerIsHeader[i] {
				header := item
				sectionItems := []string{}
				sectionValues := []string{}
				for j := i + 1; j < len(m.pickerItems); j++ {
					if j < len(m.pickerIsHeader) && m.pickerIsHeader[j] {
						break
					}
					value := ""
					if j < len(m.pickerValues) {
						value = m.pickerValues[j]
					}
					candidate := strings.ToLower(m.pickerItems[j])
					if value != "" {
						candidate += " " + strings.ToLower(value)
					}
					if modelPickerMatches(candidate, m.pickerFilter) {
						sectionItems = append(sectionItems, m.pickerItems[j])
						sectionValues = append(sectionValues, value)
					}
				}
				if len(sectionItems) > 0 {
					items = append(items, header)
					values = append(values, "")
					items = append(items, sectionItems...)
					values = append(values, sectionValues...)
				}
			}
		}
		return items, values
	}

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
	row := start + idx
	isFiltered := (m.pickerKind == "model" || m.pickerKind == "permission-model") && m.pickerFilter != ""
	if !isFiltered && row < len(m.pickerIsHeader) && m.pickerIsHeader[row] {
		return 0, false
	}
	return row, true
}

func (m model) selectPickerIndex(index int) (tea.Model, tea.Cmd) {
	items, values := m.pickerVisibleItems()
	if len(items) == 0 || index < 0 || index >= len(items) {
		if m.pickerKind == "session" && m.pickerSessionLoading {
			return m, nil
		}
		m.closePicker()
		return m, nil
	}
	isFiltered := (m.pickerKind == "model" || m.pickerKind == "permission-model") && m.pickerFilter != ""
	if !isFiltered && index < len(m.pickerIsHeader) && m.pickerIsHeader[index] {
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
	if kind == "editor" {
		return m.handleCommand("/editor " + selected)
	}
	if kind == "editor-mode" {
		return m.handleCommand("/editor-mode " + selected)
	}
	if kind == "advisor" {
		return m.handleCommand("/advisor " + selected)
	}
	if kind == "permission-model" {
		if selected == "auto" {
			return m.handleCommand("/permissions model auto")
		}
		return m.handleCommand("/permissions model " + selected)
	}
	return m.handleCommand("/models " + selected)
}

func (m model) renderPicker() string {
	hintLine := hintStyle.Render("↑/↓ select · Enter confirm · Esc cancel · type to filter")
	if m.pickerKind == "model" || m.pickerKind == "permission-model" {
		hintLine = hintStyle.Render("↑/↓ select · Enter confirm · f favorite · Esc cancel · type to filter")
	}

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
	if m.pickerKind == "editor" {
		title = "Select editor"
	}
	if m.pickerKind == "editor-mode" {
		title = "Select editor mode"
	}
	if m.pickerKind == "permission-model" {
		title = "Select permission model"
	}
	header := m.styles.Header.Render(title) + "  " + hintStyle.Render("filter: "+m.pickerFilterPending+"_")

	items, _ := m.pickerVisibleItems()
	var body strings.Builder
	if len(items) == 0 {
		empty := "(no models — check provider auth or network)"
		if m.pickerKind == "session" {
			if m.pickerSessionLoading {
				empty = "(loading sessions…)"
			} else if m.pickerSessionLoadErr != "" {
				empty = "(failed to load sessions: " + m.pickerSessionLoadErr + ")"
			} else {
				empty = "(no sessions)"
			}
		}
		if m.pickerKind == "message" {
			empty = "(no user messages)"
		}
		if m.pickerKind == "theme" {
			empty = "(no themes)"
		}
		if m.pickerKind == "editor" {
			empty = "(no editors available)"
		}
		if m.pickerKind == "editor-mode" {
			empty = "(no editor modes available)"
		}
		body.WriteString(hintStyle.Render(empty))
	} else {
		isFiltered := (m.pickerKind == "model" || m.pickerKind == "permission-model") && m.pickerFilter != ""
		start, end := m.pickerVisibleRange()
		for i := start; i < end; i++ {
			line := items[i]
			isHeader := !isFiltered && i < len(m.pickerIsHeader) && m.pickerIsHeader[i]
			switch {
			case line == "":
				// spacer line
			case isHeader:
				body.WriteString(hintStyle.Render("  " + line))
			case i == m.pickerIndex:
				body.WriteString(m.styles.Selected.Render(" " + line + " "))
			default:
				body.WriteString("  " + line)
			}
			body.WriteString("\n")
		}
		const maxRows = 15
		if len(items) > maxRows {
			if m.pickerKind == "session" && m.pickerSessionMore {
				body.WriteString(hintStyle.Render(fmt.Sprintf("  …%d of %d shown ↓ scroll for more", end-start, m.pickerSessionTotal)))
			} else {
				body.WriteString(hintStyle.Render(fmt.Sprintf("  …%d of %d shown", end-start, len(items))))
			}
		} else if m.pickerKind == "session" && m.pickerSessionMore {
			body.WriteString(hintStyle.Render(fmt.Sprintf("  …%d of %d shown ↓ scroll for more", end-start, m.pickerSessionTotal)))
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
	totalForScrollbar := len(filteredItems)
	if m.pickerKind == "session" && m.pickerSessionTotal > totalForScrollbar {
		totalForScrollbar = m.pickerSessionTotal
	}
	sb := renderListScrollbar(visibleCount, totalForScrollbar, start, visibleCount)
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
	specs := agent.PrimaryAgentSpecs()
	if len(specs) == 0 {
		return
	}
	m.currentAgentIdx = (m.currentAgentIdx + 1) % len(specs)
	spec := specs[m.currentAgentIdx]
	if m.agent != nil {
		m.agent.SetSpec(&spec)
	}
}
