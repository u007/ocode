package tool

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/jamesmercstudio/ocode/internal/snapshot"
)

var (
	todoMu             sync.RWMutex
	todoStates         map[string]string
	currentTodoSession string
)

type PatchTool struct{}

func (t PatchTool) Name() string        { return "patch" }
func (t PatchTool) Description() string { return "Apply patches to files" }
func (t PatchTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "patch",
		"description": "Apply patches to files",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"patchText": map[string]interface{}{
					"type":        "string",
					"description": "The patch content to apply",
				},
			},
			"required": []string{"patchText"},
		},
	}
}

func (t PatchTool) Execute(args json.RawMessage) (string, error) {
	var params struct {
		PatchText string `json:"patchText"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	// Check if patch command exists
	_, err := exec.LookPath("patch")
	if err != nil {
		return "Error: 'patch' command not found. Please install patch utility for your system (e.g., 'git' for Windows usually includes it).", nil
	}

	targets, err := patchTargets(params.PatchText)
	if err != nil {
		return "", err
	}

	var backedUp int
	for _, path := range targets {
		safe, err := confinedPath(path)
		if err != nil {
			if backedUp > 0 {
				_ = snapshot.DiscardRecent(backedUp)
			}
			return "", err
		}
		if err := snapshot.Backup(safe); err != nil {
			if backedUp > 0 {
				_ = snapshot.DiscardRecent(backedUp)
			}
			return "", fmt.Errorf("failed to back up %s before patch: %w", safe, err)
		}
		backedUp++
	}

	cmd := exec.Command("patch", "-p1")
	cmd.Stdin = strings.NewReader(params.PatchText)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if backedUp > 0 {
			_ = snapshot.DiscardRecent(backedUp)
		}
		return string(output), fmt.Errorf("patch failed: %w", err)
	}

	return string(output), nil
}

func patchTargets(patchText string) ([]string, error) {
	seen := make(map[string]struct{})
	var targets []string

	scanner := bufio.NewScanner(strings.NewReader(patchText))
	for scanner.Scan() {
		line := scanner.Text()
		var path string

		switch {
		case strings.HasPrefix(line, "diff --git "):
			rest := strings.TrimPrefix(line, "diff --git ")
			if idx := strings.LastIndex(rest, " b/"); idx != -1 {
				path = strings.TrimPrefix(rest[idx+1:], "b/")
			}
		case strings.HasPrefix(line, "+++ "):
			path = strings.TrimSpace(strings.TrimPrefix(line, "+++ "))
			if cut, _, ok := strings.Cut(path, "\t"); ok {
				path = cut
			}
			if path == "/dev/null" {
				path = ""
			}
		case strings.HasPrefix(line, "--- "):
			path = strings.TrimSpace(strings.TrimPrefix(line, "--- "))
			if cut, _, ok := strings.Cut(path, "\t"); ok {
				path = cut
			}
			if path == "/dev/null" {
				path = ""
			}
		case strings.HasPrefix(line, "*** Update File: "), strings.HasPrefix(line, "*** Delete File: "):
			path = strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
		}

		if path == "" {
			continue
		}
		path = filepath.Clean(strings.TrimPrefix(strings.TrimPrefix(path, "a/"), "b/"))
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		targets = append(targets, path)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return targets, nil
}

type TodoWriteTool struct{}

func (t TodoWriteTool) Name() string        { return "todowrite" }
func (t TodoWriteTool) Description() string { return "Manage todo lists during coding sessions" }
func (t TodoWriteTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "todowrite",
		"description": "Manage todo lists during coding sessions",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"todoText": map[string]interface{}{
					"type":        "string",
					"description": "The todo list content",
				},
			},
			"required": []string{"todoText"},
		},
	}
}

func (t TodoWriteTool) Execute(args json.RawMessage) (string, error) {
	var params struct {
		TodoText string `json:"todoText"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	todoMu.Lock()
	if todoStates == nil {
		todoStates = make(map[string]string)
	}
	if currentTodoSession == "" {
		todoMu.Unlock()
		return "", fmt.Errorf("todo session not set")
	}
	todoStates[currentTodoSession] = params.TodoText
	todoMu.Unlock()

	return "Successfully updated todo list", nil
}

type TodoReadTool struct{}

func (t TodoReadTool) Name() string        { return "todoread" }
func (t TodoReadTool) Description() string { return "Read the current session todo list" }
func (t TodoReadTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "todoread",
		"description": "Read the current session todo list",
		"parameters":  map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
	}
}

func (t TodoReadTool) Execute(args json.RawMessage) (string, error) {
	todoMu.RLock()
	content := todoStates[currentTodoSession]
	todoMu.RUnlock()
	if content == "" {
		return "No todo list found", nil
	}
	return string(content), nil
}

func SetTodoSession(sessionID string) {
	todoMu.Lock()
	currentTodoSession = sessionID
	if todoStates == nil {
		todoStates = make(map[string]string)
	}
	todoMu.Unlock()
}

func TodoState() string {
	todoMu.RLock()
	defer todoMu.RUnlock()
	if todoStates == nil {
		return ""
	}
	return todoStates[currentTodoSession]
}

func SetTodoState(state string) {
	todoMu.Lock()
	if currentTodoSession == "" {
		todoMu.Unlock()
		return
	}
	if todoStates == nil {
		todoStates = make(map[string]string)
	}
	todoStates[currentTodoSession] = state
	todoMu.Unlock()
}

func ResetTodoState() {
	todoMu.Lock()
	if todoStates != nil {
		delete(todoStates, currentTodoSession)
	}
	todoMu.Unlock()
}
