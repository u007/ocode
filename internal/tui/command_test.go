package tui

import (
	"strings"
	"testing"

	"github.com/jamesmercstudio/ocode/internal/agent"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/jamesmercstudio/ocode/internal/config"
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
}

func TestRenderPaletteUsesRegistryCommands(t *testing.T) {
	m := model{width: 80}
	got := m.renderPalette()

	for _, want := range []string{"/help", "/sidebar", "/exit"} {
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
	m := model{input: textarea.New(), viewport: viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))}
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
	m := model{input: textarea.New(), viewport: viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))}
	m.input.SetValue("/models ")

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	got := updated.(model)
	if !got.showPicker {
		t.Fatal("expected tab on /models to open picker")
	}
}

func TestThemeCommandOpensPicker(t *testing.T) {
	m := model{input: textarea.New(), viewport: viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))}

	updated, cmd := m.handleCommand("/theme")
	if cmd != nil {
		t.Fatalf("expected /theme to return no command, got %T", cmd)
	}

	got := updated.(*model)
	if !got.showPicker || got.pickerKind != "theme" {
		t.Fatalf("expected /theme to open theme picker, got showPicker=%v kind=%q", got.showPicker, got.pickerKind)
	}
	if len(got.pickerItems) == 0 {
		t.Fatal("expected theme picker to include themes")
	}
}

func TestTabOnThemeOpensPicker(t *testing.T) {
	m := model{input: textarea.New(), viewport: viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))}
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
		viewport:     viewport.New(viewport.WithWidth(80), viewport.WithHeight(20)),
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

func TestSidebarCommandTogglesStateWithoutMessage(t *testing.T) {
	m := model{input: textarea.New(), viewport: viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))}

	updated, cmd := m.handleCommand("/sidebar")
	if cmd != nil {
		t.Fatalf("expected /sidebar to return no command, got %T", cmd)
	}

	got := updated.(*model)
	if !got.showSidebar {
		t.Fatal("expected /sidebar to toggle sidebar state on")
	}

	if len(got.messages) != 0 {
		t.Fatalf("expected /sidebar to avoid transcript messages, got %d", len(got.messages))
	}

	updated, cmd = got.handleCommand("/sidebar")
	if cmd != nil {
		t.Fatalf("expected /sidebar toggle off to return no command, got %T", cmd)
	}

	got = updated.(*model)
	if got.showSidebar {
		t.Fatal("expected /sidebar to toggle sidebar state off")
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
	m := model{input: textarea.New(), viewport: viewport.New(viewport.WithWidth(80), viewport.WithHeight(20)), files: newFilesModel(".")}

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
		viewport: viewport.New(viewport.WithWidth(80), viewport.WithHeight(20)),
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
	m := model{input: textarea.New(), viewport: viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))}

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
		viewport: viewport.New(viewport.WithWidth(80), viewport.WithHeight(20)),
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
	m := model{input: textarea.New(), viewport: viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))}

	updated, cmd := m.handleCommand("/editor-mode bogus")
	if cmd != nil {
		t.Fatalf("expected /editor-mode bogus to return no command, got %T", cmd)
	}

	got := updated.(*model)
	if len(got.messages) == 0 {
		t.Fatal("expected error message for invalid mode")
	}
	if !strings.Contains(got.messages[0].text, "Invalid editor mode") {
		t.Fatalf("expected error to mention invalid mode, got %q", got.messages[0].text)
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
	m := model{width: 80, agent: agent.NewAgent(nil, nil, nil), config: &config.Config{}}
	m.handleContextCmd(nil)
	for _, msg := range m.messages {
		if msg.raw != nil {
			t.Fatal("context command output must not set raw field (would inject into LLM)")
		}
	}
}

func TestContextCommandOutputsSections(t *testing.T) {
	m := model{width: 80, agent: agent.NewAgent(nil, nil, nil), config: &config.Config{}}
	m.handleContextCmd(nil)
	if len(m.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(m.messages))
	}
	for _, want := range []string{"Context Budget", "Base Prompt", "Tools (injected every request)", "Skill catalog (pre-injected)", "Skills (full contents available on demand, not pre-injected)", "Session Messages"} {
		if !strings.Contains(m.messages[0].text, want) {
			t.Fatalf("expected %q in context output, got %q", want, m.messages[0].text)
		}
	}
	if m.messages[0].raw != nil {
		t.Fatal("context command output must not set raw field")
	}
}
