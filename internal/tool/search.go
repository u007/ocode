package tool

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/u007/ocode/internal/pathscope"
)

// maxUnscopedFiles caps how many files an unscoped grep (no path, no
// include) will actually open before it stops — see the comment in
// GrepTool.ExecuteCtx for why unscoped defaults to the whole project root.
const maxUnscopedFiles = 5000

// resolveSearchRoot anchors a search tool's path parameter on the session's
// project root (WithWorkDir context) instead of the process cwd. The server
// hosts sessions for many projects in one process — and the desktop shell
// launched from Finder has cwd "/" — so "." would walk the wrong tree (or the
// entire disk). An empty path means the project root itself; a relative path
// joins onto it; an absolute path is validated against the workDir
// (via Clean + EvalSymlinks + prefix check, mirroring confinedPath) so a
// model cannot walk outside the project by passing "/" or "../../". With no
// workDir in ctx (tests, TUI direct calls) the previous cwd-relative behavior
// is preserved.
func resolveSearchRoot(ctx context.Context, path string) (string, error) {
	wd := workDirFromContext(ctx)
	cleaned := filepath.Clean(path)
	if cleaned == "." {
		cleaned = ""
	}
	if cleaned == "" {
		if wd != "" {
			return wd, nil
		}
		return ".", nil
	}
	// No workDir — preserve legacy cwd-relative behavior for tests.
	if wd == "" {
		if filepath.IsAbs(cleaned) {
			return cleaned, nil
		}
		return cleaned, nil
	}
	expanded := expandTilde(cleaned)
	var abs string
	if filepath.IsAbs(expanded) {
		abs = filepath.Clean(expanded)
	} else {
		abs = filepath.Join(wd, expanded)
	}
	// Fast path: if Clean already shows it is inside wd, no symlink probe needed.
	// Still verify via EvalSymlinks to block symlink escapes.
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		dir := filepath.Dir(abs)
		resolvedDir, dirErr := filepath.EvalSymlinks(dir)
		if dirErr != nil {
			return "", fmt.Errorf("path %q is outside the working directory", path)
		}
		resolved = filepath.Join(resolvedDir, filepath.Base(abs))
	}
	resolved = filepath.Clean(resolved)
	wdResolved, ok := normalizeRootPath(wd)
	if !ok {
		return "", fmt.Errorf("could not resolve working directory")
	}
	if pathWithinRoot(resolved, wdResolved) {
		return resolved, nil
	}
	for _, root := range getExtraAllowedRoots() {
		if nr, ok := normalizeRootPath(root); ok && pathWithinRoot(resolved, nr) {
			return resolved, nil
		}
	}
	// Allow temp dir walks (tests use TempDir, and /tmp is safe).
	if pathscope.IsTempDir(resolved) {
		return resolved, nil
	}
	return "", fmt.Errorf("path %q is outside the working directory", path)
}

// searchDisplayPath converts an absolute walk path under root back to the
// path the model should see: relative to the search root, re-prefixed with
// the original (relative) path parameter so results stay resolvable against
// the project root by the file tools.
func searchDisplayPath(root, origPath, p string) string {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return p
	}
	if origPath != "" && !filepath.IsAbs(origPath) {
		return filepath.Join(origPath, rel)
	}
	if origPath != "" {
		// Absolute path param: keep results absolute.
		return p
	}
	return rel
}

const globMaxResults = 100

type GlobTool struct{}

func (t GlobTool) Name() string        { return "glob" }
func (t GlobTool) Description() string { return "Find files by pattern matching" }
func (t GlobTool) Parallel() bool      { return true }
func (t GlobTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "glob",
		"description": fmt.Sprintf("Find files by pattern matching. Supports ** for recursive matching (e.g. **/*.js, src/**/*.ts). Results sorted by modification time, capped at %d.", globMaxResults),
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"pattern": map[string]interface{}{
					"type":        "string",
					"description": "Glob pattern like **/*.js or src/**/*.ts",
				},
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Optional base directory to search in (default: project root)",
				},
			},
			"required": []string{"pattern"},
		},
	}
}

type globMatch struct {
	path  string
	mtime int64
}

func (t GlobTool) Execute(args json.RawMessage) (string, error) {
	return t.ExecuteCtx(context.Background(), args)
}

func (t GlobTool) ExecuteCtx(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Pattern string   `json:"pattern"`
		Path    string   `json:"path"`
		Ignore  []string `json:"ignore"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	if len(params.Pattern) > 500 {
		return "", fmt.Errorf("pattern too long (max 500 characters)")
	}
	searchDir, err := resolveSearchRoot(ctx, params.Path)
	if err != nil {
		return "", err
	}

	ign := NewIgnoreMatcher(searchDir, params.Ignore)

	var matches []globMatch
	walkErr := filepath.Walk(searchDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if path == "." || path == searchDir {
			return nil
		}

		if ign.IsIgnored(path, info.IsDir()) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if info.IsDir() {
			if info.Name() == ".git" || info.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}

		rel, err := filepath.Rel(searchDir, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)

		if matchGlob(params.Pattern, rel) {
			matches = append(matches, globMatch{
				path:  searchDisplayPath(searchDir, params.Path, path),
				mtime: info.ModTime().UnixNano(),
			})
		}
		return nil
	})
	if walkErr != nil {
		return "", fmt.Errorf("glob failed: %w", walkErr)
	}

	sort.Slice(matches, func(i, j int) bool {
		return matches[i].mtime > matches[j].mtime
	})

	totalMatches := len(matches)
	truncated := false
	if totalMatches > globMaxResults {
		matches = matches[:globMaxResults]
		truncated = true
	}

	var paths []string
	for _, m := range matches {
		paths = append(paths, m.path)
	}

	result := strings.Join(paths, "\n")
	if truncated {
		result += fmt.Sprintf("\n\n... (%d files matched, showing first %d)", totalMatches, globMaxResults)
	}

	if len(paths) == 0 {
		return "No files matched", nil
	}

	return result, nil
}

func matchGlob(pattern, path string) bool {
	pattern = filepath.ToSlash(pattern)

	if strings.Contains(pattern, "**") {
		re := globToRegex(pattern)
		matched, _ := regexp.MatchString("^"+re+"$", path)
		return matched
	}

	matched, _ := filepath.Match(pattern, path)
	if matched {
		return true
	}

	matched, _ = filepath.Match(pattern, "./"+path)
	return matched
}

func globToRegex(pattern string) string {
	var re strings.Builder
	parts := strings.Split(pattern, "/")
	needSlash := false
	for _, part := range parts {
		if part == "**" {
			// ** matches zero or more path segments (including none), so
			// **/foo.txt also matches foo.txt at the root.
			re.WriteString("(?:.*/)?")
			needSlash = false
			continue
		}
		if needSlash {
			re.WriteString("/")
		}
		needSlash = true
		for _, ch := range part {
			switch ch {
			case '*':
				re.WriteString("[^/]*")
			case '?':
				re.WriteString("[^/]")
			case '.', '+', '(', ')', '[', ']', '{', '}', '^', '$', '|', '\\':
				re.WriteString("\\")
				re.WriteRune(ch)
			default:
				re.WriteRune(ch)
			}
		}
	}
	return re.String()
}

type GrepTool struct{}

func (t GrepTool) Name() string { return "grep" }
func (t GrepTool) Description() string {
	return "Fast plain text/regex search across file contents"
}
func (t GrepTool) Parallel() bool { return true }
func (t GrepTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "grep",
		"description": "Fast plain text/regex search across file contents. Use this for exact strings, logs, config keys, comments, and non-structural matches. For symbol-name semantic queries (references/definition/callers), use the 'ast' tool when enabled.",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"pattern": map[string]interface{}{
					"type":        "string",
					"description": "Regular expression pattern",
				},
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Optional path to search in (default: project root)",
				},
				"include": map[string]interface{}{
					"type":        "string",
					"description": "Optional glob pattern to filter files (e.g. *.go, **/*.tsx)",
				},
				"output_mode": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"files_with_matches", "content", "count"},
					"description": "Output format: files_with_matches (paths only), content (lines with matches), count (match count per file). Default: content.",
				},
				"multiline": map[string]interface{}{
					"type":        "boolean",
					"description": "Enable multiline matching where . matches newlines (default: false)",
				},
			},
			"required": []string{"pattern"},
		},
	}
}

func (t GrepTool) Execute(args json.RawMessage) (string, error) {
	return t.ExecuteCtx(context.Background(), args)
}

func (t GrepTool) ExecuteCtx(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Pattern    string   `json:"pattern"`
		Path       string   `json:"path"`
		Include    string   `json:"include"`
		OutputMode string   `json:"output_mode"`
		Multiline  bool     `json:"multiline"`
		Ignore     []string `json:"ignore"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	searchRoot, err := resolveSearchRoot(ctx, params.Path)
	if err != nil {
		return "", err
	}
	if len(params.Pattern) > 500 {
		return "", fmt.Errorf("pattern too long (max 500 characters)")
	}
	if params.OutputMode == "" {
		params.OutputMode = "content"
	}

	var re *regexp.Regexp
	if params.Multiline {
		re, err = regexp.Compile(`(?s)` + params.Pattern)
	} else {
		re, err = regexp.Compile(params.Pattern)
	}
	if err != nil {
		return "", fmt.Errorf("invalid regex: %w", err)
	}

	ign := NewIgnoreMatcher(searchRoot, params.Ignore)
	type fileResult struct {
		path  string
		count int
		lines []string
	}
	var fileResults []fileResult

	// Reused across every file in this walk instead of allocating a fresh
	// 1MB scanner buffer per file — on an unscoped, repo-wide grep that was
	// the dominant allocation source (thousands of 1MB buffers for one call).
	scanBuf := make([]byte, 0, 1024*1024)

	// Guard against an unscoped grep (no path, no include) turning into a
	// full-repo read: without either filter, resolveSearchRoot anchors on
	// the whole project root, so this is the same blast radius as `find .
	// -exec cat {} \;` piped to a matcher. Cap the number of files actually
	// opened and bail out with a note telling the caller to narrow scope,
	// rather than silently reading tens of thousands of files.
	unscoped := params.Path == "" && params.Include == ""
	filesRead := 0
	truncated := false

	walkErr := filepath.Walk(searchRoot, func(p string, info os.FileInfo, werr error) error {
		if werr != nil {
			return nil
		}
		if ign.IsIgnored(p, info.IsDir()) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			if info.Name() == ".git" || info.Name() == "node_modules" {
				return filepath.SkipDir
			}
			if unscoped {
				switch info.Name() {
				case "vendor", "dist", "build", ".next", "target":
					return filepath.SkipDir
				}
			}
			return nil
		}

		display := searchDisplayPath(searchRoot, params.Path, p)
		if params.Include != "" && !matchGlob(params.Include, filepath.ToSlash(display)) {
			return nil
		}

		if unscoped && filesRead >= maxUnscopedFiles {
			truncated = true
			return filepath.SkipAll
		}
		filesRead++

		content, readErr := os.ReadFile(p)
		if readErr != nil {
			return nil
		}

		if params.Multiline {
			if re.Match(content) {
				count := len(re.FindAllIndex(content, -1))
				fr := fileResult{path: display, count: count}
				if params.OutputMode == "content" {
					for _, match := range re.FindAll(content, -1) {
						fr.lines = append(fr.lines, string(match))
					}
				}
				fileResults = append(fileResults, fr)
			}
		} else {
			var fr fileResult
			fr.path = display
			scanner := bufio.NewScanner(bytes.NewReader(content))
			scanner.Buffer(scanBuf, 1024*1024)
			lineNum := 1
			for scanner.Scan() {
				line := scanner.Text()
				if re.MatchString(line) {
					fr.count++
					if params.OutputMode == "content" {
						fr.lines = append(fr.lines, fmt.Sprintf("%d:%s", lineNum, line))
					}
				}
				lineNum++
			}
			if fr.count > 0 {
				fileResults = append(fileResults, fr)
			}
		}
		return nil
	})
	if walkErr != nil {
		return "", fmt.Errorf("grep failed: %w", walkErr)
	}

	if len(fileResults) == 0 {
		if truncated {
			return fmt.Sprintf("No matches found in the first %d files (search stopped there — no path/include filter was given for a repo-wide search). Narrow with \"path\" or \"include\" to search the rest.", maxUnscopedFiles), nil
		}
		return "No matches found", nil
	}

	var b strings.Builder
	for i, fr := range fileResults {
		if i > 0 {
			b.WriteString("\n")
		}
		switch params.OutputMode {
		case "files_with_matches":
			b.WriteString(fr.path)
		case "count":
			b.WriteString(fmt.Sprintf("%s: %d", fr.path, fr.count))
		case "content":
			for _, line := range fr.lines {
				b.WriteString(fmt.Sprintf("%s:%s\n", fr.path, line))
			}
		}
	}

	if truncated {
		b.WriteString(fmt.Sprintf("\n\n[stopped after %d files — no \"path\"/\"include\" filter was given for a repo-wide search; narrow scope to see the rest]", maxUnscopedFiles))
	}

	// Same output cap the bash tool uses (truncateOutput, exec.go) — bounds
	// the response even for a properly scoped grep whose pattern matches
	// pervasively (e.g. a common token across a large "include" set).
	return truncateOutput(strings.TrimRight(b.String(), "\n")), nil
}

type ListTool struct{}

func (t ListTool) Name() string        { return "list" }
func (t ListTool) Description() string { return "List files and directories in a given path" }
func (t ListTool) Parallel() bool      { return true }
func (t ListTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "list",
		"description": "List files and directories in a given path",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Optional path to list (default: current directory)",
				},
				"pattern": map[string]interface{}{
					"type":        "string",
					"description": "Optional glob pattern to filter results",
				},
			},
		},
	}
}

func (t ListTool) Execute(args json.RawMessage) (string, error) {
	return t.ExecuteCtx(context.Background(), args)
}

func (t ListTool) ExecuteCtx(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Path    string   `json:"path"`
		Pattern string   `json:"pattern"`
		Ignore  []string `json:"ignore"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	listDir, err := resolveSearchRoot(ctx, params.Path)
	if err != nil {
		return "", err
	}

	ign := NewIgnoreMatcher(listDir, params.Ignore)
	entries, err := os.ReadDir(listDir)
	if err != nil {
		return "", fmt.Errorf("failed to list directory %s: %w", listDir, err)
	}

	var results []string
	for _, e := range entries {
		name := e.Name()
		fullPath := filepath.Join(listDir, name)
		if ign.IsIgnored(fullPath, e.IsDir()) {
			continue
		}

		if params.Pattern != "" {
			matched, _ := filepath.Match(params.Pattern, name)
			if !matched {
				continue
			}
		}

		if e.IsDir() {
			name += "/"
		}
		results = append(results, name)
	}

	if len(results) == 0 {
		return "Empty directory", nil
	}

	return strings.Join(results, "\n"), nil
}
