package agent

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

type Mode string

const (
	ModeBuild  Mode = "build"
	ModePlan   Mode = "plan"
	ModeReview Mode = "review"
)

func (m Mode) Valid() bool {
	return m == ModeBuild || m == ModePlan || m == ModeReview
}

func (m Mode) String() string { return string(m) }

func NextMode(m Mode) Mode {
	switch m {
	case ModeBuild:
		return ModePlan
	case ModePlan:
		return ModeReview
	default:
		return ModeBuild
	}
}

// SystemPrompt returns a prompt prefix describing mode constraints.
func (m Mode) SystemPrompt() string {
	switch m {
	case ModePlan:
		return "You are in PLAN mode. Investigate the codebase and produce a written plan. " +
			"You MAY read any file, search, and write plan documents to paths matching PLAN.md, " +
			"*.plan.md, plans/**, or docs/plans/**. You MUST NOT edit code, run mutating shell " +
			"commands, or use the patch tool. Bash is restricted to read-only commands."
	case ModeReview:
		return "You are in REVIEW mode. Critique the code and produce a written review. " +
			"You MAY read any file, search, and write review documents to paths matching REVIEW.md, " +
			"*.review.md, or reviews/**. You MUST NOT edit code or run mutating shell commands. " +
			"Bash is restricted to read-only commands."
	default:
		return ""
	}
}

var readOnlyBashAllowlist = map[string]struct{}{
	"ls": {}, "cat": {}, "head": {}, "tail": {}, "wc": {},
	"grep": {}, "rg": {}, "find": {}, "fd": {},
	"echo": {}, "pwd": {}, "which": {}, "type": {}, "file": {},
	"git": {}, "diff": {}, "stat": {}, "tree": {}, "awk": {}, "sed": {},
}

// gitReadOnlySubcommands: only these git subcommands are allowed.
var gitReadOnly = map[string]struct{}{
	"status": {}, "log": {}, "diff": {}, "show": {}, "blame": {},
	"branch": {}, "remote": {}, "config": {}, "rev-parse": {}, "ls-files": {},
}

func isReadOnlyBashCommand(cmd string) bool {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return false
	}
	// Reject obvious mutation patterns and shell metachars that could chain mutations.
	if strings.ContainsAny(cmd, ">|&;`$(") {
		return false
	}
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return false
	}
	bin := filepath.Base(fields[0])
	if _, ok := readOnlyBashAllowlist[bin]; !ok {
		return false
	}
	if bin == "git" && len(fields) >= 2 {
		if _, ok := gitReadOnly[fields[1]]; !ok {
			return false
		}
	}
	if bin == "sed" || bin == "awk" {
		// sed -i / awk -i inplace mutate files.
		for _, f := range fields[1:] {
			if f == "-i" || strings.HasPrefix(f, "-i") {
				return false
			}
		}
	}
	return true
}

func isAllowedPlanWritePath(path string) bool {
	clean := filepath.ToSlash(filepath.Clean(path))
	base := filepath.Base(clean)
	if base == "PLAN.md" || strings.HasSuffix(base, ".plan.md") {
		return true
	}
	if strings.HasPrefix(clean, "plans/") || strings.Contains(clean, "/plans/") {
		return true
	}
	if strings.HasPrefix(clean, "docs/plans/") || strings.Contains(clean, "/docs/plans/") {
		return true
	}
	return false
}

func isAllowedReviewWritePath(path string) bool {
	clean := filepath.ToSlash(filepath.Clean(path))
	base := filepath.Base(clean)
	if base == "REVIEW.md" || strings.HasSuffix(base, ".review.md") {
		return true
	}
	if strings.HasPrefix(clean, "reviews/") || strings.Contains(clean, "/reviews/") {
		return true
	}
	return false
}

// gateToolCall returns ("", true, nil) if the tool call is permitted,
// or (denyMessage, false, nil) if denied. It does not execute the tool.
func gateToolCall(mode Mode, name string, args json.RawMessage) (string, bool) {
	if mode == ModeBuild || mode == "" {
		return "", true
	}

	switch name {
	case "read", "glob", "grep", "list", "lsp", "webfetch", "websearch", "todoread", "todowrite", "question", "skill":
		return "", true
	case "edit", "multiedit", "patch", "delete":
		return fmt.Sprintf("denied: tool %q is not permitted in %s mode (no code edits)", name, mode), false
	case "write":
		var p struct {
			Path     string `json:"path"`
			FilePath string `json:"file_path"`
		}
		_ = json.Unmarshal(args, &p)
		target := p.Path
		if target == "" {
			target = p.FilePath
		}
		if target == "" {
			return fmt.Sprintf("denied: write in %s mode requires a path", mode), false
		}
		if mode == ModePlan && isAllowedPlanWritePath(target) {
			return "", true
		}
		if mode == ModeReview && isAllowedReviewWritePath(target) {
			return "", true
		}
		return fmt.Sprintf("denied: %s mode only permits writes to plan/review docs (got %q)", mode, target), false
	case "bash":
		var p struct {
			Command string `json:"command"`
			Cmd     string `json:"cmd"`
		}
		_ = json.Unmarshal(args, &p)
		cmd := p.Command
		if cmd == "" {
			cmd = p.Cmd
		}
		if isReadOnlyBashCommand(cmd) {
			return "", true
		}
		return fmt.Sprintf("denied: bash command not in read-only allowlist for %s mode: %q", mode, cmd), false
	default:
		// Unknown / MCP / custom tools: deny in plan/review for safety.
		return fmt.Sprintf("denied: tool %q is not permitted in %s mode", name, mode), false
	}
}
