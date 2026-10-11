package tui

import (
	"fmt"
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// collectOSC52 recursively walks a command result and returns the payload of
// every OSC 52 clipboard emit it performed. bubbletea exposes the emit only as
// an unexported `setClipboardMsg`, so the type is matched by name — that name
// is exactly the signal we care about (the copy leaves the process as OSC 52
// rather than going through a local clipboard utility).
func collectOSC52(msg tea.Msg) []string {
	switch v := msg.(type) {
	case tea.BatchMsg:
		var out []string
		for _, c := range v {
			out = append(out, collectOSC52(c())...)
		}
		return out
	default:
		if msg != nil && reflect.TypeOf(msg).String() == "tea.setClipboardMsg" {
			return []string{fmt.Sprint(msg)}
		}
		return nil
	}
}

func TestCopyToClipboardEmitsOSC52(t *testing.T) {
	cmd := copyToClipboard("remote selection")
	if cmd == nil {
		t.Fatal("copyToClipboard returned nil for non-empty text")
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("copyToClipboard command produced %T, want tea.BatchMsg (OSC 52 + local fallback)", msg)
	}
	if len(batch) != 2 {
		t.Fatalf("batch has %d commands, want 2 (OSC 52 + local fallback)", len(batch))
	}

	// PATH="" makes the local clipboard utility unresolvable, so exercising the
	// fallback half cannot clobber the developer's real clipboard.
	t.Setenv("PATH", "")
	got := collectOSC52(msg)
	if len(got) != 1 || got[0] != "remote selection" {
		t.Fatalf("OSC 52 payloads = %q, want [\"remote selection\"]", got)
	}
}

func TestCopyToClipboardEmptyIsNoop(t *testing.T) {
	if cmd := copyToClipboard(""); cmd != nil {
		t.Fatalf("copyToClipboard(\"\") = %v, want nil", cmd)
	}
}

// TestTranscriptSelectionReleaseReturnsCopyCmd guards the wiring: a transcript
// selection released by the mouse must hand back the clipboard command, not a
// nil one. Before this, the release path wrote via the local utility and
// dropped the error, so over SSH nothing reached the user's machine.
func TestTranscriptSelectionReleaseReturnsCopyCmd(t *testing.T) {
	m := newTestModel(RunOptions{PermissionMode: "off"})
	m.width = 120
	m.height = 40
	m.activeTab = tabChat
	m.rawTranscriptLines = []string{"alpha beta gamma"}
	m.sel = selectionState{dragging: true, active: true, startLine: 0, startCol: 0, endLine: 0, endCol: 5}

	_, cmd, handled := m.handleMouseAction(tea.Mouse{Button: tea.MouseNone, X: 5, Y: 5}, false)
	if !handled {
		t.Fatal("transcript selection release was not handled")
	}
	if cmd == nil {
		t.Fatal("transcript selection release returned no clipboard command")
	}

	t.Setenv("PATH", "")
	if got := collectOSC52(cmd()); len(got) == 0 || got[0] != "alpha" {
		t.Fatalf("release emitted OSC 52 payloads %q, want [\"alpha\"]", got)
	}
}
