package tui

import (
	"strings"
	"testing"
)

// status and path exec/stat the cached binary; they must reach the transcript
// through a tea.Cmd, never by running on the Update goroutine.
func TestLocalServerCmd_InspectionRunsOnCmd(t *testing.T) {
	for _, args := range [][]string{nil, {"status"}, {"path"}} {
		m := newTestModel()
		before := len(m.messages)
		cmd := m.handleLocalServerCmd(args)
		if cmd == nil {
			t.Fatalf("args %v: expected a tea.Cmd", args)
		}
		if len(m.messages) != before {
			t.Fatalf("args %v: appended a message synchronously", args)
		}
		msg, ok := cmd().(localServerActionMsg)
		if !ok || msg.err != nil || strings.TrimSpace(msg.text) == "" {
			t.Fatalf("args %v: got %#v", args, cmd())
		}
	}
}

func TestLocalServerIsReadOnly(t *testing.T) {
	for args, want := range map[string]bool{
		"": true, "status": true, "PATH": true, "fix": false, "reinstall": false, "bogus": false,
	} {
		var a []string
		if args != "" {
			a = []string{args}
		}
		if got := localServerIsReadOnly(a); got != want {
			t.Errorf("localServerIsReadOnly(%q) = %v, want %v", args, got, want)
		}
	}
}
