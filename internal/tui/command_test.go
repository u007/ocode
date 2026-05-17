package tui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
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
