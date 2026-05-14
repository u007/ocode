package tool

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

type BashTool struct{}

func (t BashTool) Name() string        { return "bash" }
func (t BashTool) Description() string { return "Execute shell commands and return stdout/stderr" }
func (t BashTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "bash",
		"description": "Execute shell commands and return combined stdout and stderr",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"command": map[string]interface{}{
					"type":        "string",
					"description": "The command to execute",
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
	res := string(output)
	if err != nil {
		if res == "" {
			return fmt.Sprintf("Command failed: %v", err), nil
		}
		return fmt.Sprintf("Command failed (%v). Output:\n%s", err, res), nil
	}

	if strings.TrimSpace(res) == "" {
		return "Command executed successfully (no output).", nil
	}

	return res, nil
}
