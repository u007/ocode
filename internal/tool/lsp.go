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
func (t *LSPTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "lsp",
		"description": "Interact with LSP servers to get code intelligence like goToDefinition or hover",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"operation": map[string]interface{}{
					"type":        "string",
					"description": "LSP operation (goToDefinition, hover, symbols, status, restart)",
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
					"description": "Query for symbols search",
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

	ext := filepath.Ext(input.Path)

	switch input.Operation {
	case "status":
		return t.handleStatus()
	case "restart":
		t.mu.Lock()
		if c, ok := t.clients[ext]; ok {
			c.Close()
			delete(t.clients, ext)
		}
		t.mu.Unlock()
		return fmt.Sprintf("Restarted LSP server for %s", ext), nil
	case "goToDefinition":
		client, err := t.getClient(ext)
		if err != nil { return "", err }
		return t.handleGoToDefinition(client, input.Path, input.Line, input.Char)
	case "hover":
		client, err := t.getClient(ext)
		if err != nil { return "", err }
		return t.handleHover(client, input.Path, input.Line, input.Char)
	case "symbols":
		return t.handleSymbols(input.Path, input.Query)
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

func (t *LSPTool) handleSymbols(path, query string) (string, error) {
	if strings.HasSuffix(path, ".go") {
		out, _ := exec.Command("grep", "-E", "^func |^type ", path).Output()
		return "Go Symbols:\n" + string(out), nil
	}
	return "Symbol search only available for Go files currently.", nil
}
