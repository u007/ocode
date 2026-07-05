package tool

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode"

	"github.com/u007/ocode/internal/snapshot"
)

var (
	todoMu             sync.RWMutex
	todoStates         map[string]string
	currentTodoSession string
)

const patchDescription = `Use the apply_patch tool to edit files. Your patch language is a stripped-down, file-oriented diff format designed to be easy to parse and safe to apply. You can think of it as a high-level envelope:

*** Begin Patch
[ one or more file sections ]
*** End Patch

Within that envelope, you get a sequence of file operations.
You MUST include a header to specify the action you are taking.
Each operation starts with one of three headers:

*** Add File: <path> - create a new file. Every following line is a + line (the initial contents).
*** Delete File: <path> - remove an existing file. Nothing follows.
*** Update File: <path> - patch an existing file in place (optionally with a rename).

Example patch:

` + "```" + `
*** Begin Patch
*** Add File: hello.txt
+Hello world
*** Update File: src/app.py
*** Move to: src/main.py
@@ def greet():
-print("Hi")
+print("Hello, world!")
*** Delete File: obsolete.txt
*** End Patch
` + "```" + `

It is important to remember:

- You must include a header with your intended action (Add/Delete/Update)
- You must prefix new lines with ` + "`+`" + ` even when creating a new file`

// patchHunk represents a parsed file operation.
type patchHunk struct {
	typ      string // "add", "delete", "update"
	path     string
	movePath string
	contents string        // for add
	chunks   []updateChunk // for update
}

type updateChunk struct {
	oldLines      []string
	newLines      []string
	changeContext string
	isEndOfFile   bool
}

// parsePatch parses the *** Begin Patch / *** End Patch envelope.
func parsePatch(patchText string) ([]patchHunk, error) {
	text := strings.TrimSpace(patchText)
	lines := strings.Split(text, "\n")

	beginIdx := -1
	endIdx := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == "*** Begin Patch" {
			beginIdx = i
		} else if strings.TrimSpace(l) == "*** End Patch" {
			endIdx = i
			break
		}
	}
	if beginIdx == -1 || endIdx == -1 || beginIdx >= endIdx {
		return nil, fmt.Errorf("invalid patch format: missing *** Begin Patch / *** End Patch markers")
	}

	var hunks []patchHunk
	i := beginIdx + 1
	for i < endIdx {
		line := lines[i]
		switch {
		case strings.HasPrefix(line, "*** Add File:"):
			path := strings.TrimSpace(strings.TrimPrefix(line, "*** Add File:"))
			if path == "" {
				i++
				continue
			}
			content, nextIdx := parseAddFileContent(lines, i+1, endIdx)
			hunks = append(hunks, patchHunk{typ: "add", path: path, contents: content})
			i = nextIdx

		case strings.HasPrefix(line, "*** Delete File:"):
			path := strings.TrimSpace(strings.TrimPrefix(line, "*** Delete File:"))
			if path == "" {
				i++
				continue
			}
			hunks = append(hunks, patchHunk{typ: "delete", path: path})
			i++

		case strings.HasPrefix(line, "*** Update File:"):
			path := strings.TrimSpace(strings.TrimPrefix(line, "*** Update File:"))
			if path == "" {
				i++
				continue
			}
			movePath := ""
			nextIdx := i + 1
			if nextIdx < endIdx && strings.HasPrefix(lines[nextIdx], "*** Move to:") {
				movePath = strings.TrimSpace(strings.TrimPrefix(lines[nextIdx], "*** Move to:"))
				nextIdx++
			}
			chunks, after := parseUpdateChunks(lines, nextIdx, endIdx)
			hunks = append(hunks, patchHunk{
				typ:      "update",
				path:     path,
				movePath: movePath,
				chunks:   chunks,
			})
			i = after

		default:
			i++
		}
	}
	return hunks, nil
}

func parseAddFileContent(lines []string, start, end int) (string, int) {
	var sb strings.Builder
	i := start
	for i < end && !strings.HasPrefix(lines[i], "***") {
		if strings.HasPrefix(lines[i], "+") {
			sb.WriteString(lines[i][1:])
			sb.WriteByte('\n')
		}
		i++
	}
	content := sb.String()
	content = strings.TrimSuffix(content, "\n")
	return content, i
}

func parseUpdateChunks(lines []string, start, end int) ([]updateChunk, int) {
	var chunks []updateChunk
	i := start
	for i < end && !strings.HasPrefix(lines[i], "***") {
		if !strings.HasPrefix(lines[i], "@@") {
			i++
			continue
		}
		ctx := strings.TrimSpace(lines[i][2:])
		i++
		var oldLines, newLines []string
		isEOF := false
		for i < end && !strings.HasPrefix(lines[i], "@@") && !strings.HasPrefix(lines[i], "***") {
			l := lines[i]
			if l == "*** End of File" {
				isEOF = true
				i++
				break
			}
			if strings.HasPrefix(l, " ") {
				content := l[1:]
				oldLines = append(oldLines, content)
				newLines = append(newLines, content)
			} else if strings.HasPrefix(l, "-") {
				oldLines = append(oldLines, l[1:])
			} else if strings.HasPrefix(l, "+") {
				newLines = append(newLines, l[1:])
			}
			i++
		}
		chunks = append(chunks, updateChunk{
			oldLines:      oldLines,
			newLines:      newLines,
			changeContext: ctx,
			isEndOfFile:   isEOF,
		})
	}
	return chunks, i
}

// deriveNewContents applies update chunks to original file lines.
func deriveNewContents(originalLines []string, filePath string, chunks []updateChunk) ([]string, error) {
	lines := make([]string, len(originalLines))
	copy(lines, originalLines)

	// Drop trailing empty element for consistent counting.
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	type replacement struct {
		start  int
		oldLen int
		newSeg []string
	}
	var replacements []replacement
	lineIdx := 0

	for _, chunk := range chunks {
		if chunk.changeContext != "" {
			idx := seekSequence(lines, []string{chunk.changeContext}, lineIdx, false)
			if idx == -1 {
				// Show file context around where we were searching
				contextSnippet := getNearbyLines(lines, lineIdx, 5)
				return nil, fmt.Errorf("failed to find context %q in %s\n\nSearched from line %d. Nearby file content:\n%s", chunk.changeContext, filePath, lineIdx+1, contextSnippet)
			}
			lineIdx = idx + 1
		}

		if len(chunk.oldLines) == 0 {
			insertAt := len(lines)
			if insertAt > 0 && lines[insertAt-1] == "" {
				insertAt--
			}
			replacements = append(replacements, replacement{insertAt, 0, chunk.newLines})
			continue
		}

		pattern := chunk.oldLines
		newSlice := chunk.newLines
		found := seekSequence(lines, pattern, lineIdx, chunk.isEndOfFile)

		// Retry without trailing empty line.
		if found == -1 && len(pattern) > 0 && pattern[len(pattern)-1] == "" {
			pattern2 := pattern[:len(pattern)-1]
			newSlice2 := newSlice
			if len(newSlice2) > 0 && newSlice2[len(newSlice2)-1] == "" {
				newSlice2 = newSlice2[:len(newSlice2)-1]
			}
			found = seekSequence(lines, pattern2, lineIdx, chunk.isEndOfFile)
			if found != -1 {
				pattern = pattern2
				newSlice = newSlice2
			}
		}

		if found == -1 {
			// Show the expected lines vs actual file content at the search location
			expectedSnippet := strings.Join(chunk.oldLines[:min(len(chunk.oldLines), 3)], "\n")
			actualSnippet := getNearbyLines(lines, lineIdx, 5)
			return nil, fmt.Errorf("failed to find expected lines in %s:\n\nExpected (from patch):\n%s\n\nActual file content near line %d:\n%s", filePath, expectedSnippet, lineIdx+1, actualSnippet)
		}
		replacements = append(replacements, replacement{found, len(pattern), newSlice})
		lineIdx = found + len(pattern)
	}

	// Apply replacements in reverse order.
	for r := len(replacements) - 1; r >= 0; r-- {
		rep := replacements[r]
		after := append([]string{}, lines[rep.start+rep.oldLen:]...)
		lines = append(lines[:rep.start], rep.newSeg...)
		lines = append(lines, after...)
	}

	// Ensure trailing newline sentinel.
	if len(lines) == 0 || lines[len(lines)-1] != "" {
		lines = append(lines, "")
	}
	return lines, nil
}

func seekSequence(lines, pattern []string, startIdx int, eof bool) int {
	if len(pattern) == 0 {
		return -1
	}
	type comparator func(a, b string) bool
	passes := []comparator{
		func(a, b string) bool { return a == b },
		func(a, b string) bool { return strings.TrimRight(a, " \t") == strings.TrimRight(b, " \t") },
		func(a, b string) bool { return strings.TrimSpace(a) == strings.TrimSpace(b) },
		func(a, b string) bool {
			return normalizeUnicode(strings.TrimSpace(a)) == normalizeUnicode(strings.TrimSpace(b))
		},
	}
	tryMatch := func(cmp comparator) int {
		if eof {
			fromEnd := len(lines) - len(pattern)
			if fromEnd >= startIdx {
				match := true
				for j, p := range pattern {
					if !cmp(lines[fromEnd+j], p) {
						match = false
						break
					}
				}
				if match {
					return fromEnd
				}
			}
		}
		for i := startIdx; i <= len(lines)-len(pattern); i++ {
			match := true
			for j, p := range pattern {
				if !cmp(lines[i+j], p) {
					match = false
					break
				}
			}
			if match {
				return i
			}
		}
		return -1
	}
	for _, cmp := range passes {
		if idx := tryMatch(cmp); idx != -1 {
			return idx
		}
	}
	return -1
}

// getNearbyLines returns a formatted snippet of lines around the given index for error context.
func getNearbyLines(lines []string, aroundIdx, contextLines int) string {
	if len(lines) == 0 {
		return "(empty file)"
	}

	start := aroundIdx - contextLines
	if start < 0 {
		start = 0
	}
	end := aroundIdx + contextLines + 1
	if end > len(lines) {
		end = len(lines)
	}

	var sb strings.Builder
	for i := start; i < end; i++ {
		marker := "  "
		if i == aroundIdx {
			marker = "> " // Mark the expected position
		}
		sb.WriteString(fmt.Sprintf("%s%4d | %s\n", marker, i+1, lines[i]))
	}
	return sb.String()
}

func normalizeUnicode(s string) string {
	var sb strings.Builder
	for _, r := range s {
		switch {
		case r == '‘' || r == '’' || r == '‚' || r == '‛':
			sb.WriteByte('\'')
		case r == '“' || r == '”' || r == '„' || r == '‟':
			sb.WriteByte('"')
		case r >= '‐' && r <= '―':
			sb.WriteByte('-')
		case r == '…':
			sb.WriteString("...")
		case r == ' ':
			sb.WriteByte(' ')
		default:
			if unicode.IsPrint(r) {
				sb.WriteRune(r)
			}
		}
	}
	return sb.String()
}

// PatchTool applies file patches in the opencode marker format.
type PatchTool struct{}

func (t PatchTool) Name() string { return "apply_patch" }
func (t PatchTool) Description() string {
	return "Apply patches to files using the opencode patch format"
}
func (t PatchTool) Parallel() bool { return false }
func (t PatchTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "apply_patch",
		"description": patchDescription,
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"patchText": map[string]interface{}{
					"type":        "string",
					"description": "The full patch text that describes all changes to be made",
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
	if params.PatchText == "" {
		return "", fmt.Errorf("patchText is required")
	}

	hunks, err := parsePatch(params.PatchText)
	if err != nil {
		return "", err
	}
	if len(hunks) == 0 {
		return "", fmt.Errorf("patch rejected: no file operations found")
	}

	// Snapshot all affected files before applying.
	var backedUp int
	for _, h := range hunks {
		if h.typ == "add" {
			continue
		}
		safe, err := confinedPath(h.path)
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
			return "", fmt.Errorf("failed to back up %s: %w", safe, err)
		}
		backedUp++
	}

	var (
		results      []string
		cleanupPaths []string
	)
	rollback := func(cause error) (string, error) {
		for i := len(cleanupPaths) - 1; i >= 0; i-- {
			if err := os.Remove(cleanupPaths[i]); err != nil && !os.IsNotExist(err) {
				return strings.Join(results, "\n"), fmt.Errorf("%w (rollback cleanup failed: %v)", cause, err)
			}
		}
		if backedUp > 0 {
			seen := make(map[string]struct{}, backedUp)
			for i := len(hunks) - 1; i >= 0; i-- {
				h := hunks[i]
				if h.typ == "add" {
					continue
				}
				safe, err := confinedPath(h.path)
				if err != nil {
					return strings.Join(results, "\n"), fmt.Errorf("%w (rollback path resolution failed: %v)", cause, err)
				}
				if _, ok := seen[safe]; ok {
					continue
				}
				seen[safe] = struct{}{}
				if restoreErr := snapshot.Restore(safe); restoreErr != nil {
					return strings.Join(results, "\n"), fmt.Errorf("%w (rollback failed: %v)", cause, restoreErr)
				}
			}
			if discardErr := snapshot.DiscardRecent(backedUp); discardErr != nil {
				return strings.Join(results, "\n"), fmt.Errorf("%w (rollback cleanup failed: %v)", cause, discardErr)
			}
		}
		return strings.Join(results, "\n"), cause
	}

	for _, h := range hunks {
		switch h.typ {
		case "add":
			safe, err := confinedPath(h.path)
			if err != nil {
				return rollback(err)
			}
			if err := os.MkdirAll(filepath.Dir(safe), 0755); err != nil {
				return rollback(err)
			}
			content := h.contents
			if !strings.HasSuffix(content, "\n") {
				content += "\n"
			}
			if err := os.WriteFile(safe, []byte(content), 0644); err != nil {
				return rollback(err)
			}
			cleanupPaths = append(cleanupPaths, safe)
			results = append(results, "A "+h.path)

		case "delete":
			safe, err := confinedPath(h.path)
			if err != nil {
				return rollback(err)
			}
			if err := os.Remove(safe); err != nil {
				return rollback(err)
			}
			results = append(results, "D "+h.path)

		case "update":
			safe, err := confinedPath(h.path)
			if err != nil {
				return rollback(err)
			}
			raw, err := os.ReadFile(safe)
			if err != nil {
				return rollback(fmt.Errorf("failed to read %s: %w", h.path, err))
			}
			originalLines := strings.Split(string(raw), "\n")
			newLines, err := deriveNewContents(originalLines, safe, h.chunks)
			if err != nil {
				return rollback(err)
			}
			newContent := strings.Join(newLines, "\n")
			dest := safe
			if h.movePath != "" {
				dest, err = confinedPath(h.movePath)
				if err != nil {
					return rollback(err)
				}
				if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
					return rollback(err)
				}
				if err := os.Remove(safe); err != nil {
					return rollback(err)
				}
			}
			if err := os.WriteFile(dest, []byte(newContent), 0644); err != nil {
				return rollback(err)
			}
			if h.movePath != "" {
				cleanupPaths = append(cleanupPaths, dest)
			}
			if h.movePath != "" {
				results = append(results, "M "+h.path+" -> "+h.movePath)
			} else {
				results = append(results, "M "+h.path)
			}
		}
	}

	return "Success. Updated the following files:\n" + strings.Join(results, "\n"), nil
}

type TodoWriteTool struct{}

func (t TodoWriteTool) Name() string { return "todowrite" }
func (t TodoWriteTool) Description() string {
	return "Manage todo lists during coding sessions."
}
func (t TodoWriteTool) Parallel() bool { return false }
func (t TodoWriteTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "todowrite",
		"description": "Manage todo lists during coding sessions. Use markers: [✓], [•], [ ]",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"todoText": map[string]interface{}{
					"type":        "string",
					"description": "The todo list content. Each line is a bullet starting with a status marker: [✓], [•], [ ]",
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
func (t TodoReadTool) Parallel() bool      { return true }
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
