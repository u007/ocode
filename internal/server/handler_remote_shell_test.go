package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/u007/ocode/internal/remote"
)

// installCountingFakeSSH is installFakeSSH plus an invocation log: the `host`
// routing assertion needs to prove the command went through ssh at all, and a
// shim that silently runs everything locally cannot distinguish "routed to the
// remote" from "ran on this machine". Returns the log path.
func installCountingFakeSSH(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "ssh-invocations")
	bin := filepath.Join(dir, "ssh")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> " + logPath + "\n" +
		"args=()\n" +
		"for a in \"$@\"; do args+=(\"$a\"); done\n" +
		"cmd=\"${args[${#args[@]}-1]}\"\n" +
		"exec /bin/sh -c \"$cmd\"\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake ssh: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return logPath
}

func readSSHInvocations(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatalf("read ssh invocation log: %v", err)
	}
	return string(data)
}

// postShell issues POST /api/shell with the given body and decodes the result.
func postShell(t *testing.T, h *Handler, body string) (int, map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/shell", bytes.NewBufferString(body))
	r.Header.Set("Content-Type", "application/json")
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/shell", h.HandleShellCommand)
	mux.ServeHTTP(w, r)
	var out map[string]any
	if w.Body.Len() > 0 {
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode response %q: %v", w.Body.String(), err)
		}
	}
	return w.Code, out
}

// The reported bug: a `!` command in a remote project ran with the local
// machine's $SHELL, so a Linux remote failed with
// `fork/exec /bin/zsh: no such file or directory`. With a host the command now
// runs on the remote, through a shell the REMOTE resolves.
//
// The fake ssh shim runs the command locally, so the assertion is that the
// remote command string is built with the remote login-shell wrapper (never a
// raw local $SHELL) and returns the expected output.
func TestRemoteShellCommandRunsOnHost(t *testing.T) {
	logPath := installCountingFakeSSH(t)
	project := t.TempDir()
	h := newTestHandlerWithRemote(t, "ci.local", project)

	// A stale LOCAL $SHELL must not reach the remote command: the wrapper
	// resolves the shell on the far side.
	t.Setenv("SHELL", "/nonexistent-xyz/zsh")

	code, out := postShell(t, h, `{"command":"echo remote-ok","workDir":"`+project+`","host":"ci.local"}`)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %v)", code, out)
	}
	if got, _ := out["exitCode"].(float64); got != 0 {
		t.Fatalf("exitCode = %v, want 0 (output %q, error %q)", out["exitCode"], out["output"], out["error"])
	}
	output, _ := out["output"].(string)
	if !bytes.Contains([]byte(output), []byte("remote-ok")) {
		t.Fatalf("output = %q, want it to contain remote-ok", output)
	}

	// The command must have actually been routed through ssh. Without this the
	// test passes even when `host` is ignored and the command runs locally.
	invocations := readSSHInvocations(t, logPath)
	if invocations == "" {
		t.Fatal("no ssh invocation: the command was not routed to the remote host")
	}
	if !bytes.Contains([]byte(invocations), []byte("ci.local")) {
		t.Fatalf("ssh invocation did not target the host: %q", invocations)
	}
	// The remote command carries the remote-side shell fallback chain, never
	// the local $SHELL verbatim.
	if !bytes.Contains([]byte(invocations), []byte(`[ -x "$c" ]`)) {
		t.Fatalf("remote command lacks remote shell resolution: %q", invocations)
	}
	if bytes.Contains([]byte(invocations), []byte("/nonexistent-xyz/zsh --")) {
		t.Fatalf("local $SHELL leaked into the remote command: %q", invocations)
	}
}

// A local `!` command (no host) must NOT go through ssh.
func TestLocalShellCommandDoesNotUseSSH(t *testing.T) {
	logPath := installCountingFakeSSH(t)
	h := NewHandler()
	h.SetWorkDir(t.TempDir())

	code, out := postShell(t, h, `{"command":"echo local-only"}`)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if got, _ := out["exitCode"].(float64); got != 0 {
		t.Fatalf("exitCode = %v, want 0 (error %q)", out["exitCode"], out["error"])
	}
	if invocations := readSSHInvocations(t, logPath); invocations != "" {
		t.Fatalf("local command unexpectedly used ssh: %q", invocations)
	}
}

// A host that is not a registered remote project must be rejected — the
// endpoint must never become a way to run commands on an arbitrary ssh target.
func TestRemoteShellCommandRejectsUnregisteredHost(t *testing.T) {
	installFakeSSH(t)
	h := NewHandler()
	h.SetWorkDir(t.TempDir())

	code, _ := postShell(t, h, `{"command":"echo hi","workDir":"/tmp","host":"evil.example.com"}`)
	if code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d for an unregistered host", code, http.StatusForbidden)
	}
}

// A command that exits non-zero on the remote is reported through exitCode,
// not as a transport error, mirroring the local contract.
func TestRemoteShellCommandReportsExitCode(t *testing.T) {
	installFakeSSH(t)
	project := t.TempDir()
	h := newTestHandlerWithRemote(t, "ci.local", project)

	code, out := postShell(t, h, `{"command":"exit 7","workDir":"`+project+`","host":"ci.local"}`)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if got, _ := out["exitCode"].(float64); got != 7 {
		t.Fatalf("exitCode = %v, want 7", out["exitCode"])
	}
	if out["error"] != "" {
		t.Fatalf("error = %v, want empty for a plain non-zero exit", out["error"])
	}
}

// The local path is unchanged when no host is supplied: workDir still selects
// the shell's directory and the command runs locally.
func TestLocalShellCommandUnchanged(t *testing.T) {
	dir := t.TempDir()
	h := NewHandler()
	h.SetWorkDir(dir)

	code, out := postShell(t, h, `{"command":"echo local-ok"}`)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if got, _ := out["exitCode"].(float64); got != 0 {
		t.Fatalf("exitCode = %v, want 0 (error %q)", out["exitCode"], out["error"])
	}
	if output, _ := out["output"].(string); !bytes.Contains([]byte(output), []byte("local-ok")) {
		t.Fatalf("output = %q, want it to contain local-ok", output)
	}
}

// An empty command is rejected before any host resolution happens.
func TestShellCommandRequiresCommand(t *testing.T) {
	h := NewHandler()
	code, _ := postShell(t, h, `{"command":""}`)
	if code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", code, http.StatusBadRequest)
	}
}

// getTerminalConfigHost issues GET /api/config/terminal with a host and
// decodes the response.
func getTerminalConfigHost(t *testing.T, h *Handler, host, project string) (int, map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/config/terminal?host="+host+"&project="+project, nil)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/config/terminal", h.HandleGetTerminalConfig)
	mux.ServeHTTP(w, r)
	var out map[string]any
	if w.Body.Len() > 0 {
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode %q: %v", w.Body.String(), err)
		}
	}
	return w.Code, out
}

// stubRemoteShellProbe replaces the shell probe with canned stdout, so
// terminal-config tests never ssh. Returns a restore func.
func stubRemoteShellTransport(t *testing.T, stdout string) func() {
	t.Helper()
	prev := remoteShellProbeFn
	remoteShellProbeFn = func(context.Context, remote.Target) (string, error) {
		return stdout, nil
	}
	return func() { remoteShellProbeFn = prev }
}

// A remote project's terminal config must describe the REMOTE's shells, not
// this machine's: the local /etc/shells would offer the picker a shell the
// remote cannot run. The probe is stubbed so the test never ssh-es.
func TestTerminalConfigRemoteUsesRemoteShells(t *testing.T) {
	restore := stubRemoteShellTransport(t, "shell=/bin/bash\navailable=/bin/bash\navailable=/bin/dash\n")
	defer restore()

	project := t.TempDir()
	h := newTestHandlerWithRemote(t, "ci.local", project)

	code, out := getTerminalConfigHost(t, h, "ci.local", project)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %v)", code, out)
	}
	if out["remote"] != true {
		t.Fatalf("remote = %v, want true", out["remote"])
	}
	if out["default_shell"] != "/bin/bash" {
		t.Fatalf("default_shell = %v, want the remote's /bin/bash", out["default_shell"])
	}
	shells, _ := out["available_shells"].([]any)
	if len(shells) != 2 || shells[0] != "/bin/bash" {
		t.Fatalf("available_shells = %v, want the remote's list", shells)
	}
	// No per-host override is offered: the remote selects its own shell.
	if out["shell"] != "" {
		t.Fatalf("shell = %v, want empty (remote default)", out["shell"])
	}
}

// A remote shell probe failure must degrade to /bin/sh rather than failing the
// endpoint — the Settings panel still needs to render.
func TestTerminalConfigRemoteProbeFailureDegrades(t *testing.T) {
	restore := stubRemoteShellTransport(t, "")
	defer restore()

	project := t.TempDir()
	h := newTestHandlerWithRemote(t, "ci.local", project)

	code, out := getTerminalConfigHost(t, h, "ci.local", project)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if out["default_shell"] != "/bin/sh" {
		t.Fatalf("default_shell = %v, want the /bin/sh fallback", out["default_shell"])
	}
}

// An unregistered host is rejected, the same admission rule every other remote
// endpoint applies.
func TestTerminalConfigRemoteRejectsUnregisteredHost(t *testing.T) {
	project := t.TempDir()
	h := NewHandler()
	h.SetWorkDir(project)
	code, _ := getTerminalConfigHost(t, h, "evil.example.com", project)
	if code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", code, http.StatusForbidden)
	}
}

// Without a host the endpoint keeps reporting the local machine's shells.
func TestTerminalConfigLocalUnchanged(t *testing.T) {
	h := NewHandler()
	h.SetWorkDir(t.TempDir())
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/config/terminal", nil)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/config/terminal", h.HandleGetTerminalConfig)
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out["default_shell"] == "" {
		t.Fatal("default_shell is empty for a local request")
	}
	if _, ok := out["remote"]; ok {
		t.Fatal("local response should not claim to be remote")
	}
}
