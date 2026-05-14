package tool

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

type CustomTool struct {
	ToolName        string                 `json:"name"`
	ToolDescription string                 `json:"description"`
	Parameters      map[string]interface{} `json:"parameters"`
	// Command is a list of strings: ["prog", "arg1", "{{param}}"].
	// Positional placeholders like {{param}} are substituted with the
	// corresponding parameter value as a separate argv element — never
	// concatenated into a shell string — which prevents shell injection.
	Command []string `json:"command"`
}

func (t CustomTool) Name() string        { return t.ToolName }
func (t CustomTool) Description() string { return t.ToolDescription }
func (t CustomTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        t.ToolName,
		"description": t.ToolDescription,
		"parameters":  t.Parameters,
	}
}

func (t CustomTool) Execute(args json.RawMessage) (string, error) {
	if len(t.Command) == 0 {
		return "", fmt.Errorf("custom tool %q has no command defined", t.ToolName)
	}

	var params map[string]interface{}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	// Build argv by substituting {{key}} placeholders in each element.
	// Each argv element is substituted independently — no shell involved.
	argv := make([]string, len(t.Command))
	for i, part := range t.Command {
		for k, v := range params {
			placeholder := "{{" + k + "}}"
			if part == placeholder {
				part = fmt.Sprintf("%v", v)
				break
			}
		}
		argv[i] = part
	}

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command(argv[0], argv[1:]...)
	} else {
		cmd = exec.Command(argv[0], argv[1:]...)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("custom tool failed: %w", err)
	}

	return string(output), nil
}

func LoadCustomTools() []Tool {
	var tools []Tool

	home, _ := os.UserHomeDir()
	globalPath := filepath.Join(home, ".config", "opencode", "tools")
	if runtime.GOOS == "windows" {
		globalPath = filepath.Join(os.Getenv("APPDATA"), "opencode", "tools")
	}

	entries, err := os.ReadDir(globalPath)
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
				data, err := os.ReadFile(filepath.Join(globalPath, e.Name()))
				if err == nil {
					var ct CustomTool
					if err := json.Unmarshal(data, &ct); err == nil {
						tools = append(tools, ct)
					}
				}
			}
		}
	}

	return tools
}
