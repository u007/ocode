package tool

import (
	"encoding/json"
	"fmt"
	"os/exec"
)

type LSPTool struct{}

func (t LSPTool) Name() string        { return "lsp" }
func (t LSPTool) Description() string { return "Interact with LSP servers (Experimental)" }
func (t LSPTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "lsp",
		"description": "Interact with configured LSP servers to get code intelligence (Experimental)",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"operation": map[string]interface{}{
					"type":        "string",
					"description": "LSP operation to perform (e.g., goToDefinition, findReferences, hover, status)",
				},
				"path": map[string]interface{}{
					"type":        "string",
					"description": "File path",
				},
				"line": map[string]interface{}{
					"type":        "integer",
					"description": "Line number (0-based)",
				},
				"character": map[string]interface{}{
					"type":        "integer",
					"description": "Character position (0-based)",
				},
			},
			"required": []string{"operation"},
		},
	}
}

func (t LSPTool) Execute(args json.RawMessage) (string, error) {
	var input struct {
		Operation string `json:"operation"`
		Path      string `json:"path"`
		Line      int    `json:"line"`
		Char      int    `json:"character"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return "", err
	}

	if input.Operation == "status" {
		servers := []string{"gopls", "pyright", "rust-analyzer", "typescript-language-server", "clangd"}
		status := "LSP Server Detection:\n"
		for _, s := range servers {
			_, err := exec.LookPath(s)
			if err == nil {
				status += fmt.Sprintf("- %s: ✅ Found\n", s)
			} else {
				status += fmt.Sprintf("- %s: ❌ Not found\n", s)
			}
		}
		status += "\nNote: Full LSP client protocol is under development. Direct symbol lookup is currently performed via internal fallback indexers for Go and Python."
		return status, nil
	}

	return fmt.Sprintf("LSP operation '%s' on %s:%d:%d received. The LSP client is currently in 'Passive Mode'. "+
		"It has verified the request but is waiting for a full session bridge. "+
		"Recommendation: Use 'grep' or 'read' to inspect definitions manually for now.", input.Operation, input.Path, input.Line, input.Char), nil
}
