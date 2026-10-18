//go:build !windows

package server

import (
	"path/filepath"
	"testing"

	"github.com/u007/ocode/internal/termtabs"
)

// terminal_kill must drop the terminal's shared tab entry as well as the shell
// and its history; otherwise every connected client keeps a row for a dead shell.
func TestPulseTerminalKillRemovesTabEntry(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	requirePTY(t)
	h, _, wsURL := terminalTestHandler(t)
	h.projects = newTestProjectStore(t, h.workDir)
	store, err := termtabs.NewStoreAt(filepath.Join(t.TempDir(), "terminals.json"))
	if err != nil {
		t.Fatalf("tab store: %v", err)
	}
	h.termTabsStore = store

	const id = "term-pulse-kill-tab"
	dialTerminal(t, wsURL, id)
	sess := h.terminalSessions.lookup(id)
	if sess == nil {
		t.Fatal("terminal not registered")
	}
	pt := store.Get(sess.project)
	pt.Terminals = append(pt.Terminals, termtabs.Terminal{ID: id, Title: "Terminal"})
	if err := store.Set(sess.project, pt); err != nil {
		t.Fatalf("seed tab entry: %v", err)
	}

	mustPulseTool(t, h, "terminal_kill", `{"terminal_id":"`+id+`"}`)

	for _, tab := range store.Get(sess.project).Terminals {
		if tab.ID == id {
			t.Fatalf("tab entry for %s survived terminal_kill", id)
		}
	}
}
