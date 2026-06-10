package tui

import (
	"strings"
	"testing"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/tui/fastviewport"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/u007/ocode/internal/config"
)

func TestLookupCommandResolvesAliases(t *testing.T) {
	if got := lookupCommand("/model"); got == nil || got.name != "/models" {
		t.Fatalf("expected /model to resolve to /models, got %#v", got)
	}

	if got := lookupCommand("/clear"); got == nil || got.name != "/new" {
		t.Fatalf("expected /clear to resolve to /new, got %#v", got)
	}

	if got := lookupCommand("/q"); got == nil || got.name != "/exit" {
		t.Fatalf("expected /q to resolve to /exit, got %#v", got)
	}

	if got := lookupCommand("/quit"); got == nil || got.name != "/exit" {
		t.Fatalf("expected /quit to resolve to /exit, got %#v", got)
	}

	if got := lookupCommand("/resume"); got == nil || got.name != "/session" {
		t.Fatalf("expected /resume to resolve to /session, got %#v", got)
	}

	if got := lookupCommand("/sessions"); got == nil || got.name != "/session" {
		t.Fatalf("expected /sessions to resolve to /session, got %#v", got)
	}

	if got := lookupCommand("/theme"); got == nil || got.name != "/themes" {
		t.Fatalf("expected /theme to resolve to /themes, got %#v", got)
	}

	if got := lookupCommand("/export-claude"); got == nil || got.name != "/export-claude" {
		t.Fatalf("expected /export-claude to resolve to itself, got %#v", got)
	}
}

func TestRenderPaletteUsesRegistryCommands(t *testing.T) {
	m := model{width: 80}
	got := m.renderPalette()

	for _, want := range []string{"/help", "/sidebar", "/export-claude", "/ide", "/exit"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected palette to include %s, got %q", want, got)
		}
	}
}

func TestSlashAutocompleteResolvesCommand(t *testing.T) {
	m := model{}

	if got := autocompleteSlashInput(&m, "/m"); len(got) == 0 || got[0] != "/models" {
		t.Fatalf("expected /m to resolve to /models, got %#v", got)
	}

	got := autocompleteSlashInput(&m, "/models ")
	if len(got) == 0 {
		t.Fatalf("expected /models autocomplete to return at least one model, got empty")
	}
}

func TestTabAutocompleteRunsInUpdatePath(t *testing.T) {
	m := model{input: textarea.New(), viewport: fastviewport.New(80, 20)}
	m.input.SetValue("/m")

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	got := updated.(model)
	if got.input.Value() != "/models" {
		t.Fatalf("expected tab to complete /m to /models, got %q", got.input.Value())
	}

	if got.showPalette {
		t.Fatal("expected tab autocomplete to operate on the main input, not palette")
	}
}

func TestTabOnModelOpensPicker(t *testing.T) {
	m := model{input: textarea.New(), viewport: fastviewport.New(80, 20)}
	m.input.SetValue("/models ")

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	got := updated.(model)
	if !got.showPicker {
		t.Fatal("expected tab on /models to open picker")
	}
}

func TestThemeCommandOpensPicker(t *testing.T) {
	m := model{input: textarea.New(), viewport: fastviewport.New(80, 20)}

	updated, cmd := m.handleCommand("/theme")
	if cmd != nil {
		t.Fatalf("expected /theme to return no command, got %T", cmd)
	}

	got := updated.(*model)
	if len(got.messages) != 1 {
		t.Fatalf("expected /theme to be recorded in transcript, got %d messages", len(got.messages))
	}
	if got.messages[0].role != roleUser || got.messages[0].text != "/theme" {
		t.Fatalf("expected first transcript message to be /theme, got %#v", got.messages[0])
	}
	if !got.showPicker || got.pickerKind != "theme" {
		t.Fatalf("expected /theme to open theme picker, got showPicker=%v kind=%q", got.showPicker, got.pickerKind)
	}
	if len(got.pickerItems) == 0 {
		t.Fatal("expected theme picker to include themes")
	}
}

func TestTabOnThemeOpensPicker(t *testing.T) {
	m := model{input: textarea.New(), viewport: fastviewport.New(80, 20)}
	m.input.SetValue("/theme ")

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	got := updated.(model)
	if !got.showPicker || got.pickerKind != "theme" {
		t.Fatalf("expected tab on /theme to open theme picker, got showPicker=%v kind=%q", got.showPicker, got.pickerKind)
	}
}

func TestThemePickerSelectionSwitchesTheme(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	themes := AvailableThemes()
	if len(themes) == 0 {
		t.Fatal("expected built-in themes")
	}
	m := model{
		input:        textarea.New(),
		viewport:     fastviewport.New(80, 20),
		config:       &config.Config{Ocode: config.OcodeConfig{}},
		showPicker:   true,
		pickerKind:   "theme",
		pickerItems:  themes,
		pickerValues: themes,
	}

	updated, cmd := m.selectPickerIndex(0)
	if cmd != nil {
		t.Fatalf("expected theme picker selection to return no command, got %T", cmd)
	}

	got := updated.(*model)
	if got.showPicker {
		t.Fatal("expected theme picker to close after selection")
	}
	if got.config.Ocode.TUI.Theme != themes[0] {
		t.Fatalf("expected selected theme %q, got %q", themes[0], got.config.Ocode.TUI.Theme)
	}
}

func TestSidebarCommandRecordsTranscriptMessage(t *testing.T) {
	m := model{input: textarea.New(), viewport: fastviewport.New(80, 20)}

	updated, cmd := m.handleCommand("/sidebar")
	if cmd != nil {
		t.Fatalf("expected /sidebar to return no command, got %T", cmd)
	}

	got := updated.(*model)
	if !got.showSidebar {
		t.Fatal("expected /sidebar to toggle sidebar state on")
	}

	if len(got.messages) != 1 {
		t.Fatalf("expected /sidebar to be recorded in transcript, got %d messages", len(got.messages))
	}
	if got.messages[0].role != roleUser || got.messages[0].text != "/sidebar" {
		t.Fatalf("expected transcript to include /sidebar, got %#v", got.messages[0])
	}

	updated, cmd = got.handleCommand("/sidebar")
	if cmd != nil {
		t.Fatalf("expected /sidebar toggle off to return no command, got %T", cmd)
	}

	got = updated.(*model)
	if got.showSidebar {
		t.Fatal("expected /sidebar to toggle sidebar state off")
	}
	if len(got.messages) != 2 {
		t.Fatalf("expected second /sidebar run to add another transcript entry, got %d messages", len(got.messages))
	}
	if got.messages[1].role != roleUser || got.messages[1].text != "/sidebar" {
		t.Fatalf("expected second transcript entry to include /sidebar, got %#v", got.messages[1])
	}
}

func TestDrainQueuedCommandsProcessesAllSynchronousCommands(t *testing.T) {
	m := model{
		width:          80,
		height:         20,
		input:          textarea.New(),
		viewport:       fastviewport.New(80, 20),
		queuedCommands: []string{"/theme", "/sidebar"},
	}

	cmd, drained := m.drainQueuedCommands()
	if cmd != nil {
		t.Fatalf("expected synchronous queued commands to return no command, got %T", cmd)
	}
	if !drained {
		t.Fatal("expected queued commands to be drained")
	}
	if len(m.queuedCommands) != 0 {
		t.Fatalf("expected queue to be empty after drain, got %#v", m.queuedCommands)
	}
	if !m.showPicker || m.pickerKind != "theme" {
		t.Fatalf("expected /theme to open picker, got showPicker=%v kind=%q", m.showPicker, m.pickerKind)
	}
	if !m.showSidebar {
		t.Fatal("expected /sidebar to toggle sidebar after /theme")
	}
	if len(m.messages) != 2 {
		t.Fatalf("expected both commands to be recorded, got %#v", m.messages)
	}
	if m.messages[0].text != "/theme" || m.messages[1].text != "/sidebar" {
		t.Fatalf("expected queued commands in order, got %#v", m.messages)
	}
}

func TestLoginCommandBypassesBusyQueue(t *testing.T) {
	m := model{
		width:     80,
		height:    20,
		input:     textarea.New(),
		viewport:  fastviewport.New(80, 20),
		streaming: true,
	}

	updated, cmd := m.handleCommand("/login")
	if cmd == nil {
		t.Fatal("expected /login to run immediately while busy")
	}

	got := updated.(*model)
	if len(got.queuedCommands) != 0 {
		t.Fatalf("expected /login not to be queued, got %#v", got.queuedCommands)
	}
	if len(got.messages) == 0 || got.messages[0].role != roleUser || got.messages[0].text != "/login" {
		t.Fatalf("expected /login to be recorded immediately, got %#v", got.messages)
	}
}

func TestNewCommandBypassesBusyQueue(t *testing.T) {
	m := model{
		width:          80,
		height:         20,
		input:          textarea.New(),
		viewport:       fastviewport.New(80, 20),
		streaming:      true,
		sessionID:      "old-session",
		messages:       []message{{role: roleUser, text: "keep me busy"}},
		queuedInputs:   []string{"pending user input"},
		queuedCommands: []string{"/sidebar"},
	}

	updated, _ := m.handleCommand("/new")
	got := updated.(*model)
	if len(got.queuedCommands) != 0 {
		t.Fatalf("expected /new not to be queued, got %#v", got.queuedCommands)
	}
	if len(got.queuedInputs) != 0 {
		t.Fatalf("expected /new to clear queued inputs, got %#v", got.queuedInputs)
	}
	if got.sessionID == "old-session" {
		t.Fatal("expected /new to start a fresh session immediately")
	}
	if len(got.messages) != 1 || got.messages[0].role != roleAssistant || got.messages[0].text != "Started new session." {
		t.Fatalf("expected /new to reset the transcript immediately, got %#v", got.messages)
	}
}

func TestCompactFinishedResumesAfterQueuedLocalCommands(t *testing.T) {
	m := model{
		width:                80,
		height:               20,
		input:                textarea.New(),
		viewport:             fastviewport.New(80, 20),
		agent:                agent.NewAgent(nil, nil, &config.Config{}, nil),
		messages:             []message{{role: roleUser, text: "hello"}},
		pendingCompactResume: true,
		queuedCommands:       []string{"/sidebar"},
	}

	updated, cmd := m.Update(compactFinishedMsg{result: agent.CompactResult{OK: false}})
	if cmd == nil {
		t.Fatal("expected compact finish to resume the agent after draining queued commands")
	}

	got := derefTestModel(t, updated)
	if len(got.queuedCommands) != 0 {
		t.Fatalf("expected queued commands to be drained, got %#v", got.queuedCommands)
	}
	if !got.showSidebar {
		t.Fatal("expected queued /sidebar command to run before resuming")
	}
}

func TestCommandHelpTextShowsAliasesAndArgs(t *testing.T) {
	help := commandHelpText()
	for _, want := range []string{"/models [name], /model", "/session [list|load <id>], /sessions, /resume", "/new, /clear", "/exit, /quit, /q"} {
		if !strings.Contains(help, want) {
			t.Fatalf("expected help text to include %q, got %q", want, help)
		}
	}
}

func TestEditorCommandOpensPicker(t *testing.T) {
	m := model{input: textarea.New(), viewport: fastviewport.New(80, 20), files: newFilesModel(".")}

	updated, cmd := m.handleCommand("/editor")
	if cmd != nil {
		t.Fatalf("expected /editor to return no command, got %T", cmd)
	}

	got := updated.(*model)
	if !got.showPicker || got.pickerKind != "editor" {
		t.Fatalf("expected /editor to open editor picker, got showPicker=%v kind=%q", got.showPicker, got.pickerKind)
	}
	if len(got.pickerItems) == 0 {
		t.Fatal("expected editor picker to include editors")
	}
	if got.pickerItems[0] != "nvim" {
		t.Fatalf("expected first picker item to be nvim, got %q", got.pickerItems[0])
	}
}

func TestEditorCommandSetsEditor(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	m := model{
		input:    newTestTextarea(),
		viewport: fastviewport.New(80, 20),
		files:    newFilesModel("."),
		config:   &config.Config{Ocode: config.OcodeConfig{}},
	}

	updated, cmd := m.handleCommand("/editor cat")
	if cmd != nil {
		t.Fatalf("expected /editor cat to return no command, got %T", cmd)
	}

	got := updated.(*model)
	if got.files.editor != "cat" {
		t.Fatalf("expected editor to be set to cat, got %q", got.files.editor)
	}
}

func TestEditorModeCommandOpensPicker(t *testing.T) {
	m := model{input: textarea.New(), viewport: fastviewport.New(80, 20)}

	updated, cmd := m.handleCommand("/editor-mode")
	if cmd != nil {
		t.Fatalf("expected /editor-mode to return no command, got %T", cmd)
	}

	got := updated.(*model)
	if !got.showPicker || got.pickerKind != "editor-mode" {
		t.Fatalf("expected /editor-mode to open editor-mode picker, got showPicker=%v kind=%q", got.showPicker, got.pickerKind)
	}
	if len(got.pickerItems) != 3 {
		t.Fatalf("expected 3 editor mode options, got %d", len(got.pickerItems))
	}
	if got.pickerItems[0] != config.EditorModeExternal {
		t.Fatalf("expected first option to be external, got %q", got.pickerItems[0])
	}
}

func TestEditorModeCommandValidMode(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	m := model{
		input:    textarea.New(),
		viewport: fastviewport.New(80, 20),
		config:   &config.Config{Ocode: config.OcodeConfig{}},
		files:    newFilesModel("."),
	}

	updated, cmd := m.handleCommand("/editor-mode external")
	if cmd != nil {
		t.Fatalf("expected /editor-mode external to return no command, got %T", cmd)
	}

	got := updated.(*model)
	if got.config.Ocode.EditorMode != config.EditorModeExternal {
		t.Fatalf("expected editor mode to be set to external, got %q", got.config.Ocode.EditorMode)
	}
}

func TestEditorModeCommandInvalidMode(t *testing.T) {
	m := model{input: textarea.New(), viewport: fastviewport.New(80, 20)}

	updated, cmd := m.handleCommand("/editor-mode bogus")
	if cmd != nil {
		t.Fatalf("expected /editor-mode bogus to return no command, got %T", cmd)
	}

	got := updated.(*model)
	if len(got.messages) != 2 {
		t.Fatalf("expected transcript command plus error message for invalid mode, got %d messages", len(got.messages))
	}
	if got.messages[0].role != roleUser || got.messages[0].text != "/editor-mode bogus" {
		t.Fatalf("expected command to be recorded first, got %#v", got.messages[0])
	}
	if !strings.Contains(got.messages[1].text, "Invalid editor mode") {
		t.Fatalf("expected error to mention invalid mode, got %q", got.messages[1].text)
	}
}

func TestEstimateTok(t *testing.T) {
	cases := []struct {
		input string
		want  int
	}{
		{"", 0},
		{"abcd", 1},
		{"abcde", 1},
		{"abcdefgh", 2},
	}
	for _, c := range cases {
		if got := estimateTok(c.input); got != c.want {
			t.Errorf("estimateTok(%q) = %d, want %d", c.input, got, c.want)
		}
	}
}

func TestContextCommandIsRegistered(t *testing.T) {
	spec := lookupCommand("/context")
	if spec == nil {
		t.Fatal("expected /context to be registered")
	}
	if spec.help == "" {
		t.Fatal("expected /context to have help text")
	}
}

func TestContextCommandNilAgentGuard(t *testing.T) {
	m := model{width: 80}
	// must not panic
	m.handleContextCmd(nil)
	if len(m.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(m.messages))
	}
	if !strings.Contains(m.messages[0].text, "No agent") {
		t.Fatalf("expected no-agent message, got %q", m.messages[0].text)
	}
}

func TestContextMCPGrouping(t *testing.T) {
	serverNames := []string{"claude_ai_Gmail", "context7"}
	toolNames := map[string]struct{}{
		"claude_ai_Gmail_search": {},
		"claude_ai_Gmail_send":   {},
		"context7_query":         {},
		"bash":                   {},
	}
	defs := []map[string]interface{}{
		{"name": "claude_ai_Gmail_search", "description": "search"},
		{"name": "claude_ai_Gmail_send", "description": "send"},
		{"name": "context7_query", "description": "query"},
		{"name": "bash", "description": "run bash"},
	}

	grouped, builtin := groupMCPToolDefs(defs, toolNames, serverNames)

	if len(grouped["claude_ai_Gmail"]) != 2 {
		t.Errorf("expected 2 tools for claude_ai_Gmail, got %d", len(grouped["claude_ai_Gmail"]))
	}
	if len(grouped["context7"]) != 1 {
		t.Errorf("expected 1 tool for context7, got %d", len(grouped["context7"]))
	}
	if len(builtin) != 1 || builtin[0]["name"] != "bash" {
		t.Errorf("expected bash in builtin, got %v", builtin)
	}
}

func TestContextCommandInHelp(t *testing.T) {
	help := commandHelpText()
	if !strings.Contains(help, "/context") {
		t.Fatalf("expected /context in help text, got:\n%s", help)
	}
}

func TestContextCommandAutocompletes(t *testing.T) {
	m := model{}
	results := autocompleteSlashInput(&m, "/con")
	found := false
	for _, r := range results {
		if r == "/context" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected /context in autocomplete for '/con', got %v", results)
	}
}

func TestContextCommandOutputHasNoRaw(t *testing.T) {
	m := model{width: 80, agent: agent.NewAgent(nil, nil, nil, nil), config: &config.Config{}}
	m.handleContextCmd(nil)
	for _, msg := range m.messages {
		if msg.raw != nil {
			t.Fatal("context command output must not set raw field (would inject into LLM)")
		}
	}
}

func TestContextCommandOutputsSections(t *testing.T) {
	m := model{width: 80, agent: agent.NewAgent(nil, nil, nil, nil), config: &config.Config{}}
	m.handleContextCmd(nil)
	if len(m.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(m.messages))
	}
	for _, want := range []string{"Context Budget", "Base Prompt", "Tools (injected every request)", "MCP: (none)", "Skill catalog (pre-injected)", "Skills (full contents available on demand, not pre-injected)", "Session Messages"} {
		if !strings.Contains(m.messages[0].text, want) {
			t.Fatalf("expected %q in context output, got %q", want, m.messages[0].text)
		}
	}
	if m.messages[0].raw != nil {
		t.Fatal("context command output must not set raw field")
	}
}

// oneShotStreamClient returns a single tool-free assistant message so a.Step
// completes in one turn without retries or hanging.
type oneShotStreamClient struct{}

func (oneShotStreamClient) Chat([]agent.Message, []map[string]interface{}) (*agent.Message, error) {
	return &agent.Message{Role: "assistant", Content: "done"}, nil
}
func (oneShotStreamClient) GetProvider() string { return "test" }
func (oneShotStreamClient) GetModel() string    { return "test-model" }

// TestCustomCommandUsesStreamingPath guards the fix for custom slash commands
// (e.g. /review-changes) freezing the chat: they must stream live via
// streamStartedMsg, not run synchronously and only deliver output at the end.
func TestCustomCommandUsesStreamingPath(t *testing.T) {
	a := agent.NewAgent(oneShotStreamClient{}, nil, &config.Config{}, nil)
	m := &model{agent: a, config: &config.Config{}}

	cmd := m.sendCustomCommandPrompt("review please")
	if cmd == nil {
		t.Fatal("sendCustomCommandPrompt returned nil command")
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("expected tea.BatchMsg from the streaming path, got %T", msg)
	}
	found := false
	for _, c := range batch {
		if c == nil {
			continue
		}
		if _, ok := c().(streamStartedMsg); ok {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("custom command did not emit streamStartedMsg — it is not using the streaming path")
	}
}
