package tool

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type GlobTool struct{}

func (t GlobTool) Name() string        { return "glob" }
func (t GlobTool) Description() string { return "Find files by pattern matching" }
func (t GlobTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "glob",
		"description": "Find files by pattern matching",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"pattern": map[string]interface{}{
					"type":        "string",
					"description": "Glob pattern like **/*.js or src/**/*.ts",
				},
			},
			"required": []string{"pattern"},
		},
	}
}

func (t GlobTool) Execute(args json.RawMessage) (string, error) {
	var params struct {
		Pattern string `json:"pattern"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	ign := NewIgnoreMatcher()

	// Basic support for **
	// Replace ** with a regex that matches anything across directories
	regexPattern := regexp.QuoteMeta(params.Pattern)
	regexPattern = strings.ReplaceAll(regexPattern, "\\*\\*", ".*")
	regexPattern = strings.ReplaceAll(regexPattern, "\\*", "[^/]*")
	if !strings.HasPrefix(regexPattern, ".*") {
		regexPattern = ".*" + regexPattern
	}
	re, err := regexp.Compile("^" + regexPattern + "$")
	if err != nil {
		return "", fmt.Errorf("invalid pattern: %w", err)
	}

	var matches []string
	err = filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return fmt.Errorf("error walking path %s: %w", path, err)
		}
		if path == "." {
			return nil
		}

		if ign.IsIgnored(path, info.IsDir()) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		path = filepath.ToSlash(path) // normalize for regex
		if info.IsDir() {
			if info.Name() == ".git" || info.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}

		if re.MatchString(path) || re.MatchString("./"+path) {
			matches = append(matches, path)
		}
		return nil
	})

	if err != nil {
		return "", fmt.Errorf("glob failed: %w", err)
	}

	if len(matches) == 0 {
		return "No files matched", nil
	}

	return strings.Join(matches, "\n"), nil
}

type GrepTool struct{}

func (t GrepTool) Name() string        { return "grep" }
func (t GrepTool) Description() string { return "Search file contents using regular expressions" }
func (t GrepTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "grep",
		"description": "Search file contents using regular expressions",
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
			},
			"required": []string{"pattern"},
		},
	}
}

func (t GrepTool) Execute(args json.RawMessage) (string, error) {
	var params struct {
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	if params.Path == "" {
		params.Path = "."
	}

	re, err := regexp.Compile(params.Pattern)
	if err != nil {
		return "", fmt.Errorf("invalid regex: %w", err)
	}

	ign := NewIgnoreMatcher()
	var results []string
	err = filepath.Walk(params.Path, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return fmt.Errorf("error walking path %s: %w", path, err)
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

		file, err := os.Open(path)
		if err != nil {
			return nil // skip unreadable files
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		lineNum := 1
		for scanner.Scan() {
			line := scanner.Text()
			if re.MatchString(line) {
				results = append(results, fmt.Sprintf("%s:%d:%s", path, lineNum, line))
			}
			lineNum++
		}
		return nil
	})

	if err != nil {
		return "", fmt.Errorf("grep failed: %w", err)
	}

	if len(results) == 0 {
		return "No matches found", nil
	}

	return strings.Join(results, "\n"), nil
}

type ListTool struct{}

func (t ListTool) Name() string        { return "list" }
func (t ListTool) Description() string { return "List files and directories in a given path" }
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
			},
		},
	}
}

func (t ListTool) Execute(args json.RawMessage) (string, error) {
	var params struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	if params.Path == "" {
		params.Path = "."
	}

	ign := NewIgnoreMatcher()
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
