package tool

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

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
	path string
	mtime int64
}

func (t GlobTool) Execute(args json.RawMessage) (string, error) {
	var params struct {
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
		Ignore  []string `json:"ignore"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	searchDir := "."
	if params.Path != "" {
		searchDir = params.Path
	}

	ign := NewIgnoreMatcher(params.Ignore)

	var matches []globMatch
	err := filepath.Walk(searchDir, func(path string, info os.FileInfo, err error) error {
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
			matches = append(matches, globMatch{path: path, mtime: info.ModTime().UnixNano()})
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("glob failed: %w", err)
	}

	sort.Slice(matches, func(i, j int) bool {
		return matches[i].mtime > matches[j].mtime
	})

	truncated := false
	if len(matches) > globMaxResults {
		matches = matches[:globMaxResults]
		truncated = true
	}

	var paths []string
	for _, m := range matches {
		paths = append(paths, m.path)
	}

	result := strings.Join(paths, "\n")
	if truncated {
		result += fmt.Sprintf("\n\n... (%d+ files matched, showing first %d)", len(matches)+1, globMaxResults)
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

func (t GrepTool) Name() string        { return "grep" }
func (t GrepTool) Description() string { return "Search file contents using regular expressions" }
func (t GrepTool) Parallel() bool      { return true }
func (t GrepTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "grep",
		"description": "Search file contents using regular expressions. Supports include glob, output modes (files_with_matches, content, count), and multiline matching.",
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

	if params.Path == "" {
		params.Path = "."
	}
	if params.OutputMode == "" {
		params.OutputMode = "content"
	}

	var re *regexp.Regexp
	var err error
	if params.Multiline {
		re, err = regexp.Compile(`(?s)` + params.Pattern)
	} else {
		re, err = regexp.Compile(params.Pattern)
	}
	if err != nil {
		return "", fmt.Errorf("invalid regex: %w", err)
	}

	ign := NewIgnoreMatcher(params.Ignore)
	type fileResult struct {
		path  string
		count int
		lines []string
	}
	var fileResults []fileResult

	walkErr := filepath.Walk(params.Path, func(p string, info os.FileInfo, werr error) error {
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
			return nil
		}

		if params.Include != "" && !matchGlob(params.Include, filepath.ToSlash(p)) {
			return nil
		}

		content, readErr := os.ReadFile(p)
		if readErr != nil {
			return nil
		}

		if params.Multiline {
			if re.Match(content) {
				count := len(re.FindAllIndex(content, -1))
				fr := fileResult{path: p, count: count}
				if params.OutputMode == "content" {
					for _, match := range re.FindAll(content, -1) {
						fr.lines = append(fr.lines, string(match))
					}
				}
				fileResults = append(fileResults, fr)
			}
		} else {
			var fr fileResult
			fr.path = p
			scanner := bufio.NewScanner(strings.NewReader(string(content)))
			scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
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

	return strings.TrimRight(b.String(), "\n"), nil
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
	var params struct {
		Path    string   `json:"path"`
		Pattern string   `json:"pattern"`
		Ignore  []string `json:"ignore"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	if params.Path == "" {
		params.Path = "."
	}

	ign := NewIgnoreMatcher(params.Ignore)
	entries, err := os.ReadDir(params.Path)
	if err != nil {
		return "", fmt.Errorf("failed to list directory %s: %w", params.Path, err)
	}

	var results []string
	for _, e := range entries {
		name := e.Name()
		fullPath := filepath.Join(params.Path, name)
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
