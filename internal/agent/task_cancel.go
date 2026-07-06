package agent

import (
	"encoding/json"
	"fmt"
)

// TaskCancelTool cancels a background task the caller dispatched.
// Cooperative — stops at the next step boundary. Use after another
// racing agent already returned a sufficient answer.
type TaskCancelTool struct {
	runs             *AgentRunRegistry
	dispatcherForCall func() string
}

func (t TaskCancelTool) Name() string        { return "task_cancel" }
func (t TaskCancelTool) Description() string { return "Cancel a background task you dispatched. Cooperative — stops at the next step boundary. Use after another racing agent already returned a sufficient answer." }
func (t TaskCancelTool) Parallel() bool      { return true }
func (t TaskCancelTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "task_cancel",
		"description": "Cancel a background task you dispatched. Cooperative — stops at the next step boundary. Use after another racing agent already returned a sufficient answer.",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"task_id": map[string]interface{}{
					"type":        "string",
					"description": "The task_id returned by the task tool",
				},
			},
			"required": []string{"task_id"},
		},
	}
}

func (t TaskCancelTool) Execute(args json.RawMessage) (string, error) {
	var params struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}
	if params.TaskID == "" {
		return "", fmt.Errorf("task_id is required")
	}
	if t.runs == nil {
		return "", fmt.Errorf("no agent run registry")
	}

	dispatcher := ""
	if t.dispatcherForCall != nil {
		dispatcher = t.dispatcherForCall()
	}

	err := t.runs.CancelOwned(params.TaskID, dispatcher)
	if err != nil {
		return "", fmt.Errorf("cancel failed: %w", err)
	}

	// After CancelOwned succeeds (or is a no-op), read final status.
	run, ok := t.runs.Get(params.TaskID)
	if !ok {
		return fmt.Sprintf("task_id: %s\nstate: cancelled\n\n<task_result>\nCancelled.\n</task_result>", params.TaskID), nil
	}
	status := run.statusValue()
	if status == RunCancelled {
		return fmt.Sprintf("task_id: %s\nstate: cancelled\n\n<task_result>\nCancelled.\n</task_result>", params.TaskID), nil
	}
	// Already terminal (done/failed) is a reported no-op.
	state := "error"
	tag := "task_error"
	text := run.Err
	if status == RunDone {
		state = "completed"
		tag = "task_result"
		text = run.Result
	}
	return fmt.Sprintf("task_id: %s\nstate: %s\n\n<%s>\n%s\n</%s>", params.TaskID, state, tag, text, tag), nil
}
