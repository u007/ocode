package tool

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/jamesmercstudio/ocode/internal/lsp"
)

type LSPTool struct {
	clients map[string]*lsp.Client
	mu      sync.Mutex
}

func (t *LSPTool) Name() string        { return "lsp" }
func (t *LSPTool) Description() string { return "Interact with LSP servers for code intelligence" }
func (t *LSPTool) Parallel() bool      { return true }
func (t *LSPTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "lsp",
		"description": "Interact with LSP servers to get code intelligence: goToDefinition, findReferences, hover, documentSymbol, workspaceSymbol, goToImplementation, diagnostics.",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"operation": map[string]interface{}{
					"type":        "string",
					"description": "LSP operation: goToDefinition, findReferences, hover, documentSymbol, workspaceSymbol, goToImplementation, diagnostics, status, restart",
					"enum":        []string{"goToDefinition", "findReferences", "hover", "documentSymbol", "workspaceSymbol", "goToImplementation", "diagnostics", "status", "restart"},
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
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Query for workspace symbol search",
				},
			},
			"required": []string{"operation"},
		},
	}
}

func (t *LSPTool) getClient(ext string) (*lsp.Client, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.clients == nil {
		t.clients = make(map[string]*lsp.Client)
	}

	if client, ok := t.clients[ext]; ok {
		return client, nil
	}

	server := "gopls"
	switch ext {
	case ".go": server = "gopls"
	case ".py": server = "pyright"
	case ".rs": server = "rust-analyzer"
	default: return nil, fmt.Errorf("no LSP server configured for extension %s", ext)
	}

	if _, err := exec.LookPath(server); err != nil {
		return nil, fmt.Errorf("LSP server %s not found in PATH", server)
	}

	c, err := lsp.NewClient(server)
	if err != nil {
		return nil, err
	}
	c.Initialize(".")
	t.clients[ext] = c
	return c, nil
}

func (t *LSPTool) Execute(args json.RawMessage) (string, error) {
	var input struct {
		Operation string `json:"operation"`
		Path      string `json:"path"`
		Line      int    `json:"line"`
		Char      int    `json:"character"`
		Query     string `json:"query"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return "", err
	}

	ext := ""
	if input.Path != "" {
		ext = filepath.Ext(input.Path)
	}

	switch input.Operation {
	case "status":
		return t.handleStatus()
	case "restart":
		t.mu.Lock()
		if ext != "" {
			if c, ok := t.clients[ext]; ok {
				c.Close()
				delete(t.clients, ext)
			}
		}
		t.mu.Unlock()
		return fmt.Sprintf("Restarted LSP server for %s", ext), nil
	case "goToDefinition":
		client, err := t.getClient(ext)
		if err != nil { return "", err }
		return t.handleGoToDefinition(client, input.Path, input.Line, input.Char)
	case "findReferences":
		client, err := t.getClient(ext)
		if err != nil { return "", err }
		return t.handleFindReferences(client, input.Path, input.Line, input.Char)
	case "hover":
		client, err := t.getClient(ext)
		if err != nil { return "", err }
		return t.handleHover(client, input.Path, input.Line, input.Char)
	case "documentSymbol":
		client, err := t.getClient(ext)
		if err != nil { return "", err }
		return t.handleDocumentSymbol(client, input.Path)
	case "workspaceSymbol":
		return t.handleWorkspaceSymbol(input.Query)
	case "goToImplementation":
		client, err := t.getClient(ext)
		if err != nil { return "", err }
		return t.handleGoToImplementation(client, input.Path, input.Line, input.Char)
	case "diagnostics":
		client, err := t.getClient(ext)
		if err != nil { return "", err }
		return t.handleDiagnostics(client, input.Path)
	}

	return "Operation not supported", nil
}

func (t *LSPTool) handleStatus() (string, error) {
	servers := []string{"gopls", "pyright", "rust-analyzer"}
	var status strings.Builder
	status.WriteString("LSP Status:\n")
	for _, s := range servers {
		found := "❌"
		if _, err := exec.LookPath(s); err == nil { found = "✅" }
		status.WriteString(fmt.Sprintf("- %s: %s\n", s, found))
	}
	return status.String(), nil
}

func (t *LSPTool) handleGoToDefinition(client *lsp.Client, path string, line, char int) (string, error) {
	abs, _ := filepath.Abs(path)
	params := map[string]interface{}{
		"textDocument": map[string]interface{}{"uri": "file://" + abs},
		"position":     map[string]interface{}{"line": line, "character": char},
	}
	res, err := client.Call("textDocument/definition", params)
	if err != nil { return "", err }
	return string(res), nil
}

func (t *LSPTool) handleHover(client *lsp.Client, path string, line, char int) (string, error) {
	abs, _ := filepath.Abs(path)
	params := map[string]interface{}{
		"textDocument": map[string]interface{}{"uri": "file://" + abs},
		"position":     map[string]interface{}{"line": line, "character": char},
	}
	res, err := client.Call("textDocument/hover", params)
	if err != nil { return "", err }
	return string(res), nil
}

func (t *LSPTool) handleFindReferences(client *lsp.Client, path string, line, char int) (string, error) {
	abs, _ := filepath.Abs(path)
	params := map[string]interface{}{
		"textDocument": map[string]interface{}{"uri": "file://" + abs},
		"position":     map[string]interface{}{"line": line, "character": char},
		"context":      map[string]interface{}{"includeDeclaration": true},
	}
	res, err := client.Call("textDocument/references", params)
	if err != nil { return "", err }
	return string(res), nil
}

func (t *LSPTool) handleDocumentSymbol(client *lsp.Client, path string) (string, error) {
	abs, _ := filepath.Abs(path)
	params := map[string]interface{}{
		"textDocument": map[string]interface{}{"uri": "file://" + abs},
	}
	res, err := client.Call("textDocument/documentSymbol", params)
	if err != nil { return "", err }
	return string(res), nil
}

func (t *LSPTool) handleWorkspaceSymbol(query string) (string, error) {
	params := map[string]interface{}{
		"query": query,
	}
	res, err := t.clientsForExt("").Call("workspace/symbol", params)
	if err != nil {
		out, _ := exec.Command("grep", "-r", "--include=*.go", "-E", "^func |^type ", ".").Output()
		if len(out) > 0 {
			return "Workspace symbols (fallback):\n" + string(out)[:min(len(out), 5000)], nil
		}
		return "No workspace symbol results found", nil
	}
	return string(res), nil
}

func (t *LSPTool) handleGoToImplementation(client *lsp.Client, path string, line, char int) (string, error) {
	abs, _ := filepath.Abs(path)
	params := map[string]interface{}{
		"textDocument": map[string]interface{}{"uri": "file://" + abs},
		"position":     map[string]interface{}{"line": line, "character": char},
	}
	res, err := client.Call("textDocument/implementation", params)
	if err != nil { return "", err }
	return string(res), nil
}

func (t *LSPTool) handleDiagnostics(client *lsp.Client, path string) (string, error) {
	abs, _ := filepath.Abs(path)
	res, err := client.Call("textDocument/publishDiagnostics", map[string]interface{}{
		"uri": "file://" + abs,
	})
	if err != nil {
		return fmt.Sprintf("Diagnostics for %s: use LSP status to check server health", path), nil
	}
	return string(res), nil
}

func (t *LSPTool) clientsForExt(ext string) *lsp.Client {
	t.mu.Lock()
	defer t.mu.Unlock()
	if ext != "" {
		if c, ok := t.clients[ext]; ok {
			return c
		}
	}
	for _, c := range t.clients {
		return c
	}
	return nil
}
