package tool

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// BashOutputTool returns incremental output of a background process.
type BashOutputTool struct {
	Procs *ProcessRegistry
}

func (t BashOutputTool) Name() string        { return "bash_output" }
func (t BashOutputTool) Description() string { return "Read new output from a background process" }
func (t BashOutputTool) Parallel() bool      { return true }
func (t BashOutputTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "bash_output",
		"description": "Read output produced since the last bash_output call for a background process, plus its status and exit code.",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"id": map[string]interface{}{
					"type":        "string",
					"description": "The background process id returned by bash(run_in_background).",
				},
			},
			"required": []string{"id"},
		},
	}
}

func (t BashOutputTool) Execute(args json.RawMessage) (string, error) {
	var params struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}
	if t.Procs == nil {
		return "", fmt.Errorf("no process registry")
	}
	text, status, code, err := t.Procs.Output(params.ID)
	if err != nil {
		return fmt.Sprintf("Error: %v", err), nil
	}
	header := fmt.Sprintf("[%s status=%s", params.ID, status)
	if status != ProcRunning {
		header += fmt.Sprintf(" exit=%d", code)
	}
	header += "]\n"
	if text == "" {
		return header + "(no new output)", nil
	}
	return header + text, nil
}

// KillShellTool terminates a background process.
type KillShellTool struct {
	Procs *ProcessRegistry
}

func (t KillShellTool) Name() string        { return "kill_shell" }
func (t KillShellTool) Description() string { return "Kill a background process" }
func (t KillShellTool) Parallel() bool      { return true }
func (t KillShellTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "kill_shell",
		"description": "Terminate a background process started with bash(run_in_background). Idempotent.",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"id": map[string]interface{}{
					"type":        "string",
					"description": "The background process id to kill.",
				},
			},
			"required": []string{"id"},
		},
	}
}

func (t KillShellTool) Execute(args json.RawMessage) (string, error) {
	var params struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}
	if t.Procs == nil {
		return "", fmt.Errorf("no process registry")
	}
	msg, err := t.Procs.Kill(params.ID)
	if err != nil {
		return fmt.Sprintf("Error: %v", err), nil
	}
	return msg, nil
}

// ListProcessesTool lists still-running background processes started by ocode.
type ListProcessesTool struct {
	Procs *ProcessRegistry
}

func (t ListProcessesTool) Name() string        { return "list_processes" }
func (t ListProcessesTool) Description() string { return "List running background processes" }
func (t ListProcessesTool) Parallel() bool      { return true }
func (t ListProcessesTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "list_processes",
		"description": "List background processes started with bash(run_in_background) that are still running, sorted by start time (oldest first).",
		"parameters": map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	}
}

func (t ListProcessesTool) Execute(_ json.RawMessage) (string, error) {
	if t.Procs == nil {
		return "", fmt.Errorf("no process registry")
	}
	procs := t.Procs.Snapshot()
	running := make([]ProcInfo, 0, len(procs))
	for _, p := range procs {
		if p.Status == ProcRunning {
			running = append(running, p)
		}
	}
	if len(running) == 0 {
		return "(no running processes)", nil
	}
	sort.Slice(running, func(i, j int) bool {
		return running[i].StartedAt.Before(running[j].StartedAt)
	})
	var b strings.Builder
	for _, p := range running {
		fmt.Fprintf(&b, "%s [%s] started=%s %s\n", p.ID, p.Status, p.StartedAt.Format("2006-01-02T15:04:05Z07:00"), p.Command)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}
