//go:build !windows

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"testing"
	"time"
)

// seedTerminalSession installs a session directly in the table without a pty,
// so the list endpoint can be exercised without spawning a shell.
func seedTerminalSession(t *testing.T, tab *terminalSessionTable, id, project string, pid int, started time.Time) {
	t.Helper()
	s := &terminalSession{
		id:        id,
		project:   project,
		resumable: id != "",
		startedAt: started,
		cmd:       &exec.Cmd{Process: &os.Process{Pid: pid}},
		done:      make(chan struct{}),
	}
	if !tab.put(id, s) {
		t.Fatalf("seed terminal %q: put refused", id)
	}
}

// TestHandleTerminalListOnlyNamedSessionsForProject proves the list is
// project-scoped, excludes anonymous sessions (empty id — never reattachable),
// and is sorted by start time ascending.
func TestHandleTerminalListOnlyNamedSessionsForProject(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	h := NewHandler()
	h.workDir = dirA
	h.SetTerminalAccessPolicy(false, true)

	base := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	seedTerminalSession(t, h.terminalSessions, "term-b", dirB, 3001, base)
	seedTerminalSession(t, h.terminalSessions, "term-late", dirA, 1002, base.Add(2*time.Minute))
	seedTerminalSession(t, h.terminalSessions, "", dirA, 1003, base.Add(time.Minute)) // anonymous
	seedTerminalSession(t, h.terminalSessions, "term-early", dirA, 1001, base.Add(time.Minute))

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/terminal?project="+url.QueryEscape(dirA), nil)
	h.HandleTerminalList(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}
	var body struct {
		Terminals []terminalListEntry `json:"terminals"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Terminals) != 2 {
		t.Fatalf("got %d terminals, want 2: %+v", len(body.Terminals), body.Terminals)
	}
	if body.Terminals[0].ID != "term-early" || body.Terminals[1].ID != "term-late" {
		t.Fatalf("order = [%s %s], want [term-early term-late]",
			body.Terminals[0].ID, body.Terminals[1].ID)
	}
	if body.Terminals[0].PID != 1001 || body.Terminals[1].PID != 1002 {
		t.Fatalf("pids = [%d %d], want [1001 1002]", body.Terminals[0].PID, body.Terminals[1].PID)
	}
	if body.Terminals[0].Attached || body.Terminals[1].Attached {
		t.Fatalf("attached = [%v %v], want both false", body.Terminals[0].Attached, body.Terminals[1].Attached)
	}
	if !body.Terminals[0].StartedAt.Equal(base.Add(time.Minute)) {
		t.Fatalf("started_at = %s, want %s", body.Terminals[0].StartedAt, base.Add(time.Minute))
	}
}

// TestHandleTerminalListRejectsUnregisteredProject mirrors the terminal
// history boundary: a project that is not a registered root is 403, never an
// empty list.
func TestHandleTerminalListRejectsUnregisteredProject(t *testing.T) {
	h := NewHandler()
	h.workDir = t.TempDir()
	h.SetTerminalAccessPolicy(false, true)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/terminal?project="+url.QueryEscape("/nope/not/registered"), nil)
	h.HandleTerminalList(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}
