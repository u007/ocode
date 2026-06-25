package runcli

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/u007/ocode/internal/agent"
)

// fileAction tracks the net effect on a single file across the run.
type fileAction int

const (
	actNone     fileAction = iota // no known operation
	actCreated                    // write tool (new file)
	actModified                   // edit / multiedit / multi_file_edit / patch-update
	actDeleted                    // delete tool / patch-delete
)

// outputSummary prints a structured work summary to stdout after a headless run.
//
// It scans the response messages for tool calls, categorises file operations
// (created, modified, deleted), counts tool usage, and includes the assistant's
// final response text — all in a human-readable format.
func outputSummary(messages []agent.Message, sessionID, modelName string, startTime time.Time) error {
	fileActions := make(map[string]fileAction) // final action per path
	toolCounts := make(map[string]int)
	var responseParts []string

	for _, msg := range messages {
		if msg.Role == "assistant" && strings.TrimSpace(msg.Content) != "" {
			responseParts = append(responseParts, msg.Content)
		}
		for _, tc := range msg.ToolCalls {
			toolCounts[tc.Function.Name]++
			processToolCallForSummary(tc, fileActions)
		}
	}

	// Categorise files.
	created := make([]string, 0)
	modified := make([]string, 0)
	deleted := make([]string, 0)
	for path, act := range fileActions {
		switch act {
		case actCreated:
			created = append(created, path)
		case actModified:
			modified = append(modified, path)
		case actDeleted:
			deleted = append(deleted, path)
		}
	}
	sort.Strings(created)
	sort.Strings(modified)
	sort.Strings(deleted)

	toolNames := make([]string, 0, len(toolCounts))
	for name := range toolCounts {
		toolNames = append(toolNames, name)
	}
	sort.Strings(toolNames)

	duration := time.Since(startTime).Round(time.Millisecond)

	// ── render ──────────────────────────────────────────────────────────

	const sep = "══════════════════════════════════════════════════════════"
	fmt.Println(sep)
	fmt.Println("  OCODE RUN SUMMARY")
	fmt.Println(sep)
	fmt.Println()

	printGroup := func(emoji, label string, items []string) {
		if len(items) == 0 {
			return
		}
		fmt.Printf("  %s %s (%d)\n", emoji, label, len(items))
		for _, p := range items {
			fmt.Printf("    • %s\n", p)
		}
		fmt.Println()
	}
	printGroup("▸", "Files Created", created)
	printGroup("✏ ", "Files Modified", modified)
	printGroup("∅ ", "Files Deleted", deleted)

	if len(created)+len(modified)+len(deleted) == 0 {
		fmt.Println("  ▸ No file changes detected.")
		fmt.Println()
	}

	// Tool usage.
	fmt.Println("  ⚙ Tool Usage")
	for _, name := range toolNames {
		n := toolCounts[name]
		padded := name
		if len(padded) < 20 {
			padded += strings.Repeat(" ", 20-len(padded))
		}
		fmt.Printf("    %s · %2d\n", padded, n)
	}
	fmt.Println()

	// Metadata.
	fmt.Printf("  ⏱  Duration: %v\n", duration)
	if sessionID != "" {
		fmt.Printf("  § Session:  %s\n", sessionID)
	}
	if modelName != "" {
		fmt.Printf("  @ Model:    %s\n", modelName)
	}
	fmt.Println()

	// Assistant response.
	if len(responseParts) > 0 {
		response := strings.Join(responseParts, "\n\n")
		fmt.Println("  » Assistant Response")
		fmt.Println("  ─────────────────────")
		fmt.Println()
		for _, line := range strings.Split(strings.TrimSpace(response), "\n") {
			fmt.Printf("  %s\n", line)
		}
		fmt.Println()
	}

	fmt.Println(sep)
	return nil
}

// processToolCallForSummary updates the fileActions map based on a single tool call.
func processToolCallForSummary(tc agent.ToolCall, fileActions map[string]fileAction) {
	switch tc.Function.Name {
	case "write":
		var args struct {
			Path string `json:"path"`
			Mode string `json:"mode"`
		}
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil || args.Path == "" {
			return
		}
		if args.Mode == "append" {
			if _, exists := fileActions[args.Path]; !exists {
				fileActions[args.Path] = actModified
			}
		} else {
			// Overwrite — classify as "created".  A later delete overrides.
			fileActions[args.Path] = actCreated
		}

	case "delete":
		var args struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil || args.Path == "" {
			return
		}
		fileActions[args.Path] = actDeleted

	case "edit":
		var args struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil || args.Path == "" {
			return
		}
		if _, exists := fileActions[args.Path]; !exists {
			fileActions[args.Path] = actModified
		}

	case "multiedit":
		var args struct {
			FilePath string `json:"file_path"`
		}
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil || args.FilePath == "" {
			return
		}
		if _, exists := fileActions[args.FilePath]; !exists {
			fileActions[args.FilePath] = actModified
		}

	case "multi_file_edit":
		var args struct {
			Edits []struct {
				Path string `json:"path"`
			} `json:"edits"`
		}
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
			return
		}
		for _, e := range args.Edits {
			if e.Path == "" {
				continue
			}
			if _, exists := fileActions[e.Path]; !exists {
				fileActions[e.Path] = actModified
			}
		}

	case "apply_patch":
		var args struct {
			PatchText string `json:"patchText"`
		}
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil || args.PatchText == "" {
			return
		}
		for _, po := range parsePatchOps(args.PatchText) {
			switch po.action {
			case "add":
				fileActions[po.path] = actCreated
			case "delete":
				fileActions[po.path] = actDeleted
			case "update":
				if _, exists := fileActions[po.path]; !exists {
					fileActions[po.path] = actModified
				}
			}
		}
	}
}

// patchOp represents a single file operation extracted from an apply_patch call.
type patchOp struct {
	action string // "add", "delete", "update"
	path   string
}

// parsePatchOps does a lightweight parse of the *** Begin Patch / *** End Patch
// envelope to extract file paths and their operation types.
func parsePatchOps(patchText string) []patchOp {
	text := strings.TrimSpace(patchText)
	lines := strings.Split(text, "\n")

	beginIdx, endIdx := -1, -1
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if t == "*** Begin Patch" {
			beginIdx = i
		} else if t == "*** End Patch" {
			endIdx = i
			break
		}
	}
	if beginIdx == -1 || endIdx == -1 || beginIdx >= endIdx {
		return nil
	}

	var ops []patchOp
	prefixes := []struct {
		marker string
		action string
	}{
		{"*** Add File:", "add"},
		{"*** Delete File:", "delete"},
		{"*** Update File:", "update"},
	}

	for i := beginIdx + 1; i < endIdx; i++ {
		line := lines[i]
		for _, p := range prefixes {
			if strings.HasPrefix(line, p.marker) {
				path := strings.TrimSpace(strings.TrimPrefix(line, p.marker))
				if path != "" {
					ops = append(ops, patchOp{action: p.action, path: path})
				}
				break
			}
		}
	}
	return ops
}
