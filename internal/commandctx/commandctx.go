// Package commandctx builds the LLM prompts behind the repo-analysis slash
// commands (/standup, /changes, /review) that need server-side context
// gathering — recent commits, pending diffs, LSP diagnostics, spec files.
//
// The logic lives here (not in internal/tui) so both the TUI and the
// web/desktop server can produce byte-identical prompts: the TUI calls it
// directly, and the server exposes it via GET /api/command-context/{name}.
// internal/tui must never import internal/server (import cycle through the
// remote-control bridge), so this package is the single source of truth.
package commandctx

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/u007/ocode/internal/lsp"
)

// gitRun executes a git command in workDir and returns trimmed stdout.
func gitRun(workDir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = workDir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// ─── /changes context ────────────────────────────────────────────────────

// specFileNames are markdown file names that typically contain specs, designs,
// or planning docs. Only the basename is compared (case-insensitive).
var specFileNames = map[string]bool{
	"spec.md":             true,
	"design.md":           true,
	"architecture.md":     true,
	"plan.md":             true,
	"todo.md":             true,
	"roadmap.md":          true,
	"enhancement.md":      true,
	"enhancement_plan.md": true,
	"implementation.md":   true,
	"rfc.md":              true,
	"proposal.md":         true,
	"adr.md":              true,
	"changelog.md":        true,
	"notes.md":            true,
	"wip.md":              true,
}

// specFileInfo holds paths for a discovered spec/design markdown file.
type specFileInfo struct {
	relPath string // relative to workDir
	absPath string // absolute
}

// findSpecFiles walks the repo (max depth 2) looking for markdown files whose
// base name matches specFileNames. It also picks up any .md file in the root
// that looks like a spec/design document (name contains "spec", "design",
// "plan", "todo", "rfc", "proposal", "adr", "wip").
func findSpecFiles(workDir string) []specFileInfo {
	var result []specFileInfo

	// First pass: exact matches from the known set.
	_ = filepath.WalkDir(workDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			if d != nil && d.IsDir() && path != workDir {
				// Only go 2 levels deep.
				rel, _ := filepath.Rel(workDir, path)
				if strings.Count(rel, string(os.PathSeparator)) >= 2 {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if d.Name() == ".git" || strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		if !strings.EqualFold(filepath.Ext(path), ".md") {
			return nil
		}
		base := strings.ToLower(d.Name())
		if specFileNames[base] {
			rel, _ := filepath.Rel(workDir, path)
			result = append(result, specFileInfo{relPath: rel, absPath: path})
		}
		return nil
	})

	// Second pass: root-level .md files with spec-like keywords in name.
	entries, err := os.ReadDir(workDir)
	if err == nil {
		seen := make(map[string]bool, len(result))
		for _, r := range result {
			seen[strings.ToLower(r.relPath)] = true
		}
		for _, e := range entries {
			if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".md") {
				continue
			}
			base := strings.ToLower(e.Name())
			if seen[base] {
				continue
			}
			if specFileNames[base] {
				continue // already caught above
			}
			// Heuristic: name contains a spec-like keyword.
			if strings.Contains(base, "spec") || strings.Contains(base, "design") ||
				strings.Contains(base, "plan") || strings.Contains(base, "todo") ||
				strings.Contains(base, "rfc") || strings.Contains(base, "proposal") ||
				strings.Contains(base, "adr") || strings.Contains(base, "wip") ||
				strings.Contains(base, "roadmap") || strings.Contains(base, "enhancement") {
				result = append(result, specFileInfo{relPath: e.Name(), absPath: filepath.Join(workDir, e.Name())})
			}
		}
	}

	// Cap at 5 files to avoid context bloat.
	if len(result) > 5 {
		result = result[:5]
	}
	return result
}

// readFileLines reads the first n lines of a file and returns them as a string.
func readFileLines(path string, n int) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.SplitN(string(data), "\n", n+1)
	if len(lines) > n {
		lines = lines[:n]
		lines = append(lines, "... (truncated)")
	}
	return strings.Join(lines, "\n")
}

// formatDiagPath renders an LSP diagnostic path relative to workDir when
// possible, else absolute. Plain path — no ANSI truncation (that is a TUI
// rendering concern; the prompt consumer wants the full relative path).
func formatDiagPath(path, workDir string) string {
	if rel, err := filepath.Rel(workDir, path); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return path
}

// lspSection renders the LSP diagnostics block for the changes context, or ""
// when the manager is nil or holds no diagnostics. Mirrors the TUI's old
// getChangesContext output with plain (untruncated) paths.
func lspSection(lspMgr *lsp.Manager, workDir string) string {
	if lspMgr == nil {
		return ""
	}
	store := lspMgr.Diagnostics()
	if store == nil || store.IsEmpty() {
		return ""
	}
	snap := store.Snapshot(50)
	if snap.Total == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## LSP Diagnostics\n")
	fmt.Fprintf(&b, "%d total across %d file(s)\n", snap.Total, snap.Files)
	for i, d := range snap.FirstN {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "  %s:%d:%d  [%s]  %s",
			formatDiagPath(d.Path, workDir),
			d.Range.Start.Line+1, d.Range.Start.Character+1,
			d.Severity.String(), d.Message)
	}
	if snap.Total > len(snap.FirstN) {
		fmt.Fprintf(&b, "\n  (showing first %d of %d)", len(snap.FirstN), snap.Total)
	}
	b.WriteString("\n\n")
	return b.String()
}

// ChangesContext gathers all context for the /changes command: git status,
// staged diff, unstaged diff, LSP diagnostics, and in-progress spec/design
// markdown files. lspMgr may be nil (the server has no live LSP manager).
func ChangesContext(workDir string, lspMgr *lsp.Manager) (string, error) {
	var b strings.Builder

	// --- Git status ---
	statusOut, err := gitRun(workDir, "status", "--short")
	if err == nil && statusOut != "" {
		b.WriteString("## Git Status\n")
		b.WriteString(statusOut)
		b.WriteString("\n\n")
	}

	// --- Staged diff ---
	stagedDiff, err := gitRun(workDir, "diff", "--cached")
	if err == nil && stagedDiff != "" {
		b.WriteString("## Staged Changes\n")
		b.WriteString(stagedDiff)
		b.WriteString("\n\n")
	}

	// --- Unstaged diff ---
	unstagedDiff, err := gitRun(workDir, "diff")
	if err == nil && unstagedDiff != "" {
		b.WriteString("## Unstaged Changes\n")
		b.WriteString(unstagedDiff)
		b.WriteString("\n\n")
	}

	// --- LSP diagnostics ---
	if sect := lspSection(lspMgr, workDir); sect != "" {
		b.WriteString(sect)
	}

	// --- Spec / design markdown files ---
	specFiles := findSpecFiles(workDir)
	if len(specFiles) > 0 {
		b.WriteString("## Spec / Design Files (in-progress)\n")
		for _, sf := range specFiles {
			fmt.Fprintf(&b, "### %s\n", sf.relPath)
			// Read first 80 lines to give the LLM a taste without bloating.
			content := readFileLines(sf.absPath, 80)
			if content != "" {
				b.WriteString(content)
				b.WriteString("\n\n")
			}
		}
	}

	if b.Len() == 0 {
		return "", fmt.Errorf("no changes, diagnostics, or spec files found")
	}

	return b.String(), nil
}

// BuildChangesPrompt creates the LLM prompt for the /changes command.
func BuildChangesPrompt(context string) string {
	var b strings.Builder
	b.WriteString("You are a change analysis assistant. Analyze the following repository changes and provide a clear, structured overview.\n\n")
	b.WriteString("## Instructions\n\n")
	b.WriteString("Provide your analysis in these sections:\n\n")
	b.WriteString("### 1. Overview\n")
	b.WriteString("A brief 1-2 sentence summary of what's happening in this changeset.\n\n")
	b.WriteString("### 2. Changed Files\n")
	b.WriteString("Group changes by purpose (e.g. \"auth module\", \"UI fixes\"). List each file with a one-line description of what changed.\n\n")
	b.WriteString("### 3. LSP Errors\n")
	b.WriteString("List any LSP diagnostics (errors, warnings) found in the changed files. If none, say \"No LSP issues.\"\n\n")
	b.WriteString("### 4. Spec / Design Status\n")
	b.WriteString("For any spec or design markdown files found, note whether they appear complete or have TODO/WIP markers. Flag anything that looks unfinished.\n\n")
	b.WriteString("### 5. Risks & Suggestions\n")
	b.WriteString("Call out potential issues: missing error handling, incomplete implementations, broken patterns, or anything that looks like it needs follow-up.\n\n")
	b.WriteString("Be concise. Use bullet points. No filler.\n\n")
	b.WriteString("---\n\n")
	b.WriteString("## Repository Changes\n\n")
	b.WriteString(context)
	return b.String()
}

// ─── /standup context ────────────────────────────────────────────────────

// StandupContext gathers context for the /standup command: recent commits
// (with dates and per-file stat) plus the pending-changes context reused from
// /changes (git status, staged/unstaged diffs, LSP diagnostics, spec files).
//
// Recent commits are gathered first and independently so a standup still works
// on a clean tree where the only signal is the commit history. The pending
// changes are optional: on a clean tree ChangesContext returns an error, which
// we surface as a note rather than failing the whole command.
func StandupContext(workDir string, lspMgr *lsp.Manager) (string, error) {
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
	changesCtx, changesErr := ChangesContext(workDir, lspMgr)
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

// BuildStandupPrompt creates the LLM prompt for the /standup command. Caveman
// style (short, punchy, drop articles) but each item carries one line of
// context so the summary is actionable, not bare bullets.
func BuildStandupPrompt(context string) string {
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

// ─── /review context ──────────────────────────────────────────────────────

// ReviewTarget represents what is being reviewed.
type ReviewTarget int

const (
	ReviewWorkingDir ReviewTarget = iota // git diff HEAD (uncommitted changes)
	ReviewFile                           // specific file(s)
	ReviewCommit                         // specific commit
	ReviewBranch                         // branch comparison
	ReviewPR                             // GitHub PR
)

// DetectReviewTarget analyzes the arguments to determine what to review.
func DetectReviewTarget(args []string) (ReviewTarget, string, string) {
	if len(args) == 0 {
		return ReviewWorkingDir, "", "uncommitted changes"
	}

	arg := args[0]

	// Check if it's a file path
	if strings.HasSuffix(arg, ".go") || strings.HasSuffix(arg, ".js") || strings.HasSuffix(arg, ".ts") ||
		strings.HasSuffix(arg, ".py") || strings.HasSuffix(arg, ".rs") || strings.HasSuffix(arg, ".java") ||
		strings.HasSuffix(arg, ".c") || strings.HasSuffix(arg, ".cpp") || strings.HasSuffix(arg, ".h") ||
		strings.Contains(arg, "/") || strings.Contains(arg, ".") {
		return ReviewFile, arg, fmt.Sprintf("file %s", arg)
	}

	// Check if it looks like a commit hash (7-40 hex chars)
	if isCommitHash(arg) {
		return ReviewCommit, arg, fmt.Sprintf("commit %s", arg)
	}

	// Check if it's a PR number
	if prNum := parsePRNumber(arg); prNum > 0 {
		return ReviewPR, arg, fmt.Sprintf("PR #%s", arg)
	}

	// Default: treat as branch name
	return ReviewBranch, arg, fmt.Sprintf("branch %s vs current", arg)
}

// isCommitHash checks if a string looks like a git commit hash.
func isCommitHash(s string) bool {
	if len(s) < 7 || len(s) > 40 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// parsePRNumber extracts a PR number from a string like "#123" or "123".
func parsePRNumber(s string) int {
	s = strings.TrimPrefix(s, "#")
	var num int
	for _, c := range s {
		if c >= '0' && c <= '9' {
			num = num*10 + int(c-'0')
		} else {
			return 0
		}
	}
	return num
}

// ReviewContext gathers the context for the review based on the target.
func ReviewContext(target ReviewTarget, arg string, workDir string) (string, error) {
	switch target {
	case ReviewWorkingDir:
		return reviewWorkingDirContext(workDir)
	case ReviewFile:
		return reviewFileContext(arg, workDir)
	case ReviewCommit:
		return reviewCommitContext(arg, workDir)
	case ReviewBranch:
		return reviewBranchContext(arg, workDir)
	case ReviewPR:
		return reviewPRContext(arg, workDir)
	default:
		return "", fmt.Errorf("unknown review target")
	}
}

// reviewWorkingDirContext gets the diff for uncommitted changes.
func reviewWorkingDirContext(workDir string) (string, error) {
	var b strings.Builder

	// Get status
	statusOut, err := gitRun(workDir, "status", "--short")
	if err == nil && statusOut != "" {
		b.WriteString("## Git Status\n")
		b.WriteString(statusOut)
		b.WriteString("\n\n")
	}

	// Get staged diff
	stagedDiff, err := gitRun(workDir, "diff", "--cached")
	if err == nil && stagedDiff != "" {
		b.WriteString("## Staged Changes\n")
		b.WriteString(stagedDiff)
		b.WriteString("\n\n")
	}

	// Get unstaged diff
	unstagedDiff, err := gitRun(workDir, "diff")
	if err == nil && unstagedDiff != "" {
		b.WriteString("## Unstaged Changes\n")
		b.WriteString(unstagedDiff)
		b.WriteString("\n\n")
	}

	if b.Len() == 0 {
		return "", fmt.Errorf("no changes to review")
	}

	return b.String(), nil
}

// reviewFileContext gets the content and diff for a specific file.
func reviewFileContext(filePath string, workDir string) (string, error) {
	var b strings.Builder

	// Check if file exists and get its content
	fmt.Fprintf(&b, "## File: %s\n\n", filePath)

	// Get file diff if it has changes
	diff, err := gitRun(workDir, "diff", "--", filePath)
	if err == nil && diff != "" {
		b.WriteString("## Changes\n")
		b.WriteString(diff)
		b.WriteString("\n\n")
	}

	// Get staged changes for the file
	stagedDiff, err := gitRun(workDir, "diff", "--cached", "--", filePath)
	if err == nil && stagedDiff != "" {
		b.WriteString("## Staged Changes\n")
		b.WriteString(stagedDiff)
		b.WriteString("\n\n")
	}

	if b.Len() == len(fmt.Sprintf("## File: %s\n\n", filePath)) {
		return "", fmt.Errorf("no changes found for file %s", filePath)
	}

	return b.String(), nil
}

// reviewCommitContext gets the details and diff for a specific commit.
func reviewCommitContext(commitHash string, workDir string) (string, error) {
	var b strings.Builder

	// Get commit info
	logOut, err := gitRun(workDir, "log", "-1", "--format=fuller", commitHash)
	if err == nil {
		b.WriteString("## Commit Info\n")
		b.WriteString(logOut)
		b.WriteString("\n\n")
	}

	// Get commit diff
	diff, err := gitRun(workDir, "show", "--no-color", commitHash)
	if err == nil {
		b.WriteString("## Commit Diff\n")
		b.WriteString(diff)
		b.WriteString("\n\n")
	}

	if b.Len() == 0 {
		return "", fmt.Errorf("could not retrieve commit %s", commitHash)
	}

	return b.String(), nil
}

// reviewBranchContext gets the diff between current branch and another branch.
func reviewBranchContext(branchName string, workDir string) (string, error) {
	var b strings.Builder

	// Get current branch
	currentBranch, err := gitRun(workDir, "branch", "--show-current")
	if err == nil {
		fmt.Fprintf(&b, "## Comparing: %s → %s\n\n", branchName, currentBranch)
	}

	// Get diff between branches
	diff, err := gitRun(workDir, "diff", "--no-color", branchName+"...HEAD")
	if err == nil {
		b.WriteString("## Changes\n")
		b.WriteString(diff)
		b.WriteString("\n\n")
	}

	if b.Len() == 0 {
		return "", fmt.Errorf("could not retrieve diff for branch %s", branchName)
	}

	return b.String(), nil
}

// reviewPRContext gets the details for a GitHub PR.
func reviewPRContext(prArg string, workDir string) (string, error) {
	// Extract PR number
	prNum := parsePRNumber(prArg)
	if prNum == 0 {
		return "", fmt.Errorf("invalid PR number: %s", prArg)
	}

	// Try to get GitHub remote info
	remoteURL, err := gitRun(workDir, "remote", "get-url", "origin")
	if err != nil {
		return "", fmt.Errorf("could not determine GitHub remote: %w", err)
	}

	// Parse owner/repo from remote URL
	owner, repo := parseGitHubRemote(remoteURL)
	if owner == "" || repo == "" {
		return "", fmt.Errorf("could not parse GitHub owner/repo from remote: %s", remoteURL)
	}

	// This would integrate with the GitHub API in a real implementation
	// For now, return a placeholder
	return fmt.Sprintf("## PR #%d\n\nReview requested for %s/%s#%d\n\n(GitHub API integration would go here)", prNum, owner, repo, prNum), nil
}

// parseGitHubRemote extracts owner and repo from a GitHub remote URL.
func parseGitHubRemote(url string) (string, string) {
	// Handle SSH URLs: git@github.com:owner/repo.git
	if strings.HasPrefix(url, "git@github.com:") {
		path := strings.TrimPrefix(url, "git@github.com:")
		path = strings.TrimSuffix(path, ".git")
		parts := strings.Split(path, "/")
		if len(parts) == 2 {
			return parts[0], parts[1]
		}
	}

	// Handle HTTPS URLs: https://github.com/owner/repo.git
	if strings.Contains(url, "github.com") {
		path := strings.TrimPrefix(url, "https://github.com/")
		path = strings.TrimSuffix(path, ".git")
		parts := strings.Split(path, "/")
		if len(parts) == 2 {
			return parts[0], parts[1]
		}
	}

	return "", ""
}

// BuildReviewPrompt creates the prompt for the orchestrator
// (main LLM) to drive a grouped, notes-enabled code review
// and a final reconcile pass. The /review command no longer
// asks a single agent to review a diff end-to-end; it asks
// the orchestrator to:
//
//  1. Compute a SHARED BRIEF once: change set summary, caller
//     map, and any doc-rule digest the agents need. This is
//     context the orchestrator already has, so every agent
//     in the fan-out does not have to recompute it.
//  2. SPAWN a grouped fan-out via the `task` tool, with
//     `shared_notes: true`, partitioned by review dimension
//     (correctness, security, style, performance). For very
//     large diffs, partition by file instead — the brief
//     seeds the per-agent scope. Each agent emits cross-
//     agent-value findings to the bus and keeps own-report-
//     only findings in its own report.
//  3. RUN RECONCILE on the bus when the group finishes. The
//     orchestrator dedups, ranks severity, resolves
//     contradictions, and surfaces unreviewed partitions.
//
// The interactive SEVERITY/FILE/LINE/MESSAGE/SUGGESTION
// report format is preserved so the existing TUI overlay
// can still parse findings.
func BuildReviewPrompt(target ReviewTarget, context string, description string) string {
	var b strings.Builder

	b.WriteString("Run a code review of the following changes using a grouped, notes-enabled fan-out.\n\n")
	fmt.Fprintf(&b, "## Review Target: %s\n\n", description)
	b.WriteString("## Changes to Review\n\n")
	b.WriteString(context)
	b.WriteString("\n\n")

	b.WriteString("## Workflow\n\n")
	b.WriteString("Drive this as a grouped fan-out — do NOT do the review end-to-end yourself.\n\n")
	b.WriteString("### Step 1 — Compute a shared brief ONCE\n\n")
	b.WriteString("Before spawning any agents, build a shared brief:\n")
	b.WriteString("- A one-paragraph change set summary.\n")
	b.WriteString("- A caller map for the modified symbols (who depends on them).\n")
	b.WriteString("- Any doc-rule digest the agents need (e.g. the project's API conventions).\n")
	b.WriteString("This is context the agents will all need; compute it once and seed it.\n\n")

	b.WriteString("### Step 2 — Spawn a grouped fan-out with shared_notes: true\n\n")
	b.WriteString("Use the `task` tool to dispatch all agents in ONE parallel batch. Each call must set:\n")
	b.WriteString("  shared_notes: true\n")
	b.WriteString("2+ subagent calls with shared_notes in the same batch become a group; a single call has nobody to coordinate with and gets no bus.\n\n")
	b.WriteString("Partition by review dimension. For a typical small diff:\n")
	b.WriteString("- one agent on correctness (logic errors, missing nil checks, off-by-one, panic paths),\n")
	b.WriteString("- one agent on security (auth bypass, secret leakage, injection, unsafe input),\n")
	b.WriteString("- one agent on style and API consistency,\n")
	b.WriteString("- one agent on performance and resource use.\n\n")
	b.WriteString("For very large diffs (>2000 lines) partition by file instead. Pass each agent a short brief describing its dimension and the file(s) it owns.\n\n")
	b.WriteString("Each agent must:\n")
	b.WriteString("- Emit cross-agent-value findings to the bus as <oc-note at=\"symbol-or-snippet\">caveman text</oc-note>.\n")
	b.WriteString("- Keep own-report-only findings in its own final report.\n")
	b.WriteString("- Treat received notes as LEADS, not facts — verify against the actual code.\n")
	b.WriteString("- Resolve leads that turn out to be wrong with <oc-resolve ref=\"N\"/>.\n\n")

	b.WriteString("### Step 3 — Reconcile the bus\n\n")
	b.WriteString("When the group finishes, run reconcile on the bus:\n")
	b.WriteString("- Dedup exact-duplicate notes (keep all authors in provenance).\n")
	b.WriteString("- Resolve contradictions (cluster by file/symbol, decide severity).\n")
	b.WriteString("- For a contradiction you cannot settle from notes alone, spawn ONE focused verify agent that re-reads the actual code (medium tier acceptable, narrow scope).\n")
	b.WriteString("- Flag any partition whose agent failed or was cancelled as UNREVIEWED — never imply full coverage when an agent died.\n\n")

	b.WriteString("## Output Format (preserved — the TUI parses this)\n\n")
	b.WriteString("After reconcile, emit findings in this exact format so the TUI can render them:\n\n")
	b.WriteString("### Summary\n")
	b.WriteString("[Your summary here]\n\n")
	b.WriteString("### Findings\n\n")
	b.WriteString("For each finding, use this format:\n")
	b.WriteString("```\n")
	b.WriteString("SEVERITY: [error|warning|info|suggestion]\n")
	b.WriteString("FILE: [file path]\n")
	b.WriteString("LINE: [line number or 0]\n")
	b.WriteString("MESSAGE: [description]\n")
	b.WriteString("SUGGESTION: [suggested fix, if applicable]\n")
	b.WriteString("```\n\n")
	b.WriteString("Focus the final reconciled report on:\n")
	b.WriteString("- Behavioral bugs and logic errors\n")
	b.WriteString("- Security vulnerabilities\n")
	b.WriteString("- Performance issues\n")
	b.WriteString("- Code style and best practices\n")
	b.WriteString("- Missing error handling\n")
	b.WriteString("- Potential edge cases\n\n")
	b.WriteString("Return only the final reconciled report as your output.\n")
	return b.String()
}

// ─── Convenience entrypoints ──────────────────────────────────────────────

// Standup assembles the full /standup prompt (context + instructions).
func Standup(workDir string, lspMgr *lsp.Manager) (string, error) {
	ctx, err := StandupContext(workDir, lspMgr)
	if err != nil {
		return "", err
	}
	return BuildStandupPrompt(ctx), nil
}

// Changes assembles the full /changes prompt (context + instructions).
func Changes(workDir string, lspMgr *lsp.Manager) (string, error) {
	ctx, err := ChangesContext(workDir, lspMgr)
	if err != nil {
		return "", err
	}
	return BuildChangesPrompt(ctx), nil
}

// Review assembles the full /review prompt for the given arguments.
func Review(workDir string, args []string) (string, error) {
	target, arg, description := DetectReviewTarget(args)
	ctx, err := ReviewContext(target, arg, workDir)
	if err != nil {
		return "", err
	}
	return BuildReviewPrompt(target, ctx, description), nil
}
