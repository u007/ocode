package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/u007/ocode/internal/lsp"
)

// getStandupContext gathers context for the /standup command: recent commits
// (with dates and per-file stat) plus the pending-changes context reused from
// /changes (git status, staged/unstaged diffs, LSP diagnostics, spec files).
//
// Recent commits are gathered first and independently so a standup still works
// on a clean tree where the only signal is the commit history. The pending
// changes are optional: on a clean tree getChangesContext returns an error,
// which we surface as a note rather than failing the whole command.
func getStandupContext(workDir string, lspMgr *lsp.Manager) (string, error) {
	var b strings.Builder

	// --- Recent commits (dates + stat + full messages, no patch) ---
	// --date=short and --stat give the model enough to judge "yesterday vs
	// last 5" itself; patches are omitted because the pending diff below is
	// already full and commit patches would blow the context budget.
	commits, err := gitRun(workDir, "log", "-n", "5", "--stat", "--date=short")
	if err == nil && commits != "" {
		b.WriteString("## Recent Commits (last 5)\n")
		b.WriteString(commits)
		b.WriteString("\n\n")
	}

	// --- Pending changes (reused from /changes) ---
	changesCtx, changesErr := getChangesContext(workDir, lspMgr)
	if changesErr == nil && changesCtx != "" {
		b.WriteString("## Pending Changes\n\n")
		b.WriteString(changesCtx)
	} else {
		b.WriteString("## Pending Changes\n\nClean tree — no staged or unstaged changes.\n\n")
	}

	// Only fail if there is genuinely nothing to summarise.
	if commits == "" && changesErr != nil {
		return "", fmt.Errorf("no recent commits and no pending changes found")
	}

	return b.String(), nil
}

// buildStandupPrompt creates the LLM prompt for the /standup command. Caveman
// style (short, punchy, drop articles) but each item carries one line of
// context so the summary is actionable, not bare bullets.
func buildStandupPrompt(context string) string {
	var b strings.Builder
	b.WriteString("You are a standup assistant. Review the recent commits and pending changes below, then report in caveman style — short, punchy, drop articles and filler. But give one line of context per item; no bare bullets.\n\n")
	b.WriteString("Decide what window makes sense: if there are several commits from yesterday, focus there; otherwise summarise the last 5.\n\n")
	b.WriteString("Cover these sections, in this order:\n\n")
	b.WriteString("## WHAT DONE\n")
	b.WriteString("What was built/fixed and what was decided in the recent work. Group by purpose. One line each.\n\n")
	b.WriteString("## TODO — EASY FIRST\n")
	b.WriteString("Low-hanging fruit, sorted easiest first: quick wins, small cleanups, trivial follow-ups implied by the changes.\n\n")
	b.WriteString("## TODO — HIGH PRIORITY\n")
	b.WriteString("Bigger or more urgent tasks the work points to: incomplete features, risky gaps, things that block shipping.\n\n")
	b.WriteString("## MISSED STUBS\n")
	b.WriteString("Flag any TODO/FIXME/XXX markers, unimplemented paths, panic(\"not implemented\"), empty catch blocks, or obvious placeholders introduced in the recent work. If none, say \"None found.\"\n\n")
	b.WriteString("---\n\n")
	b.WriteString(context)
	return b.String()
}

// runStandupCmd is the command wrapper for /standup.
func runStandupCmd(m *model, args []string) tea.Cmd {
	return m.handleStandupCmd(args)
}

// handleStandupCmd gathers commit + change context and sends it to the agent.
func (m *model) handleStandupCmd(args []string) tea.Cmd {
	ctx, err := getStandupContext(m.workDir, m.lspMgr)
	if err != nil {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("/standup: %v", err)})
		return nil
	}
	prompt := buildStandupPrompt(ctx)
	return m.sendCustomCommandPrompt(prompt)
}
