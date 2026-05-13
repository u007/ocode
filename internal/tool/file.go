package tool

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

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

	content, err := os.ReadFile(params.Path)
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

	if err := os.MkdirAll(filepath.Dir(params.Path), 0755); err != nil {
		return "", fmt.Errorf("failed to create directories for %s: %w", params.Path, err)
	}

	if err := os.WriteFile(params.Path, []byte(params.Content), 0644); err != nil {
		return "", fmt.Errorf("failed to write file %s: %w", params.Path, err)
	}
	return fmt.Sprintf("Successfully wrote to %s", params.Path), nil
}
