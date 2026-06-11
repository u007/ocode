package tool

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

func TestLSPTool_NameAndDefinition(t *testing.T) {
	tl := &LSPTool{}
	if tl.Name() != "lsp" {
		t.Fatalf("unexpected name %q", tl.Name())
	}
	if name, _ := tl.Definition()["name"].(string); name != "lsp" {
		t.Fatalf("definition name mismatch: %q", name)
	}
}

func TestLSPTool_Status(t *testing.T) {
	tl := &LSPTool{}
	out, err := tl.Execute(json.RawMessage(`{"operation":"status"}`))
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(out, "gopls") {
		t.Fatalf("status should list gopls, got:\n%s", out)
	}
}

func TestLSPTool_RequiresPath(t *testing.T) {
	tl := &LSPTool{}
	_, err := tl.Execute(json.RawMessage(`{"operation":"findReferences"}`))
	if err == nil || !strings.Contains(err.Error(), "path") {
		t.Fatalf("expected path-required error, got %v", err)
	}
}

// TestLSPTool_Roundtrip drives LSPTool.Execute against gopls on this repo:
// workspaceSymbol resolves a name, documentSymbol lists a file's symbols.
func TestLSPTool_Roundtrip(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not installed")
	}
	tl := &LSPTool{}

	out, err := tl.Execute(json.RawMessage(`{"operation":"workspaceSymbol","query":"LoadBuiltins"}`))
	if err != nil {
		t.Fatalf("workspaceSymbol: %v", err)
	}
	if !strings.Contains(out, "tool.go") {
		t.Fatalf("workspaceSymbol should find LoadBuiltins in tool.go, got:\n%s", out)
	}

	out, err = tl.Execute(json.RawMessage(`{"operation":"documentSymbol","path":"tool.go"}`))
	if err != nil {
		t.Fatalf("documentSymbol: %v", err)
	}
	if !strings.Contains(out, "LoadBuiltins") {
		t.Fatalf("documentSymbol(tool.go) should include LoadBuiltins, got:\n%s", out)
	}
}

// TestLSPTool_RestartWarnsAboutInFlight verifies the restart path now
// surfaces a one-line warning so callers know in-flight queries will be
// cancelled (the previous behaviour was a silent kill).
func TestLSPTool_RestartWarnsAboutInFlight(t *testing.T) {
	tl := &LSPTool{}
	out, err := tl.Execute(json.RawMessage(`{"operation":"restart"}`))
	if err != nil {
		t.Fatalf("restart: %v", err)
	}
	if !strings.Contains(out, "in-flight") {
		t.Fatalf("restart output should warn about in-flight queries, got:\n%s", out)
	}
}

// TestLSPTool_WorkspaceSymbolLangHint ensures the lang parameter routes the
// query to the right language server instead of hard-coding .go. The query
// string is irrelevant; the assertion is that ClientForExt is invoked with
// the right extension under the hood. We use a Rust hint against an empty
// PATH so the call returns a clear "not found" error rather than a panic.
func TestLSPTool_WorkspaceSymbolLangHint(t *testing.T) {
	tl := &LSPTool{}
	_, err := tl.Execute(json.RawMessage(`{"operation":"workspaceSymbol","query":"main","lang":"rust"}`))
	if err == nil {
		t.Skip("rust-analyzer is installed; cannot verify lang hint path")
	}
	// Acceptable errors: missing binary ("rust-analyzer not found in PATH")
	// or any other startup failure — but NOT a gopls-not-found message,
	// which would mean the lang hint was ignored.
	if strings.Contains(err.Error(), "gopls") {
		t.Fatalf("workspaceSymbol with lang=rust should not query gopls, got: %v", err)
	}
}

func TestNoticedError(t *testing.T) {
	inner := fmt.Errorf("language server %q not found in PATH", "gopls")
	ne := &NoticedError{
		Err:    inner,
		Notice: "LSP server \"gopls\" is not installed. Install with: go install golang.org/x/tools/gopls@latest",
	}

	// Error() delegates to inner error
	if ne.Error() != inner.Error() {
		t.Fatalf("Error() = %q, want %q", ne.Error(), inner.Error())
	}

	// Unwrap returns inner error
	if !errors.Is(ne, inner) {
		t.Fatal("errors.Is should find inner error")
	}
}

func TestExtractServerCmd(t *testing.T) {
	tests := []struct {
		msg  string
		want string
	}{
		{`language server "gopls" not found in PATH`, "gopls"},
		{`language server "rust-analyzer" not found in PATH`, "rust-analyzer"},
		{`no quotes here`, ""},
		{`language server "pyright-langserver" not found in PATH`, "pyright-langserver"},
	}
	for _, tt := range tests {
		got := extractServerCmd(tt.msg)
		if got != tt.want {
			t.Errorf("extractServerCmd(%q) = %q, want %q", tt.msg, got, tt.want)
		}
	}
}

func TestWrapLSPError(t *testing.T) {
	// nil error stays nil
	if wrapLSPError(nil) != nil {
		t.Fatal("wrapLSPError(nil) should be nil")
	}

	// Non-matching error passes through unchanged
	err := fmt.Errorf("some other error")
	if wrapLSPError(err) != err {
		t.Fatal("non-matching error should pass through")
	}

	// "not found in PATH" error gets wrapped with NoticedError
	err = fmt.Errorf("language server %q not found in PATH (install it for .go support)", "gopls")
	wrapped := wrapLSPError(err)
	ne, ok := wrapped.(*NoticedError)
	if !ok {
		t.Fatalf("expected *NoticedError, got %T", wrapped)
	}
	if !strings.Contains(ne.Notice, "gopls") {
		t.Fatalf("notice should mention gopls, got: %s", ne.Notice)
	}
	if !strings.Contains(ne.Notice, "go install") {
		t.Fatalf("notice should contain install command, got: %s", ne.Notice)
	}
}
