package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/u007/ocode/internal/agent"
	"github.com/u007/ocode/internal/session"
	shellpkg "github.com/u007/ocode/internal/shell"
)

// fakePersistentShell is a pty-free stand-in for *shellpkg.Session. A host
// confined by ocode's own sandbox cannot allocate a pty, so the registry and
// handler contracts (shared state, lazy creation, reset, rekey, rebase, close,
// reaping, fallback) are pinned against this instead.
type fakePersistentShell struct {
	mu       sync.Mutex
	commands []string
	cwd      string
	closed   atomic.Bool
	onRun    func(command string) (shellpkg.Result, string, error)
}

func (f *fakePersistentShell) Run(_ context.Context, command string) (shellpkg.Result, string, error) {
	f.mu.Lock()
	f.commands = append(f.commands, command)
	onRun := f.onRun
	f.mu.Unlock()
	if onRun != nil {
		return onRun(command)
	}
	if path, ok := strings.CutPrefix(command, "cd "); ok {
		f.mu.Lock()
		f.cwd = unquoteShellArg(path)
		cwd := f.cwd
		f.mu.Unlock()
		return shellpkg.Result{Output: "", ExitCode: 0}, cwd, nil
	}
	f.mu.Lock()
	cwd := f.cwd
	f.mu.Unlock()
	return shellpkg.Result{Output: "out:" + command, ExitCode: 0}, cwd, nil
}

func (f *fakePersistentShell) Close() error {
	f.closed.Store(true)
	return nil
}

func (f *fakePersistentShell) Alive() bool { return !f.closed.Load() }

func (f *fakePersistentShell) sawCommand(sub string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.commands {
		if strings.Contains(c, sub) {
			return true
		}
	}
	return false
}

func (f *fakePersistentShell) cmdCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.commands)
}

// unquoteShellArg reverses shellpkg.Quote's single-quote escaping.
func unquoteShellArg(s string) string {
	s = strings.TrimPrefix(s, "'")
	s = strings.TrimSuffix(s, "'")
	return strings.ReplaceAll(s, `'\''`, "'")
}

// newShellRegistryForTest builds a registry over a factory that returns a fresh
// fake per call and records every fake it handed out.
func newShellRegistryForTest(t *testing.T, idle time.Duration, now func() time.Time) (*shellSessionRegistry, *[]*fakePersistentShell) {
	t.Helper()
	var (
		mu    sync.Mutex
		built []*fakePersistentShell
	)
	reg := newShellSessionRegistry(func(opts shellpkg.SessionOptions) (shellSession, error) {
		f := &fakePersistentShell{cwd: opts.Dir}
		mu.Lock()
		built = append(built, f)
		mu.Unlock()
		return f, nil
	}, idle, now)
	t.Cleanup(reg.closeAll)
	return reg, &built
}

func (r *shellSessionRegistry) has(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.entries[key] != nil
}

func postShellJSON(t *testing.T, h *Handler, body map[string]any) (int, map[string]any) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/shell", bytes.NewReader(raw))
	h.HandleShellCommand(rec, req)
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response %q: %v", rec.Body.String(), err)
	}
	return rec.Code, out
}

func shellTestHandler(t *testing.T, reg *shellSessionRegistry) *Handler {
	t.Helper()
	h := NewHandler()
	h.workDir = t.TempDir()
	h.shellSessions = reg
	return h
}

// (a)+(f) two sequential requests with the same session key share one shell,
// and the cwd comes back from it.
func TestHandleShellCommandSharesOneShellPerSession(t *testing.T) {
	reg, built := newShellRegistryForTest(t, time.Hour, time.Now)
	h := shellTestHandler(t, reg)
	work := h.workDir

	_, first := postShellJSON(t, h, map[string]any{"command": "cd /tmp/one", "workDir": work, "session": "tab1"})
	if got := first["cwd"]; got != "/tmp/one" {
		t.Fatalf("first cwd = %v, want /tmp/one", got)
	}
	_, second := postShellJSON(t, h, map[string]any{"command": "pwd", "workDir": work, "session": "tab1"})
	if got := second["cwd"]; got != "/tmp/one" {
		t.Errorf("second cwd = %v, want the cd from the first request", got)
	}
	if len(*built) != 1 {
		t.Fatalf("built %d shells, want 1 shared shell", len(*built))
	}
	if (*built)[0].cmdCount() != 2 {
		t.Errorf("shell saw %d commands, want 2", (*built)[0].cmdCount())
	}
}

// (b) a different session key gets an independent shell.
func TestHandleShellCommandDifferentSessionIsIndependent(t *testing.T) {
	reg, built := newShellRegistryForTest(t, time.Hour, time.Now)
	h := shellTestHandler(t, reg)
	work := h.workDir

	postShellJSON(t, h, map[string]any{"command": "cd /tmp/a", "workDir": work, "session": "tab1"})
	postShellJSON(t, h, map[string]any{"command": "cd /tmp/b", "workDir": work, "session": "tab2"})
	if len(*built) != 2 {
		t.Fatalf("built %d shells, want 2 independent shells", len(*built))
	}
}

// (c) a request without a session key keeps the historical one-shot path.
func TestHandleShellCommandWithoutSessionIsOneShot(t *testing.T) {
	reg, built := newShellRegistryForTest(t, time.Hour, time.Now)
	h := shellTestHandler(t, reg)

	code, out := postShellJSON(t, h, map[string]any{"command": "echo one-shot", "workDir": h.workDir})
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if !strings.Contains(out["output"].(string), "one-shot") {
		t.Errorf("output = %v", out["output"])
	}
	if out["cwd"] != h.workDir {
		t.Errorf("cwd = %v, want %q", out["cwd"], h.workDir)
	}
	if len(*built) != 0 {
		t.Errorf("a session-less request created %d persistent shells", len(*built))
	}
}

// (d) reset:true drops the existing shell and starts a fresh one.
func TestHandleShellCommandResetStartsFreshShell(t *testing.T) {
	reg, built := newShellRegistryForTest(t, time.Hour, time.Now)
	h := shellTestHandler(t, reg)
	work := h.workDir

	postShellJSON(t, h, map[string]any{"command": "cd /tmp/old", "workDir": work, "session": "tab1"})
	_, out := postShellJSON(t, h, map[string]any{"command": "pwd", "workDir": work, "session": "tab1", "reset": true})
	if got := out["cwd"]; got != work {
		t.Errorf("after reset cwd = %v, want the fresh shell's %q", got, work)
	}
	if len(*built) != 2 {
		t.Fatalf("built %d shells, want 2 (reset)", len(*built))
	}
	if !(*built)[0].closed.Load() {
		t.Error("reset did not close the previous shell")
	}
}

// (i) a failing session factory logs and degrades to the one-shot run.
func TestHandleShellCommandFallsBackWhenFactoryFails(t *testing.T) {
	reg := newShellSessionRegistry(func(shellpkg.SessionOptions) (shellSession, error) {
		return nil, errors.New("no pty: operation not permitted")
	}, time.Hour, time.Now)
	t.Cleanup(reg.closeAll)
	h := shellTestHandler(t, reg)

	code, out := postShellJSON(t, h, map[string]any{"command": "echo fallback-ok", "workDir": h.workDir, "session": "tab1"})
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if !strings.Contains(out["output"].(string), "fallback-ok") {
		t.Errorf("fallback output = %v", out["output"])
	}
	if out["cwd"] != h.workDir {
		t.Errorf("fallback cwd = %v, want %q", out["cwd"], h.workDir)
	}
}

// (e)+(j) a host request never touches the local registry, and it carries cwd.
func TestHandleShellCommandRemoteUntouched(t *testing.T) {
	reg, built := newShellRegistryForTest(t, time.Hour, time.Now)
	h := shellTestHandler(t, reg)

	code, _ := postShellJSON(t, h, map[string]any{"command": "echo hi", "host": "nobody@invalid", "workDir": "/srv/app", "session": "tab1"})
	if code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for an unregistered host", code)
	}
	if len(*built) != 0 {
		t.Errorf("a host request created %d local shells", len(*built))
	}
}

// (g) close drops the shell even for an id the session registry does not know.
func TestHandleCloseSessionDropsShellForUnknownID(t *testing.T) {
	reg, built := newShellRegistryForTest(t, time.Hour, time.Now)
	h := shellTestHandler(t, reg)
	work := h.workDir

	postShellJSON(t, h, map[string]any{"command": "echo hi", "workDir": work, "session": "ghost-tab"})
	if !reg.has("ghost-tab") {
		t.Fatal("shell was not created")
	}

	rec := httptest.NewRecorder()
	h.HandleCloseSession(rec, httptest.NewRequest(http.MethodPost, "/api/sessions/ghost-tab/close", nil), "ghost-tab")
	// The session registry does not know this id, so the handler 404s...
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	// ...but the shell must still be gone.
	if reg.has("ghost-tab") {
		t.Error("close left the persistent shell behind")
	}
	if len(*built) != 1 || !(*built)[0].closed.Load() {
		t.Error("close did not close the shell")
	}
}

// (h) the idle reaper drops an unused shell, driven by the injected clock.
func TestShellRegistryReapsIdleShell(t *testing.T) {
	clock := time.Now()
	reg, built := newShellRegistryForTest(t, 30*time.Minute, func() time.Time { return clock })

	if _, _, err := reg.run(t.Context(), "tab1", "/work", "echo hi", false); err != nil {
		t.Fatalf("run: %v", err)
	}
	reg.reap()
	if !reg.has("tab1") {
		t.Fatal("reaper dropped a fresh shell")
	}

	clock = clock.Add(31 * time.Minute)
	reg.reap()
	if reg.has("tab1") {
		t.Error("reaper did not drop the idle shell")
	}
	if !(*built)[0].closed.Load() {
		t.Error("reaped shell was not closed")
	}
}

// (k) a project switch rebases with a cd, and a cd inside one project persists.
func TestShellRegistryProjectSwitchRebases(t *testing.T) {
	reg, built := newShellRegistryForTest(t, time.Hour, time.Now)

	if _, cwd, err := reg.run(t.Context(), "tab1", "/proj/a", "pwd", false); err != nil || cwd != "/proj/a" {
		t.Fatalf("first run: cwd=%q err=%v", cwd, err)
	}
	if (*built)[0].cmdCount() != 1 {
		t.Errorf("first run issued %d commands, want 1 (no rebase)", (*built)[0].cmdCount())
	}

	if _, cwd, err := reg.run(t.Context(), "tab1", "/proj/b", "pwd", false); err != nil || cwd != "/proj/b" {
		t.Fatalf("switch run: cwd=%q err=%v", cwd, err)
	}
	if !(*built)[0].sawCommand("cd '/proj/b'") {
		t.Errorf("project switch did not cd to the new workdir: %v", (*built)[0].commands)
	}

	// Same workdir again: no further rebase.
	before := (*built)[0].cmdCount()
	if _, cwd, err := reg.run(t.Context(), "tab1", "/proj/b", "pwd", false); err != nil || cwd != "/proj/b" {
		t.Fatalf("same-project run: cwd=%q err=%v", cwd, err)
	}
	if (*built)[0].cmdCount() != before+1 {
		t.Errorf("same-project run issued an extra command (rebased again)")
	}
}

// (l) rekey moves the shell so state survives /reset-id.
func TestShellRegistryRekeyMovesShell(t *testing.T) {
	reg, built := newShellRegistryForTest(t, time.Hour, time.Now)

	reg.run(t.Context(), "old", "/work", "cd /tmp/kept", false)
	reg.rekey("old", "new")

	if reg.has("old") {
		t.Error("old key still present after rekey")
	}
	_, cwd, err := reg.run(t.Context(), "new", "/work", "pwd", false)
	if err != nil {
		t.Fatalf("run under new key: %v", err)
	}
	if cwd != "/tmp/kept" {
		t.Errorf("cwd after rekey = %q, want /tmp/kept (state preserved)", cwd)
	}
	if len(*built) != 1 {
		t.Errorf("rekey built a second shell (%d)", len(*built))
	}
}

// (l) HandleResetSessionID must move the registry's shell to the new id.
func TestHandleResetSessionIDMovesPersistentShell(t *testing.T) {
	reg, built := newShellRegistryForTest(t, time.Hour, time.Now)
	h := NewHandler()
	h.shellSessions = reg
	proj := t.TempDir()
	h.projects = newTestProjectStore(t, proj)
	h.SetWorkDir(proj)

	id := session.NewSessionID()
	session.SetWorkDir(proj)
	t.Cleanup(func() { session.SetWorkDir("") })
	if err := session.Save(id, "Shell", []agent.Message{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := h.sessions.BindNewOrVerify(id, proj, ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := reg.run(t.Context(), id, proj, "cd /tmp/pre-reset", false); err != nil {
		t.Fatalf("seed shell: %v", err)
	}

	rec := httptest.NewRecorder()
	h.HandleResetSessionID(rec, httptest.NewRequest(http.MethodPost, "/api/sessions/"+id+"/reset-id", nil), id)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	var raw map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	newID := raw["new_id"]
	if reg.has(id) {
		t.Error("old key still present after /reset-id")
	}
	_, cwd, err := reg.run(t.Context(), newID, proj, "pwd", false)
	if err != nil {
		t.Fatalf("run under new id: %v", err)
	}
	if cwd != "/tmp/pre-reset" {
		t.Errorf("cwd after /reset-id = %q, want /tmp/pre-reset", cwd)
	}
	if len(*built) != 1 {
		t.Errorf("/reset-id built a second shell (%d)", len(*built))
	}
}
