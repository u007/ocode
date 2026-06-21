package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/discovery"
	"github.com/u007/ocode/internal/session"
)

func (m *model) openAdvisorPicker() {
	// Reuse the model picker listing with kind="advisor" so picker selection
	// saves the advisor model instead of switching the active model.
	m.openModelPicker()
	m.pickerKind = "advisor"
	m.prependClaudeCodeSection()
}

// prependClaudeCodeSection inserts the "Claude Code (Read-Only CLI)" section at
// the TOP of the model picker for the advisor kind. It is prepended (not
// appended) because the provider list holds thousands of models — appended, the
// section sits below all of them and is unreachable by scrolling. Called from
// openAdvisorPicker (initial open) and refreshModelPickerItems (refresh).
func (m *model) prependClaudeCodeSection() {
	claudeCodeModels := []string{
		"claude-sonnet-4-6",
		"claude-opus-4-8",
		"claude-opus-4-7",
		"claude-haiku-4-5",
		"claude-fable-5",
	}
	items := []string{"⚡ Claude Code (Read-Only CLI)"}
	values := []string{""}
	isHeader := []bool{true}
	for _, model := range claudeCodeModels {
		value := "claude-code/" + model
		items = append(items, "  ⚡ "+value)
		values = append(values, value)
		isHeader = append(isHeader, false)
	}
	items = append(items, "") // blank separator below the section
	values = append(values, "")
	isHeader = append(isHeader, true)

	m.pickerItems = append(items, m.pickerItems...)
	m.pickerValues = append(values, m.pickerValues...)
	m.pickerIsHeader = append(isHeader, m.pickerIsHeader...)
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

// refreshModelPickerItems repopulates the current model-family picker
// (kind = "model" | "advisor" | "permission-model" | "small-model" | "redaction-model") from the latest registry
// data, preserving the user's filter text, pending filter text, and selection
// index. Used after operations that may change the available models without
// closing the picker — e.g. toggling a favorite, or a force refresh of the
// models.dev cache.
func (m *model) refreshModelPickerItems() {
	kind := m.pickerKind
	filterPending := m.pickerFilterPending
	filter := m.pickerFilter
	idx := m.pickerIndex

	m.openModelPicker()

	m.pickerKind = kind
	m.pickerFilterPending = filterPending
	m.pickerFilter = filter
	m.pickerIndex = idx
	if kind == "permission-model" {
		m.prependPermissionModelClearOption()
	}
	if kind == "small-model" {
		m.prependSmallModelClearOption()
	}
	if kind == "advisor" {
		m.prependClaudeCodeSection()
	}
}

// refreshModelsCacheCmd returns a tea.Cmd that force-refreshes the
// models.dev registry (and the local LM Studio live list) on a background
// goroutine, then sends a modelsRefreshedMsg back to the Update loop. The
// blocking HTTP calls are deliberately off the UI goroutine; the result is
// reported asynchronously. The caller should set m.pickerRefreshing = true
// before returning this cmd and reset it in the message handler.
func refreshModelsCacheCmd() tea.Cmd {
	return func() tea.Msg {
		_, err := agent.ForceRefreshRegistry()
		// Always re-fetch LM Studio's live list too — the picker combines
		// the static registry with the local API, and the user pressing
		// "refresh" expects both to be re-checked.
		_ = agent.FetchLMStudioModels()
		return modelsRefreshedMsg{err: err}
	}
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
	m.pushPickerModal()
}

func splitPickerModel(s string) (string, string) {
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			return s[:i], s[i+1:]
		}
	}
	return "", s
}

// previewPickerTheme applies the currently-highlighted theme in the picker
// as a live preview. Called on up/down navigation and filter changes.
func (m *model) previewPickerTheme() {
	if m.config == nil {
		return
	}
	_, values := m.pickerVisibleItems()
	if m.pickerIndex >= 0 && m.pickerIndex < len(values) {
		name := values[m.pickerIndex]
		if name != "" {
			if _, ok := GetTheme(name); ok {
				m.config.Ocode.TUI.Theme = name
				m.applyTheme()
			}
		}
	}
}

func (m *model) openThemePicker() {
	// Save current theme so we can revert if the user cancels
	if m.config != nil && m.config.Ocode.TUI.Theme != "" {
		m.pickerSavedTheme = m.config.Ocode.TUI.Theme
	} else {
		m.pickerSavedTheme = "tokyonight"
	}
	m.input.Blur()
	items := AvailableThemes()
	m.pickerKind = "theme"
	m.pickerItems = items
	m.pickerValues = items
	m.pickerIsHeader = nil
	m.pickerIndex = 0
	m.pickerFilter = ""
	m.showPicker = true
	m.pushPickerModal()
	// Preview the first theme immediately
	if len(items) > 0 && m.config != nil {
		m.config.Ocode.TUI.Theme = items[0]
		m.applyTheme()
	}
}

const sessionPickerPageSize = 20

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

func loadSessionRefsCmd(seq int, limit, offset int) tea.Cmd {
	return func() tea.Msg {
		refs, total, err := session.ListRefsPaginated(limit, offset)
		return sessionRefsLoadedMsg{seq: seq, refs: refs, total: total, err: err}
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
	m.pushPickerModal()

	// Load first page of sessions (progressive loading)
	return loadSessionRefsCmd(seq, sessionPickerPageSize, 0)
}

// loadMoreSessions loads the next page of sessions from disk and appends to the picker.
func (m *model) loadMoreSessions() tea.Cmd {
	if !m.pickerSessionMore {
		return nil
	}
	m.pickerSessionLoading = true
	m.pickerSessionLoadSeq++
	seq := m.pickerSessionLoadSeq
	offset := m.pickerSessionPage * sessionPickerPageSize
	return loadSessionRefsCmd(seq, sessionPickerPageSize, offset)
}

// appendSessionRefs appends newly loaded session refs and rebuilds picker items.
func (m *model) appendSessionRefs(newRefs []session.Ref, total int) {
	m.pickerSessionRefs = append(m.pickerSessionRefs, newRefs...)
	m.pickerSessionTotal = total
	m.pickerSessionPage++
	m.rebuildSessionPickerItems()
	m.pickerSessionMore = len(m.pickerSessionRefs) < m.pickerSessionTotal
}

// loadAllSessions loads all sessions into the picker (used when filtering).
// Returns a tea.Cmd to fetch all remaining sessions from disk.
func (m *model) loadAllSessions() tea.Cmd {
	if !m.pickerSessionMore {
		return nil
	}
	m.pickerSessionLoading = true
	m.pickerSessionLoadSeq++
	seq := m.pickerSessionLoadSeq
	offset := len(m.pickerSessionRefs)
	// Load all remaining sessions in one batch
	remaining := m.pickerSessionTotal - offset
	return loadSessionRefsCmd(seq, remaining, offset)
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
	m.pushPickerModal()
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
	m.pushPickerModal()
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
	m.pushPickerModal()
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
	// If the theme picker is cancelled (Esc/click outside, not Enter), revert
	// to the theme that was active when the picker was opened. On Enter,
	// selectPickerIndex closes the picker first, then /themes sets the new
	// theme permanently — the revert is immediately overwritten.
	if m.pickerKind == "theme" && m.pickerSavedTheme != "" && m.config != nil {
		m.config.Ocode.TUI.Theme = m.pickerSavedTheme
		m.applyTheme()
	}
	m.pickerSavedTheme = ""
	m.showPicker = false
	m.popPickerModal()
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
// We require a score >= 100_000 (the multi-token tier floor) to reject the
// subsequence fallback (tier 5, starting at 10_000). With 5 000+ provider
// models, subsequence matching produces hundreds of false positives because
// individual characters of the query (e.g. "claude") appear scattered across
// unrelated model IDs, making provider sections appear unfiltered.
func modelPickerMatches(lower, filter string) bool {
	keywords := modelPickerKeywords(filter)
	if len(keywords) == 0 {
		return true
	}
	for _, kw := range keywords {
		if fuzzyScore(lower, strings.ToLower(kw)) < 100_000 {
			return false
		}
	}
	return true
}

func (m model) pickerVisibleItems() ([]string, []string) {
	if (m.pickerKind == "model" || m.pickerKind == "advisor" || m.pickerKind == "small-model" || m.pickerKind == "permission-model" || m.pickerKind == "redaction-model" || m.pickerKind == "embedding-model") && m.pickerFilter != "" {
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
	isFiltered := (m.pickerKind == "model" || m.pickerKind == "advisor" || m.pickerKind == "permission-model" || m.pickerKind == "small-model" || m.pickerKind == "redaction-model" || m.pickerKind == "embedding-model") && m.pickerFilter != ""
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
	isFiltered := (m.pickerKind == "model" || m.pickerKind == "advisor" || m.pickerKind == "permission-model" || m.pickerKind == "small-model" || m.pickerKind == "redaction-model" || m.pickerKind == "embedding-model") && m.pickerFilter != ""
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
	if kind == "redaction-model" {
		return m.handleCommand("/mask model " + selected)
	}
	if kind == "small-model" {
		return m.handleCommand("/small-model " + selected)
	}
	if kind == "embedding-model" {
		return m.handleCommand("/discover model " + selected)
	}
	return m.handleCommand("/models " + selected)
}

func (m model) renderPicker() string {
	hintLine := hintStyle.Render("↑/↓ select · Enter confirm · Esc cancel · type to filter")
	if m.pickerKind == "model" || m.pickerKind == "permission-model" || m.pickerKind == "small-model" || m.pickerKind == "redaction-model" || m.pickerKind == "embedding-model" {
		hintLine = hintStyle.Render("↑/↓ select · Enter confirm · ctrl+f favorite · ctrl+r refresh · Esc cancel · type to filter")
	} else if m.pickerKind == "advisor" {
		hintLine = hintStyle.Render("↑/↓ select · Enter confirm · ctrl+r refresh · Esc cancel · type to filter")
	} else if m.pickerKind == "session" {
		hintLine = hintStyle.Render("↑/↓ select · Enter confirm · ctrl+d delete · Esc cancel · type to filter")
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
	if m.pickerKind == "small-model" {
		title = "Select small model"
	}
	if m.pickerKind == "redaction-model" {
		title = "Select tier-2 scanning model"
	}
	if m.pickerKind == "advisor" {
		title = "Select advisor model"
	}
	if m.pickerKind == "embedding-model" {
		title = "Select query-embedding model"
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
		isFiltered := (m.pickerKind == "model" || m.pickerKind == "advisor" || m.pickerKind == "permission-model" || m.pickerKind == "small-model" || m.pickerKind == "redaction-model" || m.pickerKind == "embedding-model") && m.pickerFilter != ""
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
	if m.pickerKind == "session" && m.sessionDeleteConfirm {
		return borderStyle.Width(width).Render(m.renderSessionDeleteConfirmDialog(width))
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
	inner := header + "\n\n" + strings.Join(bodyLines, "\n")
	if m.pickerKind == "theme" {
		inner += "\n\n" + m.renderThemePreview(width)
	}
	inner += "\n" + hintStr
	return borderStyle.Width(width).Render(inner)
}

// renderThemePreview renders a compact chat-tab mockup using the current theme
// styles, shown at the bottom of the theme picker so the user can see how the
// selected theme looks on the actual chat UI.
func (m model) renderThemePreview(width int) string {
	// Clamp the preview to a reasonable size.
	pw := width - 6 // leave margin on each side
	if pw < 30 {
		pw = 30
	}
	if pw > 72 {
		pw = 72
	}

	// Build the preview pieces using the live theme styles.
	headerLine := m.styles.Header.Render("◆ ocode") + "  " + m.styles.Hint.Render("/theme")

	userLabel := m.styles.User.Render("You")
	userMsg := m.styles.Text.Inline(true).Render(" what's new?")
	userBubble := borderStyle.Copy().
		BorderForeground(m.styles.User.GetForeground()).
		Width(pw - 2).
		Render(userLabel + " " + userMsg)

	asstLabel := m.styles.Assistant.Render("Assistant")
	asstMsg := m.styles.Text.Inline(true).Render(" I'm here to help!")
	asstBubble := lipgloss.NewStyle().
		Width(pw-2).
		Padding(0, 1).
		Render(asstLabel + " " + asstMsg)

	inputPlaceholder := m.styles.Hint.Render(" Type a message…")
	inputPreview := m.styles.Border.Copy().
		Width(pw - 2).
		Render(inputPlaceholder)

	// Status bar with auto-width — it should stretch to the full preview width.
	statusStyle := m.styles.Status.Copy().Width(pw)

	var preview strings.Builder
	preview.WriteString(headerLine)
	preview.WriteString("\n\n")
	preview.WriteString(userBubble)
	preview.WriteString("\n")
	preview.WriteString(asstBubble)
	preview.WriteString("\n")
	preview.WriteString(inputPreview)
	preview.WriteString("\n")
	preview.WriteString(statusStyle.Render(" build · idle   "))

	return m.styles.Dim.Render("Preview:") + "\n" + preview.String()
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

func (m *model) openRedactionModelPicker() {
	// Reuse the model picker listing with kind="redaction-model" so picker
	// selection saves the tier-2 redaction scanning model instead of the active model.
	m.openModelPicker()
	m.pickerKind = "redaction-model"
}

func (m *model) openEmbeddingModelPicker() {
	m.input.Blur()
	var items, values []string
	var isHeader []bool
	appendH := func(l string) {
		items = append(items, l)
		values = append(values, "")
		isHeader = append(isHeader, true)
	}
	appendM := func(l, v string) {
		items = append(items, l)
		values = append(values, v)
		isHeader = append(isHeader, false)
	}

	appendH("HTTP embedding models")
	for _, em := range discovery.HTTPModels { // sorted by ID in the registry
		appendM("  "+em.ID, em.ID)
	}
	// The local backend's model id is whatever the current platform's manifest
	// pins (e.g. "local/bge-m3"). We don't hardcode a model name in the picker
	// — that would silently drift from the manifest when artifacts are bumped.
	// If the host has no local manifest, skip the section entirely so the
	// picker doesn't show a model that will fail with "not supported on this
	// platform" at spawn time.
	if man, ok := discovery.CurrentManifest(); ok {
		appendH("Local (downloaded on first use)")
		localLabel := "  " + man.ModelID
		if m.config != nil {
			if st := m.config.Ocode.Discovery.LocalModelStatus; st != "" && st != "none" {
				localLabel += " (" + st + ")"
			}
		}
		appendM(localLabel, man.ModelID)
	}

	m.pickerKind = "embedding-model"
	m.pickerItems = items
	m.pickerValues = values
	m.pickerIsHeader = isHeader
	m.pickerIndex = 0
	m.pickerFilter = ""
	m.pickerFilterPending = ""
	m.showPicker = true
}

func (m *model) openSmallModelPicker() {
	// Reuse the model picker listing with kind="small-model" so picker
	// selection saves the small model instead of switching the active model.
	m.openModelPicker()
	m.pickerKind = "small-model"
	m.prependSmallModelClearOption()
}

func (m *model) prependSmallModelClearOption() {
	if m.pickerKind != "small-model" {
		return
	}
	m.pickerItems = append([]string{"  auto (resolve from priority list)"}, m.pickerItems...)
	m.pickerValues = append([]string{"auto"}, m.pickerValues...)
	m.pickerIsHeader = append([]bool{false}, m.pickerIsHeader...)
}
