package tool

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/u007/ocode/internal/lsp"
)

// AstTool provides symbol-name-oriented semantic code intelligence backed by a
// real language server (find references / definition / implementations /
// callers / symbols). It resolves a symbol *name* to a position via
// workspace/symbol, then runs the positional query — so the model can ask
// "where is LoadBuiltins used?" without already knowing the file:line.
//
// This tool is an opt-in plugin: it is NOT registered unless enabled in the
// ocode config (plugins.ast). Toggle it at runtime with `/plugin enable ast`.
type AstTool struct {
	// Mgr, if set, is a shared LSP manager (reused with LSPTool so only one
	// gopls runs per project). Falls back to a private lazy manager when nil.
	Mgr  *lsp.Manager
	once sync.Once
	mgr  *lsp.Manager
}

func (t *AstTool) Name() string { return "ast" }
func (t *AstTool) Description() string {
	return "Semantic code navigation via LSP: find a symbol's references, definition, implementations, or callers by name (use grep for plain text)"
}
func (t *AstTool) Parallel() bool { return true }

func (t *AstTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "ast",
		"description": "Semantic code intelligence via the language server (LSP). Resolves a symbol by NAME and reports true references/definitions/callers — not text matches. Use this for 'where is X used / defined / who calls X / what implements X'. For plain text or regex search, use grep.",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"operation": map[string]interface{}{
					"type": "string",
					"description": "The semantic operation:\n" +
						"  - references: every usage of the symbol\n" +
						"  - definition: where the symbol is defined\n" +
						"  - implementations: types/methods implementing the interface\n" +
						"  - callers: functions that call the symbol (best-effort; gopls)\n" +
						"  - symbols: search workspace symbols by name (use 'query'), or list a file's symbols (use 'path')\n" +
						"  - status: show configured language servers and install state",
					"enum": []string{"references", "definition", "implementations", "callers", "symbols", "status"},
				},
				"symbol": map[string]interface{}{
					"type":        "string",
					"description": "Symbol name to resolve (e.g. 'LoadBuiltins'). Used by references/definition/implementations/callers. Prefer this over manual positions.",
				},
				"path": map[string]interface{}{
					"type":        "string",
					"description": "File path. Optional. If given with line/character, used as the exact position instead of resolving 'symbol'. For 'symbols' without a query, lists this file's symbols.",
				},
				"line":      map[string]interface{}{"type": "integer", "description": "0-based line (only with 'path' for an exact position)"},
				"character": map[string]interface{}{"type": "integer", "description": "0-based character (only with 'path' for an exact position)"},
				"lang": map[string]interface{}{
					"type":        "string",
					"description": "Language hint when only 'symbol' is given (go, rust, python, typescript, javascript). Defaults to go.",
				},
				"query": map[string]interface{}{"type": "string", "description": "Name query for the 'symbols' operation"},
			},
			"required": []string{"operation"},
		},
	}
}

type astParams struct {
	Operation string `json:"operation"`
	Symbol    string `json:"symbol"`
	Path      string `json:"path"`
	Line      int    `json:"line"`
	Char      int    `json:"character"`
	Lang      string `json:"lang"`
	Query     string `json:"query"`
}

func (t *AstTool) manager() *lsp.Manager {
	if t.Mgr != nil {
		return t.Mgr
	}
	t.once.Do(func() { t.mgr = lsp.NewManager(".") })
	return t.mgr
}

func (t *AstTool) Execute(args json.RawMessage) (string, error) {
	var p astParams
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("ast: invalid params: %w", err)
	}

	switch p.Operation {
	case "status":
		return t.doStatus(), nil
	case "symbols":
		return t.doSymbols(p)
	case "references", "definition", "implementations", "callers":
		return t.doPositional(p)
	default:
		return "", fmt.Errorf("ast: unknown operation %q", p.Operation)
	}
}

func (t *AstTool) doStatus() string {
	var b strings.Builder
	b.WriteString("AST/LSP semantic index status\n\n")
	b.WriteString("Language servers:\n")
	for _, s := range lsp.KnownServers() {
		b.WriteString(fmt.Sprintf("  %s: %s\n", s, installedMark(s)))
	}
	b.WriteString(fmt.Sprintf("\nSupported extensions: %s\n", lsp.SupportedExtensions()))
	return b.String()
}

func (t *AstTool) doSymbols(p astParams) (string, error) {
	// File-scoped listing when a path is given without a query.
	if p.Path != "" && p.Query == "" {
		if err := t.manager().EnsureOpen(p.Path); err != nil {
			return "", err
		}
		client, err := t.manager().ClientForFile(p.Path)
		if err != nil {
			return "", err
		}
		syms, err := client.DocumentSymbols(p.Path)
		if err != nil {
			return "", fmt.Errorf("ast symbols: %w", err)
		}
		return formatSymbols(syms), nil
	}
	if p.Query == "" {
		return "", fmt.Errorf("ast: 'query' (or 'path') is required for symbols operation")
	}
	client, err := t.clientFor(p)
	if err != nil {
		return "", err
	}
	syms, err := client.WorkspaceSymbols(p.Query)
	if err != nil {
		return "", fmt.Errorf("ast symbols: %w", err)
	}
	sort.SliceStable(syms, func(i, j int) bool { return syms[i].Name < syms[j].Name })
	return formatSymbols(syms), nil
}

func (t *AstTool) doPositional(p astParams) (string, error) {
	client, err := t.clientFor(p)
	if err != nil {
		return "", err
	}

	path := p.Path
	pos := lsp.Position{Line: p.Line, Character: p.Char}

	// Name-based: resolve the symbol to a concrete position. Preferred path.
	if p.Symbol != "" {
		loc, err := resolveSymbol(client, p.Symbol)
		if err != nil {
			return "", err
		}
		path = uriToPath(loc.URI)
		pos = loc.Range.Start
	} else if path == "" {
		return "", fmt.Errorf("ast: provide 'symbol' (recommended) or 'path' + line/character")
	}
	// Register the resolved path with the manager's file watcher so any
	// subsequent edit (by the user or by another tool) pushes didChange into
	// the server before the next query. This is the only way an in-session
	// edit stays in sync with the server's view of the document.
	if path != "" {
		_ = t.manager().EnsureOpen(path)
	}

	switch p.Operation {
	case "references":
		locs, err := client.References(path, pos)
		if err != nil {
			return "", fmt.Errorf("ast references: %w", err)
		}
		return formatLocations("References", locs), nil
	case "definition":
		locs, err := client.Definition(path, pos)
		if err != nil {
			return "", fmt.Errorf("ast definition: %w", err)
		}
		return formatLocations("Definition", locs), nil
	case "implementations":
		locs, err := client.Implementation(path, pos)
		if err != nil {
			return "", fmt.Errorf("ast implementations: %w", err)
		}
		return formatLocations("Implementations", locs), nil
	case "callers":
		locs, err := client.IncomingCalls(path, pos)
		if err != nil {
			return "", fmt.Errorf("ast callers: %w", err)
		}
		return formatLocations("Callers", locs), nil
	}
	return "", fmt.Errorf("ast: unknown operation %q", p.Operation)
}

// clientFor picks the language server from path, then lang hint, defaulting to Go.
func (t *AstTool) clientFor(p astParams) (*lsp.Client, error) {
	ext := ""
	switch {
	case p.Path != "":
		ext = filepath.Ext(p.Path)
	case p.Lang != "":
		ext = extForLang(p.Lang)
		if ext == "" {
			return nil, fmt.Errorf("ast: unknown lang %q (supported: %s)", p.Lang, lsp.SupportedExtensions())
		}
	default:
		ext = ".go"
	}
	return t.manager().ClientForExt(ext)
}

// resolveSymbol finds the best workspace-symbol match for name and returns its
// location. Exact (case-sensitive) name matches win; otherwise the first hit.
func resolveSymbol(client *lsp.Client, name string) (lsp.Location, error) {
	syms, err := client.WorkspaceSymbols(name)
	if err != nil {
		return lsp.Location{}, fmt.Errorf("resolve %q: %w", name, err)
	}
	if len(syms) == 0 {
		return lsp.Location{}, fmt.Errorf("symbol %q not found in workspace", name)
	}
	for _, s := range syms {
		if s.Name == name {
			return s.Location, nil
		}
	}
	// Trailing-name match (e.g. "pkg.Name" or "Type.Method").
	for _, s := range syms {
		if strings.HasSuffix(s.Name, "."+name) || strings.HasSuffix(s.Name, "/"+name) {
			return s.Location, nil
		}
	}
	return syms[0].Location, nil
}
