package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"time"
)

const bashDefaultTimeout = 300 * time.Second
const bashMaxOutputLength = 30000

type BashTool struct {
	Procs *ProcessRegistry
}

func (t BashTool) Name() string        { return "bash" }
func (t BashTool) Description() string { return "Execute shell commands and return stdout/stderr" }
func (t BashTool) Parallel() bool      { return false }
func (t BashTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "bash",
		"description": fmt.Sprintf("Execute shell commands and return combined stdout and stderr. Timeout: %v (default).", bashDefaultTimeout),
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"command": map[string]interface{}{
					"type":        "string",
					"description": "The command to execute",
				},
				"timeout": map[string]interface{}{
					"type":        "integer",
					"description": fmt.Sprintf("Timeout in seconds (default: %d, max: 600).", int(bashDefaultTimeout.Seconds())),
				},
				"run_in_background": map[string]interface{}{
					"type":        "boolean",
					"description": "Run the command in the background. Returns a process id immediately; poll with bash_output and stop with kill_shell.",
				},
			},
			"required": []string{"command"},
		},
	}
}

func (t BashTool) Execute(args json.RawMessage) (string, error) {
	var params struct {
		Command         string `json:"command"`
		Timeout         int    `json:"timeout"`
		RunInBackground bool   `json:"run_in_background"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	if params.RunInBackground {
		if t.Procs == nil {
			return "", fmt.Errorf("background execution unavailable: no process registry")
		}
		p := t.Procs.StartBackground(params.Command)
		return fmt.Sprintf("Started background process %s. Poll with bash_output(id=%q), stop with kill_shell(id=%q).", p.ID, p.ID, p.ID), nil
	}

	timeout := bashDefaultTimeout
	if params.Timeout > 0 {
		timeout = time.Duration(params.Timeout) * time.Second
		if timeout > 600*time.Second {
			timeout = 600 * time.Second
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", params.Command)
	} else {
		cmd = exec.CommandContext(ctx, "bash", "-c", params.Command)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	var sup *ProcessSupervisor
	if t.Procs != nil {
		sup = t.Procs.Supervisor()
	}
	var waitDone chan error
	id := ""
	if sup != nil {
		waitDone = make(chan error, 1)
		id = fmt.Sprintf("bash-fg-%d-%d", os.Getpid(), time.Now().UnixNano())
		_, regErr := sup.Register(ProcessRegistration{
			ID:               id,
			Command:          params.Command,
			Kind:             ProcessKindBackgroundBash,
			Cmd:              cmd,
			OwnsProcessGroup: runtime.GOOS != "windows",
			StartedAt:        time.Now(),
			waitFn: func() error {
				return <-waitDone
			},
		})
		if regErr != nil {
			return fmt.Sprintf("Command failed: %v", regErr), nil
		}
	}

	err := cmd.Run()
	if waitDone != nil {
		waitDone <- err
	}
	res := stdout.String()
	if stderr.Len() > 0 {
		if res != "" {
			res += "\n"
		}
		res += stderr.String()
	}
	if sup != nil {
		if err == nil {
			sup.MarkExited(id, 0)
		} else {
			code := 1
			if exitErr, ok := err.(*exec.ExitError); ok {
				code = exitErr.ExitCode()
			}
			sup.MarkKilled(id, code)
		}
	}

	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Sprintf("Command timed out after %v. Output so far:\n%s", timeout, truncateOutput(res)), nil
	}

	if err != nil {
		if res == "" {
			return fmt.Sprintf("Command failed: %v", err), nil
		}
		return fmt.Sprintf("Command failed (%v). Output:\n%s", err, truncateOutput(res)), nil
	}

	if strings.TrimSpace(res) == "" {
		return "Command executed successfully (no output).", nil
	}

	return truncateOutput(res), nil
}

func truncateOutput(s string) string {
	if len(s) <= bashMaxOutputLength {
		return s
	}
	return s[:bashMaxOutputLength] + "\n\n... [output truncated, exceeds 30000 chars]"
}
