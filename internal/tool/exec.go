package tool

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/jamesmercstudio/ocode/internal/snapshot"
)

type BashTool struct{}

func (t BashTool) Name() string        { return "bash" }
func (t BashTool) Description() string { return "Execute shell commands in your project environment" }
func (t BashTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "bash",
		"description": "Execute shell commands in your project environment",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"command": map[string]interface{}{
					"type":        "string",
					"description": "The command to run",
				},
			},
			"required": []string{"command"},
		},
	}
}

func (t BashTool) Execute(args json.RawMessage) (string, error) {
	var params struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/C", params.Command)
	} else {
		cmd = exec.Command("bash", "-c", params.Command)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("command failed: %w", err)
	}

	return string(output), nil
}

type EditTool struct{}

func (t EditTool) Name() string        { return "edit" }
func (t EditTool) Description() string { return "Modify existing files using exact string replacements" }
func (t EditTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "edit",
		"description": "Modify existing files using exact string replacements",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Path to the file to edit",
				},
				"old": map[string]interface{}{
					"type":        "string",
					"description": "Exact text to find and replace",
				},
				"new": map[string]interface{}{
					"type":        "string",
					"description": "New text to replace with",
				},
			},
			"required": []string{"path", "old", "new"},
		},
	}
}

func (t EditTool) Execute(args json.RawMessage) (string, error) {
	var params struct {
		Path string `json:"path"`
		Old  string `json:"old"`
		New  string `json:"new"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	// Backup before edit
	snapshot.Backup(params.Path)

	content, err := os.ReadFile(params.Path)
	if err != nil {
		return "", fmt.Errorf("failed to read file %s: %w", params.Path, err)
	}

	text := string(content)
	if !strings.Contains(text, params.Old) {
		return "", fmt.Errorf("exact text to replace not found in %s", params.Path)
	}

	newText := strings.Replace(text, params.Old, params.New, 1)
	if err := os.WriteFile(params.Path, []byte(newText), 0644); err != nil {
		return "", fmt.Errorf("failed to write edited file %s: %w", params.Path, err)
	}

	return fmt.Sprintf("Successfully edited %s", params.Path), nil
}
