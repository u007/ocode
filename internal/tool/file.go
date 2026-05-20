package tool

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/jamesmercstudio/ocode/internal/config"
	"github.com/jamesmercstudio/ocode/internal/snapshot"
)

const defaultReadLines = 50
const maxReadLines = 250

var (
	readTrackerMu sync.RWMutex
	readTracker   = make(map[string]bool)
)

// MarkFileRead records that a file has been read in the current session.
func MarkFileRead(path string) {
	readTrackerMu.Lock()
	defer readTrackerMu.Unlock()
	readTracker[path] = true
}

// IsFileRead reports whether a file has been read in the current session.
func IsFileRead(path string) bool {
	readTrackerMu.RLock()
	defer readTrackerMu.RUnlock()
	return readTracker[path]
}

// ResetReadTracker clears the session read tracking state.
func ResetReadTracker() {
	readTrackerMu.Lock()
	defer readTrackerMu.Unlock()
	readTracker = make(map[string]bool)
}

// toolResultCacheDir returns the directory where truncated tool outputs are saved.
func toolResultCacheDir() string {
	if env := os.Getenv("XDG_STATE_HOME"); env != "" {
		return filepath.Join(env, "opencode", "tool-results")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", "opencode", "tool-results")
}

// confinedPath resolves p relative to the process working directory and
// verifies that the result is within that directory or the tool-results cache
// dir (so the model can read back truncated output). It returns the cleaned
// absolute path on success, or an error if the path would escape both allowed
// roots.
func confinedPath(p string) (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("could not determine working directory: %w", err)
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("invalid path %q: %w", p, err)
	}
	rel, err := filepath.Rel(wd, abs)
	if err == nil && !strings.HasPrefix(rel, "..") {
		return abs, nil
	}
	// Also allow reads from the tool-results state directory.
	cacheDir := toolResultCacheDir()
	if cacheRel, err := filepath.Rel(cacheDir, abs); err == nil && !strings.HasPrefix(cacheRel, "..") {
		return abs, nil
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

	MarkFileRead(safe)

	lines := strings.Split(string(content), "\n")
	total := len(lines)

	start := params.StartLine
	if start <= 0 {
		start = 1
	}
	if start > total {
		return fmt.Sprintf("(file has %d lines, start_line=%d is out of range)", total, start), nil
	}

	end := params.EndLine
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
		sb.WriteString(fmt.Sprintf("…(use start_line=%d, limit=50 to continue)\n", end+1))
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

	prev, err := os.ReadFile(safe)
	fileExists := err == nil

	if fileExists && params.Mode != "append" && !IsFileRead(safe) {
		return "", fmt.Errorf("cannot overwrite %s: file must be read with the read tool before writing to it", params.Path)
	}

	snapshot.Backup(safe) //nolint:errcheck

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

func (t ReplaceLinesToolImpl) Name() string        { return "replace_lines" }
func (t ReplaceLinesToolImpl) Description() string { return "Replace a range of lines in a file with new content" }
func (t ReplaceLinesToolImpl) Parallel() bool      { return false }
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

	snapshot.Backup(safe) //nolint:errcheck

	if err := os.WriteFile(safe, []byte(newContent), 0644); err != nil {
		return "", fmt.Errorf("failed to write file %s: %w", params.Path, err)
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

	snapshot.Backup(safe) //nolint:errcheck

	if err := os.RemoveAll(safe); err != nil {
		return "", fmt.Errorf("failed to delete %s: %w", params.Path, err)
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

	snapshot.Backup(safe) //nolint:errcheck
	var newContent string
	if params.ReplaceAll {
		newContent = strings.ReplaceAll(fileContent, params.Search, params.Replace)
	} else {
		newContent = strings.Replace(fileContent, params.Search, params.Replace, 1)
	}
	if err = os.WriteFile(safe, []byte(newContent), 0644); err != nil {
		return "", err
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

func (t MultiEditTool) Name() string        { return "multiedit" }
func (t MultiEditTool) Description() string { return "Perform multiple edits across files atomically" }
func (t MultiEditTool) Parallel() bool      { return false }
func (t MultiEditTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name": "multiedit",
		"description": "Perform multiple search/replace edits across files. All edits are validated first, then applied atomically — if any edit fails, none are applied.",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"edits": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"path":    map[string]interface{}{"type": "string"},
							"search":  map[string]interface{}{"type": "string"},
							"replace": map[string]interface{}{"type": "string"},
						},
						"required": []string{"path", "search", "replace"},
					},
				},
			},
			"required": []string{"edits"},
		},
	}
}

func (t MultiEditTool) Execute(args json.RawMessage) (string, error) {
	var params struct {
		Edits []struct {
			Path    string `json:"path"`
			Search  string `json:"search"`
			Replace string `json:"replace"`
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

	for _, e := range params.Edits {
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

		count := strings.Count(fe.newContent, e.Search)
		if count == 0 {
			return "", fmt.Errorf("search block not found in %s", e.Path)
		}
		if count > 1 {
			return "", fmt.Errorf("search block appears %d times in %s; must be unique", count, e.Path)
		}
		fe.newContent = strings.Replace(fe.newContent, e.Search, e.Replace, 1)
	}

	var backedUp []string
	for _, safe := range fileOrder {
		snapshot.Backup(safe) //nolint:errcheck
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
				_ = snapshot.Restore(bu)
			}
			return "", fmt.Errorf("failed to write %s: %w", fe.safe, err)
		}
		FormatAfterWrite(safe, formatters)
	}

	return fmt.Sprintf("Successfully performed %d edits across %d file(s)", len(params.Edits), len(fileOrder)), nil
}
