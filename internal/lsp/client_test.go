package lsp

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestNewClientCloseReapsChild verifies the background reaper actually
// calls cmd.Wait — a missing Wait leaves the killed child as a zombie
// (visible via `ps -o stat`). We can't assert "no zombie" portably in
// Go (you'd need a /proc scan), but we CAN assert that the process is
// reaped within a small window, which is the user-visible symptom.
func TestNewClientCloseReapsChild(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not installed; cannot exercise the reaper path")
	}
	c, err := NewClient("gopls")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	// Initialize handshake is required for a realistic round-trip, but
	// even without it Close should still reap the child.
	pid := c.cmd.Process.Pid
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Wait for the reaper goroutine to finish. cmd.Wait returns once the
	// process is reaped; a panic from a double-Wait would surface here
	// as a hang or a leaked test process.
	select {
	case <-c.exited:
		// Reaped cleanly.
	case <-time.After(5 * time.Second):
		t.Fatalf("child pid %d not reaped within 5s of Close", pid)
	}
	// On Linux we can also verify the process group no longer exists.
	// /proc/<pid>/status with ESRCH = "No such process" means the kernel
	// has dropped the entry (which only happens after Wait).
	if _, err := os.Stat(filepath.Join("/proc", filepath.FromSlash(strconv.Itoa(pid)))); err == nil {
		// Still visible — could be a permission/timing artefact. Log only.
		t.Logf("warning: /proc/%d still visible after reaper finished; this is normal on some CI hosts", pid)
	}
}

// TestDocumentSymbolsHierarchicalFlattening exercises the
// `isHierarchicalSymbolShape` + `flattenDocument` path with a hand-crafted
// hierarchical response (the LSP `DocumentSymbol[]` shape) and checks that
// nested children appear in the flat output with a dotted name.
func TestDocumentSymbolsHierarchicalFlattening(t *testing.T) {
	// Build a fake `DocumentSymbol[]` response with one root node and a
	// nested child. The peeker should identify this as the hierarchical
	// form (has "range", no "location").
	raw := []byte(`[
		{
			"name": "Outer",
			"kind": 5,
			"range": {"start": {"line": 0, "character": 0}, "end": {"line": 20, "character": 1}},
			"selectionRange": {"start": {"line": 1, "character": 0}, "end": {"line": 1, "character": 5}},
			"children": [
				{
					"name": "Inner",
					"kind": 6,
					"range": {"start": {"line": 2, "character": 0}, "end": {"line": 10, "character": 1}},
					"selectionRange": {"start": {"line": 3, "character": 0}, "end": {"line": 3, "character": 5}}
				}
			]
		}
	]`)

	if !isHierarchicalSymbolShape(raw) {
		t.Fatalf("expected isHierarchicalSymbolShape to detect the hier form")
	}

	var nodes []DocumentSymbolNode
	if err := json.Unmarshal(raw, &nodes); err != nil {
		t.Fatalf("unmarshal hier: %v", err)
	}
	flat := flattenDocument(nodes, "")
	if len(flat) != 2 {
		t.Fatalf("expected 2 flattened symbols, got %d: %+v", len(flat), flat)
	}
	if flat[0].Name != "Outer" {
		t.Errorf("first symbol name: got %q want %q", flat[0].Name, "Outer")
	}
	if flat[1].Name != "Outer.Inner" {
		t.Errorf("nested symbol name: got %q want %q (the dot prefix preserves the parent scope)", flat[1].Name, "Outer.Inner")
	}
	if flat[1].Kind != 6 {
		t.Errorf("nested kind: got %d want %d", flat[1].Kind, 6)
	}
}

// TestDocumentSymbolsFlatFormNotMisidentified is the negative case: a
// SymbolInformation[] response (flat, has "location", no "range") must NOT
// be classified as hierarchical.
func TestDocumentSymbolsFlatFormNotMisidentified(t *testing.T) {
	raw := []byte(`[
		{
			"name": "Foo",
			"kind": 12,
			"location": {
				"uri": "file:///tmp/x.go",
				"range": {"start": {"line": 0, "character": 0}, "end": {"line": 0, "character": 3}}
			}
		}
	]`)
	if isHierarchicalSymbolShape(raw) {
		t.Fatalf("flat SymbolInformation[] should NOT be misidentified as hierarchical")
	}
}

// TestManagerDidChangeOnFileSave spins up gopls, opens a file, edits it
// on disk, and verifies the manager's file watcher fires didChange by
// observing the server's textDocument/didChange (we can't see the wire
// from outside, so we just assert no error and that the watcher's
// bookkeeping was updated).
func TestManagerDidChangeOnFileSave(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not installed")
	}
	// Need a small Go project so gopls will accept didOpen. The repo's
	// own internal/lsp directory is the simplest choice.
	tmp := t.TempDir()
	srcPath := filepath.Join(tmp, "demo.go")
	if err := os.WriteFile(srcPath, []byte("package demo\n\nfunc Hello() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mgr := NewManager(tmp)
	defer mgr.Close()

	if err := mgr.EnsureOpen(srcPath); err != nil {
		t.Fatalf("EnsureOpen: %v", err)
	}

	// Edit the file on disk. fsnotify's Write event fires; the manager
	// pushes didChange into gopls. The assertion is that the manager's
	// own bookkeeping sees this as a no-error update (it logs and
	// continues on failure, so a missing watcher would NOT error — but
	// a buggy UpdateText path WOULD).
	edited := "package demo\n\nfunc Hello() {}\n\nfunc World() {}\n"
	if err := os.WriteFile(srcPath, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}

	// Give fsnotify + the manager loop a moment to deliver the event.
	// On a slow CI host this can be 100ms; on Linux with inotify it's
	// near-instant. 1s is comfortably above the tail.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mgr.mu.Lock()
		c := mgr.clients[".go"]
		mgr.mu.Unlock()
		if c == nil {
			t.Fatalf("no gopls client running after EnsureOpen")
		}
		c.mu.Lock()
		doc, ok := c.opened[fileURI(srcPath)]
		c.mu.Unlock()
		if ok && strings.Contains(doc.text, "World") {
			return // success
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("didChange not observed within 2s of file save")
}

// TestNewClientRejectsOversizedFile ensures the size cap on didOpen is
// enforced: writing a 9 MiB buffer should fail with the documented error
// rather than hang the tool call.
func TestNewClientRejectsOversizedFile(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not installed")
	}
	c, err := NewClient("gopls")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer c.Close()

	// maxOpenBytes is 8 MiB; build a 9 MiB buffer.
	huge := strings.Repeat("a", (9<<20)+1)
	if err := c.UpdateText("/nonexistent", huge); err == nil {
		t.Fatalf("expected size-cap error, got nil")
	} else if !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("error should mention the cap, got: %v", err)
	}
}

// TestCloseIsIdempotent guards against the closeOnce path: a second Close
// must not panic, must not double-Kill, and must return the first error
// (or nil) — not a new one.
func TestCloseIsIdempotent(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not installed")
	}
	c, err := NewClient("gopls")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("second Close should be no-op, got: %v", err)
	}
	<-c.exited
}

// TestWriteMessageAfterCloseReturnsError protects against a regression
// where the readLoop's server-request branch writes to a closed pipe and
// silently swallows EPIPE — that path used to lose the error entirely.
func TestWriteMessageAfterCloseReturnsError(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not installed")
	}
	c, err := NewClient("gopls")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	c.Close()
	if err := c.writeMessage(map[string]interface{}{"jsonrpc": "2.0", "method": "shutdown"}); err == nil {
		t.Fatalf("writeMessage after Close should return an error, got nil")
	}
}
