package tool

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/u007/ocode/internal/lsp"
)

func TestAstTool_NameAndDefinition(t *testing.T) {
	tool := &AstTool{}
	if tool.Name() != "ast" {
		t.Fatalf("unexpected tool name: %s", tool.Name())
	}
	def := tool.Definition()
	if name, _ := def["name"].(string); name != "ast" {
		t.Fatalf("definition name mismatch: got %q want %q", name, "ast")
	}
}

func TestAstTool_UnknownOperation(t *testing.T) {
	ast := &AstTool{}
	_, err := ast.Execute(json.RawMessage(`{"operation":"nonexistent"}`))
	if err == nil || !strings.Contains(err.Error(), "unknown operation") {
		t.Fatalf("expected unknown-operation error, got %v", err)
	}
}

func TestAstTool_Status(t *testing.T) {
	ast := &AstTool{}
	out, err := ast.Execute(json.RawMessage(`{"operation":"status"}`))
	if err != nil {
		t.Fatalf("status should not error: %v", err)
	}
	if !strings.Contains(out, "gopls") {
		t.Fatalf("status should list gopls, got:\n%s", out)
	}
}

func TestAstTool_SymbolsRequiresQuery(t *testing.T) {
	ast := &AstTool{}
	_, err := ast.Execute(json.RawMessage(`{"operation":"symbols"}`))
	if err == nil || !strings.Contains(err.Error(), "query") {
		t.Fatalf("expected query-required error, got %v", err)
	}
}

// TestAstTool_ReferencesByName exercises the full LSP roundtrip against gopls
// on this repo: resolve a symbol name -> position -> references.
func TestAstTool_ReferencesByName(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not installed")
	}
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/go.mod", []byte("module example.com/asttest\n\ngo 1.23\n"), 0600); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(dir+"/a.go", []byte("package main\n\nfunc LoadBuiltins() {}\n\nfunc callA() { LoadBuiltins() }\n"), 0600); err != nil {
		t.Fatalf("write a.go: %v", err)
	}
	if err := os.WriteFile(dir+"/b.go", []byte("package main\n\nfunc callB() { LoadBuiltins() }\n"), 0600); err != nil {
		t.Fatalf("write b.go: %v", err)
	}
	mgr := lsp.NewManager(dir)
	t.Cleanup(mgr.Close)
	ast := &AstTool{Mgr: mgr}
	out, err := ast.Execute(json.RawMessage(`{"operation":"references","symbol":"LoadBuiltins","lang":"go"}`))
	if err != nil {
		t.Fatalf("references: %v", err)
	}
	if !strings.Contains(out, "a.go") || !strings.Contains(out, "b.go") {
		t.Fatalf("expected references to include a.go and b.go, got:\n%s", out)
	}
}
