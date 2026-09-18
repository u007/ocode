//go:build !windows

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestOSCTitleScanner(t *testing.T) {
	tests := []struct {
		name  string
		feeds []string
		want  string
		ok    bool
	}{
		{name: "osc 0 bel", feeds: []string{"\x1b]0;my shell\x07"}, want: "my shell", ok: true},
		{name: "osc 2 st", feeds: []string{"\x1b]2;vim /tmp/x\x1b\\"}, want: "vim /tmp/x", ok: true},
		{name: "ignores osc 7 cwd", feeds: []string{"\x1b]7;file://host/tmp\x07"}},
		{name: "non-st escape aborts partial title", feeds: []string{"\x1b]2;a\x1b[31mb\x07"}},
		{name: "second osc title replaces first", feeds: []string{"\x1b]0;one\x07", "\x1b]0;two\x07"}, want: "two", ok: true},
		{name: "split across feeds", feeds: []string{"\x1b]0;he", "llo wor", "ld\x07"}, want: "hello world", ok: true},
		{name: "split at escape", feeds: []string{"\x1b]2;title\x1b", "\\"}, want: "title", ok: true},
		{name: "newlines collapse", feeds: []string{"\x1b]0;  a\nb\tc  \x07"}, want: "a b c", ok: true},
		{name: "control chars stripped", feeds: []string{"\x1b]0;a\x01\x02b\x07"}, want: "a b", ok: true},
		{name: "empty title clears", feeds: []string{"\x1b]0;\x07"}, want: "", ok: true},
		{name: "no terminator yields nothing", feeds: []string{"\x1b]0;pending"}},
		{name: "plain output yields nothing", feeds: []string{"$ ls -la\r\n"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var sc oscTitleScanner
			var (
				got   string
				gotOk bool
			)
			for _, f := range tc.feeds {
				if title, ok := sc.feed([]byte(f)); ok {
					got, gotOk = title, true
				}
			}
			if gotOk != tc.ok || got != tc.want {
				t.Fatalf("feed(%q) = (%q, %v), want (%q, %v)", tc.feeds, got, gotOk, tc.want, tc.ok)
			}
		})
	}
}

func TestOSCTitleScannerTruncates(t *testing.T) {
	long := strings.Repeat("x", terminalTitleMaxRunes+40)
	var sc oscTitleScanner
	got, ok := sc.feed([]byte("\x1b]0;" + long + "\x07"))
	if !ok {
		t.Fatal("expected a completed title")
	}
	if len([]rune(got)) != terminalTitleMaxRunes {
		t.Fatalf("title length = %d, want %d", len([]rune(got)), terminalTitleMaxRunes)
	}
}

// TestTerminalSessionRecordsOSCTitle proves the pty output path stores the
// program-set title on the session, which is what GET /api/terminal returns.
func TestTerminalSessionRecordsOSCTitle(t *testing.T) {
	dir := t.TempDir()
	sess := newTerminalSession("term-title", dir, &exec.Cmd{}, nil, 0, func(*terminalSession) {})
	t.Cleanup(sess.history.remove)

	sess.recordLocked([]byte("\x1b]0;james@host: ~/www/app\x07"))
	if got := sess.snapshot().Title; got != "james@host: ~/www/app" {
		t.Fatalf("snapshot title = %q, want %q", got, "james@host: ~/www/app")
	}

	// A later OSC 2 replaces it; the scanner state carries across chunks.
	sess.recordLocked([]byte("\x1b]2;oc"))
	sess.recordLocked([]byte("ode\x07"))
	if got := sess.snapshot().Title; got != "ocode" {
		t.Fatalf("snapshot title after replace = %q, want %q", got, "ocode")
	}
}

// TestHandleTerminalListReturnsOSCTitle closes the loop from pty bytes to the
// inventory JSON the sidebar reads: the row no longer falls back to the id.
func TestHandleTerminalListReturnsOSCTitle(t *testing.T) {
	dir := t.TempDir()
	h := NewHandler()
	h.workDir = dir
	h.SetTerminalAccessPolicy(false, true)

	seedTerminalSession(t, h.terminalSessions, "term-title", dir, 4242, time.Now())
	sess := h.terminalSessions.lookup("term-title")
	if sess == nil {
		t.Fatal("seeded session missing")
	}
	sess.recordLocked([]byte("\x1b]2;my-shell \x07"))

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/terminal?project="+url.QueryEscape(dir), nil)
	h.HandleTerminalList(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var body struct {
		Terminals []terminalListEntry `json:"terminals"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Terminals) != 1 || body.Terminals[0].Title != "my-shell" {
		t.Fatalf("terminals = %+v, want one with title %q", body.Terminals, "my-shell")
	}
}
