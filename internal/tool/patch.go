package tool

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
func (t PatchTool) Description() string { return "Apply patches to files with line range targeting and fuzzy matching" }
func (t PatchTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "patch",
		"description": "Apply patches to files with line range targeting and fuzzy matching",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"patchText": map[string]interface{}{
					"type":        "string",
					"description": "The patch content to apply (unified diff format)",
				},
				"fuzzy": map[string]interface{}{
					"type":        "boolean",
					"description": "Enable fuzzy matching for context lines (default: true)",
				},
			},
			"required": []string{"patchText"},
		},
	}
}

func (t PatchTool) Execute(args json.RawMessage) (string, error) {
	var params struct {
		PatchText string `json:"patchText"`
		Fuzzy     *bool  `json:"fuzzy"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", err
	}

	fuzzy := true
	if params.Fuzzy != nil {
		fuzzy = *params.Fuzzy
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

	if fuzzy {
		result, err := applyPatchFuzzy(params.PatchText)
		if err != nil {
			if backedUp > 0 {
				_ = snapshot.DiscardRecent(backedUp)
			}
			return result, err
		}
		return result, nil
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

type hunk struct {
	origStart int
	origCount int
	newStart  int
	newCount  int
	lines     []string
}

func parseHunkHeader(line string) (*hunk, error) {
	if !strings.HasPrefix(line, "@@") {
		return nil, fmt.Errorf("invalid hunk header: %s", line)
	}

	parts := strings.Split(line, "@@")
	if len(parts) < 3 {
		return nil, fmt.Errorf("malformed hunk header: %s", line)
	}

	header := strings.TrimSpace(parts[1])
	ranges := strings.Split(header, " ")
	if len(ranges) < 2 {
		return nil, fmt.Errorf("invalid hunk range: %s", header)
	}

	h := &hunk{}

	oldRange := strings.TrimPrefix(ranges[0], "-")
	newRange := strings.TrimPrefix(ranges[1], "+")

	if oldStart, oldCount, err := parseRange(oldRange); err == nil {
		h.origStart = oldStart
		h.origCount = oldCount
	}

	if newStart, newCount, err := parseRange(newRange); err == nil {
		h.newStart = newStart
		h.newCount = newCount
	}

	return h, nil
}

func parseRange(s string) (int, int, error) {
	if idx := strings.Index(s, ","); idx != -1 {
		start, err := strconv.Atoi(s[:idx])
		if err != nil {
			return 0, 0, err
		}
		count, err := strconv.Atoi(s[idx+1:])
		if err != nil {
			return 0, 0, err
		}
		return start, count, nil
	}
	start, err := strconv.Atoi(s)
	if err != nil {
		return 0, 0, err
	}
	return start, 1, nil
}

func parseHunks(patchText string) (map[string][]hunk, error) {
	fileHunks := make(map[string][]hunk)
	var currentFile string
	var currentHunk *hunk

	scanner := bufio.NewScanner(strings.NewReader(patchText))
	for scanner.Scan() {
		line := scanner.Text()

		switch {
		case strings.HasPrefix(line, "diff --git "):
			rest := strings.TrimPrefix(line, "diff --git ")
			if idx := strings.LastIndex(rest, " b/"); idx != -1 {
				currentFile = strings.TrimPrefix(rest[idx+1:], "b/")
			}
			currentHunk = nil
		case strings.HasPrefix(line, "@@"):
			h, err := parseHunkHeader(line)
			if err != nil {
				return nil, err
			}
			currentHunk = h
			if currentFile != "" {
				fileHunks[currentFile] = append(fileHunks[currentFile], *h)
			}
		case currentHunk != nil:
			if strings.HasPrefix(line, "-") || strings.HasPrefix(line, "+") || strings.HasPrefix(line, " ") || line == `\ No newline at end of file` {
				idx := len(fileHunks[currentFile]) - 1
				if idx >= 0 {
					h := &fileHunks[currentFile][idx]
					h.lines = append(h.lines, line)
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return fileHunks, nil
}

func applyPatchFuzzy(patchText string) (string, error) {
	fileHunks, err := parseHunks(patchText)
	if err != nil {
		return "", fmt.Errorf("parse hunks: %w", err)
	}

	if len(fileHunks) == 0 {
		return applyPatchSystem(patchText)
	}

	var results []string
	for filePath, hunks := range fileHunks {
		safe, err := confinedPath(filePath)
		if err != nil {
			return "", err
		}

		content, err := os.ReadFile(safe)
		if err != nil {
			return "", fmt.Errorf("read %s: %w", filePath, err)
		}

		lines := strings.Split(string(content), "\n")
		var applied int
		var applyErr error

		for i := len(hunks) - 1; i >= 0; i-- {
			h := hunks[i]
			lines, applyErr = applyHunk(lines, h)
			if applyErr == nil {
				applied++
			}
		}

		if applyErr != nil {
			return fmt.Sprintf("applied %d/%d hunks to %s: %v", applied, len(hunks), filePath, applyErr), applyErr
		}

		newContent := strings.Join(lines, "\n")
		if err := os.WriteFile(safe, []byte(newContent), 0644); err != nil {
			return "", fmt.Errorf("write %s: %w", filePath, err)
		}

		results = append(results, fmt.Sprintf("applied %d hunks to %s", applied, filePath))
	}

	return strings.Join(results, "\n"), nil
}

func applyHunk(lines []string, h hunk) ([]string, error) {
	if len(h.lines) == 0 {
		return lines, nil
	}

	contextLines := make([]string, 0)
	deleteLines := make([]string, 0)
	insertLines := make([]string, 0)

	for _, l := range h.lines {
		if strings.HasPrefix(l, " ") {
			contextLines = append(contextLines, strings.TrimPrefix(l, " "))
		} else if strings.HasPrefix(l, "-") {
			deleteLines = append(deleteLines, strings.TrimPrefix(l, "-"))
		} else if strings.HasPrefix(l, "+") {
			insertLines = append(insertLines, strings.TrimPrefix(l, "+"))
		}
	}

	if len(contextLines) == 0 && len(deleteLines) == 0 {
		insertPos := h.newStart - 1
		if insertPos < 0 {
			insertPos = 0
		}
		if insertPos > len(lines) {
			insertPos = len(lines)
		}
		result := make([]string, 0, len(lines)+len(insertLines))
		result = append(result, lines[:insertPos]...)
		result = append(result, insertLines...)
		result = append(result, lines[insertPos:]...)
		return result, nil
	}

	startIdx := findContextMatch(lines, contextLines, deleteLines, h.origStart-1)
	if startIdx == -1 {
		startIdx = findFuzzyMatch(lines, contextLines, deleteLines)
	}
	if startIdx == -1 {
		return lines, fmt.Errorf("could not match hunk context at line %d", h.origStart)
	}

	if len(deleteLines) > 0 {
		endIdx := startIdx
		for _, dl := range deleteLines {
			if endIdx >= len(lines) || !linesMatch(lines[endIdx], dl) {
				return lines, fmt.Errorf("could not match delete line at line %d", endIdx+1)
			}
			endIdx++
		}

		result := make([]string, 0, len(lines)+len(insertLines)-len(deleteLines))
		result = append(result, lines[:startIdx]...)
		result = append(result, insertLines...)
		result = append(result, lines[endIdx:]...)
		return result, nil
	}

	endIdx := startIdx + len(contextLines)
	result := make([]string, 0, len(lines)+len(insertLines))
	result = append(result, lines[:startIdx]...)
	result = append(result, insertLines...)
	result = append(result, lines[endIdx:]...)
	return result, nil
}

func findContextMatch(lines, contextLines, deleteLines []string, hintIdx int) int {
	if len(contextLines) == 0 {
		return hintIdx
	}

	if hintIdx >= 0 && hintIdx < len(lines) {
		if matchContextAt(lines, contextLines, hintIdx) {
			return hintIdx
		}
	}

	for i := 0; i <= len(lines)-len(contextLines); i++ {
		if matchContextAt(lines, contextLines, i) {
			return i
		}
	}

	return -1
}

func matchContextAt(lines, context []string, start int) bool {
	for i, ctx := range context {
		if start+i >= len(lines) {
			return false
		}
		if !linesMatch(lines[start+i], ctx) {
			return false
		}
	}
	return true
}

func findFuzzyMatch(lines, contextLines, deleteLines []string) int {
	if len(contextLines) == 0 {
		return 0
	}

	bestIdx := -1
	bestScore := 0

	for i := 0; i <= len(lines)-len(contextLines); i++ {
		score := 0
		for j, ctx := range contextLines {
			if i+j < len(lines) && linesFuzzyMatch(lines[i+j], ctx) {
				score++
			}
		}
		if score > bestScore {
			bestScore = score
			bestIdx = i
		}
	}

	if bestScore >= len(contextLines)/2+1 {
		return bestIdx
	}

	return -1
}

func linesMatch(a, b string) bool {
	return strings.TrimSpace(a) == strings.TrimSpace(b)
}

func linesFuzzyMatch(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == b {
		return true
	}
	if len(a) < 3 || len(b) < 3 {
		return false
	}
	aLower := strings.ToLower(a)
	bLower := strings.ToLower(b)
	if strings.Contains(aLower, bLower) || strings.Contains(bLower, aLower) {
		return true
	}
	return false
}

func applyPatchSystem(patchText string) (string, error) {
	_, err := exec.LookPath("patch")
	if err != nil {
		return "Error: 'patch' command not found. Please install patch utility for your system.", nil
	}

	cmd := exec.Command("patch", "-p1")
	cmd.Stdin = strings.NewReader(patchText)
	output, err := cmd.CombinedOutput()
	if err != nil {
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
