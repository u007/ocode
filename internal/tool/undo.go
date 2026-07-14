package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/u007/ocode/internal/config"
	"github.com/u007/ocode/internal/snapshot"
)

// defaultUndoMaxAgeDelta is the number of agent step increments after which a
// snapshot is considered expired and can no longer be undone. Used when no
// value is configured via ocode.undo_max_age_delta.
const defaultUndoMaxAgeDelta = 4

// UndoTool restores a file to its pre-edit state by tool call ID.
// It implements ContextualTool so it can access the per-agent snapshot store.
type UndoTool struct {
	Config *config.Config
}

// maxAgeDelta returns the configured undo window, falling back to the default.
func (t *UndoTool) maxAgeDelta() int {
	if t.Config != nil && t.Config.Ocode.UndoMaxAgeDelta > 0 {
		return t.Config.Ocode.UndoMaxAgeDelta
	}
	return defaultUndoMaxAgeDelta
}

func (t *UndoTool) Name() string   { return "undo_file_change" }
func (t *UndoTool) Parallel() bool { return false }
func (t *UndoTool) Description() string {
	return fmt.Sprintf("Undo a previous write tool call by its tool_call_id. Reverts all files changed by that call to their pre-edit state. Prefer this over `git checkout`/`git restore` for reverting a file edit you just made — call it promptly, as it is only available within %d agent steps of the original write.", t.maxAgeDelta())
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
	restored, err := store.UndoByToolCallID(params.ToolCallID, t.maxAgeDelta())
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Restored %d file(s): %s", len(restored), strings.Join(restored, ", ")), nil
}
