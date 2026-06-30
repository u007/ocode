package tool

import (
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func TestAstGrepTool_NameAndDefinition(t *testing.T) {
	tool := &AstGrepTool{}
	if tool.Name() != "ast_grep" {
		t.Fatalf("unexpected tool name: %s", tool.Name())
	}
	def := tool.Definition()
	if name, _ := def["name"].(string); name != "ast_grep" {
		t.Fatalf("definition name mismatch: got %q want %q", name, "ast_grep")
	}
}

func TestAstGrepTool_RequiresPattern(t *testing.T) {
	tool := &AstGrepTool{}
	_, err := tool.Execute(json.RawMessage(`{"pattern":"  "}`))
	if err == nil || !strings.Contains(err.Error(), "'pattern' is required") {
		t.Fatalf("expected missing-pattern error, got %v", err)
	}
}

func TestAstGrepTool_MissingBinaryHint(t *testing.T) {
	if _, err := exec.LookPath(astGrepBin); err == nil {
		t.Skip("ast-grep installed; skipping missing-binary path")
	}
	tool := &AstGrepTool{}
	_, err := tool.Execute(json.RawMessage(`{"pattern":"console.log($A)","lang":"javascript"}`))
	if err == nil {
		t.Fatal("expected error when ast-grep binary is absent")
	}
	var notice *NoticedError
	if !errors.As(err, &notice) {
		t.Fatalf("expected NoticedError with install hint, got %T: %v", err, err)
	}
	if !strings.Contains(notice.Notice, "ast-grep is not installed") {
		t.Fatalf("notice missing install hint: %q", notice.Notice)
	}
}

func requireAstGrep(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath(astGrepBin); err != nil {
		t.Skip("ast-grep not installed; skipping live CLI test")
	}
}

func TestAstGrepTool_NoMatchIsNotError(t *testing.T) {
	requireAstGrep(t)
	tool := &AstGrepTool{}
	out, err := tool.Execute(json.RawMessage(`{"pattern":"zzz_no_such_symbol_xyz","lang":"go","path":"ast_grep.go"}`))
	if err != nil {
		t.Fatalf("no-match should not error (ast-grep exits 1), got: %v", err)
	}
	if !strings.Contains(out, "No structural matches found") {
		t.Fatalf("expected no-match message, got %q", out)
	}
}

func TestAstGrepTool_FindsMatch(t *testing.T) {
	requireAstGrep(t)
	tool := &AstGrepTool{}
	out, err := tool.Execute(json.RawMessage(`{"pattern":"astGrepBin","lang":"go","path":"ast_grep.go"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "ast_grep.go") {
		t.Fatalf("expected a match referencing ast_grep.go, got %q", out)
	}
}

func TestAstGrepTool_UsageErrorSurfaced(t *testing.T) {
	requireAstGrep(t)
	tool := &AstGrepTool{}
	_, err := tool.Execute(json.RawMessage(`{"pattern":"x","lang":"notalang","path":"ast_grep.go"}`))
	if err == nil || !strings.Contains(err.Error(), "ast_grep failed") {
		t.Fatalf("expected usage error to surface, got %v", err)
	}
}
