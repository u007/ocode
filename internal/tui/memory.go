package tui

import (
	"strings"

	"github.com/u007/ocode/internal/commandctx"
)

// buildMemUpdatePrompt delegates to the shared commandctx.MemUpdate builder so
// the server's /api/command-context/mem-update endpoint produces byte-identical
// prompts.
func buildMemUpdatePrompt(workDir string, args []string) (string, error) {
	return commandctx.MemUpdate(workDir, args)
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
