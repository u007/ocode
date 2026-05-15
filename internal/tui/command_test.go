package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
)

func TestLookupCommandResolvesAliases(t *testing.T) {
	if got := lookupCommand("/clear"); got == nil || got.name != "/new" {
		t.Fatalf("expected /clear to resolve to /new, got %#v", got)
	}

	if got := lookupCommand("/q"); got == nil || got.name != "/exit" {
		t.Fatalf("expected /q to resolve to /exit, got %#v", got)
	}

	if got := lookupCommand("/quit"); got == nil || got.name != "/exit" {
		t.Fatalf("expected /quit to resolve to /exit, got %#v", got)
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

	if got := autocompleteSlashInput(&m, "/m"); len(got) == 0 || got[0] != "/model" {
		t.Fatalf("expected /m to resolve to /model, got %#v", got)
	}

	got := autocompleteSlashInput(&m, "/model ")
	if len(got) == 0 {
		t.Fatalf("expected /model autocomplete to return at least one model, got empty")
	}
}

func TestTabAutocompleteRunsInUpdatePath(t *testing.T) {
	m := model{input: textarea.New(), viewport: viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))}
	m.input.SetValue("/m")

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	got := updated.(model)
	if got.input.Value() != "/model" {
		t.Fatalf("expected tab to complete /m to /model, got %q", got.input.Value())
	}

	if got.showPalette {
		t.Fatal("expected tab autocomplete to operate on the main input, not palette")
	}
}

func TestTabOnModelOpensPicker(t *testing.T) {
	m := model{input: textarea.New(), viewport: viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))}
	m.input.SetValue("/model ")

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	got := updated.(model)
	if !got.showPicker {
		t.Fatal("expected tab on /model to open picker")
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
	for _, want := range []string{"/model <name>", "/new, /clear", "/exit, /quit, /q"} {
		if !strings.Contains(help, want) {
			t.Fatalf("expected help text to include %q, got %q", want, help)
		}
	}
}
