package server

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// mustRenderPulseScreen renders raw into a cols x rows screen and fails the test
// on an emulator error.
func mustRenderPulseScreen(t *testing.T, raw string, cols, rows int) []string {
	t.Helper()
	got, err := renderPulseScreen(raw, cols, rows)
	if err != nil {
		t.Fatalf("renderPulseScreen: %v", err)
	}
	return got
}

// TestRenderPulseScreenAppliesCursorAddressing: a full-screen program draws its
// bottom row first, then moves the cursor home and draws the top row. Splitting on
// newlines would show one mashed line. The screen is the top row, a blank row,
// then the bottom row.
func TestRenderPulseScreenAppliesCursorAddressing(t *testing.T) {
	got := mustRenderPulseScreen(t, "\x1b[3;1Hbottom line\x1b[1;1Htop", 40, 5)
	want := []string{"top", "", "bottom line"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("renderPulseScreen = %q, want %q", got, want)
	}
}

// TestRenderPulseScreenKeepsWideCharactersAligned: a wide character occupies two
// cells, and the continuation cell must not leak a space into the text.
func TestRenderPulseScreenKeepsWideCharactersAligned(t *testing.T) {
	got := mustRenderPulseScreen(t, "中\x1b[1;3Hx", 40, 5)
	want := []string{"中x"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("renderPulseScreen = %q, want %q", got, want)
	}
}

// TestRenderPulseScreenAnswersQueriesWithoutBlocking: a program's device status
// and attribute queries make the emulator write replies into a pipe. An
// undrained pipe would block the write forever, hanging the tool call.
func TestRenderPulseScreenAnswersQueriesWithoutBlocking(t *testing.T) {
	type result struct {
		lines []string
		err   error
	}
	done := make(chan result, 1)
	go func() {
		lines, err := renderPulseScreen("\x1b[6n\x1b[c hello", 40, 5)
		done <- result{lines, err}
	}()
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("renderPulseScreen: %v", r.err)
		}
		if strings.TrimSpace(strings.Join(r.lines, "\n")) != "hello" {
			t.Fatalf("renderPulseScreen = %q, want the text after the queries", r.lines)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("renderPulseScreen blocked on a terminal query reply")
	}
}

// TestRenderPulseScreenDropsTrailingBlankRows: the screen is as tall as the pty,
// but rows below the last text are not returned.
func TestRenderPulseScreenDropsTrailingBlankRows(t *testing.T) {
	got := mustRenderPulseScreen(t, "only", 40, 24)
	if !reflect.DeepEqual(got, []string{"only"}) {
		t.Fatalf("renderPulseScreen = %q, want [only]", got)
	}
	if got := mustRenderPulseScreen(t, "", 40, 24); len(got) != 0 {
		t.Fatalf("empty output rendered %q, want no lines", got)
	}
}

// TestRenderPulseScreenKeepsTheScreenThroughEraseScrollback: ESC[3J erases only the
// scrollback in xterm. x/vt also blanks the visible rows on it, so renderPulseScreen
// drops the sequence first. The visible rows 3, 4, 5 survive, and the home write
// overwrites only the top one.
func TestRenderPulseScreenKeepsTheScreenThroughEraseScrollback(t *testing.T) {
	got := mustRenderPulseScreen(t, "1\r\n2\r\n3\r\n4\r\n5\x1b[3J\x1b[Hx", 10, 3)
	want := []string{"x", "4", "5"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("renderPulseScreen = %q, want %q", got, want)
	}
}
