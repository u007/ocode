package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/u007/ocode/internal/lsp"
)

func lspTestDiag(uri, path, msg string, line int) lsp.Diagnostic {
	return lsp.Diagnostic{
		URI: uri, Path: path, Severity: lsp.SeverityError, Message: msg,
		Range: lsp.Range{Start: lsp.Position{Line: line}},
	}
}

// The agent never emits diagnostics as a system-role message: system-role
// tail messages are hoisted into the cached system block by every provider
// builder and would bust the cache on each change.
func TestInjectLSPDelta_UserRoleOnlyWhenChanged(t *testing.T) {
	mgr := lsp.NewManager(t.TempDir())
	a := NewAgent(nil, nil, nil, mgr)
	base := []Message{
		{Role: "system", Content: "STABLE-SYS"},
		{Role: "user", Content: "hello"},
	}

	// Empty store → no block.
	out := a.injectLSPDelta(base)
	if len(out) != len(base) {
		t.Fatalf("empty store appended a block: %d msgs", len(out))
	}

	uri := "file:///tmp/a.go"
	mgr.Diagnostics().SetURI(uri, []lsp.Diagnostic{lspTestDiag(uri, "/tmp/a.go", "undefined: x", 3)})

	out = a.injectLSPDelta(base)
	if len(out) != len(base)+1 {
		t.Fatalf("changed store did not append a block: %d msgs", len(out))
	}
	last := out[len(out)-1]
	if last.Role != "user" {
		t.Fatalf("delta role = %q, want user", last.Role)
	}
	if !strings.HasPrefix(last.Content, lspMarker) || !strings.Contains(last.Content, "undefined: x") {
		t.Fatalf("delta content unexpected:\n%s", last.Content)
	}
	for _, m := range out[:len(base)] {
		if m.Role == "system" && strings.Contains(m.Content, "undefined: x") {
			t.Fatal("diagnostics leaked into a system-role message")
		}
	}

	// Same store on the next call → nothing new → no block (cache invariant).
	out = a.injectLSPDelta(base)
	if len(out) != len(base) {
		t.Fatalf("unchanged store re-emitted the block: %d msgs", len(out))
	}

	// File cleared → reported once as clean, then quiet.
	mgr.Diagnostics().SetURI(uri, nil)
	out = a.injectLSPDelta(base)
	if len(out) != len(base)+1 || !strings.Contains(out[len(out)-1].Content, "/tmp/a.go: clean") {
		t.Fatalf("resolved file not reported:\n%+v", out[len(out)-1:])
	}
	out = a.injectLSPDelta(base)
	if len(out) != len(base) {
		t.Fatalf("resolved file re-reported: %d msgs", len(out))
	}
}

func TestInjectLSPDelta_NilManagerIsNoop(t *testing.T) {
	a := NewAgent(nil, nil, nil, nil)
	base := []Message{{Role: "user", Content: "hi"}}
	if out := a.injectLSPDelta(base); len(out) != 1 {
		t.Fatalf("nil manager appended: %d", len(out))
	}
}

func TestAppendEditDiagnostics_AttachesFreshAndSuppressesDelta(t *testing.T) {
	mgr := lsp.NewManager(t.TempDir())
	a := NewAgent(nil, nil, nil, mgr)
	uri := "file:///tmp/b.go"
	diags := []lsp.Diagnostic{lspTestDiag(uri, "/tmp/b.go", "missing return", 9)}
	var gotPath string
	a.lspWait = func(ctx context.Context, path string, since time.Time) ([]lsp.Diagnostic, bool, error) {
		gotPath = path
		return diags, true, nil
	}
	args := json.RawMessage(`{"path":"/tmp/b.go","search":"a","replace":"b"}`)

	res := a.appendEditDiagnostics("edit", args, "edited", time.Now())
	if gotPath != "/tmp/b.go" {
		t.Fatalf("waiter path = %q", gotPath)
	}
	if !strings.HasPrefix(res, "edited\n\n") || !strings.Contains(res, "missing return") {
		t.Fatalf("result missing diagnostics:\n%s", res)
	}

	// The attachment already showed this set: the delta must not repeat it.
	mgr.Diagnostics().SetURI(uri, diags)
	base := []Message{{Role: "user", Content: "hi"}}
	if out := a.injectLSPDelta(base); len(out) != 1 {
		t.Fatalf("delta repeated diagnostics already attached to the tool result")
	}
}

func TestAppendEditDiagnostics_SkipsNonEditNoServerAndStale(t *testing.T) {
	a := NewAgent(nil, nil, nil, lsp.NewManager(t.TempDir()))
	calls := 0
	a.lspWait = func(ctx context.Context, path string, since time.Time) ([]lsp.Diagnostic, bool, error) {
		calls++
		return nil, false, nil
	}
	if res := a.appendEditDiagnostics("read", json.RawMessage(`{"path":"/tmp/x.go"}`), "r", time.Now()); res != "r" || calls != 0 {
		t.Fatalf("read tool consulted LSP: res=%q calls=%d", res, calls)
	}
	// No fresh publish within the wait → result untouched.
	if res := a.appendEditDiagnostics("write", json.RawMessage(`{"path":"/tmp/x.go"}`), "w", time.Now()); res != "w" {
		t.Fatalf("stale publish altered result: %q", res)
	}
	a.lspWait = func(ctx context.Context, path string, since time.Time) ([]lsp.Diagnostic, bool, error) {
		return nil, false, errors.New(`language server "gopls" not found in PATH`)
	}
	if res := a.appendEditDiagnostics("write", json.RawMessage(`{"path":"/tmp/x.go"}`), "w", time.Now()); res != "w" {
		t.Fatalf("server error altered result: %q", res)
	}
	// Clean fresh publish → nothing appended.
	a.lspWait = func(ctx context.Context, path string, since time.Time) ([]lsp.Diagnostic, bool, error) {
		return nil, true, nil
	}
	if res := a.appendEditDiagnostics("multiedit", json.RawMessage(`{"file_path":"/tmp/x.go"}`), "m", time.Now()); res != "m" {
		t.Fatalf("clean publish altered result: %q", res)
	}
}
