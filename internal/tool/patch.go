package tool

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type ApplyPatchTool struct{}

func (t ApplyPatchTool) Name() string        { return "apply_patch" }
func (t ApplyPatchTool) Description() string { return "Apply patches to files" }
func (t ApplyPatchTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "apply_patch",
		"description": "Apply patches to files",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"patchText": map[string]interface{}{
					"type":        "string",
					"description": "The patch content to apply",
				},
			},
			"required": []string{"patchText"},
		},
	}
}

func (t ApplyPatchTool) Execute(args json.RawMessage) (string, error) {
	var params struct {
		PatchText string `json:"patchText"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	// Check if patch command exists
	_, err := exec.LookPath("patch")
	if err != nil {
		return "Error: 'patch' command not found. Please install patch utility for your system (e.g., 'git' for Windows usually includes it).", nil
	}

	cmd := exec.Command("patch", "-p1")
	cmd.Stdin = strings.NewReader(params.PatchText)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("patch failed: %w", err)
	}

	return string(output), nil
}

type TodoWriteTool struct{}

func (t TodoWriteTool) Name() string        { return "todowrite" }
func (t TodoWriteTool) Description() string { return "Manage todo lists during coding sessions" }
func (t TodoWriteTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "todowrite",
		"description": "Manage todo lists during coding sessions",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"todoText": map[string]interface{}{
					"type":        "string",
					"description": "The todo list content",
				},
			},
			"required": []string{"todoText"},
		},
	}
}

func (t TodoWriteTool) Execute(args json.RawMessage) (string, error) {
	var params struct {
		TodoText string `json:"todoText"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	// By default, write to TODO_OCODE.md
	if err := os.WriteFile("TODO_OCODE.md", []byte(params.TodoText), 0644); err != nil {
		return "", err
	}

	return "Successfully updated TODO_OCODE.md", nil
}
