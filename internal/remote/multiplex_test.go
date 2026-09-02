package remote

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDetectMultiplexer(t *testing.T) {
	cases := []struct {
		stdout string
		want   Multiplexer
	}{
		{"tmux\n", MultiplexerTmux},
		{"screen\n", MultiplexerScreen},
		{"\n", MultiplexerNone},
		{"", MultiplexerNone},
	}
	for _, c := range cases {
		ft := newFakeTransport()
		ft.execDefault = ExecResult{Stdout: c.stdout}
		if got := DetectMultiplexer(ft); got != c.want {
			t.Errorf("DetectMultiplexer(stdout=%q) = %v, want %v", c.stdout, got, c.want)
		}
	}
}

func TestDetectMultiplexerDegradesOnError(t *testing.T) {
	ft := newFakeTransport()
	ft.execErrs[detectMultiplexCmd] = errors.New("boom")
	if got := DetectMultiplexer(ft); got != MultiplexerNone {
		t.Errorf("DetectMultiplexer on error = %v, want MultiplexerNone (never fail the connect over it)", got)
	}
}

func TestSessionNameDeterministic(t *testing.T) {
	a := SessionName("/home/user/project")
	b := SessionName("/home/user/project")
	if a != b {
		t.Fatalf("SessionName not deterministic: %q vs %q", a, b)
	}
	c := SessionName("/home/user/other")
	if a == c {
		t.Fatalf("SessionName collided for different paths: %q", a)
	}
	if len(a) == 0 {
		t.Fatal("empty session name")
	}
}

func TestWrapLaunch(t *testing.T) {
	remoteCmd := "'/home/x/.ocode/bin/1.0/ocode' '/home/x/proj'"
	name := SessionName("/home/x/proj")

	tmux := WrapLaunch(MultiplexerTmux, "/home/x/proj", remoteCmd)
	wantTmux := "tmux new-session -A -s '" + name + "' " + remoteCmd
	if tmux != wantTmux {
		t.Errorf("tmux wrap = %q, want %q", tmux, wantTmux)
	}

	screen := WrapLaunch(MultiplexerScreen, "/home/x/proj", remoteCmd)
	wantScreen := "screen -xRR '" + name + "' " + remoteCmd
	if screen != wantScreen {
		t.Errorf("screen wrap = %q, want %q", screen, wantScreen)
	}

	none := WrapLaunch(MultiplexerNone, "/home/x/proj", remoteCmd)
	if none != remoteCmd {
		t.Errorf("none wrap = %q, want unchanged %q", none, remoteCmd)
	}
}

func TestWrapLaunchReattachesSameSessionAcrossCalls(t *testing.T) {
	// The whole resume story hinges on this: two independent connect
	// invocations to the same remote path must produce the identical
	// launch command, so tmux/screen's "-A"/"-xRR" attaches to the
	// session left running by the first instead of creating a new one.
	first := WrapLaunch(MultiplexerTmux, "/home/x/proj", "cmd")
	second := WrapLaunch(MultiplexerTmux, "/home/x/proj", "cmd")
	if first != second {
		t.Fatalf("launch command not stable across invocations: %q vs %q", first, second)
	}
}

func TestResumeWarning(t *testing.T) {
	if w := ResumeWarning(MultiplexerTmux); w != "" {
		t.Errorf("expected no warning for tmux, got %q", w)
	}
	if w := ResumeWarning(MultiplexerScreen); w != "" {
		t.Errorf("expected no warning for screen, got %q", w)
	}
	if w := ResumeWarning(MultiplexerNone); w == "" {
		t.Errorf("expected a warning for MultiplexerNone, got none")
	}
}

func TestShellQuoteEscapesSingleQuotes(t *testing.T) {
	got := shellQuote(`o'brien`)
	want := `'o'\''brien'`
	if got != want {
		t.Errorf("shellQuote = %q, want %q", got, want)
	}
}

func TestShellQuotePathPreservesTildeExpansion(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"~", `"$HOME"`},
		{"~/.ocode/bin", `"$HOME/.ocode/bin"`},
		{"~/.ocode/bin/0.9.3/ocode", `"$HOME/.ocode/bin/0.9.3/ocode"`},
		{"/home/user/proj", `'/home/user/proj'`}, // absolute: no tilde, quote whole thing
		{"~/o'brien", `"$HOME/o'brien"`},         // single quote is not special inside double quotes
		{`~/$(evil)`, `"$HOME/\$(evil)"`},        // $ must be escaped: no command substitution
		{"~/back`tick", "\"$HOME/back\\`tick\""},
	}
	for _, c := range cases {
		if got := shellQuotePath(c.in); got != c.want {
			t.Errorf("shellQuotePath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestShellQuotePathActuallyExpandsUnderSh(t *testing.T) {
	// Regression guard for the bug this quoting exists to fix: naively
	// single-quoting a "~..." path (shellQuote's behavior) makes POSIX
	// shells treat "~" as a literal character instead of $HOME, so
	// `test -x` (and every other remote command built from RemoteBinDir/
	// RemoteBinaryPath/opts.Path) silently operates on a directory named
	// "~" that never exists. shellQuotePath must not have that problem —
	// verified against a real shell, not just string-compared, since the
	// first fix attempt (bare "~" + single-quoted rest) looked plausible
	// but does not actually expand under either dash or bash.
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh on PATH")
	}
	home := t.TempDir()
	sub := filepath.Join(home, "marker dir with spaces")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	quoted := shellQuotePath("~/marker dir with spaces")
	cmd := exec.Command("sh", "-c", "test -d "+quoted)
	cmd.Env = append(os.Environ(), "HOME="+home)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("sh -c 'test -d %s' (HOME=%s) failed: %v: %s — tilde expansion broken", quoted, home, err, out)
	}
}
