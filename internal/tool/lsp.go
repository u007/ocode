package tool

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/u007/ocode/internal/lsp"
)

type LSPTool struct {
	// Mgr, if set, is a shared LSP manager (so LSPTool and AstTool reuse one
	// gopls per project instead of spawning two). Falls back to a private lazy
	// manager when nil (e.g. tools instantiated directly in tests).
	Mgr  *lsp.Manager
	once sync.Once
	mgr  *lsp.Manager
}

func (t *LSPTool) Name() string        { return "lsp" }
func (t *LSPTool) Description() string { return "Interact with LSP servers for code intelligence" }
func (t *LSPTool) Parallel() bool      { return true }
func (t *LSPTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "lsp",
		"description": "Low-level LSP code intelligence at an exact file position: goToDefinition, findReferences, hover, documentSymbol, workspaceSymbol, goToImplementation, status, restart. Requires path + 0-based line/character. For symbol-name-based navigation, prefer the 'ast' tool when enabled.",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"operation": map[string]interface{}{
					"type":        "string",
					"description": "LSP operation",
					"enum":        []string{"goToDefinition", "findReferences", "hover", "documentSymbol", "workspaceSymbol", "goToImplementation", "status", "restart"},
				},
				"path":      map[string]interface{}{"type": "string", "description": "File path"},
				"line":      map[string]interface{}{"type": "integer", "description": "Line number (0-based)"},
				"character": map[string]interface{}{"type": "integer", "description": "Character position (0-based)"},
				"query":     map[string]interface{}{"type": "string", "description": "Query for workspace symbol search"},
				"lang": map[string]interface{}{
					"type":        "string",
					"description": "Language hint for workspaceSymbol when no path is given (go, rust, python, typescript, javascript). Defaults to go.",
				},
			},
			"required": []string{"operation"},
		},
	}
}

func (t *LSPTool) manager() *lsp.Manager {
	if t.Mgr != nil {
		return t.Mgr
	}
	t.once.Do(func() { t.mgr = lsp.NewManager(".") })
	return t.mgr
}

func (t *LSPTool) Execute(args json.RawMessage) (string, error) {
	var input struct {
		Operation string `json:"operation"`
		Path      string `json:"path"`
		Line      int    `json:"line"`
		Char      int    `json:"character"`
		Query     string `json:"query"`
		Lang      string `json:"lang"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return "", err
	}

	mgr := t.manager()
	pos := lsp.Position{Line: input.Line, Character: input.Char}

	switch input.Operation {
	case "status":
		return lspStatus(), nil
	case "restart":
		// Restart kills the running server process. In-flight Call/workspaceSymbol
		// from the other tool (lsp or ast) will return "LSP client closed" —
		// surface that risk in the result so the caller doesn't think it was
		// a clean swap.
		const warn = "Note: any in-flight LSP queries will be cancelled."
		if input.Path != "" {
			ext := filepath.Ext(input.Path)
			mgr.Restart(ext)
			return fmt.Sprintf("Restarted LSP server for %s. %s", ext, warn), nil
		}
		mgr.Close()
		return "Restarted all LSP servers. " + warn, nil
	case "workspaceSymbol":
		// Choose the language server from the lang hint, defaulting to go when
		// neither path nor lang was supplied. Without this, a Rust/TS workspace
		// would silently query gopls (the only validated server) and get
		// confusing empty results.
		ext := extForLang(input.Lang)
		if ext == "" {
			ext = ".go"
		}
		client, err := mgr.ClientForExt(ext)
		if err != nil {
			return "", err
		}
		syms, err := client.WorkspaceSymbols(input.Query)
		if err != nil {
			return "", err
		}
		return formatSymbols(syms), nil
	}

	if input.Path == "" {
		return "", fmt.Errorf("lsp: 'path' is required for operation %q", input.Operation)
	}
	client, err := mgr.ClientForFile(input.Path)
	if err != nil {
		return "", err
	}
	// Open through the manager so the file watcher registers this URI; the
	// positional query helpers below also call EnsureOpen internally, but
	// they do so on the Client, which bypasses the watcher. Pre-registering
	// ensures post-edit didChange notifications reach the server.
	if err := mgr.EnsureOpen(input.Path); err != nil {
		// Non-fatal: many operations (hover) re-open internally.
		_ = err
	}

	switch input.Operation {
	case "goToDefinition":
		locs, err := client.Definition(input.Path, pos)
		if err != nil {
			return "", err
		}
		return formatLocations("Definition", locs), nil
	case "findReferences":
		locs, err := client.References(input.Path, pos)
		if err != nil {
			return "", err
		}
		return formatLocations("References", locs), nil
	case "goToImplementation":
		locs, err := client.Implementation(input.Path, pos)
		if err != nil {
			return "", err
		}
		return formatLocations("Implementations", locs), nil
	case "documentSymbol":
		syms, err := client.DocumentSymbols(input.Path)
		if err != nil {
			return "", err
		}
		return formatSymbols(syms), nil
	case "hover":
		if err := client.EnsureOpen(input.Path); err != nil {
			return "", err
		}
		res, err := client.Call("textDocument/hover", client.HoverParams(input.Path, pos))
		if err != nil {
			return "", err
		}
		return string(res), nil
	}

	return "", fmt.Errorf("lsp: unsupported operation %q", input.Operation)
}

func lspStatus() string {
	var b strings.Builder
	b.WriteString("LSP servers:\n")
	for _, s := range lsp.KnownServers() {
		b.WriteString(fmt.Sprintf("- %s: %s\n", s, installedMark(s)))
	}
	return b.String()
}
