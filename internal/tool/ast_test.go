package tool

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestAstTool_Status_NoSG(t *testing.T) {
	ast := &AstTool{}
	result, err := ast.Execute(json.RawMessage(`{"operation":"status"}`))
	if err != nil {
		t.Fatalf("status should not error: %v", err)
	}
	fmt.Println("Status output:")
	fmt.Println(result)

	// Should indicate sg is not installed.
	if len(result) == 0 {
		t.Fatal("expected non-empty result")
	}
}

func TestAstTool_UnknownOperation(t *testing.T) {
	ast := &AstTool{}
	_, err := ast.Execute(json.RawMessage(`{"operation":"nonexistent"}`))
	if err == nil {
		t.Fatal("expected error for unknown operation")
	}
	if err.Error() != `code_rel: unknown operation "nonexistent"` {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAstTool_MissingPattern(t *testing.T) {
	ast := &AstTool{}
	_, err := ast.Execute(json.RawMessage(`{"operation":"search"}`))
	if err == nil {
		t.Fatal("expected error for missing pattern")
	}
	if err.Error() != "code_rel: 'pattern' is required for search operation" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAstTool_MissingKind(t *testing.T) {
	ast := &AstTool{}
	_, err := ast.Execute(json.RawMessage(`{"operation":"symbols"}`))
	if err == nil {
		t.Fatal("expected error for missing kind")
	}
	if err.Error() != "code_rel: 'kind' is required for symbols operation (e.g. 'function', 'class', 'struct')" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAstTool_NameAndDefinition(t *testing.T) {
	tool := &AstTool{}
	if tool.Name() != "code_rel" {
		t.Fatalf("unexpected tool name: %s", tool.Name())
	}
	def := tool.Definition()
	name, _ := def["name"].(string)
	if name != tool.Name() {
		t.Fatalf("definition name mismatch: got %q want %q", name, tool.Name())
	}
}
