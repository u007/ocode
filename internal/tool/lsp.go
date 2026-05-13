package tool

import (
	"encoding/json"
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
					"description": "LSP operation to perform (e.g., goToDefinition, findReferences, hover)",
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
			"required": []string{"operation", "path"},
		},
	}
}

func (t LSPTool) Execute(args json.RawMessage) (string, error) {
	return "LSP tool is currently experimental and requires server-side configuration. " +
		"Please ensure an LSP server is running and configured in your opencode.json under the 'lsp' key. " +
		"Supported operations: goToDefinition, findReferences, hover.", nil
}
