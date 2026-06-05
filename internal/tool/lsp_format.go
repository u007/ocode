package tool

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/u007/ocode/internal/lsp"
)

// uriToPath converts a file:// URI to a local filesystem path.
func uriToPath(uri string) string {
	if u, err := url.Parse(uri); err == nil && u.Scheme == "file" {
		return u.Path
	}
	return strings.TrimPrefix(uri, "file://")
}

// relPath shortens an absolute path relative to cwd for display.
func relPath(p string) string {
	if wd, err := os.Getwd(); err == nil {
		if rel, err := filepath.Rel(wd, p); err == nil && !strings.HasPrefix(rel, "..") {
			return rel
		}
	}
	return p
}

// lineAt returns the trimmed source line (1-based) of a file, or "".
func lineAt(path string, oneBasedLine int) string {
	data, err := os.ReadFile(path)
	if err != nil || oneBasedLine < 1 {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	if oneBasedLine > len(lines) {
		return ""
	}
	return strings.TrimSpace(lines[oneBasedLine-1])
}

// formatLocations renders LSP locations as `path:line:col  source` rows,
// reading the snippet from disk. Results are deduplicated and capped.
func formatLocations(title string, locs []lsp.Location) string {
	if len(locs) == 0 {
		return fmt.Sprintf("No %s found.", strings.ToLower(title))
	}
	const cap = 200
	var b strings.Builder
	shown := 0
	seen := map[string]bool{}
	for _, loc := range locs {
		path := uriToPath(loc.URI)
		line := loc.Range.Start.Line + 1
		col := loc.Range.Start.Character + 1
		key := fmt.Sprintf("%s:%d:%d", path, line, col)
		if seen[key] {
			continue
		}
		seen[key] = true
		if shown == 0 {
			b.WriteString(fmt.Sprintf("%s (%d):\n", title, len(locs)))
		}
		snippet := lineAt(path, line)
		if snippet != "" {
			b.WriteString(fmt.Sprintf("  %s:%d:%d  %s\n", relPath(path), line, col, snippet))
		} else {
			b.WriteString(fmt.Sprintf("  %s:%d:%d\n", relPath(path), line, col))
		}
		shown++
		if shown >= cap {
			b.WriteString(fmt.Sprintf("  … (%d more)\n", len(locs)-shown))
			break
		}
	}
	return b.String()
}

// formatSymbols renders workspace/document symbols as readable rows.
func formatSymbols(syms []lsp.SymbolInformation) string {
	if len(syms) == 0 {
		return "No symbols found."
	}
	const cap = 200
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Symbols (%d):\n", len(syms)))
	for i, s := range syms {
		if i >= cap {
			b.WriteString(fmt.Sprintf("  … (%d more)\n", len(syms)-cap))
			break
		}
		path := uriToPath(s.Location.URI)
		line := s.Location.Range.Start.Line + 1
		b.WriteString(fmt.Sprintf("  %s %s  %s:%d\n", symbolKindName(s.Kind), s.Name, relPath(path), line))
	}
	return b.String()
}

func installedMark(cmd string) string {
	if _, err := exec.LookPath(cmd); err == nil {
		return "✅ installed"
	}
	return "❌ not found"
}

// extForLang maps a language name to a representative file extension.
func extForLang(lang string) string {
	switch strings.ToLower(lang) {
	case "go", "golang":
		return ".go"
	case "rust", "rs":
		return ".rs"
	case "python", "py":
		return ".py"
	case "typescript", "ts":
		return ".ts"
	case "javascript", "js":
		return ".js"
	}
	return ""
}

// symbolKindName maps LSP SymbolKind numbers to names (subset).
func symbolKindName(kind int) string {
	switch kind {
	case 5:
		return "class"
	case 6:
		return "method"
	case 9:
		return "constructor"
	case 10:
		return "enum"
	case 11:
		return "interface"
	case 12:
		return "function"
	case 13:
		return "variable"
	case 14:
		return "constant"
	case 23:
		return "struct"
	case 26:
		return "type"
	}
	return "symbol"
}
