//go:build !windows

package server

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/termtabs"
)

// TestPulseLinePagePaging pins the paging arithmetic: a tail page counts offset
// back from the end, a head page counts it forward from the start, and the
// more-before / more-after flags say what lies outside the page.
func TestPulseLinePagePaging(t *testing.T) {
	lines := []string{"a", "b", "c", "d", "e"}
	cases := []struct {
		name       string
		n, offset  int
		head       bool
		want       []string
		wantBefore bool
		wantAfter  bool
	}{
		{"tail default", 2, 0, false, []string{"d", "e"}, true, false},
		{"tail offset pages back", 2, 2, false, []string{"b", "c"}, true, true},
		{"tail reaches the start", 10, 0, false, lines, false, false},
		{"tail offset past start", 2, 9, false, []string{}, false, true},
		{"head default", 2, 0, true, []string{"a", "b"}, false, true},
		{"head offset pages forward", 2, 2, true, []string{"c", "d"}, true, true},
		{"head reaches the end", 10, 3, true, []string{"d", "e"}, true, false},
	}
	for _, c := range cases {
		got, before, after := pulseLinePage(lines, c.n, c.offset, c.head)
		if !reflect.DeepEqual(got, c.want) || before != c.wantBefore || after != c.wantAfter {
			t.Fatalf("%s: got %q before=%v after=%v, want %q before=%v after=%v",
				c.name, got, before, after, c.want, c.wantBefore, c.wantAfter)
		}
	}
}

func TestTerminalReadPagesBackAndFromHead(t *testing.T) {
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
		fmt.Fprintf(&b, "line %d\r\n", i)
	}
	if err := os.WriteFile(logPath, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	type page struct {
		Lines         []string `json:"lines"`
		HasMoreBefore bool     `json:"has_more_before"`
		HasMoreAfter  bool     `json:"has_more_after"`
		Head          bool     `json:"head"`
	}
	read := func(args string) page {
		t.Helper()
		var p page
		if err := json.Unmarshal([]byte(mustPulseTool(t, h, "terminal_read", args)), &p); err != nil {
			t.Fatalf("%s: %v", args, err)
		}
		return p
	}

	// Tail paging: offset 100 with 100 lines is the hundred lines before the last hundred.
	p := read(`{"terminal_id":"term-1","lines":100,"offset":100}`)
	if len(p.Lines) != 100 || p.Lines[0] != "line 100" || p.Lines[99] != "line 199" {
		t.Fatalf("tail offset 100: first %q last %q (%d lines)", p.Lines[0], p.Lines[len(p.Lines)-1], len(p.Lines))
	}
	if !p.HasMoreBefore || !p.HasMoreAfter {
		t.Fatalf("tail offset 100 must report lines on both sides: %+v", p)
	}

	// Head: the first lines of the output, then paging forward from the start.
	p = read(`{"terminal_id":"term-1","lines":3,"head":true}`)
	if !reflect.DeepEqual(p.Lines, []string{"line 0", "line 1", "line 2"}) || !p.Head || p.HasMoreBefore || !p.HasMoreAfter {
		t.Fatalf("head: %+v", p)
	}
	p = read(`{"terminal_id":"term-1","lines":2,"offset":3,"head":true}`)
	if !reflect.DeepEqual(p.Lines, []string{"line 3", "line 4"}) {
		t.Fatalf("head offset 3: %q", p.Lines)
	}

	// A head read of a log wider than one history chunk still starts at the first byte.
	var big strings.Builder
	for i := 0; i < 20000; i++ {
		fmt.Fprintf(&big, "big %05d %s\n", i, strings.Repeat("z", 60))
	}
	if err := os.WriteFile(logPath, []byte(big.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	p = read(`{"terminal_id":"term-1","lines":1,"head":true}`)
	if len(p.Lines) != 1 || !strings.HasPrefix(p.Lines[0], "big 00000 ") || !p.HasMoreAfter {
		t.Fatalf("head of a large log: %+v", p)
	}

	if _, err := runPulseTool(t, h, "terminal_read", `{"terminal_id":"term-1","offset":-1}`); err == nil {
		t.Fatal("negative offset must be rejected")
	}
}

// TestTerminalReadFlagsLineClippedAtWindowEdge: a window holding no newline is a
// single line cut at the window edge. Its bytes are kept (a line-based offset
// cannot reach them otherwise), but line_clipped says the edge is a fragment.
// A window of whole lines never sets it.
func TestTerminalReadFlagsLineClippedAtWindowEdge(t *testing.T) {
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
	type page struct {
		Lines         []string `json:"lines"`
		HasMoreBefore bool     `json:"has_more_before"`
		HasMoreAfter  bool     `json:"has_more_after"`
		LineClipped   bool     `json:"line_clipped"`
	}
	read := func(args string) page {
		t.Helper()
		var p page
		if err := json.Unmarshal([]byte(mustPulseTool(t, h, "terminal_read", args)), &p); err != nil {
			t.Fatalf("%s: %v", args, err)
		}
		return p
	}

	// One newline-free line wider than the window: tail and head both clip it.
	if err := os.WriteFile(logPath, []byte(strings.Repeat("x", pulseTerminalWindowBytes+100)), 0o600); err != nil {
		t.Fatal(err)
	}
	if p := read(`{"terminal_id":"term-1"}`); !p.LineClipped || !p.HasMoreBefore || len(p.Lines) != 1 {
		t.Fatalf("tail of an over-window line must keep the bytes and flag the clip: %+v", p)
	}
	if p := read(`{"terminal_id":"term-1","head":true}`); !p.LineClipped || !p.HasMoreAfter || len(p.Lines) != 1 {
		t.Fatalf("head of an over-window line must keep the bytes and flag the clip: %+v", p)
	}

	// Whole lines: nothing is clipped.
	if err := os.WriteFile(logPath, []byte("alpha\nbeta\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if p := read(`{"terminal_id":"term-1"}`); p.LineClipped {
		t.Fatalf("whole lines must not set line_clipped: %+v", p)
	}
}
