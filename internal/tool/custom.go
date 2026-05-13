package tool

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type CustomTool struct {
	ToolName        string                 `json:"name"`
	ToolDescription string                 `json:"description"`
	Parameters      map[string]interface{} `json:"parameters"`
	Command         string                 `json:"command"`
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
	var params map[string]interface{}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	cmdStr := t.Command
	for k, v := range params {
		placeholder := fmt.Sprintf("{{%s}}", k)
		cmdStr = strings.ReplaceAll(cmdStr, placeholder, fmt.Sprintf("%v", v))
	}

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/C", cmdStr)
	} else {
		cmd = exec.Command("bash", "-c", cmdStr)
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
