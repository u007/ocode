package tui

import (
	"fmt"
	"strings"

	"github.com/u007/ocode/internal/memory"
)

func buildMemUpdatePrompt(workDir string, args []string) (string, error) {
	scope, focus := parseMemUpdateArgs(args)
	snap, err := memory.Status(workDir)
	if err != nil {
		return "", err
	}

	targetPath := snap.Project.Path
	targetTitle := "project memory"
	switch scope {
	case "user":
		targetPath = snap.User.Path
		targetTitle = "user preferences"
	case "global":
		targetPath = snap.Global.Path
		targetTitle = "global history"
	case "project":
		// default already set
	default:
		return "", fmt.Errorf("unknown memory scope %q (want user, project, or global)", scope)
	}

	var b strings.Builder
	b.WriteString("You are the /mem update command for ocode.\n\n")
	b.WriteString("Goal: update the selected memory scope with only durable, reusable information.\n")
	b.WriteString("Scope: ")
	b.WriteString(scope)
	b.WriteString("\n")
	b.WriteString("Target scope: ")
	b.WriteString(targetTitle)
	b.WriteString("\n")
	b.WriteString("Target file: ")
	b.WriteString(targetPath)
	b.WriteString("\n\n")
	if strings.TrimSpace(focus) != "" {
		fmt.Fprintf(&b, "User focus: %s\n\n", strings.TrimSpace(focus))
	}
	b.WriteString("Current memory snapshot:\n")
	for _, scopeInfo := range []struct {
		label string
		s     memory.Scope
	}{
		{label: "Project memory", s: snap.Project},
		{label: "User memory", s: snap.User},
		{label: "Global history", s: snap.Global},
	} {
		b.WriteString("- ")
		b.WriteString(scopeInfo.label)
		b.WriteString("\n")
		b.WriteString("  path: ")
		b.WriteString(scopeInfo.s.Path)
		b.WriteString("\n")
		preview := scopeInfo.s.Preview
		if preview == "" {
			if scopeInfo.s.Present {
				preview = "(empty)"
			} else {
				preview = "(not set)"
			}
		}
		b.WriteString("  preview: ")
		b.WriteString(preview)
		b.WriteString("\n")
	}
	b.WriteString("\nInstructions:\n")
	b.WriteString("- Read the target file first, then rewrite it with the updated durable context.\n")
	b.WriteString("- Update only the selected scope file; do not modify the other two memory scopes.\n")
	b.WriteString("- Keep content concise, factual, and reusable. Prefer bullets over prose.\n")
	b.WriteString("- For user preferences, capture stable personal defaults that should apply everywhere.\n")
	b.WriteString("- For project memory, capture repository/worktree-specific decisions, conventions, and history.\n")
	b.WriteString("- For global history, capture cross-project lessons that are broadly reusable.\n")
	b.WriteString("- Remove stale, duplicated, or transient notes; never preserve chat noise.\n")
	b.WriteString("- End with a brief summary of what you changed.\n")
	return b.String(), nil
}

func parseMemUpdateArgs(args []string) (scope, focus string) {
	scope = "project"
	if len(args) == 0 {
		return scope, ""
	}

	first := strings.ToLower(strings.TrimSpace(args[0]))
	switch first {
	case "user", "u", "personal":
		scope = "user"
		focus = strings.Join(args[1:], " ")
	case "project", "p", "repo", "worktree":
		scope = "project"
		focus = strings.Join(args[1:], " ")
	case "global", "g", "shared":
		scope = "global"
		focus = strings.Join(args[1:], " ")
	default:
		focus = strings.Join(args, " ")
	}
	return strings.TrimSpace(scope), strings.TrimSpace(focus)
}
