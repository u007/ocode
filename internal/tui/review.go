package tui

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/u007/ocode/internal/commandctx"
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

// reviewTargetToCtx maps the TUI's reviewTarget to the shared commandctx
// ReviewTarget so /review logic stays in one place (internal/commandctx).
func reviewTargetToCtx(t reviewTarget) commandctx.ReviewTarget {
	switch t {
	case reviewTargetWorkingDir:
		return commandctx.ReviewWorkingDir
	case reviewTargetFile:
		return commandctx.ReviewFile
	case reviewTargetCommit:
		return commandctx.ReviewCommit
	case reviewTargetBranch:
		return commandctx.ReviewBranch
	case reviewTargetPR:
		return commandctx.ReviewPR
	default:
		return commandctx.ReviewWorkingDir
	}
}

// detectReviewTarget analyzes the arguments to determine what to review.
// Thin wrapper over commandctx.DetectReviewTarget.
func detectReviewTarget(args []string) (reviewTarget, string, string) {
	ct, arg, desc := commandctx.DetectReviewTarget(args)
	var t reviewTarget
	switch ct {
	case commandctx.ReviewWorkingDir:
		t = reviewTargetWorkingDir
	case commandctx.ReviewFile:
		t = reviewTargetFile
	case commandctx.ReviewCommit:
		t = reviewTargetCommit
	case commandctx.ReviewBranch:
		t = reviewTargetBranch
	case commandctx.ReviewPR:
		t = reviewTargetPR
	}
	return t, arg, desc
}

// getReviewContext gathers the context for the review based on the target.
// Thin wrapper over commandctx.ReviewContext.
func getReviewContext(target reviewTarget, arg string, workDir string) (string, error) {
	return commandctx.ReviewContext(reviewTargetToCtx(target), arg, workDir)
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
	return commandctx.BuildReviewPrompt(reviewTargetToCtx(target), context, description)
}

// handleReviewCmd implements /review: detect target, gather context, build
// prompt, and stream it through the agent. Logic lives in commandctx.
func (m *model) handleReviewCmd(args []string) tea.Cmd {
	// Detect what to review
	target, arg, description := detectReviewTarget(args)

	// Get the review context (git diff, file content, etc.)
	context, err := getReviewContext(target, arg, m.workDir)
	if err != nil {
		m.messages = append(m.messages, message{role: roleAssistant, text: fmt.Sprintf("Review error: %v", err)})
		return nil
	}

	// Build the review prompt
	prompt := buildReviewPrompt(target, context, description)

	// Send to agent for review
	return m.sendCustomCommandPrompt(prompt)
}

// severityIcon returns an icon for the severity level.
func severityIcon(s reviewSeverity) string {
	switch s {
	case reviewSeverityError:
		return "✗"
	case reviewSeverityWarning:
		return "⚠"
	case reviewSeveritySuggestion:
		return "◆"
	case reviewSeverityInfo:
		return "ℹ"
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
