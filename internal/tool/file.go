package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/paths"
	"github.com/u007/ocode/internal/pathscope"
	"github.com/u007/ocode/internal/snapshot"
)

const defaultReadLines = 50
const maxReadLines = 250

var (
	extraAllowedRootsMu    sync.RWMutex
	persistentAllowedRoots map[string]struct{}
	tempAllowedRootRefs    map[string]int
)

func setExtraAllowedPaths(paths []string) {
	normalized := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		if root, ok := normalizeRootPath(p); ok {
			normalized[root] = struct{}{}
		}
	}
	extraAllowedRootsMu.Lock()
	persistentAllowedRoots = normalized
	if tempAllowedRootRefs == nil {
		tempAllowedRootRefs = make(map[string]int)
	}
	extraAllowedRootsMu.Unlock()
}

// AddExtraAllowedPath adds one path root to the runtime allowlist.
// Returns true when the path was normalized and is present in the allowlist.
func AddExtraAllowedPath(path string) bool {
	root, ok := normalizeRootPath(path)
	if !ok {
		return false
	}
	extraAllowedRootsMu.Lock()
	defer extraAllowedRootsMu.Unlock()
	if persistentAllowedRoots == nil {
		persistentAllowedRoots = make(map[string]struct{})
	}
	persistentAllowedRoots[root] = struct{}{}
	return true
}

// HasExtraAllowedPath reports whether path (after normalization) is in the
// runtime extra allowlist.
func HasExtraAllowedPath(path string) bool {
	root, ok := normalizeRootPath(path)
	if !ok {
		return false
	}
	extraAllowedRootsMu.RLock()
	defer extraAllowedRootsMu.RUnlock()
	if _, ok := persistentAllowedRoots[root]; ok {
		return true
	}
	return tempAllowedRootRefs[root] > 0
}

// RemoveExtraAllowedPath removes one normalized root from the runtime
// allowlist. It returns true when a matching entry existed and was removed.
func RemoveExtraAllowedPath(path string) bool {
	root, ok := normalizeRootPath(path)
	if !ok {
		return false
	}
	extraAllowedRootsMu.Lock()
	defer extraAllowedRootsMu.Unlock()
	if _, ok := persistentAllowedRoots[root]; !ok {
		return false
	}
	delete(persistentAllowedRoots, root)
	return true
}

// AcquireTemporaryAllowedPath increments a temporary in-memory lease for root.
// The path remains allowed until a matching ReleaseTemporaryAllowedPath call.
func AcquireTemporaryAllowedPath(path string) bool {
	root, ok := normalizeRootPath(path)
	if !ok {
		return false
	}
	extraAllowedRootsMu.Lock()
	defer extraAllowedRootsMu.Unlock()
	if tempAllowedRootRefs == nil {
		tempAllowedRootRefs = make(map[string]int)
	}
	tempAllowedRootRefs[root]++
	return true
}

// ReleaseTemporaryAllowedPath decrements one temporary lease for root.
// It returns true when a temporary lease existed.
func ReleaseTemporaryAllowedPath(path string) bool {
	root, ok := normalizeRootPath(path)
	if !ok {
		return false
	}
	extraAllowedRootsMu.Lock()
	defer extraAllowedRootsMu.Unlock()
	count := tempAllowedRootRefs[root]
	if count <= 0 {
		return false
	}
	if count == 1 {
		delete(tempAllowedRootRefs, root)
	} else {
		tempAllowedRootRefs[root] = count - 1
	}
	return true
}

func getExtraAllowedRoots() []string {
	extraAllowedRootsMu.RLock()
	defer extraAllowedRootsMu.RUnlock()
	if len(persistentAllowedRoots) == 0 && len(tempAllowedRootRefs) == 0 {
		return nil
	}
	out := make([]string, 0, len(persistentAllowedRoots)+len(tempAllowedRootRefs))
	for root := range persistentAllowedRoots {
		out = append(out, root)
	}
	for root := range tempAllowedRootRefs {
		if _, ok := persistentAllowedRoots[root]; ok {
			continue
		}
		out = append(out, root)
	}
	return out
}

func normalizeRootPath(p string) (string, bool) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", false
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		dir := filepath.Dir(abs)
		resolvedDir, dirErr := filepath.EvalSymlinks(dir)
		if dirErr != nil {
			return "", false
		}
		resolved = filepath.Join(resolvedDir, filepath.Base(abs))
	}
	return filepath.Clean(resolved), true
}

func pathWithinRoot(path, root string) bool {
	if path == root {
		return true
	}
	rootWithSep := root + string(filepath.Separator)
	return strings.HasPrefix(path, rootWithSep)
}

// toolResultCacheDir returns the directory where truncated tool outputs are saved.
func toolResultCacheDir() string {
	if env := os.Getenv("XDG_STATE_HOME"); env != "" {
		return filepath.Join(env, "opencode", "tool-results")
	}
	home, _ := os.UserHomeDir()
	if runtime.GOOS == "windows" {
		if base := os.Getenv("LOCALAPPDATA"); base != "" {
			return filepath.Join(base, "opencode", "tool-results")
		}
	}
	return filepath.Join(home, ".local", "state", "opencode", "tool-results")
}

// expandTilde replaces a leading ~ or ~user with the user's home directory.
// If the input doesn't start with ~, it is returned unchanged.
func expandTilde(p string) string {
	if !strings.HasPrefix(p, "~") {
		return p
	}
	// Exactly "~" or "~/" prefix.
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return p // can't resolve; return as-is
		}
		if p == "~" {
			return home
		}
		return filepath.Join(home, p[2:])
	}
	// ~otheruser form – not supported, return as-is.
	return p
}

// confinedPath resolves p relative to the process working directory and
// verifies that the result is within the workspace, a configured extra root,
// a well-known temp directory, the tool-results cache, or the managed
// repository cache. It returns the cleaned absolute path on success, or an
// error if the path would escape all allowed roots.
func confinedPath(p string) (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("could not determine working directory: %w", err)
	}
	abs, err := filepath.Abs(expandTilde(p))
	if err != nil {
		return "", fmt.Errorf("invalid path %q: %w", p, err)
	}
	if pathscope.IsTempDir(abs) {
		return abs, nil
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		dir := filepath.Dir(abs)
		resolvedDir, dirErr := filepath.EvalSymlinks(dir)
		if dirErr != nil {
			return "", fmt.Errorf("path %q is outside the working directory", p)
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
		if pathWithinRoot(resolved, root) {
			return resolved, nil
		}
	}
	if pathscope.IsTempDir(resolved) {
		return resolved, nil
	}
	// Also allow access to the tool-results state directory.
	cacheDir := toolResultCacheDir()
	if cacheResolved, ok := normalizeRootPath(cacheDir); ok && pathWithinRoot(resolved, cacheResolved) {
		return resolved, nil
	}
	// Also allow reads from the managed repository cache.
	if repoCache, err := repoCacheDir(); err == nil {
		if repoResolved, ok := normalizeRootPath(repoCache); ok && pathWithinRoot(resolved, repoResolved) {
			return resolved, nil
		}
	}
	// Allow access to ocode's global data dir (memory, sessions, auth, usage).
	if dataDir, err := paths.GlobalDataDir(); err == nil {
		if dataDirResolved, ok := normalizeRootPath(dataDir); ok && pathWithinRoot(resolved, dataDirResolved) {
			return resolved, nil
		}
	}
	return "", fmt.Errorf("path %q is outside the working directory", p)
}

type ReadTool struct{}

func (t ReadTool) Name() string        { return "read" }
func (t ReadTool) Description() string { return "Read file contents from the codebase" }
func (t ReadTool) Parallel() bool      { return true }
func (t ReadTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "read",
		"description": fmt.Sprintf("Read file contents. Returns up to %d lines by default (max %d). Use start_line/end_line to paginate large files.", defaultReadLines, maxReadLines),
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Path to the file to read",
				},
				"start_line": map[string]interface{}{
					"type":        "integer",
					"description": "1-based line to start reading from (default: 1)",
				},
				"end_line": map[string]interface{}{
					"type":        "integer",
					"description": fmt.Sprintf("1-based last line to read inclusive (default: start_line + %d - 1, max range: %d lines)", defaultReadLines, maxReadLines),
				},
				"offset": map[string]interface{}{
					"type":        "integer",
					"description": "Alias for start_line (1-based line to start from)",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": fmt.Sprintf("Alias for a line count from start: reads `limit` lines (max %d)", maxReadLines),
				},
			},
			"required": []string{"path"},
		},
	}
}

func (t ReadTool) Execute(args json.RawMessage) (string, error) {
	var params struct {
		Path      string `json:"path"`
		StartLine int    `json:"start_line"`
		EndLine   int    `json:"end_line"`
		// Claude-Code-style aliases. Models are strongly biased toward emitting
		// {offset, limit} regardless of the advertised schema; honoring them as
		// aliases prevents a silent reread loop where every paginated read
		// returns lines 1..50 again. offset → start line, limit → line count.
		Offset int `json:"offset"`
		Limit  int `json:"limit"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	safe, err := confinedPath(params.Path)
	if err != nil {
		return "", err
	}

	content, err := os.ReadFile(safe)
	if err != nil {
		return "", fmt.Errorf("failed to read file %s: %w", params.Path, err)
	}

	lines := strings.Split(string(content), "\n")
	total := len(lines)

	start := params.StartLine
	if start <= 0 {
		start = params.Offset
	}
	if start <= 0 {
		start = 1
	}
	if start > total {
		return fmt.Sprintf("(file has %d lines, start_line=%d is out of range)", total, start), nil
	}

	end := params.EndLine
	if end <= 0 && params.Limit > 0 {
		end = start + params.Limit - 1
	}
	if end <= 0 {
		end = start + defaultReadLines - 1
	}
	// Clamp range to maxReadLines.
	if end-start+1 > maxReadLines {
		end = start + maxReadLines - 1
	}
	if end > total {
		end = total
	}

	var sb strings.Builder
	for i := start; i <= end; i++ {
		sb.WriteString(fmt.Sprintf("%d\t%s\n", i, lines[i-1]))
	}
	if end < total {
		sb.WriteString(fmt.Sprintf("…(use start_line=%d, end_line=%d to continue)\n", end+1, end+defaultReadLines))
	}
	return sb.String(), nil
}

type WriteTool struct {
	Config *config.Config
}

func (t WriteTool) Name() string        { return "write" }
func (t WriteTool) Description() string { return "Create or overwrite a file, or append to it" }
func (t WriteTool) Parallel() bool      { return false }
func (t WriteTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "write",
		"description": "Create or overwrite a file, or append to it. Use mode=append to add to the end without touching existing content.",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Path to the file to write",
				},
				"content": map[string]interface{}{
					"type":        "string",
					"description": "Content to write or append",
				},
				"mode": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"overwrite", "append"},
					"description": "overwrite (default) replaces the entire file; append adds content to the end",
				},
			},
			"required": []string{"path", "content"},
		},
	}
}

func (t WriteTool) Execute(args json.RawMessage) (string, error) {
	return t.ExecuteCtx(context.Background(), args)
}

func (t WriteTool) ExecuteCtx(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Path    string `json:"path"`
		Content string `json:"content"`
		Mode    string `json:"mode"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	safe, err := confinedPath(params.Path)
	if err != nil {
		return "", err
	}

	prev, _ := os.ReadFile(safe)

	tcID := snapshot.ToolCallIDFromContext(ctx)
	store := snapshot.FromContext(ctx)
	if tcID != "" {
		_ = store.Backup(safe, tcID) //nolint:errcheck
	} else {
		_ = store.Backup(safe, "") //nolint:errcheck
	}

	if err := os.MkdirAll(filepath.Dir(safe), 0755); err != nil {
		return "", fmt.Errorf("failed to create directories for %s: %w", params.Path, err)
	}

	var newContent string
	if params.Mode == "append" {
		f, err := os.OpenFile(safe, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return "", fmt.Errorf("failed to open file %s for append: %w", params.Path, err)
		}
		if _, err = f.WriteString(params.Content); err != nil {
			f.Close()
			return "", fmt.Errorf("failed to append to file %s: %w", params.Path, err)
		}
		f.Close()
		newContent = string(prev) + params.Content
	} else {
		if err := os.WriteFile(safe, []byte(params.Content), 0644); err != nil {
			return "", fmt.Errorf("failed to write file %s: %w", params.Path, err)
		}
		newContent = params.Content
	}

	if tcID != "" {
		store.RegisterWrite(safe, tcID)
	}

	var formatters map[string]config.FormatterConfig
	if t.Config != nil {
		formatters = t.Config.Formatters
	}
	FormatAfterWrite(safe, formatters)

	return FormatDiff(params.Path, string(prev), newContent), nil
}

// ReplaceLinesTool replaces a line range with new content — positional, no string search.
type ReplaceLinesToolImpl struct {
	Config *config.Config
}

func (t ReplaceLinesToolImpl) Name() string { return "replace_lines" }
func (t ReplaceLinesToolImpl) Description() string {
	return "Replace a range of lines in a file with new content"
}
func (t ReplaceLinesToolImpl) Parallel() bool { return false }
func (t ReplaceLinesToolImpl) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "replace_lines",
		"description": "Replace lines start_line through end_line (inclusive, 1-based) with content. Use read with start_line/end_line to locate the target range first.",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Path to the file to patch",
				},
				"start_line": map[string]interface{}{
					"type":        "integer",
					"description": "1-based first line to replace",
				},
				"end_line": map[string]interface{}{
					"type":        "integer",
					"description": "1-based last line to replace (inclusive)",
				},
				"content": map[string]interface{}{
					"type":        "string",
					"description": "Replacement content (replaces the entire line range)",
				},
			},
			"required": []string{"path", "start_line", "end_line", "content"},
		},
	}
}

func (t ReplaceLinesToolImpl) Execute(args json.RawMessage) (string, error) {
	return t.ExecuteCtx(context.Background(), args)
}

func (t ReplaceLinesToolImpl) ExecuteCtx(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Path      string `json:"path"`
		StartLine int    `json:"start_line"`
		EndLine   int    `json:"end_line"`
		Content   string `json:"content"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}
	if params.StartLine < 1 {
		return "", fmt.Errorf("start_line must be >= 1")
	}
	if params.EndLine < params.StartLine {
		return "", fmt.Errorf("end_line must be >= start_line")
	}

	safe, err := confinedPath(params.Path)
	if err != nil {
		return "", err
	}

	prev, err := os.ReadFile(safe)
	if err != nil {
		return "", fmt.Errorf("failed to read file %s: %w", params.Path, err)
	}

	lines := strings.Split(string(prev), "\n")
	total := len(lines)

	if params.StartLine > total {
		return "", fmt.Errorf("start_line=%d exceeds file length (%d lines)", params.StartLine, total)
	}
	end := params.EndLine
	if end > total {
		end = total
	}

	// Replace lines [start_line-1 .. end-1] with the new content lines.
	replacement := strings.Split(params.Content, "\n")
	updated := make([]string, 0, len(lines)-(end-params.StartLine+1)+len(replacement))
	updated = append(updated, lines[:params.StartLine-1]...)
	updated = append(updated, replacement...)
	updated = append(updated, lines[end:]...)

	newContent := strings.Join(updated, "\n")

	tcID := snapshot.ToolCallIDFromContext(ctx)
	store := snapshot.FromContext(ctx)
	if tcID != "" {
		_ = store.Backup(safe, tcID) //nolint:errcheck
	} else {
		_ = store.Backup(safe, "") //nolint:errcheck
	}

	if err := os.WriteFile(safe, []byte(newContent), 0644); err != nil {
		return "", fmt.Errorf("failed to write file %s: %w", params.Path, err)
	}

	if tcID != "" {
		store.RegisterWrite(safe, tcID)
	}

	var formatters map[string]config.FormatterConfig
	if t.Config != nil {
		formatters = t.Config.Formatters
	}
	FormatAfterWrite(safe, formatters)

	return FormatDiff(params.Path, string(prev), newContent), nil
}

type DeleteTool struct{}

func (t DeleteTool) Name() string        { return "delete" }
func (t DeleteTool) Description() string { return "Delete a file or directory" }
func (t DeleteTool) Parallel() bool      { return false }
func (t DeleteTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "delete",
		"description": "Delete a file or directory",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Path to the file or directory to delete",
				},
			},
			"required": []string{"path"},
		},
	}
}

func (t DeleteTool) Execute(args json.RawMessage) (string, error) {
	return t.ExecuteCtx(context.Background(), args)
}

func (t DeleteTool) ExecuteCtx(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	safe, err := confinedPath(params.Path)
	if err != nil {
		return "", err
	}

	tcID := snapshot.ToolCallIDFromContext(ctx)
	store := snapshot.FromContext(ctx)
	if tcID != "" {
		_ = store.Backup(safe, tcID) //nolint:errcheck
	} else {
		_ = store.Backup(safe, "") //nolint:errcheck
	}

	if err := os.RemoveAll(safe); err != nil {
		return "", fmt.Errorf("failed to delete %s: %w", params.Path, err)
	}

	if tcID != "" {
		store.RegisterWrite(safe, tcID)
	}

	return fmt.Sprintf("Successfully deleted %s", params.Path), nil
}

type EditTool struct {
	Config *config.Config
}

func (t EditTool) Name() string        { return "edit" }
func (t EditTool) Description() string { return "Edit a file by replacing a block of text" }
func (t EditTool) Parallel() bool      { return false }
func (t EditTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "edit",
		"description": "Edit a file by replacing a search block with a replace block. The search block must appear exactly once, or replace_all must be true.",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":    map[string]interface{}{"type": "string"},
				"search":  map[string]interface{}{"type": "string"},
				"replace": map[string]interface{}{"type": "string"},
				"replace_all": map[string]interface{}{
					"type":        "boolean",
					"description": "Replace all occurrences of the search block (default: false).",
				},
			},
			"required": []string{"path", "search", "replace"},
		},
	}
}

func (t EditTool) Execute(args json.RawMessage) (string, error) {
	return t.ExecuteCtx(context.Background(), args)
}

func (t EditTool) ExecuteCtx(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Path       string `json:"path"`
		Search     string `json:"search"`
		Replace    string `json:"replace"`
		ReplaceAll bool   `json:"replace_all"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	safe, err := confinedPath(params.Path)
	if err != nil {
		return "", err
	}

	content, err := os.ReadFile(safe)
	if err != nil {
		return "", err
	}

	fileContent := string(content)
	count := strings.Count(fileContent, params.Search)
	if count == 0 {
		return "", fmt.Errorf("search block not found in file")
	}
	if count > 1 && !params.ReplaceAll {
		return "", fmt.Errorf("search block appears %d times; must be unique or set replace_all=true", count)
	}

	tcID := snapshot.ToolCallIDFromContext(ctx)
	store := snapshot.FromContext(ctx)
	if tcID != "" {
		_ = store.Backup(safe, tcID) //nolint:errcheck
	} else {
		_ = store.Backup(safe, "") //nolint:errcheck
	}

	var newContent string
	if params.ReplaceAll {
		newContent = strings.ReplaceAll(fileContent, params.Search, params.Replace)
	} else {
		newContent = strings.Replace(fileContent, params.Search, params.Replace, 1)
	}
	if err = os.WriteFile(safe, []byte(newContent), 0644); err != nil {
		return "", err
	}

	if tcID != "" {
		store.RegisterWrite(safe, tcID)
	}

	var formatters map[string]config.FormatterConfig
	if t.Config != nil {
		formatters = t.Config.Formatters
	}
	FormatAfterWrite(safe, formatters)

	return FormatDiff(params.Path, fileContent, newContent), nil
}

type MultiEditTool struct {
	Config *config.Config
}

func (t MultiEditTool) Name() string { return "multiedit" }
func (t MultiEditTool) Description() string {
	return "Perform multiple edits to a single file atomically"
}
func (t MultiEditTool) Parallel() bool { return false }
func (t MultiEditTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "multiedit",
		"description": "Perform multiple search/replace edits to a single file. All edits are applied in sequence on the result of the previous edit, and all are validated before writing — if any edit fails, none are applied.",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"file_path": map[string]interface{}{"type": "string"},
				"edits": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"oldString":  map[string]interface{}{"type": "string"},
							"newString":  map[string]interface{}{"type": "string"},
							"replaceAll": map[string]interface{}{"type": "boolean"},
						},
						"required": []string{"oldString", "newString"},
					},
				},
			},
			"required": []string{"file_path", "edits"},
		},
	}
}

func (t MultiEditTool) Execute(args json.RawMessage) (string, error) {
	return t.ExecuteCtx(context.Background(), args)
}

func (t MultiEditTool) ExecuteCtx(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		FilePath string `json:"file_path"`
		Edits    []struct {
			OldString  string `json:"oldString"`
			NewString  string `json:"newString"`
			ReplaceAll bool   `json:"replaceAll"`
		} `json:"edits"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	if params.FilePath == "" {
		return "", fmt.Errorf("file_path is required")
	}
	if len(params.Edits) == 0 {
		return "", fmt.Errorf("no edits provided")
	}

	safe, err := confinedPath(params.FilePath)
	if err != nil {
		return "", err
	}

	content, err := os.ReadFile(safe)
	origContent := ""
	fileExists := err == nil
	if err != nil {
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("cannot read %s: %w", params.FilePath, err)
		}
	} else {
		origContent = string(content)
	}

	newContent := origContent
	for i, e := range params.Edits {
		if e.OldString == e.NewString {
			return "", fmt.Errorf("edit %d is a no-op: oldString and newString are identical", i+1)
		}
		if !fileExists && i == 0 && e.OldString == "" {
			newContent = e.NewString
			fileExists = true
			continue
		}

		count := strings.Count(newContent, e.OldString)
		if count == 0 {
			return "", fmt.Errorf("oldString not found in content")
		}
		if count > 1 && !e.ReplaceAll {
			return "", fmt.Errorf("Found multiple matches for oldString. Provide more surrounding lines in oldString to identify the correct match, or use replaceAll.")
		}
		if e.ReplaceAll {
			newContent = strings.ReplaceAll(newContent, e.OldString, e.NewString)
		} else {
			newContent = strings.Replace(newContent, e.OldString, e.NewString, 1)
		}
	}

	tcID := snapshot.ToolCallIDFromContext(ctx)
	store := snapshot.FromContext(ctx)
	if tcID != "" {
		_ = store.Backup(safe, tcID) //nolint:errcheck
	} else {
		_ = store.Backup(safe, "") //nolint:errcheck
	}

	if err := os.MkdirAll(filepath.Dir(safe), 0755); err != nil {
		return "", fmt.Errorf("failed to create directories for %s: %w", params.FilePath, err)
	}
	if err := os.WriteFile(safe, []byte(newContent), 0644); err != nil {
		_ = store.Restore(safe)
		return "", fmt.Errorf("failed to write %s: %w", params.FilePath, err)
	}

	if tcID != "" {
		store.RegisterWrite(safe, tcID)
	}

	var formatters map[string]config.FormatterConfig
	if t.Config != nil {
		formatters = t.Config.Formatters
	}
	FormatAfterWrite(safe, formatters)

	return FormatDiff(params.FilePath, origContent, newContent), nil
}

type MultiFileEditTool struct {
	Config *config.Config
}

func (t MultiFileEditTool) Name() string { return "multi_file_edit" }
func (t MultiFileEditTool) Description() string {
	return "Perform multiple edits across files atomically"
}
func (t MultiFileEditTool) Parallel() bool { return false }
func (t MultiFileEditTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "multi_file_edit",
		"description": "Perform multiple search/replace edits across files. All edits are validated first, then applied atomically — if any edit fails, none are applied.",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"edits": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"path":        map[string]interface{}{"type": "string"},
							"search":      map[string]interface{}{"type": "string"},
							"replace":     map[string]interface{}{"type": "string"},
							"replace_all": map[string]interface{}{"type": "boolean"},
						},
						"required": []string{"path", "search", "replace"},
					},
				},
			},
			"required": []string{"edits"},
		},
	}
}

func (t MultiFileEditTool) Execute(args json.RawMessage) (string, error) {
	return t.ExecuteCtx(context.Background(), args)
}

func (t MultiFileEditTool) ExecuteCtx(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		Edits []struct {
			Path       string `json:"path"`
			Search     string `json:"search"`
			Replace    string `json:"replace"`
			ReplaceAll bool   `json:"replace_all"`
		} `json:"edits"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	if len(params.Edits) == 0 {
		return "", fmt.Errorf("no edits provided")
	}

	type fileEdit struct {
		safe        string
		origContent string
		newContent  string
	}

	byFile := make(map[string]*fileEdit)
	var fileOrder []string

	for i, e := range params.Edits {
		safe, err := confinedPath(e.Path)
		if err != nil {
			return "", err
		}

		fe, exists := byFile[safe]
		if !exists {
			content, err := os.ReadFile(safe)
			if err != nil {
				return "", fmt.Errorf("cannot read %s: %w", e.Path, err)
			}
			fe = &fileEdit{safe: safe, origContent: string(content), newContent: string(content)}
			byFile[safe] = fe
			fileOrder = append(fileOrder, safe)
		}

		if e.Search == e.Replace {
			return "", fmt.Errorf("edit %d is a no-op: search and replace are identical", i+1)
		}

		count := strings.Count(fe.newContent, e.Search)
		if count == 0 {
			return "", fmt.Errorf("search block not found in %s", e.Path)
		}
		if count > 1 && !e.ReplaceAll {
			return "", fmt.Errorf("search block appears %d times in %s; must be unique or set replace_all=true", count, e.Path)
		}
		if e.ReplaceAll {
			fe.newContent = strings.ReplaceAll(fe.newContent, e.Search, e.Replace)
		} else {
			fe.newContent = strings.Replace(fe.newContent, e.Search, e.Replace, 1)
		}
	}

	tcID := snapshot.ToolCallIDFromContext(ctx)
	store := snapshot.FromContext(ctx)

	var backedUp []string
	for _, safe := range fileOrder {
		if tcID != "" {
			_ = store.Backup(safe, tcID) //nolint:errcheck
		} else {
			_ = store.Backup(safe, "") //nolint:errcheck
		}
		backedUp = append(backedUp, safe)
	}

	var formatters map[string]config.FormatterConfig
	if t.Config != nil {
		formatters = t.Config.Formatters
	}

	for _, safe := range fileOrder {
		fe := byFile[safe]
		if fe.newContent == fe.origContent {
			continue
		}
		if err := os.WriteFile(safe, []byte(fe.newContent), 0644); err != nil {
			for _, bu := range backedUp {
				_ = store.Restore(bu)
			}
			return "", fmt.Errorf("failed to write %s: %w", fe.safe, err)
		}
		if tcID != "" {
			store.RegisterWrite(safe, tcID)
		}
		FormatAfterWrite(safe, formatters)
	}

	return fmt.Sprintf("Successfully performed %d edits across %d file(s)", len(params.Edits), len(fileOrder)), nil
}

func ExtraAllowedRoots() []string {
	return getExtraAllowedRoots()
}

// CacheRoots returns the managed cache directories that confinedPath treats as
// allowed beyond the workdir and extra roots: the tool-results cache and the
// cloned-repo cache. Exposed so the permission scope model (AllowedRoots) and
// tool confinement (confinedPath) agree on a single root set — bash reads of
// truncated tool outputs must be auto-allowable just like the read tool's are.
// Paths are symlink-normalized; unresolvable roots are skipped.
func CacheRoots() []string {
	var out []string
	if root, ok := normalizeRootPath(toolResultCacheDir()); ok {
		out = append(out, root)
	}
	if repoCache, err := repoCacheDir(); err == nil {
		if root, ok := normalizeRootPath(repoCache); ok {
			out = append(out, root)
		}
	}
	return out
}
