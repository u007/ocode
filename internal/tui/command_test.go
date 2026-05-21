package tui

import (
	"strings"
	"testing"

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
