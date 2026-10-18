//go:build !windows

package server

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/termtabs"
)

// TestTerminalReadTool lives here rather than in pulse_tools_test.go because it
// seeds a terminal history file via terminalHistoryPath, which is Unix-only
// (terminal_history.go is //go:build !windows). Keeping it in the untagged file
// broke `GOOS=windows go vet ./internal/server/` with an undefined symbol.
func TestTerminalReadTool(t *testing.T) {
	h := pulseTestHandler(t)
	proj := t.TempDir()
	h.workDir = proj
	h.terminalLoopback = true
	store, err := termtabs.NewStoreAt(filepath.Join(t.TempDir(), "term.json"))
	if err != nil {
		t.Fatal(err)
	}
	h.termTabsStore = store
	if err := store.Set(proj, termtabs.ProjectTerminals{Terminals: []termtabs.Terminal{{ID: "term-1", Title: "dev"}}}); err != nil {
		t.Fatal(err)
	}
	logPath, err := terminalHistoryPath(proj, "term-1")
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for i := 0; i < 300; i++ {
		fmt.Fprintf(&b, "\x1b[32mline %d\x1b[0m\r\n", i)
	}
	if err := os.WriteFile(logPath, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	var out struct {
		TerminalID string   `json:"terminal_id"`
		Project    string   `json:"project"`
		Lines      []string `json:"lines"`
		Truncated  bool     `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(mustPulseTool(t, h, "terminal_read", `{"terminal_id":"term-1"}`)), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Lines) != pulseTerminalDefaultLines || out.Lines[0] != "line 200" || out.Lines[99] != "line 299" || out.Project != proj {
		t.Fatalf("default read: %d lines, first %q last %q project %q", len(out.Lines), out.Lines[0], out.Lines[len(out.Lines)-1], out.Project)
	}
	if err := json.Unmarshal([]byte(mustPulseTool(t, h, "terminal_read", `{"terminal_id":"term-1","lines":2}`)), &out); err != nil || len(out.Lines) != 2 || out.Lines[0] != "line 298" {
		t.Fatalf("lines=2 -> %q (err %v)", out.Lines, err)
	}
	for _, bad := range []string{`{"terminal_id":"term-1","lines":0}`, `{"terminal_id":"term-1","lines":1001}`, `{}`, `{"terminal_id":"nope"}`, `{"terminal_id":"anon-1"}`} {
		if _, err := runPulseTool(t, h, "terminal_read", bad); err == nil {
			t.Fatalf("%s must be rejected", bad)
		}
	}

	// Access gate mirrors the history endpoint.
	h.terminalLoopback = false
	if _, err := runPulseTool(t, h, "terminal_read", `{"terminal_id":"term-1"}`); err == nil {
		t.Fatal("terminal access gate must apply")
	}
	h.terminalLoopback = true

	// A terminal of an unregistered project is refused.
	other := t.TempDir()
	if err := store.Set(other, termtabs.ProjectTerminals{Terminals: []termtabs.Terminal{{ID: "term-x"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := runPulseTool(t, h, "terminal_read", `{"terminal_id":"term-x"}`); err == nil {
		t.Fatal("terminal outside the served projects must be refused")
	}

	// A huge log is read through its tail window and still fits the cap.
	bigPath, err := terminalHistoryPath(proj, "term-1")
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(bigPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20000; i++ {
		fmt.Fprintf(f, "big %05d %s\n", i, strings.Repeat("z", 60))
	}
	f.Close()
	if err := json.Unmarshal([]byte(mustPulseTool(t, h, "terminal_read", `{"terminal_id":"term-1","lines":1000}`)), &out); err != nil {
		t.Fatal(err)
	}
	if !out.Truncated || out.Lines[len(out.Lines)-1] != "big 19999 "+strings.Repeat("z", 60) {
		t.Fatalf("truncated=%v last=%q", out.Truncated, out.Lines[len(out.Lines)-1])
	}
}
