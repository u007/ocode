package tui

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// reviewTarget represents what is being reviewed.
type reviewTarget int

const (
	reviewTargetWorkingDir reviewTarget = iota // git diff HEAD (uncommitted changes)
	reviewTargetFile                           // specific file(s)
	reviewTargetCommit                         // specific commit
	reviewTargetBranch                         // branch comparison
	reviewTargetPR                             // GitHub PR
)

// reviewSeverity indicates the severity of a review finding.
type reviewSeverity int

const (
	reviewSeverityInfo reviewSeverity = iota
	reviewSeverityWarning
	reviewSeverityError
	reviewSeveritySuggestion
)

// reviewFinding represents a single finding in the review.
type reviewFinding struct {
	Severity   reviewSeverity
	File       string // file path
	Line       int    // line number (0 if not applicable)
	Message    string // description of the finding
	Suggestion string // suggested fix (optional)
	Patch      string // unified diff patch for the fix (optional)
	Accepted   bool   // whether the user accepted this fix
}

// reviewResult holds the complete review output.
type reviewResult struct {
	Target    reviewTarget
	Context   string // description of what was reviewed
	Findings  []reviewFinding
	Summary   string
	RawOutput string // raw LLM output
	Timestamp time.Time
}

// reviewState tracks the state of the review overlay.
type reviewState struct {
	active   bool
	result   reviewResult
	scrollY  int
	selected int // index of selected finding (-1 for none)
}

// detectReviewTarget analyzes the arguments to determine what to review.
func detectReviewTarget(args []string) (reviewTarget, string, string) {
	if len(args) == 0 {
		return reviewTargetWorkingDir, "", "uncommitted changes"
	}

	arg := args[0]

	// Check if it's a file path
	if strings.HasSuffix(arg, ".go") || strings.HasSuffix(arg, ".js") || strings.HasSuffix(arg, ".ts") ||
		strings.HasSuffix(arg, ".py") || strings.HasSuffix(arg, ".rs") || strings.HasSuffix(arg, ".java") ||
		strings.HasSuffix(arg, ".c") || strings.HasSuffix(arg, ".cpp") || strings.HasSuffix(arg, ".h") ||
		strings.Contains(arg, "/") || strings.Contains(arg, ".") {
		return reviewTargetFile, arg, fmt.Sprintf("file %s", arg)
	}

	// Check if it looks like a commit hash (7-40 hex chars)
	if isCommitHash(arg) {
		return reviewTargetCommit, arg, fmt.Sprintf("commit %s", arg)
	}

	// Check if it's a PR number
	if len(args) > 0 {
		if prNum := parsePRNumber(arg); prNum > 0 {
			return reviewTargetPR, arg, fmt.Sprintf("PR #%s", arg)
		}
	}

	// Default: treat as branch name
	return reviewTargetBranch, arg, fmt.Sprintf("branch %s vs current", arg)
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

// getReviewContext gathers the context for the review based on the target.
func getReviewContext(target reviewTarget, arg string, workDir string) (string, error) {
	switch target {
	case reviewTargetWorkingDir:
		return getWorkingDirContext(workDir)
	case reviewTargetFile:
		return getFileContext(arg, workDir)
	case reviewTargetCommit:
		return getCommitContext(arg, workDir)
	case reviewTargetBranch:
		return getBranchContext(arg, workDir)
	case reviewTargetPR:
		return getPRContext(arg, workDir)
	default:
		return "", fmt.Errorf("unknown review target")
	}
}

// getWorkingDirContext gets the diff for uncommitted changes.
func getWorkingDirContext(workDir string) (string, error) {
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

// getFileContext gets the content and diff for a specific file.
func getFileContext(filePath string, workDir string) (string, error) {
	var b strings.Builder

	// Check if file exists and get its content
	b.WriteString(fmt.Sprintf("## File: %s\n\n", filePath))

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

// getCommitContext gets the details and diff for a specific commit.
func getCommitContext(commitHash string, workDir string) (string, error) {
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

// getBranchContext gets the diff between current branch and another branch.
func getBranchContext(branchName string, workDir string) (string, error) {
	var b strings.Builder

	// Get current branch
	currentBranch, err := gitRun(workDir, "branch", "--show-current")
	if err == nil {
		b.WriteString(fmt.Sprintf("## Comparing: %s → %s\n\n", branchName, currentBranch))
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

// getPRContext gets the details for a GitHub PR.
func getPRContext(prArg string, workDir string) (string, error) {
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

// gitRun executes a git command and returns the output.
func gitRun(workDir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = workDir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// buildReviewPrompt creates the prompt for the orchestrator
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
func buildReviewPrompt(target reviewTarget, context string, description string) string {
	var b strings.Builder

	b.WriteString("Run a code review of the following changes using a grouped, notes-enabled fan-out.\n\n")
	b.WriteString(fmt.Sprintf("## Review Target: %s\n\n", description))
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
	b.WriteString("Append a final 'Unreviewed partitions' section listing any agent that failed or was cancelled, with the dimension or file it was assigned.\n")

	return b.String()
}

// parseReviewOutput parses the LLM output into a structured reviewResult.
func parseReviewOutput(raw string) reviewResult {
	result := reviewResult{
		Timestamp: time.Now(),
		RawOutput: raw,
	}

	lines := strings.Split(raw, "\n")
	var currentFinding *reviewFinding
	inFindings := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Parse summary section
		if strings.HasPrefix(trimmed, "### Summary") || strings.HasPrefix(trimmed, "## Summary") {
			continue
		}

		// Parse findings section
		if strings.HasPrefix(trimmed, "### Findings") || strings.HasPrefix(trimmed, "## Findings") {
			inFindings = true
			continue
		}

		// Parse individual findings
		if inFindings {
			if strings.HasPrefix(trimmed, "SEVERITY:") {
				if currentFinding != nil {
					result.Findings = append(result.Findings, *currentFinding)
				}
				currentFinding = &reviewFinding{}
				severity := strings.TrimSpace(strings.TrimPrefix(trimmed, "SEVERITY:"))
				switch strings.ToLower(severity) {
				case "error":
					currentFinding.Severity = reviewSeverityError
				case "warning":
					currentFinding.Severity = reviewSeverityWarning
				case "suggestion":
					currentFinding.Severity = reviewSeveritySuggestion
				default:
					currentFinding.Severity = reviewSeverityInfo
				}
			} else if strings.HasPrefix(trimmed, "FILE:") && currentFinding != nil {
				currentFinding.File = strings.TrimSpace(strings.TrimPrefix(trimmed, "FILE:"))
			} else if strings.HasPrefix(trimmed, "LINE:") && currentFinding != nil {
				fmt.Sscanf(strings.TrimSpace(strings.TrimPrefix(trimmed, "LINE:")), "%d", &currentFinding.Line)
			} else if strings.HasPrefix(trimmed, "MESSAGE:") && currentFinding != nil {
				currentFinding.Message = strings.TrimSpace(strings.TrimPrefix(trimmed, "MESSAGE:"))
			} else if strings.HasPrefix(trimmed, "SUGGESTION:") && currentFinding != nil {
				currentFinding.Suggestion = strings.TrimSpace(strings.TrimPrefix(trimmed, "SUGGESTION:"))
			}
		}
	}

	// Add the last finding
	if currentFinding != nil {
		result.Findings = append(result.Findings, *currentFinding)
	}

	// Extract summary from raw output
	if idx := strings.Index(raw, "### Summary"); idx != -1 {
		summaryStart := idx + len("### Summary")
		if idx2 := strings.Index(raw[summaryStart:], "###"); idx2 != -1 {
			result.Summary = strings.TrimSpace(raw[summaryStart : summaryStart+idx2])
		} else {
			result.Summary = strings.TrimSpace(raw[summaryStart:])
		}
	}

	return result
}

// severityIcon returns an icon for the severity level.
func severityIcon(s reviewSeverity) string {
	switch s {
	case reviewSeverityError:
		return "❌"
	case reviewSeverityWarning:
		return "⚠️"
	case reviewSeveritySuggestion:
		return "💡"
	case reviewSeverityInfo:
		return "ℹ️"
	default:
		return "•"
	}
}

// severityLabel returns a label for the severity level.
func severityLabel(s reviewSeverity) string {
	switch s {
	case reviewSeverityError:
		return "ERROR"
	case reviewSeverityWarning:
		return "WARNING"
	case reviewSeveritySuggestion:
		return "SUGGESTION"
	case reviewSeverityInfo:
		return "INFO"
	default:
		return "UNKNOWN"
	}
}
