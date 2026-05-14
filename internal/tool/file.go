package tool

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jamesmercstudio/ocode/internal/snapshot"
)

// confinedPath resolves p relative to the process working directory and
// verifies that the result is within that directory. It returns the cleaned
// absolute path on success, or an error if the path would escape the working
// directory.
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
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("path %q is outside the working directory", p)
	}
	return abs, nil
}

type ReadTool struct{}

func (t ReadTool) Name() string        { return "read" }
func (t ReadTool) Description() string { return "Read file contents from the codebase" }
func (t ReadTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "read",
		"description": "Read file contents from the codebase",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Path to the file to read",
				},
			},
			"required": []string{"path"},
		},
	}
}

func (t ReadTool) Execute(args json.RawMessage) (string, error) {
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

	content, err := os.ReadFile(safe)
	if err != nil {
		return "", fmt.Errorf("failed to read file %s: %w", params.Path, err)
	}
	return string(content), nil
}

type WriteTool struct{}

func (t WriteTool) Name() string        { return "write" }
func (t WriteTool) Description() string { return "Create new files or overwrite existing ones" }
func (t WriteTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "write",
		"description": "Create new files or overwrite existing ones",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Path to the file to write",
				},
				"content": map[string]interface{}{
					"type":        "string",
					"description": "Content to write to the file",
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
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	safe, err := confinedPath(params.Path)
	if err != nil {
		return "", err
	}

	snapshot.Backup(safe) //nolint:errcheck

	if err := os.MkdirAll(filepath.Dir(safe), 0755); err != nil {
		return "", fmt.Errorf("failed to create directories for %s: %w", params.Path, err)
	}

	if err := os.WriteFile(safe, []byte(params.Content), 0644); err != nil {
		return "", fmt.Errorf("failed to write file %s: %w", params.Path, err)
	}
	return fmt.Sprintf("Successfully wrote to %s", params.Path), nil
}

type DeleteTool struct{}

func (t DeleteTool) Name() string        { return "delete" }
func (t DeleteTool) Description() string { return "Delete a file or directory" }
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

type EditTool struct{}

func (t EditTool) Name() string        { return "edit" }
func (t EditTool) Description() string { return "Edit a file by replacing a block of text" }
func (t EditTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "edit",
		"description": "Edit a file by replacing a search block with a replace block",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":    map[string]interface{}{"type": "string"},
				"search":  map[string]interface{}{"type": "string"},
				"replace": map[string]interface{}{"type": "string"},
			},
			"required": []string{"path", "search", "replace"},
		},
	}
}

func (t EditTool) Execute(args json.RawMessage) (string, error) {
	var params struct {
		Path    string `json:"path"`
		Search  string `json:"search"`
		Replace string `json:"replace"`
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

	if !strings.Contains(string(content), params.Search) {
		return "", fmt.Errorf("search block not found in file")
	}

	snapshot.Backup(safe) //nolint:errcheck
	newContent := strings.Replace(string(content), params.Search, params.Replace, 1)
	if err = os.WriteFile(safe, []byte(newContent), 0644); err != nil {
		return "", err
	}

	return fmt.Sprintf("Successfully edited %s", params.Path), nil
}

type MultiEditTool struct{}

func (t MultiEditTool) Name() string        { return "multiedit" }
func (t MultiEditTool) Description() string { return "Perform multiple edits across files" }
func (t MultiEditTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name": "multiedit",
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

	for _, e := range params.Edits {
		edit := EditTool{}
		data, _ := json.Marshal(e)
		_, err := edit.Execute(data)
		if err != nil {
			return "", fmt.Errorf("edit failed for %s: %w", e.Path, err)
		}
	}

	return fmt.Sprintf("Successfully performed %d edits", len(params.Edits)), nil
}
