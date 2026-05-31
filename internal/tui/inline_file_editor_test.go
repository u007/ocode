package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestInlineFileEditorInsertAndMovement(t *testing.T) {
	ed := newInlineFileEditor("alpha\nbeta\n")

	ed = ed.update(tea.KeyPressMsg{Code: 'j'})
	if ed.cursorRow != 1 {
		t.Fatalf("expected cursor row 1, got %d", ed.cursorRow)
	}

	ed = ed.update(tea.KeyPressMsg{Code: '$'})
	if ed.cursorCol != len("beta")-1 {
		t.Fatalf("expected cursor at end of line, got %d", ed.cursorCol)
	}

	ed = ed.update(tea.KeyPressMsg{Code: 'a'})
	if ed.mode != inlineEditorInsert {
		t.Fatalf("expected insert mode after a, got %v", ed.mode)
	}

	ed = ed.update(tea.KeyPressMsg{Code: '!', Text: "!"})
	ed = ed.update(tea.KeyPressMsg{Code: tea.KeyEsc})

	if got := ed.content(); got != "alpha\nbeta!\n" {
		t.Fatalf("expected appended content, got %q", got)
	}
	if !ed.dirty {
		t.Fatal("expected editor to be dirty")
	}
	if ed.mode != inlineEditorNormal {
		t.Fatalf("expected normal mode after esc, got %v", ed.mode)
	}
}

func TestInlineFileEditorCommandMode(t *testing.T) {
	ed := newInlineFileEditor("hello\n")
	ed = ed.update(tea.KeyPressMsg{Code: ':'})
	if ed.mode != inlineEditorCommand {
		t.Fatalf("expected command mode, got %v", ed.mode)
	}

	ed = ed.update(tea.KeyPressMsg{Code: 'w', Text: "w"})
	ed = ed.update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	ed = ed.update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if ed.lastCommand != "wq" {
		t.Fatalf("expected wq command, got %q", ed.lastCommand)
	}
	if ed.mode != inlineEditorNormal {
		t.Fatalf("expected normal mode after command submit, got %v", ed.mode)
	}
}

func TestInlineFileEditorDirtyQuitRules(t *testing.T) {
	ed := newInlineFileEditor("hello\n")
	ed = ed.update(tea.KeyPressMsg{Code: 'i'})
	ed = ed.update(tea.KeyPressMsg{Code: 'X', Text: "X"})
	ed = ed.update(tea.KeyPressMsg{Code: tea.KeyEsc})

	ed = ed.update(tea.KeyPressMsg{Code: ':'})
	ed = ed.update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	ed = ed.update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if ed.lastCommand != "q" {
		t.Fatalf("expected q command, got %q", ed.lastCommand)
	}
	if !ed.dirty {
		t.Fatal("expected q command to leave dirty buffer intact")
	}
}

func TestInlineFileEditorViewShowsModeAndCommand(t *testing.T) {
	ed := newInlineFileEditor("hello\n")
	ed = ed.update(tea.KeyPressMsg{Code: ':'})
	ed = ed.update(tea.KeyPressMsg{Code: 'w', Text: "w"})

	view := ed.view(40, 10)
	for _, want := range []string{"-- COMMAND --", ":w", "hello"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected view to contain %q, got:\n%s", want, view)
		}
	}
}
