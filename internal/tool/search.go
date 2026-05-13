package tool

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
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

	// filepath.Glob doesn't support **
	// For a real implementation we might want a more powerful glob library
	// But let's try a simple approach or use `find` if available

	var matches []string
	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == ".git" || info.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}

		matched, err := filepath.Match(params.Pattern, path)
		if err != nil {
			return err
		}
		// If simple match fails, try a very basic ** simulation by matching the base name if pattern has no slashes
		if !matched && !strings.Contains(params.Pattern, "/") {
			matched, _ = filepath.Match(params.Pattern, filepath.Base(path))
		}

		if matched {
			matches = append(matches, path)
		}
		return nil
	})

	if err != nil {
		return "", err
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

	// Use grep command if available for efficiency
	cmd := exec.Command("grep", "-rnE", params.Pattern, params.Path)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// grep returns non-zero exit code if no matches found
		if len(output) == 0 {
			return "No matches found", nil
		}
		return string(output), nil
	}

	return string(output), nil
}
