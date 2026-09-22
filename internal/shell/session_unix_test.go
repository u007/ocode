//go:build !windows

package shell

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
)

// requirePTY skips a test when the host cannot allocate a pseudo-terminal.
// A process already confined by ocode's own sandbox is denied /dev/ptmx, so
// these integration tests skip there rather than fail with an opaque EPERM; the
// framing helpers they build on are covered by session_test.go, which needs no
// pty.
func requirePTY(t *testing.T) {
	t.Helper()
	ptmx, tty, err := pty.Open()
	if err != nil {
		t.Skipf("pty unavailable on this host: %v", err)
	}
	_ = ptmx.Close()
	_ = tty.Close()
}

type testRC struct {
	dir       string
	histPath  string
	shellBase string
	env       []string
}

// writeTestShellRC creates a throwaway rc whose effects are recognisable, and
// the env overrides that point the spawned shell at it. Never the developer's
// real rc: HOME and ZDOTDIR both aim at the temp dir.
func writeTestShellRC(t *testing.T) testRC {
	t.Helper()
	shellPath := Resolve("")
	base := filepath.Base(shellPath)
	if base != "zsh" && base != "bash" {
		t.Skipf("persistent shell session tests need zsh or bash, resolved %q", shellPath)
	}
	dir := t.TempDir()
	hist := filepath.Join(dir, "history")
	rc := testRC{
		dir:       dir,
		histPath:  hist,
		shellBase: base,
		env:       []string{"HOME=" + dir, "ZDOTDIR=" + dir},
	}
	var body string
	if base == "zsh" {
		body = strings.Join([]string{
			"export OCODE_TEST_MARK=zsh",
			"HISTFILE=" + Quote(hist),
			"cc() { print -r -- 'cc-from-rc' }",
			"ocode_fn() { print -r -- 'fn-ok' }",
			"alias ocode_alias='print -r -- alias-ok'",
			"autoload -Uz add-zsh-hook",
			"_ocode_test_precmd() { print -r -- 'PRECMD-SENTINEL' }",
			"_ocode_test_preexec() { print -r -- 'PREEXEC-SENTINEL' }",
			"add-zsh-hook precmd _ocode_test_precmd",
			"add-zsh-hook preexec _ocode_test_preexec",
		}, "\n") + "\n"
		writeFile(t, filepath.Join(dir, ".zshenv"), "export OCODE_TEST_ZSHRC=1\n")
		writeFile(t, filepath.Join(dir, ".zshrc"), body)
	} else {
		body = strings.Join([]string{
			"export OCODE_TEST_MARK=bash",
			"HISTFILE=" + Quote(hist),
			"cc() { echo 'cc-from-rc'; }",
			"ocode_fn() { echo 'fn-ok'; }",
			"alias ocode_alias='echo alias-ok'",
			"_ocode_test_debug() { echo 'PRECMD-SENTINEL'; }",
			"trap _ocode_test_debug DEBUG",
		}, "\n") + "\n"
		writeFile(t, filepath.Join(dir, ".bash_profile"), body)
	}
	return rc
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func newTestSession(t *testing.T, tweak ...func(*SessionOptions)) *Session {
	t.Helper()
	requirePTY(t)
	rc := writeTestShellRC(t)
	opts := SessionOptions{
		Dir:     t.TempDir(),
		Env:     rc.env,
		Timeout: 30 * time.Second,
		Grace:   2 * time.Second,
	}
	for _, f := range tweak {
		f(&opts)
	}
	sess, err := NewSession(opts)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	t.Cleanup(func() {
		if err := sess.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return sess
}

func mustRun(t *testing.T, sess *Session, command string) (Result, string) {
	t.Helper()
	res, cwd, err := sess.Run(t.Context(), command)
	if err != nil {
		t.Fatalf("Run(%q): %v (output %q)", command, err, res.Output)
	}
	return res, cwd
}

// (a) the user's rc functions/aliases are present.
func TestSessionRCFunctionsAvailable(t *testing.T) {
	sess := newTestSession(t)
	probe := "whence -w ocode_fn"
	if filepath.Base(Resolve("")) == "bash" {
		probe = "type -t ocode_fn"
	}
	res, _ := mustRun(t, sess, probe)
	if !strings.Contains(res.Output, "function") {
		t.Errorf("rc function not present: %q", res.Output)
	}
	res2, _ := mustRun(t, sess, "ocode_fn")
	if !strings.Contains(res2.Output, "fn-ok") {
		t.Errorf("calling rc function: %q", res2.Output)
	}
}

// (b) exit code and cwd for success and failure.
func TestSessionExitCodeAndCwd(t *testing.T) {
	dir := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	sess := newTestSession(t, func(o *SessionOptions) { o.Dir = dir })

	res, cwd := mustRun(t, sess, "pwd")
	if res.ExitCode != 0 {
		t.Errorf("pwd exit = %d, want 0", res.ExitCode)
	}
	if strings.TrimSpace(res.Output) != dir {
		t.Errorf("pwd output = %q, want %q", res.Output, dir)
	}
	if cwd != dir {
		t.Errorf("cwd = %q, want %q", cwd, dir)
	}

	res, _ = mustRun(t, sess, "false")
	if res.ExitCode != 1 {
		t.Errorf("false exit = %d, want 1", res.ExitCode)
	}
}

// (c) shell state (cwd, exported vars, functions) persists across Runs.
func TestSessionStatePersists(t *testing.T) {
	sess := newTestSession(t)
	sub := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(sub); err == nil {
		sub = resolved
	}

	mustRun(t, sess, "cd "+Quote(sub))
	res, cwd := mustRun(t, sess, "pwd")
	if strings.TrimSpace(res.Output) != sub || cwd != sub {
		t.Errorf("after cd: output %q cwd %q, want %q", res.Output, cwd, sub)
	}

	mustRun(t, sess, "export OCODE_TEST_VAR=persisted")
	res, _ = mustRun(t, sess, "echo $OCODE_TEST_VAR")
	if strings.TrimSpace(res.Output) != "persisted" {
		t.Errorf("exported var = %q, want persisted", res.Output)
	}
}

// (d) clean output: no prompt text, no ANSI escapes, no CR.
func TestSessionOutputIsClean(t *testing.T) {
	sess := newTestSession(t)
	res, _ := mustRun(t, sess, "printf 'plain'")
	if res.Output != "plain" {
		t.Errorf("output = %q, want %q", res.Output, "plain")
	}
	res, _ = mustRun(t, sess, "printf 'a\\nb\\n'")
	if strings.ContainsAny(res.Output, "\x1b\r") {
		t.Errorf("output carried escape/CR bytes: %q", res.Output)
	}
	if strings.Contains(res.Output, "__OCODE_DONE_") {
		t.Errorf("marker leaked into output: %q", res.Output)
	}
}

// (e) the rc's HISTFILE is neutralised: the history file is never created.
func TestSessionDoesNotWriteHistory(t *testing.T) {
	requirePTY(t)
	rc := writeTestShellRC(t)
	sess, err := NewSession(SessionOptions{Dir: t.TempDir(), Env: rc.env, Timeout: 30 * time.Second})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	mustRun(t, sess, "echo history-check")
	if _, err := os.Stat(rc.histPath); err == nil {
		t.Errorf("history file was created at %s", rc.histPath)
	}
	if err := sess.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(rc.histPath); err == nil {
		t.Errorf("history file appeared after close at %s", rc.histPath)
	}
}

// (f) a command that outlives the timeout is interrupted, and the session keeps
// working afterwards.
func TestSessionTimeoutRecovers(t *testing.T) {
	sess := newTestSession(t, func(o *SessionOptions) {
		o.Timeout = 2 * time.Second
		o.Grace = 3 * time.Second
	})

	start := time.Now()
	res, _, err := sess.Run(t.Context(), "sleep 30")
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if res.ExitCode != 1 {
		t.Errorf("exit = %d, want 1", res.ExitCode)
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("error = %v, want a timeout message", err)
	}
	if elapsed := time.Since(start); elapsed > 15*time.Second {
		t.Errorf("timeout path took %s", elapsed)
	}

	// Either the same shell recovered or the next Run respawned it; both are
	// fine as long as the next command succeeds.
	res, _, err = sess.Run(t.Context(), "echo recovered")
	if err != nil {
		t.Fatalf("next Run after timeout: %v", err)
	}
	if !strings.Contains(res.Output, "recovered") {
		t.Errorf("next Run output = %q", res.Output)
	}
}

// (g) concurrent Runs on one session serialise and each returns its own output.
func TestSessionConcurrentRunsSerialize(t *testing.T) {
	sess := newTestSession(t)
	var wg sync.WaitGroup
	outputs := make([]string, 2)
	errs := make([]error, 2)
	for i := range 2 {
		wg.Go(func() {
			res, _, err := sess.Run(t.Context(), "echo begin-"+strconv.Itoa(i)+"; sleep 0.3; echo end-"+strconv.Itoa(i))
			outputs[i] = res.Output
			errs[i] = err
		})
	}
	wg.Wait()
	for i := range 2 {
		if errs[i] != nil {
			t.Fatalf("run %d: %v", i, errs[i])
		}
		if !strings.Contains(outputs[i], "begin-"+strconv.Itoa(i)) || !strings.Contains(outputs[i], "end-"+strconv.Itoa(i)) {
			t.Errorf("run %d output %q missing its own lines", i, outputs[i])
		}
		if strings.Contains(outputs[i], "begin-"+strconv.Itoa(1-i)) {
			t.Errorf("run %d output %q contains the other run's output", i, outputs[i])
		}
	}
}

// (h) a multi-line command is one result with the last line's status, and an
// unterminated quote fails promptly instead of hanging until the timeout.
func TestSessionMultiLineAndIncompleteInput(t *testing.T) {
	sess := newTestSession(t, func(o *SessionOptions) { o.Timeout = 8 * time.Second })

	res, _ := mustRun(t, sess, "echo one\necho two")
	if !strings.Contains(res.Output, "one") || !strings.Contains(res.Output, "two") {
		t.Errorf("multi-line output = %q", res.Output)
	}
	if res.ExitCode != 0 {
		t.Errorf("multi-line exit = %d, want 0", res.ExitCode)
	}

	res2, _ := mustRun(t, sess, "echo clean")
	if !strings.Contains(res2.Output, "clean") || strings.Contains(res2.Output, "two") {
		t.Errorf("next run leaked the previous command's tail: %q", res2.Output)
	}

	// Unterminated quote: eval reports a syntax error, so a marker follows
	// immediately rather than the shell waiting for a continuation line.
	start := time.Now()
	res3, _, err := sess.Run(t.Context(), "printf 'unterminated")
	if err != nil {
		t.Fatalf("unterminated quote should return a result, got %v", err)
	}
	if res3.ExitCode == 0 {
		t.Errorf("unterminated quote exit = 0, want non-zero (output %q)", res3.Output)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("unterminated quote took %s; it should fail promptly", elapsed)
	}
}

// (i) rc-installed precmd/preexec hooks are silenced.
func TestSessionSilencesRCHooks(t *testing.T) {
	sess := newTestSession(t)
	res, _ := mustRun(t, sess, "echo hookcheck")
	for _, sentinel := range []string{"PRECMD-SENTINEL", "PREEXEC-SENTINEL"} {
		if strings.Contains(res.Output, sentinel) {
			t.Errorf("rc hook output %q leaked into result: %q", sentinel, res.Output)
		}
	}
}

// (j) a cwd containing spaces round-trips intact.
func TestSessionCwdWithSpaces(t *testing.T) {
	sess := newTestSession(t)
	sub := filepath.Join(t.TempDir(), "has space")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if resolved, err := filepath.EvalSymlinks(sub); err == nil {
		sub = resolved
	}
	mustRun(t, sess, "cd "+Quote(sub))
	res, cwd := mustRun(t, sess, "pwd")
	if cwd != sub {
		t.Errorf("cwd = %q, want %q", cwd, sub)
	}
	if strings.TrimSpace(res.Output) != sub {
		t.Errorf("pwd output = %q, want %q", res.Output, sub)
	}
}

// (k) a command carrying the heredoc delimiter as a whole line is rejected and
// nothing is written to the shell.
func TestSessionRejectsDelimiterLine(t *testing.T) {
	sess := newTestSession(t)
	command := "echo before\n" + sess.delim + "\necho after"
	if _, _, err := sess.Run(t.Context(), command); err == nil {
		t.Fatal("expected the delimiter-bearing command to be rejected")
	}
	res, _ := mustRun(t, sess, "echo still-alive")
	if !strings.Contains(res.Output, "still-alive") {
		t.Errorf("session unusable after rejection: %q", res.Output)
	}
}
