//go:build !windows

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/termtabs"
)

// seedLiveTerminal registers a terminal in the live registry. A pid that is not
// running reads as idle; the test binary's own pid reads as a running program,
// because its command line is not an interactive shell.
func seedLiveTerminal(h *Handler, id, project string, pid int32) {
	h.terminalProcs.register(id, terminalProcEntry{Project: project, PID: pid})
}

const idleTestPID = 999999999

func TestPulseTerminalsRunningFirstAndPaged(t *testing.T) {
	h := NewHandler()
	h.SetTerminalAccessPolicy(false, true)
	seedLiveTerminal(h, "term-idle", "/work/a", idleTestPID)
	seedLiveTerminal(h, "term-run-b", "/work/b", int32(os.Getpid()))
	seedLiveTerminal(h, "term-run-a", "/work/a", int32(os.Getpid()))

	get := func(query string) (int, struct {
		Terminals []pulseTerminalRow `json:"terminals"`
		Total     int                `json:"total"`
	}) {
		t.Helper()
		w := httptest.NewRecorder()
		h.HandlePulseTerminals(w, httptest.NewRequest(http.MethodGet, "/api/pulse/terminals"+query, nil))
		var body struct {
			Terminals []pulseTerminalRow `json:"terminals"`
			Total     int                `json:"total"`
		}
		if w.Code == http.StatusOK {
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode %q: %v", w.Body.String(), err)
			}
		}
		return w.Code, body
	}

	code, page := get("?limit=2")
	if code != http.StatusOK || page.Total != 3 || len(page.Terminals) != 2 {
		t.Fatalf("limit=2: code=%d total=%d rows=%d, want 200/3/2", code, page.Total, len(page.Terminals))
	}
	if !page.Terminals[0].Running || !page.Terminals[1].Running {
		t.Fatalf("running terminals must come first: %+v", page.Terminals)
	}
	if page.Terminals[0].Command == "" {
		t.Fatalf("a running terminal must report its command: %+v", page.Terminals[0])
	}
	if page.Terminals[0].Project != "/work/a" || page.Terminals[0].ID != "term-run-a" {
		t.Fatalf("running ties sort by project then id; got %+v", page.Terminals[0])
	}

	code, rest := get("?limit=2&offset=2")
	if code != http.StatusOK || len(rest.Terminals) != 1 {
		t.Fatalf("offset=2: code=%d rows=%d, want 200 and 1 row", code, len(rest.Terminals))
	}
	if rest.Terminals[0].ID != "term-idle" || rest.Terminals[0].Running {
		t.Fatalf("idle terminal must sort last and not be running: %+v", rest.Terminals[0])
	}
}

func TestPulseTerminalsRejectsBadPagingAndUnauthorizedAccess(t *testing.T) {
	h := NewHandler()
	h.SetTerminalAccessPolicy(false, true)
	for _, q := range []string{"?limit=0", "?limit=abc", "?offset=-1"} {
		w := httptest.NewRecorder()
		h.HandlePulseTerminals(w, httptest.NewRequest(http.MethodGet, "/api/pulse/terminals"+q, nil))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s: status %d, want 400", q, w.Code)
		}
	}

	denied := NewHandler()
	denied.SetTerminalAccessPolicy(false, false)
	w := httptest.NewRecorder()
	denied.HandlePulseTerminals(w, httptest.NewRequest(http.MethodGet, "/api/pulse/terminals", nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("no auth and not loopback: status %d, want 403", w.Code)
	}
}

func TestRenderPulseTerminalsListsOnlyRunningPrograms(t *testing.T) {
	rows := []pulseTerminalRow{
		{ID: "term-run", Project: "/work/app", Title: "dev", Command: "npm run dev", Running: true},
		{ID: "term-idle", Project: "/work/app", Title: "shell", Command: "zsh", Running: false},
	}
	out := renderPulseTerminals(rows)
	if !strings.HasPrefix(out, "Running terminals (1 of 1 shown)") {
		t.Fatalf("header: %q", out)
	}
	if !strings.Contains(out, "term-run | app | dev | npm run dev") {
		t.Fatalf("running row missing: %q", out)
	}
	if strings.Contains(out, "term-idle") {
		t.Fatalf("idle terminal must not be listed: %q", out)
	}
	if got := renderPulseTerminals([]pulseTerminalRow{rows[1]}); got != "No programs running in terminals." {
		t.Fatalf("no running programs: %q", got)
	}
}

func TestRenderPulseTerminalsCapsTheBlock(t *testing.T) {
	var rows []pulseTerminalRow
	for i := 0; i < pulseBoardMaxTerminals+5; i++ {
		rows = append(rows, pulseTerminalRow{ID: fmt.Sprintf("t%02d", i), Project: "/p", Command: "vite", Running: true})
	}
	out := renderPulseTerminals(rows)
	want := fmt.Sprintf("Running terminals (%d of %d shown)", pulseBoardMaxTerminals, len(rows))
	if !strings.HasPrefix(out, want) {
		t.Fatalf("cap header: %q, want prefix %q", out, want)
	}
	if got := strings.Count(out, "\n"); got != pulseBoardMaxTerminals {
		t.Fatalf("rows shown = %d, want %d", got, pulseBoardMaxTerminals)
	}
}

func TestPulseTerminalTabsMarksLiveAndRunningTerminals(t *testing.T) {
	h := NewHandler()
	h.SetTerminalAccessPolicy(false, true)
	store, err := termtabs.NewStoreAt(filepath.Join(t.TempDir(), "terminals.json"))
	if err != nil {
		t.Fatalf("termtabs.NewStoreAt: %v", err)
	}
	h.termTabsStore = store
	project := t.TempDir()
	if err := store.Set(project, termtabs.ProjectTerminals{Terminals: []termtabs.Terminal{
		{ID: "term-run", Title: "dev"},
		{ID: "term-closed", Title: "old"},
	}}); err != nil {
		t.Fatalf("seed tabs: %v", err)
	}
	seedLiveTerminal(h, "term-run", project, int32(os.Getpid()))

	out, err := h.pulseTerminalTabsTool(json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("terminal_tabs: %v", err)
	}
	var body struct {
		Projects []pulseTabsProject `json:"projects"`
	}
	if err := json.Unmarshal([]byte(out), &body); err != nil {
		t.Fatalf("decode %q: %v", out, err)
	}
	if len(body.Projects) != 1 || len(body.Projects[0].Terminals) != 2 {
		t.Fatalf("terminal_tabs = %s", out)
	}
	byID := map[string]pulseTabTerminal{}
	for _, tt := range body.Projects[0].Terminals {
		byID[tt.ID] = tt
	}
	if run := byID["term-run"]; !run.Live || !run.Running || run.Command == "" {
		t.Fatalf("live running tab: %+v", run)
	}
	if closed := byID["term-closed"]; closed.Live || closed.Running || closed.Command != "" {
		t.Fatalf("tab with no live process must read as not live: %+v", closed)
	}
}

// TestPulseTerminalReportsRealRunningProgram checks the running-app signal end to
// end against a real child process: a terminal whose process is `sleep` is a
// running program, not an idle shell.
func TestPulseTerminalReportsRealRunningProgram(t *testing.T) {
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skipf("no sleep binary: %v", err)
	}
	cmd := exec.Command(sleep, "30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleep: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	h := NewHandler()
	h.SetTerminalAccessPolicy(false, true)
	seedLiveTerminal(h, "term-sleep", "/work/app", int32(cmd.Process.Pid))
	rows := h.pulseTerminalRows()
	if len(rows) != 1 {
		t.Fatalf("rows = %+v, want one terminal", rows)
	}
	if !rows[0].Running || !strings.Contains(rows[0].Command, "sleep") {
		t.Fatalf("a running sleep must read as a running program: %+v", rows[0])
	}
}

// TestPulseTerminalCommandsStayBehindTheAccessGate pins the rule that a terminal's
// command line reaches the model only when the terminal access gate passes: the
// per-turn board and terminal_tabs both leave it out when the gate is closed.
func TestPulseTerminalCommandsStayBehindTheAccessGate(t *testing.T) {
	home := t.TempDir()
	setHomeTree(t, home)
	h := newPulseAssistantHandler(t, home)
	proj := t.TempDir()
	h.workDir = proj
	store, err := termtabs.NewStoreAt(filepath.Join(t.TempDir(), "terminals.json"))
	if err != nil {
		t.Fatalf("termtabs.NewStoreAt: %v", err)
	}
	h.termTabsStore = store
	if err := store.Set(proj, termtabs.ProjectTerminals{Terminals: []termtabs.Terminal{{ID: "term-run", Title: "dev"}}}); err != nil {
		t.Fatalf("seed tabs: %v", err)
	}
	seedLiveTerminal(h, "term-run", proj, int32(os.Getpid()))

	h.SetTerminalAccessPolicy(false, false)
	if board := h.pulseBoardSnapshot(); strings.Contains(board, "Running terminals") {
		t.Fatalf("gate closed: board leaked a terminal block:\n%s", board)
	}
	out, err := h.pulseTerminalTabsTool(json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("terminal_tabs: %v", err)
	}
	if strings.Contains(out, `"command"`) || strings.Contains(out, `"running":true`) {
		t.Fatalf("gate closed: terminal_tabs leaked a command: %s", out)
	}

	h.SetTerminalAccessPolicy(false, true)
	if board := h.pulseBoardSnapshot(); !strings.Contains(board, "Running terminals (1 of 1 shown)") {
		t.Fatalf("gate open: running terminal missing from board:\n%s", board)
	}
}
