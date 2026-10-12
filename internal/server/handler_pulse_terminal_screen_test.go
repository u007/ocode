//go:build !windows

package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/termtabs"
)

// screenTestHandler gives one open tab, and a live registry entry with the given
// pty size when cols and rows are not zero. The history log is written from raw.
func screenTestHandler(t *testing.T, cols, rows uint16, raw string) *Handler {
	t.Helper()
	h := pulseTestHandler(t)
	proj := t.TempDir()
	h.workDir = proj
	h.terminalLoopback = true
	store, err := termtabs.NewStoreAt(filepath.Join(t.TempDir(), "term.json"))
	if err != nil {
		t.Fatal(err)
	}
	h.termTabsStore = store
	if err := store.Set(proj, termtabs.ProjectTerminals{Terminals: []termtabs.Terminal{{ID: "term-1", Title: "claude"}}}); err != nil {
		t.Fatal(err)
	}
	logPath, err := terminalHistoryPath(proj, "term-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	h.terminalProcs.register("term-1", terminalProcEntry{Project: proj, PID: 1, Cols: cols, Rows: rows})
	return h
}

// TestPulseTerminalScreenToolReturnsTheLaidOutScreen: the assistant reads the
// same screen the dashboard row shows, at the pty's reported size.
func TestPulseTerminalScreenToolReturnsTheLaidOutScreen(t *testing.T) {
	h := screenTestHandler(t, 40, 5, "\x1b[3;1Hbottom line\x1b[1;1Htop")
	out := mustPulseTool(t, h, "terminal_screen", `{"terminal_id":"term-1"}`)

	var got struct {
		TerminalID string   `json:"terminal_id"`
		Cols       int      `json:"cols"`
		Rows       int      `json:"rows"`
		Lines      []string `json:"lines"`
		Truncated  bool     `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode %q: %v", out, err)
	}
	if got.TerminalID != "term-1" || got.Cols != 40 || got.Rows != 5 || got.Truncated {
		t.Fatalf("envelope = %+v, want term-1 at 40x5, not truncated", got)
	}
	if want := []string{"top", "", "bottom line"}; !reflect.DeepEqual(got.Lines, want) {
		t.Fatalf("lines = %q, want %q", got.Lines, want)
	}
}

// TestPulseTerminalScreenToolStartsAtAnEscape: the newest 256 KiB window can
// begin inside an escape sequence's tail. Replaying that tail would paint it as
// text, so the window starts at the first ESC in its first page. Here the window
// opens on "2;40HNEW", the tail of "ESC[12;40H", and the first ESC is the
// home-cursor write. The OSC title pad is invisible and fills the rest of the window.
func TestPulseTerminalScreenToolStartsAtAnEscape(t *testing.T) {
	const head = "\x1b[12;40HNEW\x1b[1;1Htop"
	// The window starts 3 bytes in, so the log is pulseTerminalScreenBytes+3 bytes.
	pad := strings.Repeat("x", pulseTerminalScreenBytes+3-len(head)-len("\x1b]0;\x07"))
	raw := head + "\x1b]0;" + pad + "\x07"
	h := screenTestHandler(t, 40, 5, raw)
	out := mustPulseTool(t, h, "terminal_screen", `{"terminal_id":"term-1"}`)

	var got struct {
		Lines []string `json:"lines"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode %q: %v", out, err)
	}
	if want := []string{"top"}; !reflect.DeepEqual(got.Lines, want) {
		t.Fatalf("lines = %q, want %q", got.Lines, want)
	}
}

// TestPulseTerminalScreenTextKeepsAWindowWhoseFirstPageHasNoEscape: the window
// starts at the first ESC in its first 64 KiB page. When that page has none, the
// window is kept whole, even though its first ESC lies past the page.
func TestPulseTerminalScreenTextKeepsAWindowWhoseFirstPageHasNoEscape(t *testing.T) {
	const head = "\x1b[12;40HNEW"
	const tail = "\x1b[1;1Htop"
	// The window starts 3 bytes in, at "2;40HNEW". The only ESC after it is the tail,
	// far past the first page.
	raw := head + strings.Repeat("z", pulseTerminalScreenBytes+3-len(head)-len(tail)) + tail
	h := screenTestHandler(t, 40, 5, raw)
	_, _, _, history, err := h.pulseTerminalScreenText("term-1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(history, "2;40HNEW") {
		t.Fatalf("window starts %q, want it kept from its first byte", history[:min(len(history), 12)])
	}
}

// TestPulseTerminalScreenToolRefusesWithoutAScreen: a terminal with no reported
// size, one that is not live, and a closed gate each fail loudly. None of them
// guesses a size.
func TestPulseTerminalScreenToolRefusesWithoutAScreen(t *testing.T) {
	t.Run("no pty size yet", func(t *testing.T) {
		h := screenTestHandler(t, 0, 0, "output")
		_, err := runPulseTool(t, h, "terminal_screen", `{"terminal_id":"term-1"}`)
		if err == nil || !strings.Contains(err.Error(), "no pty size") {
			t.Fatalf("err = %v, want a no-pty-size refusal", err)
		}
	})
	t.Run("not live", func(t *testing.T) {
		h := screenTestHandler(t, 40, 5, "output")
		h.terminalProcs.unregister("term-1")
		_, err := runPulseTool(t, h, "terminal_screen", `{"terminal_id":"term-1"}`)
		if err == nil || !strings.Contains(err.Error(), "not live") {
			t.Fatalf("err = %v, want a not-live refusal", err)
		}
	})
	t.Run("closed gate", func(t *testing.T) {
		h := screenTestHandler(t, 40, 5, "output")
		h.terminalLoopback = false
		if _, err := runPulseTool(t, h, "terminal_screen", `{"terminal_id":"term-1"}`); err == nil {
			t.Fatal("closed terminal gate still returned a screen")
		}
	})
	t.Run("missing terminal_id", func(t *testing.T) {
		h := screenTestHandler(t, 40, 5, "output")
		if _, err := runPulseTool(t, h, "terminal_screen", `{}`); err == nil {
			t.Fatal("empty terminal_id was accepted")
		}
	})
}
