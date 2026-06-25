package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/u007/ocode/internal/snapshot"
)

// undoMaxAgeDelta is the number of agent step increments after which a
// snapshot is considered expired and can no longer be undone.
const undoMaxAgeDelta = 2

// UndoTool restores a file to its pre-edit state by tool call ID.
// It implements ContextualTool so it can access the per-agent snapshot store.
type UndoTool struct{}

func (t *UndoTool) Name() string    { return "undo_file_change" }
func (t *UndoTool) Parallel() bool  { return false }
func (t *UndoTool) Description() string {
	return "Undo a previous write tool call by its tool_call_id. Reverts all files changed by that call to their pre-edit state. Only available within 2 agent steps of the original write."
}

func (t *UndoTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "undo_file_change",
		"description": t.Description(),
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tool_call_id": map[string]interface{}{
					"type":        "string",
					"description": "The tool_call_id of the write tool call to undo (write, edit, multi_edit, multi_file_edit, replace_lines, delete).",
				},
			},
			"required": []string{"tool_call_id"},
		},
	}
}

func (t *UndoTool) Execute(args json.RawMessage) (string, error) {
	return t.ExecuteCtx(context.Background(), args)
}

func (t *UndoTool) ExecuteCtx(ctx context.Context, args json.RawMessage) (string, error) {
	var params struct {
		ToolCallID string `json:"tool_call_id"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}
	if params.ToolCallID == "" {
		return "", fmt.Errorf("tool_call_id is required")
	}

	store := snapshot.FromContext(ctx)
	restored, err := store.UndoByToolCallID(params.ToolCallID, undoMaxAgeDelta)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Restored %d file(s): %s", len(restored), strings.Join(restored, ", ")), nil
}
