package remote

import (
	"errors"
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
