package tool

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// planDir returns the directory where plan files are stored.
func planDir() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	p := filepath.Join(wd, ".opencode", "plans")
	_ = os.MkdirAll(p, 0o755)
	return p
}

// planFileName returns the filename for today's plan.
func planFileName() string {
	return time.Now().Format("2006-01-02") + ".md"
}

// ---------------------------------------------------------------------------
// PlanEnterTool
// ---------------------------------------------------------------------------

type PlanEnterTool struct{}

func (t PlanEnterTool) Name() string { return "plan_enter" }
func (t PlanEnterTool) Description() string {
	return "Begin a planning phase by creating a structured plan file"
}
func (t PlanEnterTool) Parallel() bool { return false }

func (t PlanEnterTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "plan_enter",
		"description": "Begin a planning phase. Creates a structured plan file to document the approach before implementation. Use this for complex tasks that benefit from planning first.",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"title": map[string]interface{}{
					"type":        "string",
					"description": "Short title for this plan (e.g. 'Add OAuth login')",
				},
			},
			"required": []string{"title"},
		},
	}
}

func (t PlanEnterTool) Execute(args json.RawMessage) (string, error) {
	var params struct {
		Title string `json:"title"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("invalid params: %w", err)
	}
	if params.Title == "" {
		return "", fmt.Errorf("title is required")
	}

	dir := planDir()
	name := planFileName()
	path := filepath.Join(dir, name)

	if _, err := os.Stat(path); err == nil {
		return fmt.Sprintf("A plan already exists for today: %s\nUse plan_exit to review it, or delete it manually to start fresh.", path), nil
	}

	timestamp := time.Now().Format("2006-01-02 15:04:05 MST")

	template := fmt.Sprintf(`# Plan: %s

**Created:** %s
**Status:** Draft

## Context

<!-- Why is this work needed? -->

## Goals

<!-- What are the success criteria? -->

## Approach

<!-- What is the high-level approach? -->

## Files to Change

<!-- List of files that need modification -->

## Implementation Steps

1. 
2. 
3. 

## Risks & Considerations

<!-- Potential pitfalls, testing strategy, edge cases -->
`, params.Title, timestamp)

	if err := os.WriteFile(path, []byte(template), 0o644); err != nil {
		return "", fmt.Errorf("failed to create plan: %w", err)
	}

	return fmt.Sprintf("Plan started: %s\nPath: %s\n\nBegin filling out the plan. When done, use `plan_exit` to finalize and switch to implementation.", params.Title, path), nil
}

// ---------------------------------------------------------------------------
// PlanExitTool
// ---------------------------------------------------------------------------

type PlanExitTool struct{}

func (t PlanExitTool) Name() string { return "plan_exit" }
func (t PlanExitTool) Description() string {
	return "Finalize the planning phase and prepare for implementation"
}
func (t PlanExitTool) Parallel() bool { return false }

func (t PlanExitTool) Definition() map[string]interface{} {
	return map[string]interface{}{
		"name":        "plan_exit",
		"description": "Finalize the planning phase. Reads the current plan and asks if you're ready to switch to implementation.",
		"parameters": map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	}
}

func (t PlanExitTool) Execute(args json.RawMessage) (string, error) {
	dir := planDir()
	name := planFileName()
	path := filepath.Join(dir, name)

	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("plan not found: %s (has the plan been created yet?)", path)
	}

	planStr := string(content)

	// Verify the user actually filled in implementation steps: after the
	// "## Implementation Steps" header there must be at least one non-blank,
	// non-list-marker line of real content.
	if !hasImplementationContent(planStr) {
		return fmt.Sprintf("Plan at %s appears incomplete. Add at least one implementation step before exiting.\n\n%s", path, planStr), nil
	}

	return fmt.Sprintf(
		"Plan ready for implementation:\n\n---\n%s\n---\n\n"+
			"To implement, ask the user to switch to BUILD mode (`/agent build` in the TUI, or run with `--agent build`). "+
			"From inside plan mode you may also delegate a focused implementation step via the `task` tool, but a mode switch is preferred for multi-step work.",
		planStr,
	), nil
}

// hasImplementationContent reports whether the plan contains at least one
// non-blank, non-list-marker line under the "## Implementation Steps" header.
func hasImplementationContent(planStr string) bool {
	idx := strings.Index(planStr, "## Implementation Steps")
	if idx < 0 {
		return false
	}
	rest := planStr[idx+len("## Implementation Steps"):]
	for _, line := range strings.Split(rest, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		// Stop scanning at the next section header.
		if strings.HasPrefix(trimmed, "## ") {
			break
		}
		// Strip a leading list/ordered marker and any HTML comment markers.
		stripped := strings.TrimSpace(strings.TrimLeft(trimmed, "-*0123456789.) "))
		stripped = strings.TrimSpace(strings.NewReplacer("<!--", "", "-->", "").Replace(stripped))
		if stripped != "" {
			return true
		}
	}
	return false
}
