//go:build !windows

package shell

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The prelude and its marker hook are the one part of the shell protocol the
// pty tests would cover but which can also be validated against a real shell
// without a tty: `-n` typechecks the script, and invoking the hook by hand shows
// the exact marker line it emits. This catches a typo'd prelude that would
// otherwise only surface on a host with a pty.

func preludeTestSession(t *testing.T) *Session {
	t.Helper()
	nonce := "preludenonce"
	return &Session{
		nonce:  nonce,
		marker: doneMarker(nonce),
		delim:  commandDelimiter(nonce),
	}
}

func resolvedShellBase(t *testing.T) (string, string) {
	t.Helper()
	path := Resolve("")
	base := filepath.Base(path)
	if base != "zsh" && base != "bash" {
		t.Skipf("prelude tests need zsh or bash, resolved %q", path)
	}
	return path, base
}

func runShellScript(t *testing.T, shellPath, script string) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "script.sh")
	if err := os.WriteFile(file, []byte(script), 0o600); err != nil {
		t.Fatalf("write script: %v", err)
	}
	out, err := exec.Command(shellPath, "-f", file).CombinedOutput()
	if err != nil {
		t.Fatalf("shell %s exited with %v:\n%s", shellPath, err, out)
	}
	return string(out)
}

func TestPreludeIsValidShellSyntax(t *testing.T) {
	shellPath, _ := resolvedShellBase(t)
	sess := preludeTestSession(t)
	// `-n` parses without executing, so a syntax error in the prelude fails here
	// rather than silently during a real session's startup handshake.
	file := filepath.Join(t.TempDir(), "prelude.sh")
	if err := os.WriteFile(file, []byte(sess.prelude()), 0o600); err != nil {
		t.Fatalf("write prelude: %v", err)
	}
	if out, err := exec.Command(shellPath, "-n", file).CombinedOutput(); err != nil {
		t.Fatalf("%s -n rejected the prelude: %v\n%s", shellPath, err, out)
	}
}

func TestPreludeMarkerHookReportsStatusAndCwd(t *testing.T) {
	shellPath, base := resolvedShellBase(t)
	sess := preludeTestSession(t)

	var script string
	if base == "zsh" {
		// `cd` then a failing command, then invoke the hook by hand. `local
		// st=$?` must capture 1, and $PWD must be the new directory.
		script = sess.prelude() + "cd /tmp\nfalse\nprecmd\n"
	} else {
		script = sess.prelude() + "cd /tmp\nfalse\neval \"$PROMPT_COMMAND\"\n"
	}
	out := runShellScript(t, shellPath, script)

	marker := sess.marker
	idx := strings.Index(out, marker)
	if idx < 0 {
		t.Fatalf("marker %q not found in hook output:\n%s", marker, out)
	}
	line := out[idx:]
	if nl := strings.IndexByte(line, '\n'); nl >= 0 {
		line = line[:nl]
	}
	want := marker + " 1 /tmp"
	if line != want {
		t.Errorf("marker line = %q, want %q", line, want)
	}
}

func TestPreludeMarkerHookQuotesCwdWithSpaces(t *testing.T) {
	shellPath, base := resolvedShellBase(t)
	sess := preludeTestSession(t)
	dir := filepath.Join(t.TempDir(), "has space")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	var script string
	if base == "zsh" {
		script = sess.prelude() + "cd " + Quote(dir) + "\ntrue\nprecmd\n"
	} else {
		script = sess.prelude() + "cd " + Quote(dir) + "\ntrue\neval \"$PROMPT_COMMAND\"\n"
	}
	out := runShellScript(t, shellPath, script)

	idx := strings.Index(out, sess.marker)
	if idx < 0 {
		t.Fatalf("marker not found:\n%s", out)
	}
	line := out[idx:]
	if nl := strings.IndexByte(line, '\n'); nl >= 0 {
		line = line[:nl]
	}
	// Everything after the status field is the cwd, so the space survives.
	if !strings.HasSuffix(line, " 0 "+dir) {
		t.Errorf("marker line = %q, want it to end with %q", line, " 0 "+dir)
	}
}

// The prelude must remove rc-installed hooks, not just define precmd(). This is
// the mechanism the pty test for "no hook output in the result" relies on, and
// it can be checked without a tty by inspecting the hook arrays.
func TestPreludeClearsRCHooks(t *testing.T) {
	shellPath, base := resolvedShellBase(t)
	sess := preludeTestSession(t)

	var script string
	if base == "zsh" {
		script = "autoload -Uz add-zsh-hook\n" +
			"_ocode_probe_pre() { : }\n" +
			"_ocode_probe_exec() { : }\n" +
			"add-zsh-hook precmd _ocode_probe_pre\n" +
			"add-zsh-hook preexec _ocode_probe_exec\n" +
			sess.prelude() +
			"printf 'COUNTS:%s:%s\\n' \"${#precmd_functions}\" \"${#preexec_functions}\"\n"
	} else {
		// bash has no add-zsh-hook; the prelude's `trap - DEBUG` is the
		// equivalent, so assert the trap list is empty instead.
		script = "trap '_ocode_probe() { :; }; _ocode_probe' DEBUG\n" +
			sess.prelude() +
			"printf 'TRAP:%s\\n' \"$(trap -p DEBUG)\"\n"
	}
	out := runShellScript(t, shellPath, script)

	if base == "zsh" {
		if !strings.Contains(out, "COUNTS:0:0") {
			t.Errorf("rc hooks were not cleared; output:\n%s", out)
		}
	} else if strings.Contains(out, "TRAP:trap --") {
		t.Errorf("DEBUG trap was not cleared; output:\n%s", out)
	}
}

// The heredoc/eval framing must behave as one shell unit: a two-line command
// runs both lines, and an unterminated quote fails promptly with a non-zero
// status instead of leaving the shell waiting for a continuation line.
func TestFrameCommandRunsThroughRealShell(t *testing.T) {
	shellPath, _ := resolvedShellBase(t)
	delim := commandDelimiter("framecheck")

	framed, err := frameCommand("echo one\necho two", delim)
	if err != nil {
		t.Fatalf("frameCommand: %v", err)
	}
	out := runShellScript(t, shellPath, framed+"printf 'STATUS:%s\\n' \"$?\"\n")
	if !strings.Contains(out, "one") || !strings.Contains(out, "two") {
		t.Errorf("multi-line frame lost a line:\n%s", out)
	}
	if !strings.Contains(out, "STATUS:0") {
		t.Errorf("multi-line frame status not 0:\n%s", out)
	}

	bad, err := frameCommand("printf 'unterminated", delim)
	if err != nil {
		t.Fatalf("frameCommand: %v", err)
	}
	out = runShellScript(t, shellPath, bad+"printf 'STATUS:%s\\n' \"$?\"\n")
	if strings.Contains(out, "STATUS:0") {
		t.Errorf("unterminated quote reported success:\n%s", out)
	}
}

// The startup handshake's ready token must not appear literally in the command
// text. At startup the tty may still echo the command (the prelude's stty -echo
// has been read but not necessarily executed), and an echoed copy of the token
// would let the handshake match the prelude's own marker instead of the
// sentinel's — which desynchronised every Run by one command.
func TestStartupCommandPrintsTokenWithoutLiteral(t *testing.T) {
	shellPath, _ := resolvedShellBase(t)
	nonce := "startupnonce"
	cmd := startupCommand(nonce)
	token := readyToken(nonce)

	if strings.Contains(cmd, token) {
		t.Fatalf("startup command text contains the literal token %q (an echoed copy would confuse the handshake): %q", token, cmd)
	}
	out := strings.TrimSpace(runShellScript(t, shellPath, cmd+"\n"))
	if out != token {
		t.Fatalf("startup command printed %q, want %q", out, token)
	}
}

// PROMPT_SP makes zsh pad a partial line to the full terminal width before
// every prompt — with a 400-column pty that lands as a ~400-space blob appended
// to each result. The prelude must turn it off.
func TestPreludeDisablesPromptSp(t *testing.T) {
	_, base := resolvedShellBase(t)
	if base != "zsh" {
		t.Skip("PROMPT_SP is zsh-only")
	}
	sess := preludeTestSession(t)
	if !strings.Contains(sess.prelude(), "unsetopt prompt_sp") {
		t.Fatalf("prelude does not disable prompt_sp:\n%s", sess.prelude())
	}
}
