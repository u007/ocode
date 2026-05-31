package tool

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sync"

	"github.com/jamesmercstudio/ocode/internal/astdaemon"
)

// AstTool provides AST-aware code search via ast-grep.
// It starts a shared per-project daemon (first instance acquires lock,
// subsequent instances reuse it) that keeps the AST index fresh via
// fsnotify + incremental sg scan --update.
type AstTool struct {
	mu       sync.Mutex
	instance *astdaemon.Instance // shared daemon instance
}

func (t *AstTool) Name() string { return "code_rel" }
func (t *AstTool) Description() string {
	return "Symbol/structure-aware code relation search (use grep for plain text)"
}
func (t *AstTool) Parallel() bool { return false }

func (t *AstTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "code_rel",
		"description": "Code relation and AST-structure queries via ast-grep with persisted background index updates. Use this for symbol/structure-aware tasks (find function declarations, symbol kinds, relation-oriented navigation). For plain text or regex search, use grep.",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"operation": map[string]interface{}{
					"type": "string",
					"description": "The code relation operation to perform:\n" +
						"  - search: Find code by AST structure/pattern (e.g. 'fn $NAME($$$)' finds functions by syntax)\n" +
						"  - symbols: List symbols by kind (function, class, struct, interface, method, enum)\n" +
						"  - status: Check index/daemon health\n" +
						"  - scan: Force a re-index (normally automatic).\n" +
						"Use grep instead when you need plain text/regex matches.",
					"enum": []string{"search", "symbols", "status", "scan"},
				},
				"pattern": map[string]interface{}{
					"type":        "string",
					"description": "AST pattern to search for. Uses ast-grep pattern syntax: write code with $UPPERCASE wildcards for AST nodes, e.g. 'fn $NAME($$$)' matches any function. Use '$$$' for rest parameters.",
				},
				"kind": map[string]interface{}{
					"type":        "string",
					"description": "Symbol kind filter for the 'symbols' operation. One of: function, class, struct, method, interface, enum, variable, constant, type, module",
				},
				"lang": map[string]interface{}{
					"type":        "string",
					"description": "Programming language to restrict the search to (e.g. 'go', 'rust', 'python', 'typescript', 'java'). Optional — when omitted all supported languages are searched.",
				},
				"path": map[string]interface{}{
					"type":        "string",
					"description": "File or directory path to restrict the search to. Optional — when omitted the entire project is searched.",
				},
				"max_results": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of results to return (default: 50, max: 200)",
					"default":     50,
				},
			},
			"required": []string{"operation"},
		},
	}
}

func (t *AstTool) Execute(args json.RawMessage) (string, error) {
	var params struct {
		Operation  string `json:"operation"`
		Pattern    string `json:"pattern"`
		Kind       string `json:"kind"`
		Lang       string `json:"lang"`
		Path       string `json:"path"`
		MaxResults int    `json:"max_results"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("code_rel: invalid params: %w", err)
	}

	// Determine project root (cwd is the project root for tool execution).
	projectRoot, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("code_rel: get project root: %w", err)
	}

	switch params.Operation {
	case "search":
		return t.doSearch(projectRoot, params)
	case "symbols":
		return t.doSymbols(projectRoot, params)
	case "status":
		return t.doStatus(projectRoot)
	case "scan":
		return t.doScan(projectRoot)
	default:
		return "", fmt.Errorf("code_rel: unknown operation %q", params.Operation)
	}
}

func (t *AstTool) ensureDaemon(projectRoot string) error {
	t.mu.Lock()
	if t.instance != nil {
		t.mu.Unlock()
		return nil
	}
	t.mu.Unlock()

	inst, err := astdaemon.EnsureRunning(projectRoot)
	if err != nil {
		return err
	}

	t.mu.Lock()
	if t.instance == nil {
		t.instance = inst
	}
	t.mu.Unlock()
	return nil
}

func (t *AstTool) doSearch(projectRoot string, params struct {
	Operation  string `json:"operation"`
	Pattern    string `json:"pattern"`
	Kind       string `json:"kind"`
	Lang       string `json:"lang"`
	Path       string `json:"path"`
	MaxResults int    `json:"max_results"`
}) (string, error) {
	if params.Pattern == "" {
		return "", fmt.Errorf("code_rel: 'pattern' is required for search operation")
	}

	if err := t.ensureDaemon(projectRoot); err != nil {
		return "", fmt.Errorf("code_rel: daemon: %w", err)
	}

	maxResults := params.MaxResults
	if maxResults <= 0 {
		maxResults = 50
	}
	if maxResults > 200 {
		maxResults = 200
	}

	result, err := astdaemon.Search(projectRoot, astdaemon.SearchParams{
		Pattern:    params.Pattern,
		Language:   params.Lang,
		Path:       params.Path,
		MaxResults: maxResults,
	})
	if err != nil {
		return "", fmt.Errorf("code_rel search: %w", err)
	}

	return formatSearchResult(result), nil
}

func (t *AstTool) doSymbols(projectRoot string, params struct {
	Operation  string `json:"operation"`
	Pattern    string `json:"pattern"`
	Kind       string `json:"kind"`
	Lang       string `json:"lang"`
	Path       string `json:"path"`
	MaxResults int    `json:"max_results"`
}) (string, error) {
	if params.Kind == "" {
		return "", fmt.Errorf("code_rel: 'kind' is required for symbols operation (e.g. 'function', 'class', 'struct')")
	}

	if err := t.ensureDaemon(projectRoot); err != nil {
		return "", fmt.Errorf("code_rel: daemon: %w", err)
	}

	maxResults := params.MaxResults
	if maxResults <= 0 {
		maxResults = 50
	}
	if maxResults > 200 {
		maxResults = 200
	}

	result, err := astdaemon.ListSymbols(projectRoot, astdaemon.SymbolsParams{
		Kind:       astdaemon.SymbolKind(params.Kind),
		Language:   params.Lang,
		Path:       params.Path,
		MaxResults: maxResults,
	})
	if err != nil {
		return "", fmt.Errorf("code_rel symbols: %w", err)
	}

	return formatSearchResult(result), nil
}

func (t *AstTool) doStatus(projectRoot string) (string, error) {
	status, err := astdaemon.GetIndexStatus(projectRoot)
	if err != nil {
		return "", fmt.Errorf("code_rel status: %w", err)
	}

	out := "## AST Index Status\n\n"
	if !status.Installed {
		out += "❌ ast-grep (sg) is NOT installed.\n"
		out += "   Install with: npm install -g @ast-grep/cli  or  brew install ast-grep\n"
		return out, nil
	}

	out += fmt.Sprintf("✅ ast-grep: installed (%s)\n", status.Version)
	if status.DaemonAlive {
		out += "✅ Index daemon: running\n"
	} else {
		out += "❌ Index daemon: not running (will start on first query)\n"
	}
	if status.IndexExists {
		out += "✅ AST index: exists\n"
	} else {
		out += "❌ AST index: not yet built (will be built on first query)\n"
	}
	out += fmt.Sprintf("📁 Index directory: %s\n", status.IndexDir)
	return out, nil
}

func (t *AstTool) doScan(projectRoot string) (string, error) {
	// Force re-index by running sg scan --update directly.
	// First ensure daemon is running so the project is tracked.
	if err := t.ensureDaemon(projectRoot); err != nil {
		return "", fmt.Errorf("code_rel: daemon: %w", err)
	}

	// Run scan synchronously so the user sees progress.
	sg, err := astdaemon.FindSG()
	if err != nil {
		return "", err
	}

	cmd := exec.Command(sg, "scan", "--update")
	cmd.Dir = projectRoot
	// Capture output but also write it back.
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Sprintf("scan failed: %v\nOutput:\n%s", err, string(out)), nil
	}
	return fmt.Sprintf("Re-index complete.\n%s", string(out)), nil
}

func formatSearchResult(result *astdaemon.SearchResult) string {
	if result == nil || len(result.Matches) == 0 {
		return "No matches found."
	}

	out := fmt.Sprintf("Found %d match", result.Total)
	if result.Total != 1 {
		out += "es"
	}
	if result.Truncated {
		out += fmt.Sprintf(" (showing %d of %d)", len(result.Matches), result.Total)
	}
	out += ":\n\n"

	for i, m := range result.Matches {
		if i > 0 {
			out += "---\n"
		}
		out += fmt.Sprintf("File: %s\n", m.File)
		if m.Language != "" {
			out += fmt.Sprintf("Language: %s\n", m.Language)
		}
		out += fmt.Sprintf("Lines: %d-%d\n", m.Range.Start.Line+1, m.Range.End.Line+1)
		out += fmt.Sprintf("Match:\n  %s\n", m.Text)

		// Show sub-matches (captured groups) if present.
		if len(m.Matches) > 0 {
			out += "Captures:\n"
			for _, sm := range m.Matches {
				label := sm.Group
				if label == "" {
					label = fmt.Sprintf("match-%d", i)
				}
				out += fmt.Sprintf("  %s: \"%s\"\n", label, sm.Text)
			}
		}
	}

	return out
}
