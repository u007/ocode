package tui

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/jamesmercstudio/ocode/internal/agent"
	"github.com/jamesmercstudio/ocode/internal/auth"
	"github.com/jamesmercstudio/ocode/internal/config"
	"github.com/jamesmercstudio/ocode/internal/session"
	"github.com/jamesmercstudio/ocode/internal/snapshot"
	"github.com/jamesmercstudio/ocode/internal/tool"
)

type retryTestClient struct{}

func (retryTestClient) Chat([]agent.Message, []map[string]interface{}) (*agent.Message, error) {
	return nil, context.DeadlineExceeded
}

func (retryTestClient) GetProvider() string { return "test" }

func (retryTestClient) GetModel() string { return "test-model" }

func TestLeaderTimeoutClearsActiveState(t *testing.T) {
	m := model{leaderActive: true, leaderSeq: 1}

	updated, _ := m.Update(leaderTimeoutMsg{seq: 1})
	got := updated.(model)

	if got.leaderActive {
		t.Fatal("expected leader mode to clear after timeout")
	}
}

func TestInitialToolsIncludesList(t *testing.T) {
	m := model{}
	tools := m.getInitialTools()
	for _, tool := range tools {
		if tool.Name() == "list" {
			return
		}
	}

	t.Fatal("expected default tools to include list")
}

func TestFormatReadToolCallHintShowsLineParams(t *testing.T) {
	var tc agent.ToolCall
	tc.Function.Name = "read"
	tc.Function.Arguments = `{"filePath":"/tmp/model.go","offset":400,"limit":51}`

	got := formatToolCallHint(tc)
	want := "📖 read /tmp/model.go offset=400 limit=51"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestShellExecCommandUsesPlatformShell(t *testing.T) {
	cmd := shellExecCommand("echo hello")
	if runtime.GOOS == "windows" {
		if cmd.Path == "" || len(cmd.Args) != 3 || cmd.Args[1] != "/C" || cmd.Args[2] != "echo hello" {
			t.Fatalf("expected cmd /C invocation, got path=%q args=%v", cmd.Path, cmd.Args)
		}
		return
	}
	if cmd.Path == "" || len(cmd.Args) != 3 || cmd.Args[1] != "-c" || cmd.Args[2] != "echo hello" {
		t.Fatalf("expected bash -c invocation, got path=%q args=%v", cmd.Path, cmd.Args)
	}
}

func TestShellFinishedMessageIsRecorded(t *testing.T) {
	m := model{
		input:     textarea.New(),
		viewport:  viewport.New(viewport.WithWidth(80), viewport.WithHeight(20)),
		styles:    ApplyThemeColors("tokyonight"),
		sessionID: "test-shell",
	}

	updated, _ := m.Update(shellFinishedMsg{command: "echo hello"})
	got := updated.(model)
	if len(got.messages) == 0 || !strings.Contains(got.messages[len(got.messages)-1].text, "Shell command finished: echo hello") {
		t.Fatalf("expected shell completion message, got %#v", got.messages)
	}
}

func TestRenderAssistantTextThinkingToggle(t *testing.T) {
	m := model{styles: ApplyThemeColors("tokyonight"), showThinking: true}
	shown := m.renderAssistantText("before <think>hidden</think> after")
	if !strings.Contains(shown, "before ") || !strings.Contains(shown, "hidden") || !strings.Contains(shown, " after") {
		t.Fatalf("expected visible thinking and normal text, got %q", shown)
	}
	if strings.Contains(shown, "<think>") || strings.Contains(shown, "</think>") {
		t.Fatalf("expected thinking tags to be removed, got %q", shown)
	}

	m.showThinking = false
	hidden := m.renderAssistantText("before <think>hidden</think> after")
	if strings.Contains(hidden, "hidden") {
		t.Fatalf("expected thinking content hidden, got %q", hidden)
	}
	if !strings.Contains(hidden, "before ") || !strings.Contains(hidden, " after") {
		t.Fatalf("expected normal text preserved, got %q", hidden)
	}
}

func TestSidebarToggleWithCtrlB(t *testing.T) {
	m := model{input: textarea.New(), viewport: viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))}

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl})
	got := updated.(model)
	if !got.showSidebar {
		t.Fatal("expected Ctrl+B to toggle sidebar on")
	}

	updated, _ = got.Update(tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl})
	got = updated.(model)
	if got.showSidebar {
		t.Fatal("expected Ctrl+B to toggle sidebar off")
	}
}

func TestCtrlOTogglesYoloMode(t *testing.T) {
	m := model{
		input:    textarea.New(),
		viewport: viewport.New(viewport.WithWidth(80), viewport.WithHeight(20)),
		agent:    agent.NewAgent(nil, nil, nil),
	}

	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	if cmd != nil {
		t.Fatalf("expected Ctrl+O to return no command, got %T", cmd)
	}
	got := updated.(*model)
	if got.agent.Permissions().Mode() != agent.PermissionModeYOLO {
		t.Fatalf("expected Ctrl+O to enable YOLO, got %s", got.agent.Permissions().Mode())
	}

	updated, _ = got.Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	got2 := updated.(*model)
	if got2.agent.Permissions().Mode() != agent.PermissionModeNormal {
		t.Fatalf("expected Ctrl+O to disable YOLO, got %s", got2.agent.Permissions().Mode())
	}
}

func TestMCPCmdListsConfiguredServers(t *testing.T) {
	m := model{
		config: &config.Config{MCP: map[string]config.MCPConfig{
			"demo": {Type: "local", Enabled: true},
		}},
		agent: agent.NewAgent(nil, nil, nil),
	}
	m.agent.RestoreMCPToolNames([]string{"demo_search"})

	runMCPCmd(&m, nil)

	if len(m.messages) != 1 {
		t.Fatalf("expected one message, got %d", len(m.messages))
	}
	text := m.messages[0].text
	for _, want := range []string{"MCP servers:", "demo", "enabled", "1 tools"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %q in MCP output, got %q", want, text)
		}
	}
}

func TestCtrlCClearsNonEmptyInputBeforeQuitConfirmation(t *testing.T) {
	m := model{
		input:             textarea.New(),
		viewport:          viewport.New(viewport.WithWidth(80), viewport.WithHeight(20)),
		inputHistoryIndex: 2,
		ctrlCPressed:      true,
		showSlashPopup:    true,
	}
	m.input.SetValue("draft message")

	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd != nil {
		t.Fatalf("expected Ctrl+C with input to return no command, got %T", cmd)
	}
	got := updated.(model)
	if got.input.Value() != "" {
		t.Fatalf("expected Ctrl+C to clear input, got %q", got.input.Value())
	}
	if got.ctrlCPressed {
		t.Fatal("expected Ctrl+C clearing input to reset quit confirmation")
	}
	if got.inputHistoryIndex != -1 {
		t.Fatalf("expected input history index reset, got %d", got.inputHistoryIndex)
	}
	if got.showSlashPopup {
		t.Fatal("expected slash popup to close when input is cleared")
	}
}

func TestSidebarViewUsesSplitLayoutWhenWide(t *testing.T) {
	snapshot.Reset()
	defer snapshot.Reset()

	spend := 0.1234
	m := model{
		ready:       true,
		width:       140,
		height:      40,
		showSidebar: true,
		sessionID:   "session-123",
		input:       textarea.New(),
		viewport:    viewport.New(viewport.WithWidth(100), viewport.WithHeight(20)),
		config:      &config.Config{Model: "gpt-4o"},
		messages: []message{{
			role: roleAssistant,
			text: "hello",
			raw: &agent.Message{
				Role:  "assistant",
				Usage: &agent.TokenUsage{PromptTokens: int64Ptr(1000), CompletionTokens: int64Ptr(2000)},
				Spend: &spend,
			},
		}},
	}

	view := m.View().Content
	for _, want := range []string{"Session", "session-123", "Model", "gpt-4o", "Context", "Spend", "MCP", "LSP", "TODO", "Ctrl+B"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected wide view to include %q, got %q", want, view)
		}
	}
}

func TestSidebarMiddleSectionsCapAtFortyPercent(t *testing.T) {
	m := model{height: 100}
	data := sidebarRenderData{
		topLines:    make([]string, 10),
		bottomLines: make([]string, 2),
	}

	got := m.sidebarScrollBoxHeight(data, 1)
	want := 38 // (height 100 - outer border 2 - header 1) * 40%.
	if got != want {
		t.Fatalf("expected sidebar middle section height %d, got %d", want, got)
	}
}

func TestSidebarViewHidesOnNarrowTerminals(t *testing.T) {
	m := model{
		ready:       true,
		width:       80,
		height:      30,
		showSidebar: true,
		input:       textarea.New(),
		viewport:    viewport.New(viewport.WithWidth(76), viewport.WithHeight(20)),
	}

	view := m.View().Content
	if strings.Contains(view, "No live session todo state yet.") || strings.Contains(view, "Ctrl+B toggle sidebar") {
		t.Fatalf("expected narrow view to hide sidebar, got %q", view)
	}
}

func TestLayoutKeepsInputAndStatusWithinTerminalHeight(t *testing.T) {
	m := model{
		ready:     true,
		width:     80,
		height:    24,
		sessionID: strings.Repeat("session-", 12),
		input:     textarea.New(),
		viewport:  viewport.New(viewport.WithWidth(76), viewport.WithHeight(20)),
		styles:    ApplyThemeColors("tokyonight"),
		messages: []message{{
			role: roleAssistant,
			text: strings.Repeat("long transcript line that should stay in the viewport\n", 80),
		}},
	}
	m.input.SetValue(strings.Repeat("draft input ", 12))

	m.layout()
	content := m.renderContent()

	if got := lipgloss.Height(content); got > m.height {
		t.Fatalf("rendered content height %d exceeds terminal height %d", got, m.height)
	}
	if !strings.Contains(content, "draft input") {
		t.Fatalf("expected input to remain visible, got %q", content)
	}
	if !strings.Contains(content, "Agent:") {
		t.Fatalf("expected status to remain visible, got %q", content)
	}
}

func TestLayoutHeightDoesNotChangeWhenTranscriptScrolls(t *testing.T) {
	m := model{
		ready:     true,
		width:     80,
		height:    24,
		sessionID: strings.Repeat("session-", 12),
		input:     textarea.New(),
		viewport:  viewport.New(viewport.WithWidth(76), viewport.WithHeight(20)),
		styles:    ApplyThemeColors("tokyonight"),
		messages: []message{{
			role: roleAssistant,
			text: strings.Repeat("long transcript line that should stay in the viewport\n", 80),
		}},
	}
	m.input.SetValue("draft input")
	m.layout()

	m.viewport.GotoTop()
	top := m.renderContent()
	m.viewport.GotoBottom()
	bottom := m.renderContent()

	if topHeight, bottomHeight := lipgloss.Height(top), lipgloss.Height(bottom); topHeight != bottomHeight {
		t.Fatalf("expected scroll position not to change layout height, top=%d bottom=%d", topHeight, bottomHeight)
	}
	if got := lipgloss.Height(bottom); got > m.height {
		t.Fatalf("rendered content height %d exceeds terminal height %d", got, m.height)
	}
	for _, content := range []string{top, bottom} {
		if !strings.Contains(content, "draft input") || !strings.Contains(content, "Agent:") {
			t.Fatalf("expected input and status to remain visible, got %q", content)
		}
	}
}

func TestInputNavigationDoesNotScrollTranscript(t *testing.T) {
	m := model{
		input:    textarea.New(),
		viewport: viewport.New(viewport.WithWidth(80), viewport.WithHeight(6)),
		styles:   ApplyThemeColors("tokyonight"),
		messages: []message{{
			role: roleAssistant,
			text: strings.Repeat("long transcript line\n", 40),
		}},
	}
	m.input.SetValue("first line\nsecond line")
	m.renderTranscript()
	m.viewport.GotoBottom()
	offset := m.viewport.YOffset()

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	got := updated.(model)
	if got.viewport.YOffset() != offset {
		t.Fatalf("expected input up key not to scroll transcript, before=%d after=%d", offset, got.viewport.YOffset())
	}

	updated, _ = got.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	got = updated.(model)
	if got.viewport.YOffset() != offset {
		t.Fatalf("expected input down key not to scroll transcript, before=%d after=%d", offset, got.viewport.YOffset())
	}
}

func TestLayoutConstrainsLongTranscriptLines(t *testing.T) {
	m := model{
		ready:     true,
		width:     80,
		height:    24,
		sessionID: "session-1",
		input:     textarea.New(),
		viewport:  viewport.New(viewport.WithWidth(76), viewport.WithHeight(20)),
		styles:    ApplyThemeColors("tokyonight"),
		messages: []message{{
			role: roleAssistant,
			text: "PERMISSION_ASK:" + strings.Repeat(`{"very_long_argument":`, 20),
		}},
	}
	m.input.SetValue("draft input")
	m.layout()
	content := m.renderContent()

	if got := lipgloss.Height(content); got > m.height {
		t.Fatalf("rendered content height %d exceeds terminal height %d", got, m.height)
	}
	for _, line := range strings.Split(content, "\n") {
		if got := lipgloss.Width(line); got > m.width {
			t.Fatalf("rendered line width %d exceeds terminal width %d: %q", got, m.width, line)
		}
	}
	if !strings.Contains(content, "draft input") || !strings.Contains(content, "Agent:") {
		t.Fatalf("expected input and status to remain visible, got %q", content)
	}
}

func TestLayoutWrapsLongInputLines(t *testing.T) {
	m := model{
		ready:     true,
		width:     80,
		height:    24,
		sessionID: "session-1",
		input:     textarea.New(),
		viewport:  viewport.New(viewport.WithWidth(76), viewport.WithHeight(20)),
		styles:    ApplyThemeColors("tokyonight"),
		messages: []message{{
			role: roleAssistant,
			text: "ready",
		}},
	}
	m.input.Prompt = "▍ "
	m.input.SetValue(strings.Repeat("unbroken-input", 20))
	m.layout()
	content := m.renderContent()

	for _, line := range strings.Split(content, "\n") {
		if got := lipgloss.Width(line); got > m.width {
			t.Fatalf("rendered line width %d exceeds terminal width %d: %q", got, m.width, line)
		}
	}
	if got := m.input.MaxWidth; got <= 0 {
		t.Fatalf("expected input max width to be constrained, got %d", got)
	}
}

func TestLayoutAccountsForSlashPopupAndActivityRow(t *testing.T) {
	m := model{
		ready:               true,
		width:               80,
		height:              24,
		sessionID:           strings.Repeat("session-", 12),
		input:               newTestTextarea(),
		viewport:            viewport.New(viewport.WithWidth(76), viewport.WithHeight(20)),
		styles:              ApplyThemeColors("tokyonight"),
		showSlashPopup:      true,
		slashPopupItems:     []slashSuggestion{{name: "/compact", display: "/compact", desc: "Reduce context"}},
		activityRowReserved: true,
		messages: []message{{
			role: roleAssistant,
			text: strings.Repeat("long transcript line that should stay in the viewport\n", 80),
		}},
	}
	m.input.SetValue("/co")

	m.layout()
	content := m.renderContent()

	if got := lipgloss.Height(content); got > m.height {
		t.Fatalf("rendered content height %d exceeds terminal height %d", got, m.height)
	}
	for _, want := range []string{"/compact", "/co", "Agent:"} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected %q to remain visible, got %q", want, content)
		}
	}
}

func TestSidebarViewShowsChangedFilesAndTodoState(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	snapshot.Reset()
	defer snapshot.Reset()
	tool.SetTodoSession("session-1")
	tool.ResetTodoState()

	if err := os.WriteFile("changed.go", []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := snapshot.Backup("changed.go"); err != nil {
		t.Fatal(err)
	}
	if _, err := (tool.TodoWriteTool{}).Execute(mustJSON(t, map[string]string{"todoText": "- [ ] ship task 4"})); err != nil {
		t.Fatal(err)
	}

	m := model{
		ready:         true,
		width:         140,
		height:        40,
		showSidebar:   true,
		sidebarScroll: 2,
		input:         textarea.New(),
		viewport:      viewport.New(viewport.WithWidth(100), viewport.WithHeight(20)),
	}

	view := m.View().Content
	for _, want := range []string{"Files", "changed.go", "TODO", "- [○] ship task 4"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected sidebar to include %q, got %q", want, view)
		}
	}
}

func TestFormatSidebarFilePathStripsProjectPrefix(t *testing.T) {
	tmpDir := t.TempDir()
	absPath := filepath.Join(tmpDir, "internal", "tui", "model.go")

	if got := formatSidebarFilePath(absPath, tmpDir, 80); got != "internal/tui/model.go" {
		t.Fatalf("expected project-relative path, got %q", got)
	}
	if got := formatSidebarFilePath("./internal/tui/model.go", tmpDir, 80); got != "internal/tui/model.go" {
		t.Fatalf("expected ./ prefix stripped, got %q", got)
	}
}

func TestFormatSidebarFilePathTruncatesMiddlePreservingFilename(t *testing.T) {
	path := "very/long/path/to/important.go"
	got := formatSidebarFilePath(path, "", 24)

	if len(got) > 24 {
		t.Fatalf("expected truncated path to fit width, got %q len=%d", got, len(got))
	}
	if !strings.Contains(got, "...") {
		t.Fatalf("expected middle truncation marker, got %q", got)
	}
	if !strings.HasSuffix(got, "important.go") {
		t.Fatalf("expected filename ending to be preserved, got %q", got)
	}

	longFile := "absurdly_long_generated_filename_with_suffix_test.go"
	got = formatSidebarFilePath("nested/"+longFile, "", 24)
	if len(got) > 24 {
		t.Fatalf("expected long filename to fit width, got %q len=%d", got, len(got))
	}
	if !strings.HasSuffix(got, "suffix_test.go") {
		t.Fatalf("expected long filename ending to be prioritized, got %q", got)
	}
}

func TestSidebarFileClickLaunchesEditor(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	snapshot.Reset()
	tool.SetTodoSession("session-1")
	tool.ResetTodoState()

	if err := os.WriteFile("changed.go", []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := snapshot.Backup("changed.go"); err != nil {
		t.Fatal(err)
	}

	prev := openSidebarFileInEditor
	defer func() { openSidebarFileInEditor = prev }()
	var gotPath string
	openSidebarFileInEditor = func(path string) tea.Cmd {
		gotPath = path
		return func() tea.Msg { return nil }
	}

	m := model{ready: true, width: 140, height: 40, showSidebar: true, input: textarea.New(), viewport: viewport.New(viewport.WithWidth(100), viewport.WithHeight(20))}
	updated, cmd := m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 120, Y: 23})
	_ = updated
	if cmd == nil {
		t.Fatal("expected sidebar file click to return editor command")
	}
	cmd()

	if gotPath != "changed.go" {
		t.Fatalf("expected clicked file to open, got %q", gotPath)
	}
}

func TestSidebarContextWindowLookup(t *testing.T) {
	if got, ok := modelContextWindow("gpt-4o"); !ok || got == 0 {
		t.Fatalf("expected known context window for gpt-4o, got %d ok=%v", got, ok)
	}

	if _, ok := modelContextWindow("made-up-model"); ok {
		t.Fatal("expected unknown model to have no context window")
	}
}

func TestHandleModelCmdUpdatesCurrentModel(t *testing.T) {
	m := model{config: &config.Config{Model: "gpt-4o"}}
	m.handleModelCmd([]string{"gpt-4o-mini"})

	if got := m.currentModelName(); got != "gpt-4o-mini" {
		t.Fatalf("expected active model to update, got %q", got)
	}
}

func TestModelPickerShowsFavoritesAndRecentsFirst(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if err := config.SaveRecentModel("openai/gpt-4o-mini"); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveRecentModel("anthropic/claude-sonnet-4-20250514"); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveFavoriteModel("openai/gpt-4o-mini"); err != nil {
		t.Fatal(err)
	}

	m := model{}
	m.openModelPicker()

	if len(m.pickerItems) < 4 {
		t.Fatalf("expected grouped picker items, got %#v", m.pickerItems)
	}
	if m.pickerItems[0] != "★ Favorites" {
		t.Fatalf("expected favorites header first, got %#v", m.pickerItems[:4])
	}
	if !strings.Contains(m.pickerItems[1], "gpt-4o-mini") || m.pickerValues[1] != "openai/gpt-4o-mini" {
		t.Fatalf("expected favorite model first, got items=%#v values=%#v", m.pickerItems[:4], m.pickerValues[:4])
	}
	if !containsString(m.pickerItems, "Recently Used") {
		t.Fatalf("expected recent section, got %#v", m.pickerItems)
	}
	for i, value := range m.pickerValues {
		if value == "openai/gpt-4o-mini" && i != 1 {
			t.Fatalf("favorite should not be duplicated in recents/providers, got duplicate at %d in %#v", i, m.pickerValues)
		}
	}
}

func TestModelPickerToggleFavorite(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	m := model{showPicker: true, pickerKind: "model", pickerItems: []string{"openai/gpt-4o-mini"}, pickerValues: []string{"openai/gpt-4o-mini"}}

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	got := derefTestModel(t, updated)
	if !config.IsFavorite("openai/gpt-4o-mini") {
		t.Fatal("expected f to add selected model to favorites")
	}
	if !got.showPicker || got.pickerKind != "model" {
		t.Fatalf("expected model picker to remain open, got showPicker=%v kind=%q", got.showPicker, got.pickerKind)
	}

	got.pickerIndex = 1
	updated, _ = got.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	_ = updated
	if config.IsFavorite("openai/gpt-4o-mini") {
		t.Fatal("expected f to remove selected model from favorites")
	}
}

func TestModelPickerFilterDebounces(t *testing.T) {
	m := model{showPicker: true, pickerKind: "model", pickerItems: []string{"openai/gpt-4o-mini"}, pickerValues: []string{"openai/gpt-4o-mini"}}

	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	got := derefTestModel(t, updated)

	if cmd == nil {
		t.Fatal("expected debounce cmd after filter keypress, got nil")
	}
	// pending input visible immediately, applied filter deferred
	if got.pickerFilterPending != "g" {
		t.Fatalf("expected pickerFilterPending=%q, got %q", "g", got.pickerFilterPending)
	}
	if got.pickerFilter != "" {
		t.Fatalf("expected pickerFilter to remain empty before debounce fires, got %q", got.pickerFilter)
	}
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func TestPickerSelectsSessionByValue(t *testing.T) {
	m := model{
		input:        textarea.New(),
		viewport:     viewport.New(viewport.WithWidth(80), viewport.WithHeight(20)),
		showPicker:   true,
		pickerKind:   "session",
		pickerItems:  []string{"session-1  First session"},
		pickerValues: []string{"session-1"},
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	got := derefTestModel(t, updated)
	if got.showPicker {
		t.Fatal("expected session picker to close")
	}
	if len(got.messages) == 0 || !strings.Contains(got.messages[len(got.messages)-1].text, "Error loading session") {
		t.Fatalf("expected picker to load selected session id, got %#v", got.messages)
	}
}

func TestMessagePickerOnlyListsActualUserInputs(t *testing.T) {
	m := model{
		messages: []message{
			{
				role: roleUser,
				text: "include raw user",
				raw:  &agent.Message{Role: "user", Content: "include raw user"},
			},
			{
				role: roleUser,
				text: "exclude restored system context",
				raw:  &agent.Message{Role: "system", Content: "exclude restored system context"},
			},
			{
				role: roleAssistant,
				text: "exclude assistant",
				raw:  &agent.Message{Role: "assistant", Content: "exclude assistant"},
			},
			{
				role: roleUser,
				text: "include live user",
			},
		},
	}

	m.openMessagePicker()

	if got, want := len(m.pickerItems), 2; got != want {
		t.Fatalf("expected %d revert items, got %d: %#v", want, got, m.pickerItems)
	}
	if !strings.Contains(m.pickerItems[0], "include raw user") || !strings.Contains(m.pickerItems[1], "include live user") {
		t.Fatalf("expected picker to list user inputs, got %#v", m.pickerItems)
	}
	for _, item := range m.pickerItems {
		if strings.Contains(item, "system") || strings.Contains(item, "assistant") {
			t.Fatalf("expected picker to exclude non-user messages, got %#v", m.pickerItems)
		}
	}
	if got, want := strings.Join(m.pickerValues, ","), "0,3"; got != want {
		t.Fatalf("expected picker values to retain transcript indexes, got %q", got)
	}
}

func TestMessagePickerRestoresBeforeSelectedInputAndPrefillsIt(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(".ocode/sessions", 0755); err != nil {
		t.Fatal(err)
	}

	m := model{
		sessionID: "restore-test",
		input:     textarea.New(),
		viewport:  viewport.New(viewport.WithWidth(80), viewport.WithHeight(20)),
		messages: []message{
			{role: roleUser, text: "first request"},
			{role: roleAssistant, text: "first answer"},
			{role: roleUser, text: "retry this request"},
			{role: roleAssistant, text: "failed answer"},
		},
	}
	m.openMessagePicker()
	m.pickerIndex = 1

	updated, cmd := m.selectPickerIndex(1)
	if cmd != nil {
		t.Fatalf("expected no command, got %T", cmd)
	}
	got := derefTestModel(t, updated)

	if len(got.messages) != 2 {
		t.Fatalf("expected transcript before selected input, got %#v", got.messages)
	}
	if got.input.Value() != "retry this request" {
		t.Fatalf("expected selected input to be prefixed, got %q", got.input.Value())
	}
}

func TestCtrlYRetriesLastRetryableLLMError(t *testing.T) {
	errText := "Error: context deadline exceeded"
	m := model{
		input:               textarea.New(),
		viewport:            viewport.New(viewport.WithWidth(80), viewport.WithHeight(20)),
		agent:               agent.NewAgent(retryTestClient{}, nil, nil),
		lastRetryableLLMErr: errText,
		messages: []message{
			{role: roleUser, text: "please retry"},
			{role: roleAssistant, text: errText},
		},
	}

	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
	got := derefTestModel(t, updated)

	if cmd == nil {
		t.Fatal("expected retry command")
	}
	if got.lastRetryableLLMErr != "" {
		t.Fatalf("expected retryable error state to clear, got %q", got.lastRetryableLLMErr)
	}
	if len(got.messages) != 1 || got.messages[0].text != "please retry" {
		t.Fatalf("expected transient error removed before retry, got %#v", got.messages)
	}
}

func TestMouseWheelScrollsTranscriptOnlyWhenOverMessages(t *testing.T) {
	lines := strings.Repeat("message line\n", 20)
	m := model{
		width:       80,
		height:      24,
		input:       newTestTextarea(),
		viewport:    viewport.New(viewport.WithWidth(40), viewport.WithHeight(3)),
		styles:      ApplyThemeColors("tokyonight"),
		scrollSpeed: 3,
	}
	m.viewport.SetContent(lines)

	updated, _ := m.Update(tea.MouseWheelMsg{X: 2, Y: 2, Button: tea.MouseWheelDown})
	got := derefTestModel(t, updated)
	if got.viewport.YOffset() == 0 {
		t.Fatal("expected mouse wheel over transcript to scroll messages")
	}

	before := got.viewport.YOffset()
	updated, _ = got.Update(tea.MouseWheelMsg{X: 2, Y: 8, Button: tea.MouseWheelDown})
	got = derefTestModel(t, updated)
	if got.viewport.YOffset() != before {
		t.Fatalf("expected wheel outside transcript to leave messages offset at %d, got %d", before, got.viewport.YOffset())
	}
}

func TestGitDiffMouseDragSelectsDiffText(t *testing.T) {
	m := model{
		width:     100,
		height:    30,
		activeTab: tabGit,
		styles:    ApplyThemeColors("tokyonight"),
		git: gitModel{
			diff: viewport.New(viewport.WithWidth(45), viewport.WithHeight(10)),
		},
	}
	m.git.setDiffContent("line one\nline two\nline three")

	panelW := m.panelWidth()
	diffLeft := panelW*20/100 + panelW*30/100 + 1
	gitBodyTop := lipgloss.Height(m.styles.Header.Render("◆ ocode  Git")) + 1
	updated, _, ok := m.handleMouseAction(tea.Mouse{Button: tea.MouseLeft, X: diffLeft, Y: gitBodyTop + 1}, true)
	if !ok {
		t.Fatal("expected mouse press in git diff panel to be handled")
	}
	got := updated.(model)
	updated, _, ok = got.handleMouseMotion(tea.Mouse{Button: tea.MouseLeft, X: diffLeft + 4, Y: gitBodyTop + 1})
	if !ok {
		t.Fatal("expected git diff drag motion to be handled")
	}
	got = updated.(model)

	if !got.gitSel.active || !got.gitSel.dragging {
		t.Fatalf("expected active dragging selection, got %#v", got.gitSel)
	}
	if got.gitSel.startLine != 0 || got.gitSel.endLine != 0 || got.gitSel.startCol != 0 || got.gitSel.endCol != 4 {
		t.Fatalf("unexpected selection state: %#v", got.gitSel)
	}
	if !strings.Contains(got.git.diff.View(), "\x1b[7m") {
		t.Fatalf("expected highlighted diff content, got %q", got.git.diff.View())
	}
}

func TestUpKeyUsesInputHistoryWithoutScrollingTranscript(t *testing.T) {
	m := model{
		input:             newTestTextarea(),
		viewport:          viewport.New(viewport.WithWidth(40), viewport.WithHeight(3)),
		inputHistory:      []string{"first", "second"},
		inputHistoryIndex: -1,
		scrollSpeed:       3,
	}
	m.viewport.SetContent(strings.Repeat("message line\n", 20))
	m.viewport.ScrollDown(6)
	before := m.viewport.YOffset()

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	got := derefTestModel(t, updated)
	if got.input.Value() != "second" {
		t.Fatalf("expected up key to recall latest input history entry, got %q", got.input.Value())
	}
	if got.viewport.YOffset() != before {
		t.Fatalf("expected up key not to scroll transcript, offset changed from %d to %d", before, got.viewport.YOffset())
	}
}

func TestEnterWhileStreamingQueuesUserInput(t *testing.T) {
	m := model{
		ready:     true,
		width:     80,
		height:    24,
		streaming: true,
		input:     newTestTextarea(),
		viewport:  viewport.New(viewport.WithWidth(76), viewport.WithHeight(20)),
		styles:    ApplyThemeColors("tokyonight"),
	}
	m.input.SetValue("follow up after this")
	m.layout()

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	got := derefTestModel(t, updated)

	if cmd != nil {
		t.Fatalf("expected queued input to avoid starting a command, got %T", cmd)
	}
	if len(got.queuedInputs) != 1 || got.queuedInputs[0] != "follow up after this" {
		t.Fatalf("expected input to be queued, got %#v", got.queuedInputs)
	}
	if got.input.Value() != "" {
		t.Fatalf("expected input to reset after queueing, got %q", got.input.Value())
	}
	if len(got.messages) != 0 {
		t.Fatalf("expected queued input not to enter transcript yet, got %#v", got.messages)
	}
	if content := got.renderContent(); !strings.Contains(content, "Queued (1)") || !strings.Contains(content, "follow up after this") {
		t.Fatalf("expected queued input to render, got %q", content)
	}
}

func TestStreamDoneStartsNextQueuedInput(t *testing.T) {
	m := model{
		ready:        true,
		width:        80,
		height:       24,
		streaming:    true,
		agent:        agent.NewAgent(nil, nil, nil),
		input:        newTestTextarea(),
		viewport:     viewport.New(viewport.WithWidth(76), viewport.WithHeight(20)),
		styles:       ApplyThemeColors("tokyonight"),
		queuedInputs: []string{"next request"},
	}
	m.layout()

	updated, cmd := m.Update(streamDoneMsg{})
	got := derefTestModel(t, updated)

	if got.streaming {
		t.Fatal("expected streaming to stop")
	}
	if len(got.queuedInputs) != 0 {
		t.Fatalf("expected queued input to be consumed, got %#v", got.queuedInputs)
	}
	if cmd == nil {
		t.Fatal("expected next queued input command to start")
	}
}

func TestStreamDonePreservesActivityRowAndShowsIdleStatus(t *testing.T) {
	m := model{
		ready:               true,
		width:               80,
		height:              24,
		streaming:           true,
		activityRowReserved: true,
		lastActivity:        agent.ActivitySnapshot{LLMRunning: true},
		input:               newTestTextarea(),
		viewport:            viewport.New(viewport.WithWidth(76), viewport.WithHeight(20)),
		styles:              ApplyThemeColors("tokyonight"),
	}
	m.layout()

	updated, _ := m.Update(streamDoneMsg{})
	got := derefTestModel(t, updated)

	if got.streaming {
		t.Fatal("expected streaming to stop")
	}
	if !got.activityRowReserved {
		t.Fatal("expected activity row reservation to remain after stream done")
	}
	status := got.renderStatus()
	if !strings.Contains(status, "LLM: ○ idle") {
		t.Fatalf("expected idle LLM status, got %q", status)
	}
}

func TestRetryableLLMErrorDetection(t *testing.T) {
	if !isRetryableLLMError(context.DeadlineExceeded) {
		t.Fatal("expected context deadline to be retryable")
	}
	if isRetryableLLMError(os.ErrPermission) {
		t.Fatal("expected permission error to not be retryable")
	}
}

func TestPickerMouseRowUsesVisibleWindow(t *testing.T) {
	m := model{
		showPicker:   true,
		pickerKind:   "model",
		pickerIndex:  16,
		pickerItems:  []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12", "13", "14", "15", "16"},
		pickerValues: []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12", "13", "14", "15", "16"},
	}

	idx, ok := m.pickerRowForY(3)
	if !ok {
		t.Fatal("expected picker row hit")
	}
	if idx != 2 {
		t.Fatalf("expected first visible picker row to map to index 2, got %d", idx)
	}

}

func TestPickerMouseReleaseSelectsModel(t *testing.T) {
	m := model{
		showPicker:   true,
		pickerKind:   "model",
		pickerItems:  []string{"test-model"},
		pickerValues: []string{"test-model"},
		input:        newTestTextarea(),
	}

	updated, _ := m.Update(tea.MouseReleaseMsg{Button: tea.MouseNone, X: 3, Y: 3})
	got := derefTestModel(t, updated)

	if got.showPicker {
		t.Fatal("expected picker to close after model row release")
	}
	if len(got.messages) == 0 || !strings.Contains(got.messages[len(got.messages)-1].text, "test-model") {
		t.Fatalf("expected selected model message, got %#v", got.messages)
	}
}

func TestTabMouseReleaseUsesRightAlignedHeaderPosition(t *testing.T) {
	m := model{
		ready:     true,
		width:     100,
		height:    24,
		activeTab: tabFiles,
		input:     newTestTextarea(),
		viewport:  viewport.New(viewport.WithWidth(96), viewport.WithHeight(20)),
		styles:    ApplyThemeColors("tokyonight"),
	}
	barWidth := lipgloss.Width(renderTabBar(m.activeTab, m.chatUnread))
	barStart := m.panelWidth() - barWidth
	chatWidth := lipgloss.Width(hintStyle.Padding(0, 1).Render("1:chat"))
	filesWidth := lipgloss.Width(hintStyle.Padding(0, 1).Render("2:files"))

	updated, _ := m.Update(tea.MouseReleaseMsg{Button: tea.MouseNone, X: barStart + chatWidth + filesWidth + 1, Y: 0})
	got := derefTestModel(t, updated)

	if got.activeTab != tabGit {
		t.Fatalf("expected git tab after release, got %d", got.activeTab)
	}
}

func TestTabMouseMotionSwitchesWhenTerminalReportsDrag(t *testing.T) {
	m := model{
		ready:     true,
		width:     100,
		height:    24,
		activeTab: tabChat,
		input:     newTestTextarea(),
		viewport:  viewport.New(viewport.WithWidth(96), viewport.WithHeight(20)),
		styles:    ApplyThemeColors("tokyonight"),
	}
	barWidth := lipgloss.Width(renderTabBar(m.activeTab, m.chatUnread))
	barStart := m.tabBarStartXs(barWidth)[0]
	chatWidth := lipgloss.Width(hintStyle.Padding(0, 1).Render("1:chat"))

	updated, _ := m.Update(tea.MouseMotionMsg{Button: tea.MouseLeft, X: barStart + chatWidth + 1, Y: 0})
	got := derefTestModel(t, updated)

	if got.activeTab != tabFiles {
		t.Fatalf("expected files tab after left-button motion, got %d", got.activeTab)
	}
}

func TestMouseModeDefaultsOnWithoutConfig(t *testing.T) {
	m := model{ready: true, input: newTestTextarea()}

	if got := m.View().MouseMode; got != tea.MouseModeCellMotion {
		t.Fatalf("expected default mouse mode on without config, got %v", got)
	}
}

func TestMouseModeCanBeDisabledByConfig(t *testing.T) {
	disabled := false
	m := model{ready: true, input: newTestTextarea(), config: &config.Config{Ocode: &config.OcodeConfig{}}}
	m.config.Ocode.TUI.Mouse = &disabled

	if got := m.View().MouseMode; got != tea.MouseModeNone {
		t.Fatalf("expected mouse mode off when configured false, got %v", got)
	}
}

func TestTuiRoleForAgentMessageOnlyMapsUserToUser(t *testing.T) {
	tests := []struct {
		role string
		want role
	}{
		{role: "user", want: roleUser},
		{role: "assistant", want: roleAssistant},
		{role: "tool", want: roleAssistant},
		{role: "system", want: roleAssistant},
	}

	for _, tt := range tests {
		if got := tuiRoleForAgentMessage(agent.Message{Role: tt.role}); got != tt.want {
			t.Fatalf("expected raw role %q to map to %v, got %v", tt.role, tt.want, got)
		}
	}
}

func TestConnectMouseSelectsProviderAndMethodRows(t *testing.T) {
	if len(auth.Providers) < 2 {
		t.Fatal("expected at least two auth providers")
	}

	m := model{
		showConnect: true,
		connect: &connectDialog{
			stage: connectStageProvider,
		},
	}

	idx, ok := m.connectRowForY(4)
	if !ok {
		t.Fatal("expected provider row hit")
	}
	if idx != 1 {
		t.Fatalf("expected second provider row, got %d", idx)
	}

	updated, cmd := m.selectConnectRow(idx)
	if cmd != nil {
		t.Fatal("expected provider selection to stay in dialog without command")
	}
	got := derefTestModel(t, updated)
	if got.connect.stage != connectStageMethod || got.connect.providerIdx != 1 {
		t.Fatalf("expected provider click to open method stage, got %#v", got.connect)
	}

	idx, ok = got.connectRowForY(3)
	if !ok {
		t.Fatal("expected method row hit")
	}
	if idx != 0 {
		t.Fatalf("expected first method row, got %d", idx)
	}
}

func TestHandleModelCmdPreservesMCPProvenance(t *testing.T) {
	m := model{config: &config.Config{Model: "gpt-4o"}}
	a := agent.NewAgent(nil, nil, nil)
	a.RestoreMCPToolNames([]string{"demo_tool"})
	m.agent = a

	m.handleModelCmd([]string{"gpt-4o-mini"})

	if m.agent == nil {
		t.Fatal("expected agent to remain initialized")
	}
	if got := m.agent.MCPToolCount(); got != 1 {
		t.Fatalf("expected MCP provenance to survive model switch, got %d", got)
	}
}

func TestHandleNewCmdClearsTelemetry(t *testing.T) {
	spend := 0.5
	m := model{
		sessionTelemetry: sidebarTelemetry{
			promptTokens:     10,
			completionTokens: 20,
			totalTokens:      30,
			spend:            &spend,
		},
	}

	m.handleNewCmd(nil)

	if m.sessionTelemetry.usedTokens() != 0 || m.sessionTelemetry.spend != nil {
		t.Fatalf("expected telemetry to clear on new session, got %#v", m.sessionTelemetry)
	}
}

func TestHandleNewCmdResetsSessionScopedState(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	snapshot.Reset()
	tool.SetTodoSession("session-1")
	tool.ResetTodoState()
	if err := os.WriteFile("changed.go", []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := snapshot.Backup("changed.go"); err != nil {
		t.Fatal(err)
	}
	if _, err := (tool.TodoWriteTool{}).Execute(mustJSON(t, map[string]string{"todoText": "- [ ] ship task 4"})); err != nil {
		t.Fatal(err)
	}

	m := model{}
	m.handleNewCmd(nil)

	if got := snapshot.ChangedFiles(); len(got) != 0 {
		t.Fatalf("expected snapshot state to reset, got %v", got)
	}
	if got := tool.TodoState(); got != "" {
		t.Fatalf("expected todo state to reset, got %q", got)
	}
}

func TestHandleNewCmdFirstRequestTitlesSession(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(".ocode/sessions", 0755); err != nil {
		t.Fatal(err)
	}

	m := model{}
	m.handleNewCmd(nil)
	m.messages = append(m.messages, message{role: roleUser, text: "first real request"})
	m.saveSession()

	sess, err := session.Load(m.sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if sess.Title != "first real request" {
		t.Fatalf("expected first request title, got %q", sess.Title)
	}
	if len(sess.Messages) != 1 || sess.Messages[0].Content != "first real request" {
		t.Fatalf("expected transient new-session notice to stay out of history, got %#v", sess.Messages)
	}
}

func TestCountLoadedMCPToolsIgnoresCustomTools(t *testing.T) {
	a := agent.NewAgent(nil, nil, nil)
	a.AddTools([]tool.Tool{&tool.CustomTool{ToolName: "demo_tool"}})
	if got := a.MCPToolCount(); got != 0 {
		t.Fatalf("expected custom tools not to count as MCP tools, got %d", got)
	}
}

func TestSidebarTelemetryAggregationSumsUsageAndSpend(t *testing.T) {
	spendA := 0.1
	spendB := 0.2
	telemetry := aggregateSidebarTelemetry([]message{
		{raw: &agent.Message{Usage: &agent.TokenUsage{PromptTokens: int64Ptr(10), CompletionTokens: int64Ptr(20)}, Spend: &spendA}},
		{raw: &agent.Message{Usage: &agent.TokenUsage{PromptTokens: int64Ptr(5), CompletionTokens: int64Ptr(15)}, Spend: &spendB}},
	})

	if telemetry.promptTokens != 15 || telemetry.completionTokens != 35 || telemetry.totalTokens != 50 {
		t.Fatalf("expected summed usage totals, got %#v", telemetry)
	}
	if telemetry.spend == nil || math.Abs(*telemetry.spend-0.3) > 1e-9 {
		t.Fatalf("expected summed spend 0.3, got %#v", telemetry.spend)
	}
}

func TestSessionRestoreScrollsToBottom(t *testing.T) {
	m := model{
		restoredPendingScroll: true,
		messages: []message{
			{role: roleUser, text: "hello"},
			{role: roleAssistant, text: "world"},
		},
	}
	m.viewport = viewport.New(viewport.WithWidth(80), viewport.WithHeight(10))
	m.input = textarea.New()
	m.files = newFilesModel(".")
	m.git = newGitModel(".")
	m.width = 100
	m.height = 30

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	result := updated.(model)

	if result.restoredPendingScroll {
		t.Error("restoredPendingScroll should be false after first WindowSizeMsg")
	}
	if !result.viewport.AtBottom() {
		t.Error("viewport should be at bottom after session restore")
	}
}

func int64Ptr(v int64) *int64 { return &v }

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
