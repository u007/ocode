package agent

import (
	"encoding/json"
	"strings"

	"github.com/u007/ocode/internal/notebus"
)

// noteGroup is the minimal interface needed to append a touch
// entry. We keep this an interface (not a concrete *Agent) so the
// parallel block can pass a transient {bus, id} pair without
// constructing a full Agent — cheaper, and avoids any future
// temptation to mutate shared agent state from a tool goroutine.
type noteGroup interface {
	groupBus() *notebus.Bus
	groupID() string
}

// appendWriteTouchIfGrouped appends a touch entry to the bus if
// the agent is in a group AND the tool is a write-class tool.
// Read tools (read/glob/grep/bash/ls/list/...) and unknown tools
// produce no touch. Touches are auto-derived (Part 01's design:
// the agent never emits them).
func appendWriteTouchIfGrouped(g noteGroup, toolName, args string) {
	if g == nil {
		return
	}
	bus := g.groupBus()
	id := g.groupID()
	if bus == nil || id == "" {
		return
	}
	if !isWriteTool(toolName) {
		return
	}
	files := extractTouchFilePaths(toolName, args)
	if len(files) == 0 {
		// Defensive: a write tool without a recognizable path
		// produces no touch. The orchestrator still sees the
		// original tool result; this is a non-fatal skip.
		return
	}
	for _, file := range files {
		_, _ = bus.Append(notebus.Touch(0, id, file, "edit", 0))
	}
}

// agentCtx is a tiny adapter that lets a transient {bus, id}
// pair satisfy noteGroup. It holds the bus and id the parallel
// block resolved for this call, without mutating the agent.
type agentCtx struct {
	noteBus     *notebus.Bus
	noteAgentID string
}

func (a *agentCtx) groupBus() *notebus.Bus { return a.noteBus }
func (a *agentCtx) groupID() string        { return a.noteAgentID }

// groupBus / groupID methods on *Agent let a live agent satisfy
// noteGroup directly. We use these in tests.
func (a *Agent) groupBus() *notebus.Bus { return a.noteBus }
func (a *Agent) groupID() string        { return a.noteAgentID }

// extractTouchFilePath returns the file path that a write-class
// tool call operated on, or "" if not extractable. The argument
// keys we recognize (path, file_path, filePath) cover write,
// edit, apply_patch, and the single-file write tools; multi-file
// edits return the first path from their edits[] list.
func extractTouchFilePath(toolName, args string) string {
	paths := extractTouchFilePaths(toolName, args)
	if len(paths) == 0 {
		return ""
	}
	return paths[0]
}

// extractTouchFilePaths returns the file paths that a write-class
// tool call operated on, or nil if not extractable.
func extractTouchFilePaths(toolName, args string) []string {
	var a map[string]any
	if err := json.Unmarshal([]byte(args), &a); err != nil {
		return nil
	}
	paths := make([]string, 0, 4)
	seen := make(map[string]struct{}, 4)
	add := func(path string) {
		if path == "" {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	for _, key := range []string{"path", "file_path", "filePath"} {
		if v, ok := a[key].(string); ok && v != "" {
			add(v)
			if toolName != "multi_file_edit" {
				return []string{paths[0]}
			}
		}
	}
	if toolName == "multi_file_edit" {
		if edits, ok := a["edits"].([]any); ok {
			for _, edit := range edits {
				m, ok := edit.(map[string]any)
				if !ok {
					continue
				}
				if v, ok := m["path"].(string); ok && v != "" {
					add(v)
				}
			}
		}
	}
	if len(paths) == 0 {
		return nil
	}
	return paths
}

// isWriteTool returns true for the write-class tools that the
// bus should observe. The list centralizes mutating-tool
// classification used by both the note bus and the advisor
// checkpoints (plan/done). Read-class tools (read, glob, grep,
// list, lsp, bash, webfetch, websearch, agent_status,
// task_status, …) are never logged — the design explicitly
// forbids read-touches.
func isWriteTool(name string) bool {
	switch strings.ToLower(name) {
	case "write", "edit", "apply_patch", "replace_lines",
		"multiedit", "multi_file_edit", "delete", "format":
		return true
	}
	return false
}
